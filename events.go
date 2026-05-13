package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/gorilla/mux"
)

type Event struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	StartTime   string   `json:"start_time"`
	EndTime     string   `json:"end_time"`
	HasBall     bool     `json:"has_ball"`
	HasWorkshop bool     `json:"has_workshop"`
	Tags        []string `json:"tags"`
	IsPublished bool     `json:"is_published"`
	CreatedAt   string   `json:"created_at"`
	Location    string   `json:"-"` // For iCal
	TagsJSON    string   `json:"-"` // Internal
}

type EventCreateRequest struct {
	Title       string               `json:"title"`
	Description string               `json:"description"`
	StartTime   string               `json:"start_time"`
	EndTime     string               `json:"end_time"`
	HasBall     bool                 `json:"has_ball"`
	HasWorkshop bool                 `json:"has_workshop"`
	Tags        []string             `json:"tags"`
	Location    EventLocationRequest `json:"location"`
}

type EventLocationRequest struct {
	Location  string `json:"location"`
	Address   string `json:"address"`
	Zipcode   string `json:"zipcode"`
	Town      string `json:"town"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Eventsite string `json:"eventsite"`
}

// GET /api/v1/events
func getEvents(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")

	// Check user authorization status
	userRole := r.Header.Get("X-User-Role")
	isAuthorizedAdmin := userRole == "user" || userRole == "admin"

	query := "SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE 1=1"
	args := []interface{}{}

	// Add is_published filter based on authorization
	if !isAuthorizedAdmin {
		// Non-authorized or viewers can only see published events
		query += " AND e.is_published = 1"
	} else if isPublished := r.URL.Query().Get("is_published"); isPublished != "" {
		// Authorized users can optionally filter by is_published
		val := 0
		if isPublished == "true" {
			val = 1
		}
		query += " AND e.is_published = ?"
		args = append(args, val)
	}

	if startAfter := r.URL.Query().Get("start_time_after"); startAfter != "" {
		query += " AND e.start_time > ?"
		args = append(args, startAfter)
	}
	if startBefore := r.URL.Query().Get("start_time_before"); startBefore != "" {
		query += " AND e.start_time < ?"
		args = append(args, startBefore)
	}
	if endAfter := r.URL.Query().Get("end_time_after"); endAfter != "" {
		query += " AND e.end_time > ?"
		args = append(args, endAfter)
	}
	if endBefore := r.URL.Query().Get("end_time_before"); endBefore != "" {
		query += " AND e.end_time < ?"
		args = append(args, endBefore)
	}
	if loc := r.URL.Query().Get("location"); loc != "" {
		query += " AND l.location LIKE ?"
		args = append(args, "%"+loc+"%")
	}
	if hb := r.URL.Query().Get("has_ball"); hb != "" {
		val := 0
		if hb == "true" {
			val = 1
		}
		query += " AND e.has_ball = ?"
		args = append(args, val)
	}
	if hw := r.URL.Query().Get("has_workshop"); hw != "" {
		val := 0
		if hw == "true" {
			val = 1
		}
		query += " AND e.has_workshop = ?"
		args = append(args, val)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var hasBallInt, hasWorkshopInt, isPublishedInt int
		err := rows.Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.CreatedAt, &event.Location)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		event.HasBall = hasBallInt == 1
		event.HasWorkshop = hasWorkshopInt == 1
		event.IsPublished = isPublishedInt == 1
		if event.TagsJSON != "" {
			json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
		}
		events = append(events, event)
	}

	if strings.Contains(accept, "text/calendar") {
		// Generate iCal
		cal := ics.NewCalendar()
		cal.SetMethod(ics.MethodPublish)
		for _, event := range events {
			vevent := cal.AddEvent(fmt.Sprintf("event-%d@go-calendar", event.ID))
			vevent.SetSummary(event.Title)
			if event.Description != "" {
				vevent.SetDescription(event.Description)
			}
			// Parse times, assume format is RFC3339 or similar
			if start, err := time.Parse(time.RFC3339, event.StartTime); err == nil {
				vevent.SetProperty(ics.ComponentPropertyDtStart, start.Format("20060102T150405Z"))
			}
			if end, err := time.Parse(time.RFC3339, event.EndTime); err == nil {
				vevent.SetProperty(ics.ComponentPropertyDtEnd, end.Format("20060102T150405Z"))
			}
			if event.Location != "" {
				vevent.SetLocation(event.Location)
			}
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(cal.Serialize()))
	} else {
		// Default to JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	}
}

// POST /api/v1/events
func createEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Determine if event should be published based on auth and role
	isPublished := false
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		userRole := r.Header.Get("X-User-Role")
		if userRole == "user" || userRole == "admin" {
			isPublished = true
		}
	}

	contentType := r.Header.Get("Content-Type")
	var req EventCreateRequest
	if contentType == "application/json" {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else if contentType == "text/calendar" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cal, err := ics.ParseCalendar(strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, "Invalid iCal format", http.StatusBadRequest)
			return
		}
		// Assume first event
		for _, event := range cal.Events() {
			req.Title = event.GetProperty(ics.ComponentPropertySummary).Value
			req.Description = event.GetProperty(ics.ComponentPropertyDescription).Value
			req.StartTime = event.GetProperty(ics.ComponentPropertyDtStart).Value
			req.EndTime = event.GetProperty(ics.ComponentPropertyDtEnd).Value
			location := event.GetProperty(ics.ComponentPropertyLocation).Value
			req.Location = EventLocationRequest{
				Location:  location,
				Address:   "",
				Zipcode:   "",
				Town:      "",
				Latitude:  "",
				Longitude: "",
				Eventsite: "",
			}
			break // only first event
		}
	} else {
		http.Error(w, "Content-Type must be application/json or text/calendar", http.StatusUnsupportedMediaType)
		return
	}

	// Insert location
	result, err := db.Exec(
		"INSERT INTO locations (location, address, zipcode, town, latitude, longitude, internetsite) VALUES (?, ?, ?, ?, ?, ?, ?)",
		req.Location.Location, req.Location.Address, req.Location.Zipcode, req.Location.Town, req.Location.Latitude, req.Location.Longitude, req.Location.Eventsite,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	locationID, _ := result.LastInsertId()

	// Marshal tags to JSON
	tagsJSON, _ := json.Marshal(req.Tags)

	// Insert event
	result, err = db.Exec(
		"INSERT INTO events (title, description, start_time, end_time, location_id, has_ball, has_workshop, tags, is_published) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		req.Title, req.Description, req.StartTime, req.EndTime, locationID, req.HasBall, req.HasWorkshop, string(tagsJSON), isPublished,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	event := Event{
		ID:          int(id),
		Title:       req.Title,
		Description: req.Description,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		HasBall:     req.HasBall,
		HasWorkshop: req.HasWorkshop,
		Tags:        req.Tags,
		IsPublished: isPublished,
		CreatedAt:   "",
	}

	// To get created_at, perhaps query back
	err = db.QueryRow("SELECT created_at FROM events WHERE id = ?", id).Scan(&event.CreatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// GET /api/v1/events/{id}
func getEvent(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")

	vars := mux.Vars(r)
	id := vars["id"]

	var event Event
	var hasBallInt, hasWorkshopInt, isPublishedInt int
	err := db.QueryRow(
		"SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE e.id = ?",
		id,
	).Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.CreatedAt, &event.Location)

	if err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	event.HasBall = hasBallInt == 1
	event.HasWorkshop = hasWorkshopInt == 1
	event.IsPublished = isPublishedInt == 1
	if event.TagsJSON != "" {
		json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
	}

	if strings.Contains(accept, "text/calendar") {
		// Generate iCal
		cal := ics.NewCalendar()
		cal.SetMethod(ics.MethodPublish)
		vevent := cal.AddEvent(fmt.Sprintf("event-%d@go-calendar", event.ID))
		vevent.SetSummary(event.Title)
		if event.Description != "" {
			vevent.SetDescription(event.Description)
		}
		if start, err := time.Parse(time.RFC3339, event.StartTime); err == nil {
			vevent.SetProperty(ics.ComponentPropertyDtStart, start.Format("20060102T150405Z"))
		}
		if end, err := time.Parse(time.RFC3339, event.EndTime); err == nil {
			vevent.SetProperty(ics.ComponentPropertyDtEnd, end.Format("20060102T150405Z"))
		}
		if event.Location != "" {
			vevent.SetLocation(event.Location)
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(cal.Serialize()))
	} else {
		// Default to JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(event)
	}
}

// DELETE /api/v1/events/{id}
func deleteEvent(w http.ResponseWriter, r *http.Request) {
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
