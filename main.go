package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func init() {
	var err error
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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
	`
	_, err := db.Exec(schema)
	return err
}

type Event struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	CreatedAt   string `json:"created_at"`
}

// GET /api/v1/events
func getEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT id, title, description, start_time, end_time, created_at FROM events")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &event.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		events = append(events, event)
	}

	json.NewEncoder(w).Encode(events)
}

// POST /api/v1/events
func createEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		"INSERT INTO events (title, description, start_time, end_time) VALUES (?, ?, ?, ?)",
		event.Title, event.Description, event.StartTime, event.EndTime,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	event.ID = int(id)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// GET /api/v1/events/{id}
func getEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	var event Event
	err := db.QueryRow(
		"SELECT id, title, description, start_time, end_time, created_at FROM events WHERE id = ?",
		id,
	).Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &event.CreatedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(event)
}

// DELETE /api/v1/events/{id}
func deleteEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	result, err := db.Exec("DELETE FROM events WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func main() {
	router := mux.NewRouter()

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
