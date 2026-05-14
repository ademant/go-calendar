package main

import (
	"database/sql"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB
var rateLimiter *RateLimiter
var connLimiter *ConnLimiter

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

func init() {
	var err error
	berlinLoc, err = time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatal(err)
	}

	config, err = loadConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	db, err = sql.Open("sqlite3", "./calendar.db")
	if err != nil {
		log.Fatal(err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}

	if err = createTables(); err != nil {
		log.Fatal(err)
	}

	ensureAdminUser(db)

	rateLimiter = NewRateLimiter(config.Server.RateLimit, time.Minute)
	connLimiter = NewConnLimiter(config.Server.MaxConnsPerIP)

	log.Println("Database initialized successfully")
}

func createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT DEFAULT 'user' CHECK(role IN ('admin', 'user', 'viewer')),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
	CREATE INDEX IF NOT EXISTS idx_events_published_start ON events(is_published, start_time);
	CREATE INDEX IF NOT EXISTS idx_events_title_location  ON events(title, location_id);
	CREATE INDEX IF NOT EXISTS idx_events_location_id     ON events(location_id);
	CREATE INDEX IF NOT EXISTS idx_locations_location     ON locations(location);
	`
	_, err := db.Exec(schema)
	return err
}

func main() {
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

	// Tags endpoint (protected)
	tagsRoutes := router.PathPrefix("/api/v1/tags").Subrouter()
	tagsRoutes.Use(TokenMiddleware)
	tagsRoutes.HandleFunc("", getTags).Methods("GET")
	tagsRoutes.HandleFunc("", handleOptions).Methods("OPTIONS")

	port := getPort()
	log.Printf("Server starting on %s\n", port)
	srv := &http.Server{
		Addr:         port,
		Handler:      router,
		ReadTimeout:  time.Duration(config.Server.ReadTimeoutSecs) * time.Second,
		WriteTimeout: time.Duration(config.Server.WriteTimeoutSecs) * time.Second,
		IdleTimeout:  time.Duration(config.Server.IdleTimeoutSecs) * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
