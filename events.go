package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	ImageURL    string   `json:"image_url,omitempty"`
	Location    string   `json:"-"`
	TagsJSON    string   `json:"-"`
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

// ── package-level state ────────────────────────────────────────────────────

var berlinLoc *time.Location

var timeFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
}

// SELECT used by all event list / single-event queries
const eventListSelect = `SELECT e.id, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.short_code, e.created_at, COALESCE(l.location, '') FROM events e LEFT JOIN locations l ON e.location_id = l.id`

// ── low-level helpers ──────────────────────────────────────────────────────

func epochToLocal(epoch int64) string {
	return time.Unix(epoch, 0).In(berlinLoc).Format(time.RFC3339)
}

func parseTimeToUnix(s string) (int64, error) {
	for _, layout := range timeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix(), nil
		}
	}
	return 0, fmt.Errorf("unrecognised time format: %q", s)
}

// boolParam converts a "true"/"false" query param string to a SQLite integer.
func boolParam(s string) int {
	if s == "true" {
		return 1
	}
	return 0
}

// eventImageURL returns the API path for an event's image if one exists on disk.
func eventImageURL(id int) string {
	path := filepath.Join(config.Server.ImagesDir, fmt.Sprintf("%d.avif", id))
	if _, err := os.Stat(path); err == nil {
		return fmt.Sprintf("/api/v1/images/%d", id)
	}
	return ""
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanEventRow decodes one row from the eventListSelect query (12 columns including location).
func scanEventRow(s scanner) (Event, error) {
	var event Event
	var hasBallInt, hasWorkshopInt, isPublishedInt int
	var startEpoch, endEpoch int64
	if err := s.Scan(&event.ID, &event.Title, &event.Description, &startEpoch, &endEpoch,
		&hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt,
		&event.ShortCode, &event.CreatedAt, &event.Location); err != nil {
		return Event{}, err
	}
	event.StartTime = epochToLocal(startEpoch)
	event.EndTime = epochToLocal(endEpoch)
	event.HasBall = hasBallInt == 1
	event.HasWorkshop = hasWorkshopInt == 1
	event.IsPublished = isPublishedInt == 1
	event.ImageURL = eventImageURL(event.ID)
	if event.TagsJSON != "" {
		json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
	}
	return event, nil
}

// fetchEventByID loads a single event by primary key (no location join).
func fetchEventByID(id int) (Event, error) {
	var event Event
	var hasBallInt, hasWorkshopInt, isPublishedInt int
	var startEpoch, endEpoch int64
	err := db.QueryRow(
		"SELECT id, title, description, start_time, end_time, has_ball, has_workshop, tags, is_published, short_code, created_at FROM events WHERE id = ?", id,
	).Scan(&event.ID, &event.Title, &event.Description, &startEpoch, &endEpoch,
		&hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt,
		&event.ShortCode, &event.CreatedAt)
	if err != nil {
		return Event{}, err
	}
	event.StartTime = epochToLocal(startEpoch)
	event.EndTime = epochToLocal(endEpoch)
	event.HasBall = hasBallInt == 1
	event.HasWorkshop = hasWorkshopInt == 1
	event.IsPublished = isPublishedInt == 1
	event.ImageURL = eventImageURL(event.ID)
	if event.TagsJSON != "" {
		json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
	}
	return event, nil
}

// ── iCal helpers ───────────────────────────────────────────────────────────

// addEventToCalendar appends one Event to an iCal calendar object.
func addEventToCalendar(cal *ics.Calendar, event Event) {
	vevent := cal.AddEvent(fmt.Sprintf("event-%d@go-calendar", event.ID))
	vevent.SetSummary(event.Title)
	if event.Description != "" {
		vevent.SetDescription(event.Description)
	}
	if start, err := time.Parse(time.RFC3339, event.StartTime); err == nil {
		vevent.SetProperty(ics.ComponentPropertyDtStart, start.UTC().Format("20060102T150405Z"))
	}
	if end, err := time.Parse(time.RFC3339, event.EndTime); err == nil {
		vevent.SetProperty(ics.ComponentPropertyDtEnd, end.UTC().Format("20060102T150405Z"))
	}
	if event.Location != "" {
		vevent.SetLocation(event.Location)
	}
}

// ── query-building helpers ─────────────────────────────────────────────────

// applyEventFilters appends shared WHERE clauses from query parameters.
func applyEventFilters(r *http.Request, query *string, args *[]interface{}) {
	q := r.URL.Query()

	if title := q.Get("title"); title != "" {
		*query += " AND e.title LIKE ?"
		*args = append(*args, "%"+title+"%")
	}
	if desc := q.Get("description"); desc != "" {
		*query += " AND e.description LIKE ?"
		*args = append(*args, "%"+desc+"%")
	}
	if v := q.Get("start_time_after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*query += " AND e.start_time > ?"
			*args = append(*args, n)
		}
	}
	if v := q.Get("start_time_before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*query += " AND e.start_time < ?"
			*args = append(*args, n)
		}
	}
	if loc := q.Get("location"); loc != "" {
		*query += " AND l.location LIKE ?"
		*args = append(*args, "%"+loc+"%")
	}
	if v := q.Get("has_ball"); v != "" {
		*query += " AND e.has_ball = ?"
		*args = append(*args, boolParam(v))
	}
	if v := q.Get("has_workshop"); v != "" {
		*query += " AND e.has_workshop = ?"
		*args = append(*args, boolParam(v))
	}
	if tag := q.Get("tag"); tag != "" {
		*query += " AND e.tags LIKE ?"
		*args = append(*args, "%"+tag+"%")
	}
}

// applyPagination appends ORDER BY + LIMIT/OFFSET clauses.
func applyPagination(r *http.Request, query *string, args *[]interface{}) {
	q := r.URL.Query()
	limit, offset := 100, 0
	if l := q.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	if o := q.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	*query += " ORDER BY e.start_time ASC LIMIT ? OFFSET ?"
	*args = append(*args, limit, offset)
}

// ── event insert / update ──────────────────────────────────────────────────

func generateShortCode(eventID int, title string) string {
	hash := md5.Sum([]byte(fmt.Sprintf("%d-%s-%d", eventID, title, time.Now().Unix())))
	return fmt.Sprintf("%x", hash)[:8]
}

// insertEvent upserts an event. Returns (id, shortCode, created, error) where
// created=false means an existing event was updated instead of inserted.
func insertEvent(title, description string, startTime, endTime int64, locationID int64, hasBall, hasWorkshop bool, tags []string, isPublished bool) (int, string, bool, error) {
	const threeHours = int64(3 * 60 * 60)
	var existingID int
	var existingShortCode string
	err := db.QueryRow(
		"SELECT id, short_code FROM events WHERE title = ? AND location_id = ? AND ABS(start_time - ?) < ?",
		title, locationID, startTime, threeHours,
	).Scan(&existingID, &existingShortCode)
	if err != nil && err != sql.ErrNoRows {
		return 0, "", false, err
	}

	tagsJSON, _ := json.Marshal(tags)

	if err == nil {
		// Duplicate — update existing event
		_, err = db.Exec(
			"UPDATE events SET description=?, start_time=?, end_time=?, location_id=?, has_ball=?, has_workshop=?, tags=?, is_published=? WHERE id=?",
			description, startTime, endTime, locationID, hasBall, hasWorkshop, string(tagsJSON), isPublished, existingID,
		)
		if err != nil {
			return 0, "", false, err
		}
		return existingID, existingShortCode, false, nil
	}

	result, err := db.Exec(
		"INSERT INTO events (title, description, start_time, end_time, location_id, has_ball, has_workshop, tags, is_published) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		title, description, startTime, endTime, locationID, hasBall, hasWorkshop, string(tagsJSON), isPublished,
	)
	if err != nil {
		return 0, "", false, err
	}

	id, _ := result.LastInsertId()
	shortCode := generateShortCode(int(id), title)
	if _, err = db.Exec("UPDATE events SET short_code = ? WHERE id = ?", shortCode, id); err != nil {
		return 0, "", false, err
	}
	return int(id), shortCode, true, nil
}

// createEventFromRequest inserts or updates all events described by req.
// Returns (events, allCreated, error); allCreated=false if any event was updated.
func createEventFromRequest(req EventCreateRequest, locationID int64, isPublished bool) ([]Event, bool, error) {
	var createdEvents []Event
	allCreated := true

	type dateEntry struct {
		description, startTime, endTime string
	}

	var entries []dateEntry
	if len(req.Date) > 0 {
		for _, d := range req.Date {
			desc := d.Description
			if desc == "" {
				desc = req.Description
			}
			entries = append(entries, dateEntry{desc, d.StartTime, d.EndTime})
		}
	} else {
		entries = []dateEntry{{req.Description, req.StartTime, req.EndTime}}
	}

	for _, entry := range entries {
		startTime, err := parseTimeToUnix(entry.startTime)
		if err != nil {
			return nil, false, fmt.Errorf("start_time: %w", err)
		}
		endTime, err := parseTimeToUnix(entry.endTime)
		if err != nil {
			return nil, false, fmt.Errorf("end_time: %w", err)
		}

		id, shortCode, created, err := insertEvent(req.Title, entry.description, startTime, endTime, locationID, req.HasBall, req.HasWorkshop, req.Tags, isPublished)
		if err != nil {
			return nil, false, err
		}
		if !created {
			allCreated = false
		}

		event, err := fetchEventByID(id)
		if err != nil {
			return nil, false, err
		}
		event.ShortCode = shortCode
		createdEvents = append(createdEvents, event)
	}

	return createdEvents, allCreated, nil
}

// ── HTTP handlers ──────────────────────────────────────────────────────────

// GET /api/v1/events
func getEvents(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	userRole := r.Header.Get("X-User-Role")
	isAuthorizedAdmin := userRole == "user" || userRole == "admin"

	query := eventListSelect + " WHERE 1=1"
	args := []interface{}{}

	if !isAuthorizedAdmin {
		query += " AND e.is_published = 1"
	} else if v := r.URL.Query().Get("is_published"); v != "" {
		query += " AND e.is_published = ?"
		args = append(args, boolParam(v))
	}

	// end_time filters (not exposed on public endpoint)
	if v := r.URL.Query().Get("end_time_after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			query += " AND e.end_time > ?"
			args = append(args, n)
		}
	}
	if v := r.URL.Query().Get("end_time_before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			query += " AND e.end_time < ?"
			args = append(args, n)
		}
	}

	applyEventFilters(r, &query, &args)
	applyPagination(r, &query, &args)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		event, err := scanEventRow(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		events = append(events, event)
	}

	if strings.Contains(accept, "text/calendar") {
		cal := ics.NewCalendar()
		cal.SetMethod(ics.MethodPublish)
		for _, event := range events {
			addEventToCalendar(cal, event)
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(cal.Serialize()))
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	}
}

// POST /api/v1/events
func createEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	isPublished := false
	if r.Header.Get("Authorization") != "" {
		role := r.Header.Get("X-User-Role")
		isPublished = role == "user" || role == "admin"
	}

	contentType := r.Header.Get("Content-Type")
	var requests []EventCreateRequest

	if contentType == "application/json" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			status := http.StatusBadRequest
			if errors.As(err, new(*http.MaxBytesError)) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, err.Error(), status)
			return
		}
		var arrayReqs []EventCreateRequest
		if err := json.Unmarshal(body, &arrayReqs); err == nil && len(arrayReqs) > 0 && arrayReqs[0].Title != "" {
			requests = arrayReqs
		} else {
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
			status := http.StatusBadRequest
			if errors.As(err, new(*http.MaxBytesError)) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, err.Error(), status)
			return
		}
		cal, err := ics.ParseCalendar(strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, "Invalid iCal format", http.StatusBadRequest)
			return
		}
		for _, event := range cal.Events() {
			var startTime, endTime string
			if p := event.GetProperty(ics.ComponentPropertyDtStart); p != nil {
				if t, err := time.Parse("20060102T150405Z", p.Value); err == nil {
					startTime = t.UTC().Format(time.RFC3339)
				}
			}
			if p := event.GetProperty(ics.ComponentPropertyDtEnd); p != nil {
				if t, err := time.Parse("20060102T150405Z", p.Value); err == nil {
					endTime = t.UTC().Format(time.RFC3339)
				}
			}
			requests = append(requests, EventCreateRequest{
				Title:       event.GetProperty(ics.ComponentPropertySummary).Value,
				Description: event.GetProperty(ics.ComponentPropertyDescription).Value,
				StartTime:   startTime,
				EndTime:     endTime,
				Location: EventLocationRequest{
					Location: event.GetProperty(ics.ComponentPropertyLocation).Value,
				},
			})
		}
		if len(requests) == 0 {
			http.Error(w, "No events found in iCal file", http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "Content-Type must be application/json or text/calendar", http.StatusUnsupportedMediaType)
		return
	}

	var allCreatedEvents []Event
	allCreated := true
	for _, req := range requests {
		var locationID int64
		err := db.QueryRow("SELECT id FROM locations WHERE location = ?", req.Location.Location).Scan(&locationID)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if err == sql.ErrNoRows {
			result, err := db.Exec(
				"INSERT INTO locations (location, address, zipcode, town, latitude, longitude, internetsite) VALUES (?, ?, ?, ?, ?, ?, ?)",
				req.Location.Location, req.Location.Address, req.Location.Zipcode, req.Location.Town,
				req.Location.Latitude, req.Location.Longitude, req.Location.Eventsite,
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			locationID, _ = result.LastInsertId()
		}

		createdEvents, created, err := createEventFromRequest(req, locationID, isPublished)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !created {
			allCreated = false
		}
		allCreatedEvents = append(allCreatedEvents, createdEvents...)
	}

	if allCreated {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(allCreatedEvents)
}

// GET /api/v1/events/{id}
func getEvent(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	id := mux.Vars(r)["id"]

	event, err := scanEventRow(db.QueryRow(eventListSelect+" WHERE e.id = ?", id))
	if err == sql.ErrNoRows {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if strings.Contains(accept, "text/calendar") {
		cal := ics.NewCalendar()
		cal.SetMethod(ics.MethodPublish)
		addEventToCalendar(cal, event)
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(cal.Serialize()))
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(event)
	}
}

// POST /api/v1/events/{id}/publish
func publishEvent(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	if userRole != "admin" && userRole != "user" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]
	result, err := db.Exec("UPDATE events SET is_published = 1 WHERE id = ?", id)
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

// DELETE /api/v1/events/{id}
func deleteEvent(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
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

// GET /events — public endpoint: resolve short code or list published events
func publicGetEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if shortCode := r.URL.Query().Get("code"); shortCode != "" {
		event, err := scanEventRow(db.QueryRow(
			eventListSelect+" WHERE e.short_code = ? AND e.is_published = 1", shortCode,
		))
		if err == sql.ErrNoRows {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(event)
		return
	}

	query := eventListSelect + " WHERE e.is_published = 1"
	args := []interface{}{}
	applyEventFilters(r, &query, &args)
	applyPagination(r, &query, &args)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		event, err := scanEventRow(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		events = append(events, event)
	}
	json.NewEncoder(w).Encode(events)
}

// GET /events.ics — public iCal feed of future published events, filterable by tag and location
func publicGetEventsICS(w http.ResponseWriter, r *http.Request) {
	query := eventListSelect + " WHERE e.is_published = 1 AND e.start_time >= ?"
	args := []interface{}{time.Now().Unix()}

	if tag := r.URL.Query().Get("tag"); tag != "" {
		query += " AND e.tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}
	if loc := r.URL.Query().Get("location"); loc != "" {
		query += " AND l.location LIKE ?"
		args = append(args, "%"+loc+"%")
	}

	query += " ORDER BY e.start_time ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	for rows.Next() {
		event, err := scanEventRow(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		addEventToCalendar(cal, event)
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=events.ics")
	w.Write([]byte(cal.Serialize()))
}

// GET /events/tag/{tag}.ics — public iCal feed of future published events for a specific tag
func publicGetEventsByTagICS(w http.ResponseWriter, r *http.Request) {
	tag := mux.Vars(r)["tag"]
	query := eventListSelect + " WHERE e.is_published = 1 AND e.start_time >= ? AND e.tags LIKE ? ORDER BY e.start_time ASC"
	rows, err := db.Query(query, time.Now().Unix(), "%"+tag+"%")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	for rows.Next() {
		event, err := scanEventRow(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		addEventToCalendar(cal, event)
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+tag+".ics")
	w.Write([]byte(cal.Serialize()))
}

// GET /events/town/{town}.ics — public iCal feed of future published events for a specific town
func publicGetEventsByTownICS(w http.ResponseWriter, r *http.Request) {
	town := mux.Vars(r)["town"]
	query := eventListSelect + " WHERE e.is_published = 1 AND e.start_time >= ? AND l.town LIKE ? ORDER BY e.start_time ASC"
	rows, err := db.Query(query, time.Now().Unix(), "%"+town+"%")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	for rows.Next() {
		event, err := scanEventRow(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		addEventToCalendar(cal, event)
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+town+".ics")
	w.Write([]byte(cal.Serialize()))
}

// GET /api/v1/tags
func getTags(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userRole := r.Header.Get("X-User-Role")
	isAuthorizedAdmin := userRole == "user" || userRole == "admin"

	query := "SELECT tags FROM events WHERE 1=1"
	var args []interface{}
	if !isAuthorizedAdmin {
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
		if err := rows.Scan(&tagsJSON); err != nil {
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

	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	json.NewEncoder(w).Encode(tags)
}
