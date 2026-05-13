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

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if times, ok := rl.requests[ip]; ok {
		var valid []time.Time
		for _, t := range times {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}
		rl.requests[ip] = valid
		if len(valid) >= rl.limit {
			return false
		}
	}
	rl.requests[ip] = append(rl.requests[ip], now)
	return true
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

func init() {
	var err error
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
		start_time DATETIME NOT NULL,
		end_time DATETIME NOT NULL,
		location_id INTEGER,
		has_ball INTEGER DEFAULT 0,
		has_workshop INTEGER DEFAULT 0,
		tags TEXT,
		is_published INTEGER DEFAULT 0,
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
	`
	_, err := db.Exec(schema)
	return err
}

func main() {
	router := mux.NewRouter()
	router.Use(RateLimitMiddleware)

	// Authentication endpoint (no token required)
	router.HandleFunc("/api/v1/login", login).Methods("GET", "POST")

	// User endpoints (protected)
	userRoutes := router.PathPrefix("/api/v1/users").Subrouter()
	userRoutes.Use(TokenMiddleware)
	userRoutes.HandleFunc("", getUsers).Methods("GET")
	userRoutes.HandleFunc("", createUser).Methods("POST")
	userRoutes.HandleFunc("/{id}", getUser).Methods("GET")
	userRoutes.HandleFunc("/{id}", updateUser).Methods("PUT")
	userRoutes.HandleFunc("/{id}", deleteUser).Methods("DELETE")

	// Location endpoints (protected)
	locationRoutes := router.PathPrefix("/api/v1/locations").Subrouter()
	locationRoutes.Use(TokenMiddleware)
	locationRoutes.HandleFunc("", getLocations).Methods("GET")
	locationRoutes.HandleFunc("", createLocation).Methods("POST")
	locationRoutes.HandleFunc("/{id}", getLocation).Methods("GET")
	locationRoutes.HandleFunc("/{id}", updateLocation).Methods("PUT")
	locationRoutes.HandleFunc("/{id}", deleteLocation).Methods("DELETE")

	// Musician endpoints (protected)
	musicianRoutes := router.PathPrefix("/api/v1/musicians").Subrouter()
	musicianRoutes.Use(TokenMiddleware)
	musicianRoutes.HandleFunc("", getMusicians).Methods("GET")
	musicianRoutes.HandleFunc("", createMusician).Methods("POST")
	musicianRoutes.HandleFunc("/{id}", getMusician).Methods("GET")
	musicianRoutes.HandleFunc("/{id}", updateMusician).Methods("PUT")
	musicianRoutes.HandleFunc("/{id}", deleteMusician).Methods("DELETE")

	// Event endpoints (protected)
	eventRoutes := router.PathPrefix("/api/v1/events").Subrouter()
	eventRoutes.Use(TokenMiddleware)
	eventRoutes.HandleFunc("", getEvents).Methods("GET")
	eventRoutes.HandleFunc("", createEvent).Methods("POST")
	eventRoutes.HandleFunc("/{id}", getEvent).Methods("GET")
	eventRoutes.HandleFunc("/{id}", deleteEvent).Methods("DELETE")

	port := getPort()
	log.Printf("Server starting on %s\n", port)
	if err := http.ListenAndServe(port, router); err != nil {
		log.Fatal(err)
	}
}
