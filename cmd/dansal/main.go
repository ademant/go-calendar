package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB
var rateLimiter *RateLimiter
var loginRateLimiter *RateLimiter
var connLimiter *ConnLimiter
var configFilePath string

type ConnLimiter struct {
	mu     sync.Mutex
	active map[string]int
	limit  int
}

func NewConnLimiter(limit int) *ConnLimiter {
	return &ConnLimiter{active: make(map[string]int), limit: limit}
}

func (cl *ConnLimiter) acquire(ip string) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.active[ip] >= cl.limit {
		return false
	}
	cl.active[ip]++
	return true
}

func (cl *ConnLimiter) release(ip string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.active[ip]--
	if cl.active[ip] <= 0 {
		delete(cl.active, ip)
	}
}

func ConnLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getIP(r)
		if !connLimiter.acquire(ip) {
			http.Error(w, "Too many concurrent connections", http.StatusTooManyRequests)
			return
		}
		defer connLimiter.release(ip)
		next.ServeHTTP(w, r)
	})
}

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	go rl.sweepLoop()
	return rl
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	valid := rl.prune(rl.requests[ip], now)
	if len(valid) >= rl.limit {
		return false
	}
	rl.requests[ip] = append(valid, now)
	return true
}

// prune removes timestamps outside the window and deletes the map key when empty.
func (rl *RateLimiter) prune(times []time.Time, now time.Time) []time.Time {
	var valid []time.Time
	for _, t := range times {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}
	return valid
}

// sweepLoop periodically removes stale IP entries from the map.
func (rl *RateLimiter) sweepLoop() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, times := range rl.requests {
			if valid := rl.prune(times, now); len(valid) == 0 {
				delete(rl.requests, ip)
			} else {
				rl.requests[ip] = valid
			}
		}
		rl.mu.Unlock()
	}
}

func MaxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, config.Server.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getIP(r)
		if !rateLimiter.Allow(ip) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// OPTIONS handler for CORS preflight requests
func handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-Role, X-User-ID")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusOK)
}


func migrateDB() {
	// Errors are silently ignored (column already exists).
	db.Exec("ALTER TABLE events ADD COLUMN organization_id INTEGER")
	db.Exec("ALTER TABLE locations ADD COLUMN organization_id INTEGER")
	db.Exec("ALTER TABLE events ADD COLUMN uid TEXT")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_events_uid ON events(uid) WHERE uid IS NOT NULL")
}

func createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT DEFAULT 'user' CHECK(role IN ('admin', 'user', 'publisher', 'viewer')),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		uid TEXT UNIQUE,
		title TEXT NOT NULL,
		description TEXT,
		start_time INTEGER NOT NULL,
		end_time INTEGER NOT NULL,
		location_id INTEGER,
		has_ball INTEGER DEFAULT 0,
		has_workshop INTEGER DEFAULT 0,
		tags TEXT,
		is_published INTEGER DEFAULT 0,
		short_code TEXT UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (location_id) REFERENCES locations(id)
	);
	CREATE TABLE IF NOT EXISTS tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		token TEXT UNIQUE NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS locations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		location TEXT NOT NULL,
		address TEXT,
		zipcode TEXT,
		town TEXT,
		latitude TEXT,
		longitude TEXT,
		internetsite TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS musicians (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bandname TEXT NOT NULL,
		internetsite TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS fetch_sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT UNIQUE NOT NULL,
		type TEXT NOT NULL DEFAULT 'ical',
		tags TEXT,
		last_fetched_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		api_key TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS organizations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		description TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS organization_members (
		organization_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (organization_id, user_id),
		FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_events_published_start ON events(is_published, start_time);
	CREATE INDEX IF NOT EXISTS idx_events_title_location  ON events(title, location_id);
	CREATE INDEX IF NOT EXISTS idx_events_location_id     ON events(location_id);
	CREATE INDEX IF NOT EXISTS idx_locations_location     ON locations(location);
	`
	_, err := db.Exec(schema)
	return err
}

func reloadConfig(path string) {
	newCfg, err := loadConfig(path)
	if err != nil {
		log.Printf("Config reload failed: %v", err)
		return
	}
	applyDefaults(newCfg)

	if newCfg.Server.Port != config.Server.Port ||
		newCfg.Server.DBPath != config.Server.DBPath ||
		newCfg.Server.AdminSocket != config.Server.AdminSocket {
		log.Printf("Warning: port, db_path and admin_socket changes require a restart to take effect")
	}

	config = newCfg
	rateLimiter = NewRateLimiter(config.Server.RateLimit, time.Minute)
	loginRateLimiter = NewRateLimiter(config.Server.LoginRateLimit, time.Minute)
	connLimiter = NewConnLimiter(config.Server.MaxConnsPerIP)
	log.Printf("Config reloaded from %s", path)
}

func main() {
	configPath := flag.String("config", "/etc/dansal/config.yaml", "path to config.yaml")
	flag.Parse()

	var err error

	berlinLoc, err = time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatal(err)
	}

	configFilePath = *configPath
	config, err = loadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: could not load %s, using defaults: %v", *configPath, err)
		config = &Config{}
	}
	applyDefaults(config)

	db, err = sql.Open("sqlite3", config.Server.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}
	if err = createTables(); err != nil {
		log.Fatal(err)
	}
	migrateDB()
	log.Println("Database initialized successfully")

	rateLimiter = NewRateLimiter(config.Server.RateLimit, time.Minute)
	loginRateLimiter = NewRateLimiter(config.Server.LoginRateLimit, time.Minute)
	connLimiter = NewConnLimiter(config.Server.MaxConnsPerIP)

	router := mux.NewRouter()
	router.Use(MaxBodyMiddleware)
	router.Use(ConnLimitMiddleware)
	router.Use(RateLimitMiddleware)

	// Public endpoints (no authentication required)
	router.HandleFunc("/events", publicGetEvents).Methods("GET")
	router.HandleFunc("/events", handleOptions).Methods("OPTIONS")
	router.HandleFunc("/events.ics", publicGetEventsICS).Methods("GET")
	router.HandleFunc("/events/tag/{tag}.ics", publicGetEventsByTagICS).Methods("GET")
	router.HandleFunc("/events/town/{town}.ics", publicGetEventsByTownICS).Methods("GET")

	// Info endpoint (public)
	router.HandleFunc("/api/v1/info", getInfo).Methods("GET")
	router.HandleFunc("/api/v1/info", handleOptions).Methods("OPTIONS")

	// Authentication endpoint (no token required)
	router.HandleFunc("/api/v1/login", login).Methods("GET", "POST")
	router.HandleFunc("/api/v1/login", handleOptions).Methods("OPTIONS")

	// User endpoints (protected)
	userRoutes := router.PathPrefix("/api/v1/users").Subrouter()
	userRoutes.Use(TokenMiddleware)
	userRoutes.HandleFunc("", getUsers).Methods("GET")
	userRoutes.HandleFunc("", createUser).Methods("POST")
	userRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	userRoutes.HandleFunc("/{id}", getUser).Methods("GET")
	userRoutes.HandleFunc("/{id}", updateUser).Methods("PUT")
	userRoutes.HandleFunc("/{id}", deleteUser).Methods("DELETE")
	userRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")

	// Location endpoints (protected)
	locationRoutes := router.PathPrefix("/api/v1/locations").Subrouter()
	locationRoutes.Use(TokenMiddleware)
	locationRoutes.HandleFunc("", getLocations).Methods("GET")
	locationRoutes.HandleFunc("", createLocation).Methods("POST")
	locationRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	locationRoutes.HandleFunc("/{id}", getLocation).Methods("GET")
	locationRoutes.HandleFunc("/{id}", updateLocation).Methods("PUT")
	locationRoutes.HandleFunc("/{id}", deleteLocation).Methods("DELETE")
	locationRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")

	// Musician endpoints (protected)
	musicianRoutes := router.PathPrefix("/api/v1/musicians").Subrouter()
	musicianRoutes.Use(TokenMiddleware)
	musicianRoutes.HandleFunc("", getMusicians).Methods("GET")
	musicianRoutes.HandleFunc("", createMusician).Methods("POST")
	musicianRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	musicianRoutes.HandleFunc("/{id}", getMusician).Methods("GET")
	musicianRoutes.HandleFunc("/{id}", updateMusician).Methods("PUT")
	musicianRoutes.HandleFunc("/{id}", deleteMusician).Methods("DELETE")
	musicianRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")

	// Event endpoints (protected)
	eventRoutes := router.PathPrefix("/api/v1/events").Subrouter()
	eventRoutes.Use(TokenMiddleware)
	eventRoutes.HandleFunc("", getEvents).Methods("GET")
	eventRoutes.HandleFunc("", createEvent).Methods("POST")
	eventRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	eventRoutes.HandleFunc("/{id}", getEvent).Methods("GET")
	eventRoutes.HandleFunc("/{id}", deleteEvent).Methods("DELETE")
	eventRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")
	eventRoutes.HandleFunc("/{id}/publish", publishEvent).Methods("POST")
	eventRoutes.HandleFunc("/{id}/publish", handleOptions).Methods("OPTIONS")

	// Image upload (protected)
	imageRoutes := router.PathPrefix("/api/v1/images").Subrouter()
	imageRoutes.Use(TokenMiddleware)
	imageRoutes.HandleFunc("/{event_id}", getEventImage).Methods("GET")
	imageRoutes.HandleFunc("/{event_id}", uploadEventImage).Methods("POST")
	imageRoutes.HandleFunc("/{event_id}", deleteEventImage).Methods("DELETE")
	imageRoutes.HandleFunc("/{event_id}", handleOptions).Methods("OPTIONS")

	// Fetch URL endpoints (protected)
	fetchRoutes := router.PathPrefix("/api/v1/fetchurl").Subrouter()
	fetchRoutes.Use(TokenMiddleware)
	fetchRoutes.HandleFunc("", getFetchSources).Methods("GET")
	fetchRoutes.HandleFunc("", fetchURL).Methods("POST")
	fetchRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	fetchRoutes.HandleFunc("/fetch-all", fetchAllURLs).Methods("POST")
	fetchRoutes.HandleFunc("/fetch-all", handleOptions).Methods("OPTIONS")
	fetchRoutes.HandleFunc("/{id}", getFetchSource).Methods("GET")
	fetchRoutes.HandleFunc("/{id}/fetch", fetchURLByID).Methods("POST")
	fetchRoutes.HandleFunc("/{id}/fetch", handleOptions).Methods("OPTIONS")
	fetchRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")

	// Tags endpoint (protected)
	tagsRoutes := router.PathPrefix("/api/v1/tags").Subrouter()
	tagsRoutes.Use(TokenMiddleware)
	tagsRoutes.HandleFunc("", getTags).Methods("GET")
	tagsRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")

	// Organization endpoints (protected)
	orgRoutes := router.PathPrefix("/api/v1/organizations").Subrouter()
	orgRoutes.Use(TokenMiddleware)
	orgRoutes.HandleFunc("", getOrganizations).Methods("GET")
	orgRoutes.HandleFunc("", createOrganization).Methods("POST")
	orgRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	orgRoutes.HandleFunc("/{id}", getOrganization).Methods("GET")
	orgRoutes.HandleFunc("/{id}", updateOrganization).Methods("PUT")
	orgRoutes.HandleFunc("/{id}", deleteOrganization).Methods("DELETE")
	orgRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")
	orgRoutes.HandleFunc("/{id}/members", getOrganizationMembers).Methods("GET")
	orgRoutes.HandleFunc("/{id}/members", addOrganizationMember).Methods("POST")
	orgRoutes.HandleFunc("/{id}/members", handleOptions).Methods("OPTIONS")
	orgRoutes.HandleFunc("/{id}/members/{user_id}", removeOrganizationMember).Methods("DELETE")
	orgRoutes.HandleFunc("/{id}/members/{user_id}", handleOptions).Methods("OPTIONS")

	// API key endpoints (protected)
	apiKeyRoutes := router.PathPrefix("/api/v1/apikeys").Subrouter()
	apiKeyRoutes.Use(TokenMiddleware)
	apiKeyRoutes.HandleFunc("", listAPIKeys).Methods("GET")
	apiKeyRoutes.HandleFunc("", createAPIKey).Methods("POST")
	apiKeyRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")
	apiKeyRoutes.HandleFunc("/{id}", deleteAPIKey).Methods("DELETE")
	apiKeyRoutes.HandleFunc("/{id}", handleOptions).Methods("OPTIONS")

	adminLn := startAdminSocket(config.Server.AdminSocket)

	port := getPort()
	log.Printf("Server starting on %s\n", port)
	srv := &http.Server{
		Addr:         port,
		Handler:      router,
		ReadTimeout:  time.Duration(config.Server.ReadTimeoutSecs) * time.Second,
		WriteTimeout: time.Duration(config.Server.WriteTimeoutSecs) * time.Second,
		IdleTimeout:  time.Duration(config.Server.IdleTimeoutSecs) * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	for sig := range sigs {
		if sig == syscall.SIGHUP {
			reloadConfig(*configPath)
			continue
		}
		break
	}
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if adminLn != nil {
		adminLn.Close()
	}
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
