package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	ShortCode   string   `json:"short_code"`
	CreatedAt   string   `json:"created_at"`
	Location    string   `json:"-"` // For iCal
	TagsJSON    string   `json:"-"` // Internal
}

type EventDate struct {
	Description string `json:"description"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
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
	Date        []EventDate          `json:"date"`
	Musicians   []string             `json:"musicians"`
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

	query := "SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.short_code, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE 1=1"
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

	// Title filter
	if title := r.URL.Query().Get("title"); title != "" {
		query += " AND e.title LIKE ?"
		args = append(args, "%"+title+"%")
	}

	// Description filter
	if desc := r.URL.Query().Get("description"); desc != "" {
		query += " AND e.description LIKE ?"
		args = append(args, "%"+desc+"%")
	}

	// Time filters
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

	// Location filter
	if loc := r.URL.Query().Get("location"); loc != "" {
		query += " AND l.location LIKE ?"
		args = append(args, "%"+loc+"%")
	}

	// Boolean filters
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

	// Tag filter
	if tag := r.URL.Query().Get("tag"); tag != "" {
		query += " AND e.tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}

	// Pagination
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	query += " ORDER BY e.start_time ASC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

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
		err := rows.Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.ShortCode, &event.CreatedAt, &event.Location)
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

// Helper function to generate a short code for an event
func generateShortCode(eventID int, title string) string {
	// Create a hash using event ID and title
	hash := md5.Sum([]byte(fmt.Sprintf("%d-%s-%d", eventID, title, time.Now().Unix())))
	// Use first 8 characters of hex hash as short code
	shortCode := fmt.Sprintf("%x", hash)[:8]
	return shortCode
}

// Helper function to insert a single event
func insertEvent(title, description, startTime, endTime string, locationID int64, hasBall, hasWorkshop bool, tags []string, isPublished bool) (int, string, error) {
	// Marshal tags to JSON
	tagsJSON, _ := json.Marshal(tags)

	// Insert event
	result, err := db.Exec(
		"INSERT INTO events (title, description, start_time, end_time, location_id, has_ball, has_workshop, tags, is_published) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		title, description, startTime, endTime, locationID, hasBall, hasWorkshop, string(tagsJSON), isPublished,
	)
	if err != nil {
		return 0, "", err
	}

	id, _ := result.LastInsertId()
	shortCode := generateShortCode(int(id), title)

	// Update the event with the short code
	_, err = db.Exec("UPDATE events SET short_code = ? WHERE id = ?", shortCode, id)
	if err != nil {
		return 0, "", err
	}

	return int(id), shortCode, nil
}

// Helper function to create an event from a request and fetch it back
func createEventFromRequest(req EventCreateRequest, locationID int64, isPublished bool) ([]Event, error) {
	var createdEvents []Event

	// Check if this is an event series (date array provided) or single event
	if len(req.Date) > 0 {
		// Handle event series - create an event for each date entry
		for _, dateEntry := range req.Date {
			// Use dateEntry description if provided, otherwise use request description
			description := dateEntry.Description
			if description == "" {
				description = req.Description
			}

			id, shortCode, err := insertEvent(req.Title, description, dateEntry.StartTime, dateEntry.EndTime, locationID, req.HasBall, req.HasWorkshop, req.Tags, isPublished)
			if err != nil {
				return nil, err
			}

			// Fetch the created event
			var event Event
			var hasBallInt, hasWorkshopInt, isPublishedInt int
			err = db.QueryRow("SELECT id, title, description, start_time, end_time, has_ball, has_workshop, tags, is_published, short_code, created_at FROM events WHERE id = ?", id).Scan(
				&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.ShortCode, &event.CreatedAt)
			if err != nil {
				return nil, err
			}
			event.ShortCode = shortCode
			event.HasBall = hasBallInt == 1
			event.HasWorkshop = hasWorkshopInt == 1
			event.IsPublished = isPublishedInt == 1
			if event.TagsJSON != "" {
				json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
			}
			createdEvents = append(createdEvents, event)
		}
	} else {
		// Handle single event
		id, shortCode, err := insertEvent(req.Title, req.Description, req.StartTime, req.EndTime, locationID, req.HasBall, req.HasWorkshop, req.Tags, isPublished)
		if err != nil {
			return nil, err
		}

		var event Event
		var hasBallInt, hasWorkshopInt, isPublishedInt int
		err = db.QueryRow("SELECT id, title, description, start_time, end_time, has_ball, has_workshop, tags, is_published, short_code, created_at FROM events WHERE id = ?", id).Scan(
			&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.ShortCode, &event.CreatedAt)
		if err != nil {
			return nil, err
		}
		event.ShortCode = shortCode
		event.HasBall = hasBallInt == 1
		event.HasWorkshop = hasWorkshopInt == 1
		event.IsPublished = isPublishedInt == 1
		if event.TagsJSON != "" {
			json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
		}
		createdEvents = append(createdEvents, event)
	}

	return createdEvents, nil
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
	var requests []EventCreateRequest

	if contentType == "application/json" {
		// Try to parse as array first, then fall back to single object
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Try parsing as array
		var arrayReqs []EventCreateRequest
		if err := json.Unmarshal(body, &arrayReqs); err == nil && len(arrayReqs) > 0 && arrayReqs[0].Title != "" {
			// It's a valid array of events
			requests = arrayReqs
		} else {
			// Try parsing as single event object
			var singleReq EventCreateRequest
			if err := json.Unmarshal(body, &singleReq); err != nil {
				http.Error(w, "Invalid JSON: must be a single event object or array of events", http.StatusBadRequest)
				return
			}
			requests = []EventCreateRequest{singleReq}
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

		// Parse all events from the calendar
		for _, event := range cal.Events() {
			var eventStartTime, eventEndTime string
			if dtStart := event.GetProperty(ics.ComponentPropertyDtStart); dtStart != nil {
				eventStartTime = dtStart.Value
			}
			if dtEnd := event.GetProperty(ics.ComponentPropertyDtEnd); dtEnd != nil {
				eventEndTime = dtEnd.Value
			}
			req := EventCreateRequest{
				Title:       event.GetProperty(ics.ComponentPropertySummary).Value,
				Description: event.GetProperty(ics.ComponentPropertyDescription).Value,
				StartTime:   eventStartTime,
				EndTime:     eventEndTime,
				//				StartTime:   event.GetProperty(ics.ComponentPropertyDtStart).Value,
				//				EndTime: event.GetProperty(ics.ComponentPropertyDtEnd).Value,
				Location: EventLocationRequest{
					Location:  event.GetProperty(ics.ComponentPropertyLocation).Value,
					Address:   "",
					Zipcode:   "",
					Town:      "",
					Latitude:  "",
					Longitude: "",
					Eventsite: "",
				},
			}
			requests = append(requests, req)
		}

		if len(requests) == 0 {
			http.Error(w, "No events found in iCal file", http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "Content-Type must be application/json or text/calendar", http.StatusUnsupportedMediaType)
		return
	}

	// Process all requests
	var allCreatedEvents []Event
	for _, req := range requests {
		// Check for existing location with same name to avoid duplicates
		var locationID int64
		err := db.QueryRow(
			"SELECT id FROM locations WHERE location = ? ",
			req.Location.Location).Scan(&locationID)

		if err != nil && err != sql.ErrNoRows {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if err == sql.ErrNoRows {

			// Insert location
			result, err := db.Exec(
				"INSERT INTO locations (location, address, zipcode, town, latitude, longitude, internetsite) VALUES (?, ?, ?, ?, ?, ?, ?)",
				req.Location.Location, req.Location.Address, req.Location.Zipcode, req.Location.Town, req.Location.Latitude, req.Location.Longitude, req.Location.Eventsite,
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			locationID, _ = result.LastInsertId()
		}
		// Create events from this request
		createdEvents, err := createEventFromRequest(req, locationID, isPublished)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		allCreatedEvents = append(allCreatedEvents, createdEvents...)
	}

	w.WriteHeader(http.StatusCreated)
	// Always return array, even if single event
	json.NewEncoder(w).Encode(allCreatedEvents)
}

// GET /api/v1/events/{id}
func getEvent(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")

	vars := mux.Vars(r)
	id := vars["id"]

	var event Event
	var hasBallInt, hasWorkshopInt, isPublishedInt int
	err := db.QueryRow(
		"SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.short_code, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE e.id = ?",
		id,
	).Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.ShortCode, &event.CreatedAt, &event.Location)

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

// GET /events - Public endpoint for resolving short codes and searching events
func publicGetEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// If short code is provided, resolve it
	if shortCode := r.URL.Query().Get("code"); shortCode != "" {
		var event Event
		var hasBallInt, hasWorkshopInt, isPublishedInt int
		err := db.QueryRow(
			"SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.short_code, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE e.short_code = ? AND e.is_published = 1",
			shortCode,
		).Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.ShortCode, &event.CreatedAt, &event.Location)

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

		json.NewEncoder(w).Encode(event)
		return
	}

	// Otherwise, list published events with filters
	query := "SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.short_code, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE e.is_published = 1"
	args := []interface{}{}

	// Title filter
	if title := r.URL.Query().Get("title"); title != "" {
		query += " AND e.title LIKE ?"
		args = append(args, "%"+title+"%")
	}

	// Description filter
	if desc := r.URL.Query().Get("description"); desc != "" {
		query += " AND e.description LIKE ?"
		args = append(args, "%"+desc+"%")
	}

	// Time filters
	if startAfter := r.URL.Query().Get("start_time_after"); startAfter != "" {
		query += " AND e.start_time > ?"
		args = append(args, startAfter)
	}
	if startBefore := r.URL.Query().Get("start_time_before"); startBefore != "" {
		query += " AND e.start_time < ?"
		args = append(args, startBefore)
	}

	// Location filter
	if loc := r.URL.Query().Get("location"); loc != "" {
		query += " AND l.location LIKE ?"
		args = append(args, "%"+loc+"%")
	}

	// Boolean filters
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

	// Tag filter
	if tag := r.URL.Query().Get("tag"); tag != "" {
		query += " AND e.tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}

	// Pagination
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	query += " ORDER BY e.start_time ASC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

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
		err := rows.Scan(&event.ID, &event.Title, &event.Description, &event.StartTime, &event.EndTime, &hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt, &event.ShortCode, &event.CreatedAt, &event.Location)
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

	json.NewEncoder(w).Encode(events)
}

// GET /api/v1/tags
func getTags(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check user authorization status
	userRole := r.Header.Get("X-User-Role")
	isAuthorizedAdmin := userRole == "user" || userRole == "admin"

	query := "SELECT tags FROM events WHERE 1=1"
	args := []interface{}{}

	// Add is_published filter based on authorization
	if !isAuthorizedAdmin {
		// Non-authorized or viewers can only see tags from published events
		query += " AND is_published = 1"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tagSet := make(map[string]bool)
	for rows.Next() {
		var tagsJSON string
		err := rows.Scan(&tagsJSON)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if tagsJSON != "" {
			var tags []string
			json.Unmarshal([]byte(tagsJSON), &tags)
			for _, tag := range tags {
				tagSet[tag] = true
			}
		}
	}

	// Convert map to slice
	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	json.NewEncoder(w).Encode(tags)
}
