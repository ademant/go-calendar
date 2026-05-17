package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// ThreadType values for threads.type.
const (
	ThreadTypeDiscussion  = "discussion"
	ThreadTypeRidesharing = "ridesharing"
)

type Thread struct {
	ID                int    `json:"id"`
	EventID           int    `json:"event_id"`
	Type              string `json:"type"`
	Title             string `json:"title"`
	AuthorID          *int   `json:"author_id,omitempty"`
	AuthorName        string `json:"author_name,omitempty"`
	IsClosed          bool   `json:"is_closed"`
	PostCount         int    `json:"post_count"`
	// Ridesharing-specific fields (null for discussion threads).
	DepartureLocation string `json:"departure_location,omitempty"`
	DepartureTime     string `json:"departure_time,omitempty"`
	AvailableSeats    *int   `json:"available_seats,omitempty"`
	CreatedAt         string `json:"created_at"`
}

type Post struct {
	ID         int    `json:"id"`
	ThreadID   int    `json:"thread_id"`
	AuthorID   *int   `json:"author_id,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
}

type ThreadCreateRequest struct {
	Type              string `json:"type"`
	Title             string `json:"title"`
	// Ridesharing-specific fields.
	DepartureLocation string `json:"departure_location,omitempty"`
	DepartureTime     string `json:"departure_time,omitempty"`
	AvailableSeats    *int   `json:"available_seats,omitempty"`
}

type ThreadPatchRequest struct {
	IsClosed          *bool  `json:"is_closed"`
	DepartureLocation string `json:"departure_location,omitempty"`
	DepartureTime     string `json:"departure_time,omitempty"`
	AvailableSeats    *int   `json:"available_seats,omitempty"`
}

type PostCreateRequest struct {
	Body string `json:"body"`
}

// ── helpers ────────────────────────────────────────────────────────────────

// threadAuthCheck verifies the event exists and the caller may manage its threads.
func threadAuthCheck(w http.ResponseWriter, userRole string, callerID, eventID int) bool {
	var orgID sql.NullInt64
	err := db.QueryRow("SELECT organization_id FROM events WHERE id = ?", eventID).Scan(&orgID)
	if err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if userRole != RoleAdmin && (!orgID.Valid || !isOrgMember(callerID, int(orgID.Int64))) {
		http.Error(w, "Forbidden: not authorized to manage threads for this event", http.StatusForbidden)
		return false
	}
	return true
}

// ── HTTP handlers ──────────────────────────────────────────────────────────

// GET /api/v1/events/{id}/threads
func getThreads(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = mux.Vars(r)["id"]
	_ = r.URL.Query().Get("type") // optional filter: "discussion" or "ridesharing"
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// POST /api/v1/events/{id}/threads
func createThread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userRole := r.Header.Get("X-User-Role")
	if !validateRole(userRole) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	_ = callerID
	_ = eventID

	var req ThreadCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// GET /api/v1/events/{id}/threads/{thread_id}
func getThread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = mux.Vars(r)["id"]
	_ = mux.Vars(r)["thread_id"]
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// PATCH /api/v1/events/{id}/threads/{thread_id}
func patchThread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userRole := r.Header.Get("X-User-Role")
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	if !threadAuthCheck(w, userRole, callerID, eventID) {
		return
	}

	var req ThreadPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// DELETE /api/v1/events/{id}/threads/{thread_id}
func deleteThread(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	if !threadAuthCheck(w, userRole, callerID, eventID) {
		return
	}
	_ = mux.Vars(r)["thread_id"]
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// POST /api/v1/events/{id}/threads/{thread_id}/posts
func createPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userRole := r.Header.Get("X-User-Role")
	if !validateRole(userRole) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	_ = callerID
	_ = mux.Vars(r)["id"]
	_ = mux.Vars(r)["thread_id"]

	var req PostCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// DELETE /api/v1/events/{id}/threads/{thread_id}/posts/{post_id}
func deletePost(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	if !threadAuthCheck(w, userRole, callerID, eventID) {
		return
	}
	_ = mux.Vars(r)["thread_id"]
	_ = mux.Vars(r)["post_id"]
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}
