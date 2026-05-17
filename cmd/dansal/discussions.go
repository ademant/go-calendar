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
	DepartureLocation string `json:"departure_location,omitempty"`
	DepartureTime     string `json:"departure_time,omitempty"`
	AvailableSeats    *int   `json:"available_seats,omitempty"`
	CreatedAt         string `json:"created_at"`
	Posts             []Post `json:"posts,omitempty"`
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

// threadSelect is the base SELECT for threads with post_count and author_name.
// Must be followed by a WHERE clause, GROUP BY t.id, and ORDER BY.
const threadSelect = `
	SELECT t.id, t.event_id, t.type, t.title, t.author_id, COALESCE(u.username,''),
	       t.is_closed, COUNT(p.id), COALESCE(t.departure_location,''), COALESCE(t.departure_time,''),
	       t.available_seats, t.created_at
	FROM threads t
	LEFT JOIN users u ON t.author_id = u.id
	LEFT JOIN posts p ON p.thread_id = t.id`

func scanThread(row scanner) (Thread, error) {
	var t Thread
	var authorID sql.NullInt64
	var seats sql.NullInt64
	var isClosed int
	err := row.Scan(
		&t.ID, &t.EventID, &t.Type, &t.Title,
		&authorID, &t.AuthorName, &isClosed, &t.PostCount,
		&t.DepartureLocation, &t.DepartureTime, &seats, &t.CreatedAt,
	)
	if err != nil {
		return Thread{}, err
	}
	t.IsClosed = isClosed == 1
	if authorID.Valid {
		v := int(authorID.Int64)
		t.AuthorID = &v
	}
	if seats.Valid {
		v := int(seats.Int64)
		t.AvailableSeats = &v
	}
	return t, nil
}

func fetchThreadPosts(threadID int) ([]Post, error) {
	rows, err := db.Query(`
		SELECT p.id, p.thread_id, p.author_id, COALESCE(u.username,''), p.body, p.created_at
		FROM posts p LEFT JOIN users u ON p.author_id = u.id
		WHERE p.thread_id = ? ORDER BY p.created_at, p.id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var post Post
		var authorID sql.NullInt64
		if err := rows.Scan(&post.ID, &post.ThreadID, &authorID, &post.AuthorName,
			&post.Body, &post.CreatedAt); err != nil {
			return nil, err
		}
		if authorID.Valid {
			v := int(authorID.Int64)
			post.AuthorID = &v
		}
		posts = append(posts, post)
	}
	return posts, nil
}

// ── HTTP handlers ──────────────────────────────────────────────────────────

// GET /api/v1/events/{id}/threads
func getThreads(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}

	var exists int
	if err := db.QueryRow("SELECT 1 FROM events WHERE id = ?", eventID).Scan(&exists); err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query := threadSelect + " WHERE t.event_id = ?"
	args := []interface{}{eventID}
	if typeFilter := r.URL.Query().Get("type"); typeFilter != "" {
		query += " AND t.type = ?"
		args = append(args, typeFilter)
	}
	query += " GROUP BY t.id ORDER BY t.created_at, t.id"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	threads := []Thread{}
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		threads = append(threads, t)
	}
	json.NewEncoder(w).Encode(threads)
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

	var exists int
	if err := db.QueryRow("SELECT 1 FROM events WHERE id = ?", eventID).Scan(&exists); err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var req ThreadCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = ThreadTypeDiscussion
	}
	if req.Type != ThreadTypeDiscussion && req.Type != ThreadTypeRidesharing {
		http.Error(w, "type must be 'discussion' or 'ridesharing'", http.StatusBadRequest)
		return
	}
	if req.Type == ThreadTypeRidesharing && req.DepartureLocation == "" {
		http.Error(w, "departure_location is required for ridesharing threads", http.StatusBadRequest)
		return
	}

	var authorIDArg interface{}
	if callerID != 0 {
		authorIDArg = callerID
	}
	res, err := db.Exec(
		`INSERT INTO threads (event_id, type, title, author_id, departure_location, departure_time, available_seats)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventID, req.Type, req.Title, authorIDArg, req.DepartureLocation, req.DepartureTime, req.AvailableSeats,
	)
	if err != nil {
		http.Error(w, "Failed to create thread", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	thread, err := scanThread(db.QueryRow(
		threadSelect+" WHERE t.id = ? GROUP BY t.id", id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(thread)
}

// GET /api/v1/events/{id}/threads/{thread_id}
func getThread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	threadID, err := strconv.Atoi(mux.Vars(r)["thread_id"])
	if err != nil {
		http.Error(w, "Invalid thread ID", http.StatusBadRequest)
		return
	}

	thread, err := scanThread(db.QueryRow(
		threadSelect+" WHERE t.id = ? AND t.event_id = ? GROUP BY t.id", threadID, eventID))
	if err == sql.ErrNoRows {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	thread.Posts, err = fetchThreadPosts(threadID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(thread)
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
	threadID, err := strconv.Atoi(mux.Vars(r)["thread_id"])
	if err != nil {
		http.Error(w, "Invalid thread ID", http.StatusBadRequest)
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

	// Fetch current thread to verify it belongs to this event.
	var tid, tEventID int
	if err := db.QueryRow("SELECT id, event_id FROM threads WHERE id = ?", threadID).
		Scan(&tid, &tEventID); err == sql.ErrNoRows {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tEventID != eventID {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}

	if req.IsClosed != nil {
		if _, err := db.Exec("UPDATE threads SET is_closed = ? WHERE id = ?",
			*req.IsClosed, threadID); err != nil {
			http.Error(w, "Failed to update thread", http.StatusInternalServerError)
			return
		}
	}
	if req.DepartureLocation != "" {
		if _, err := db.Exec("UPDATE threads SET departure_location = ? WHERE id = ?",
			req.DepartureLocation, threadID); err != nil {
			http.Error(w, "Failed to update thread", http.StatusInternalServerError)
			return
		}
	}
	if req.DepartureTime != "" {
		if _, err := db.Exec("UPDATE threads SET departure_time = ? WHERE id = ?",
			req.DepartureTime, threadID); err != nil {
			http.Error(w, "Failed to update thread", http.StatusInternalServerError)
			return
		}
	}
	if req.AvailableSeats != nil {
		if _, err := db.Exec("UPDATE threads SET available_seats = ? WHERE id = ?",
			*req.AvailableSeats, threadID); err != nil {
			http.Error(w, "Failed to update thread", http.StatusInternalServerError)
			return
		}
	}

	thread, err := scanThread(db.QueryRow(
		threadSelect+" WHERE t.id = ? GROUP BY t.id", threadID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(thread)
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
	threadID, err := strconv.Atoi(mux.Vars(r)["thread_id"])
	if err != nil {
		http.Error(w, "Invalid thread ID", http.StatusBadRequest)
		return
	}
	if !threadAuthCheck(w, userRole, callerID, eventID) {
		return
	}

	var tEventID int
	if err := db.QueryRow("SELECT event_id FROM threads WHERE id = ?", threadID).
		Scan(&tEventID); err == sql.ErrNoRows {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tEventID != eventID {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}

	if _, err := db.Exec("DELETE FROM threads WHERE id = ?", threadID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	threadID, err := strconv.Atoi(mux.Vars(r)["thread_id"])
	if err != nil {
		http.Error(w, "Invalid thread ID", http.StatusBadRequest)
		return
	}

	var req PostCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}

	var tEventID, isClosed int
	if err := db.QueryRow("SELECT event_id, is_closed FROM threads WHERE id = ?", threadID).
		Scan(&tEventID, &isClosed); err == sql.ErrNoRows {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tEventID != eventID {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}
	if isClosed == 1 {
		http.Error(w, "Thread is closed", http.StatusConflict)
		return
	}

	var authorIDArg interface{}
	if callerID != 0 {
		authorIDArg = callerID
	}
	res, err := db.Exec(
		"INSERT INTO posts (thread_id, author_id, body) VALUES (?, ?, ?)",
		threadID, authorIDArg, req.Body,
	)
	if err != nil {
		http.Error(w, "Failed to create post", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	var post Post
	var authorID sql.NullInt64
	if err := db.QueryRow(`
		SELECT p.id, p.thread_id, p.author_id, COALESCE(u.username,''), p.body, p.created_at
		FROM posts p LEFT JOIN users u ON p.author_id = u.id
		WHERE p.id = ?`, id).
		Scan(&post.ID, &post.ThreadID, &authorID, &post.AuthorName, &post.Body, &post.CreatedAt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if authorID.Valid {
		v := int(authorID.Int64)
		post.AuthorID = &v
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(post)
}

// DELETE /api/v1/events/{id}/threads/{thread_id}/posts/{post_id}
// Allowed for: admin, org member of the event's organisation, or the post's own author.
func deletePost(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	eventID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	threadID, err := strconv.Atoi(mux.Vars(r)["thread_id"])
	if err != nil {
		http.Error(w, "Invalid thread ID", http.StatusBadRequest)
		return
	}
	postID, err := strconv.Atoi(mux.Vars(r)["post_id"])
	if err != nil {
		http.Error(w, "Invalid post ID", http.StatusBadRequest)
		return
	}

	// Fetch post and verify it belongs to this thread.
	var postAuthorID sql.NullInt64
	var pThreadID int
	if err := db.QueryRow("SELECT thread_id, author_id FROM posts WHERE id = ?", postID).
		Scan(&pThreadID, &postAuthorID); err == sql.ErrNoRows {
		http.Error(w, "Post not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pThreadID != threadID {
		http.Error(w, "Post not found", http.StatusNotFound)
		return
	}

	// Allow if admin, or the post's own author.
	isAuthor := postAuthorID.Valid && int(postAuthorID.Int64) == callerID
	if !isAuthor {
		// Fall back to org-member / admin check.
		if !threadAuthCheck(w, userRole, callerID, eventID) {
			return
		}
	}

	// Verify the thread belongs to this event.
	var tEventID int
	if err := db.QueryRow("SELECT event_id FROM threads WHERE id = ?", threadID).
		Scan(&tEventID); err == sql.ErrNoRows || tEventID != eventID {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec("DELETE FROM posts WHERE id = ?", postID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
