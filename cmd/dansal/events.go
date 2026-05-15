package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/gorilla/mux"
)

// querier is satisfied by both *sql.DB and *sql.Tx, allowing helpers to
// participate in a caller-managed transaction without changing their signature.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

type Event struct {
	ID             int      `json:"id"`
	UID            string   `json:"uid,omitempty"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	StartTime      string   `json:"start_time"`
	EndTime        string   `json:"end_time"`
	HasBall        bool     `json:"has_ball"`
	HasWorkshop    bool     `json:"has_workshop"`
	Tags           []string `json:"tags"`
	IsPublished    bool     `json:"is_published"`
	ShortCode      string   `json:"short_code"`
	CreatedAt      string   `json:"created_at"`
	ImageURL       string   `json:"image_url,omitempty"`
	OrganizationID *int     `json:"organization_id,omitempty"`
	Location       string   `json:"-"`
	TagsJSON       string   `json:"-"`
}

type EventDate struct {
	Description string `json:"description"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
}

type EventCreateRequest struct {
	UID            string               `json:"uid,omitempty"`
	Title          string               `json:"title"`
	Description    string               `json:"description"`
	StartTime      string               `json:"start_time"`
	EndTime        string               `json:"end_time"`
	HasBall        bool                 `json:"has_ball"`
	HasWorkshop    bool                 `json:"has_workshop"`
	Tags           []string             `json:"tags"`
	Location       EventLocationRequest `json:"location"`
	Date           []EventDate          `json:"date"`
	Musicians      []string             `json:"musicians"`
	OrganizationID *int                 `json:"organization_id,omitempty"`
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
const eventListSelect = `SELECT e.id, e.uid, e.title, e.description, e.start_time, e.end_time, e.has_ball, e.has_workshop, e.tags, e.is_published, e.short_code, e.created_at, COALESCE(l.location, ''), e.organization_id FROM events e LEFT JOIN locations l ON e.location_id = l.id`

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

// eventImageURL returns the API path for an event's image if the cache knows one exists.
func eventImageURL(id int) string {
	if hasImage(id) {
		return fmt.Sprintf("/api/v1/images/%d", id)
	}
	return ""
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanEventRow decodes one row from the eventListSelect query.
func scanEventRow(s scanner) (Event, error) {
	var event Event
	var hasBallInt, hasWorkshopInt, isPublishedInt int
	var startEpoch, endEpoch int64
	var orgID sql.NullInt64
	var uid sql.NullString
	if err := s.Scan(&event.ID, &uid, &event.Title, &event.Description, &startEpoch, &endEpoch,
		&hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt,
		&event.ShortCode, &event.CreatedAt, &event.Location, &orgID); err != nil {
		return Event{}, err
	}
	if uid.Valid {
		event.UID = uid.String
	}
	event.StartTime = epochToLocal(startEpoch)
	event.EndTime = epochToLocal(endEpoch)
	event.HasBall = hasBallInt == 1
	event.HasWorkshop = hasWorkshopInt == 1
	event.IsPublished = isPublishedInt == 1
	event.ImageURL = eventImageURL(event.ID)
	if orgID.Valid {
		v := int(orgID.Int64)
		event.OrganizationID = &v
	}
	if event.TagsJSON != "" {
		json.Unmarshal([]byte(event.TagsJSON), &event.Tags)
	}
	return event, nil
}

// fetchEventByID loads a single event by primary key (no location join).
func fetchEventByID(q querier, id int) (Event, error) {
	var event Event
	var hasBallInt, hasWorkshopInt, isPublishedInt int
	var startEpoch, endEpoch int64
	var orgID sql.NullInt64
	var uid sql.NullString
	err := q.QueryRow(
		"SELECT id, uid, title, description, start_time, end_time, has_ball, has_workshop, tags, is_published, short_code, created_at, organization_id FROM events WHERE id = ?", id,
	).Scan(&event.ID, &uid, &event.Title, &event.Description, &startEpoch, &endEpoch,
		&hasBallInt, &hasWorkshopInt, &event.TagsJSON, &isPublishedInt,
		&event.ShortCode, &event.CreatedAt, &orgID)
	if uid.Valid {
		event.UID = uid.String
	}
	if err != nil {
		return Event{}, err
	}
	event.StartTime = epochToLocal(startEpoch)
	event.EndTime = epochToLocal(endEpoch)
	event.HasBall = hasBallInt == 1
	event.HasWorkshop = hasWorkshopInt == 1
	event.IsPublished = isPublishedInt == 1
	event.ImageURL = eventImageURL(event.ID)
	if orgID.Valid {
		v := int(orgID.Int64)
		event.OrganizationID = &v
	}
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
		*query += " AND EXISTS (SELECT 1 FROM json_each(e.tags) WHERE value = ?)"
		*args = append(*args, tag)
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

func generateShortCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// insertEvent upserts an event. Returns (id, shortCode, created, error) where
// created=false means an existing event was updated instead of inserted.
// When uid is non-empty, deduplication is done by uid; otherwise by title+location+time.
func insertEvent(q querier, title, description string, startTime, endTime int64, locationID int64, hasBall, hasWorkshop bool, tags []string, isPublished bool, organizationID *int, uid string) (int, string, bool, error) {
	var existingID int
	var existingShortCode string
	var lookupErr error

	if uid != "" {
		lookupErr = q.QueryRow(
			"SELECT id, short_code FROM events WHERE uid = ?", uid,
		).Scan(&existingID, &existingShortCode)
	} else {
		const threeHours = int64(3 * 60 * 60)
		lookupErr = q.QueryRow(
			"SELECT id, short_code FROM events WHERE title = ? AND location_id = ? AND ABS(start_time - ?) < ?",
			title, locationID, startTime, threeHours,
		).Scan(&existingID, &existingShortCode)
	}
	if lookupErr != nil && lookupErr != sql.ErrNoRows {
		return 0, "", false, lookupErr
	}

	tagsJSON, _ := json.Marshal(tags)

	if lookupErr == nil {
		// Duplicate — update existing event
		_, err := q.Exec(
			"UPDATE events SET description=?, start_time=?, end_time=?, location_id=?, has_ball=?, has_workshop=?, tags=?, is_published=? WHERE id=?",
			description, startTime, endTime, locationID, hasBall, hasWorkshop, string(tagsJSON), isPublished, existingID,
		)
		if err != nil {
			return 0, "", false, err
		}
		return existingID, existingShortCode, false, nil
	}

	var orgIDArg interface{}
	if organizationID != nil {
		orgIDArg = *organizationID
	}
	var uidArg interface{}
	if uid != "" {
		uidArg = uid
	}
	// short_code is pre-computed so the INSERT is a single round-trip (no follow-up UPDATE).
	// Retry up to 5 times on the rare collision of the 4-byte random short code.
	var result sql.Result
	var err error
	var shortCode string
	for range 5 {
		shortCode = generateShortCode()
		result, err = q.Exec(
			"INSERT INTO events (uid, title, description, start_time, end_time, location_id, has_ball, has_workshop, tags, is_published, organization_id, short_code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			uidArg, title, description, startTime, endTime, locationID, hasBall, hasWorkshop, string(tagsJSON), isPublished, orgIDArg, shortCode,
		)
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "short_code") {
			return 0, "", false, err
		}
	}
	if err != nil {
		return 0, "", false, err
	}
	id, _ := result.LastInsertId()
	return int(id), shortCode, true, nil
}

// createEventFromRequest inserts or updates all events described by req.
// Returns (events, allCreated, error); allCreated=false if any event was updated.
func createEventFromRequest(q querier, req EventCreateRequest, locationID int64, isPublished bool) ([]Event, bool, error) {
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

		id, shortCode, created, err := insertEvent(q, req.Title, entry.description, startTime, endTime, locationID, req.HasBall, req.HasWorkshop, req.Tags, isPublished, req.OrganizationID, req.UID)
		if err != nil {
			return nil, false, err
		}
		if !created {
			allCreated = false
		}

		event, err := fetchEventByID(q, id)
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
	isAuthorizedAdmin := userRole == RoleUser || userRole == RoleAdmin || userRole == RolePublisher

	query := eventListSelect + " WHERE 1=1"
	args := []interface{}{}

	if !isAuthorizedAdmin {
		query += " AND e.is_published = 1"
	} else if v := r.URL.Query().Get("is_published"); v != "" {
		query += " AND e.is_published = ?"
		args = append(args, boolParam(v))
	}

	// Exclude past events by default; authorized users can opt in with include_past=true
	if r.URL.Query().Get("include_past") != "true" {
		query += " AND e.end_time >= ?"
		args = append(args, time.Now().Unix())
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

	callerRole := r.Header.Get("X-User-Role")
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	isPublished := callerRole == RoleUser || callerRole == RoleAdmin

	contentType := r.Header.Get("Content-Type")
	var requests []EventCreateRequest
	var vevents []*ics.VEvent

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
		var icalOrgID *int
		if s := r.URL.Query().Get("organization_id"); s != "" {
			if v, err2 := strconv.Atoi(s); err2 == nil {
				icalOrgID = &v
			}
		}
		for _, event := range cal.Events() {
			var startTime, endTime string
			if p := event.GetProperty(ics.ComponentPropertyDtStart); p != nil {
				startTime, _ = parseICalTime(p.Value)
			}
			if p := event.GetProperty(ics.ComponentPropertyDtEnd); p != nil {
				endTime, _ = parseICalTime(p.Value)
			}
			var uid string
			if p := event.GetProperty(ics.ComponentPropertyUniqueId); p != nil {
				uid = p.Value
			}
			orgID := icalOrgID
			if orgID == nil {
				orgID = ensureOrgFromOrganizer(event)
			}
			requests = append(requests, EventCreateRequest{
				UID:            uid,
				Title:          event.GetProperty(ics.ComponentPropertySummary).Value,
				Description:    event.GetProperty(ics.ComponentPropertyDescription).Value,
				StartTime:      startTime,
				EndTime:        endTime,
				Tags:           parseICalCategories(event),
				OrganizationID: orgID,
				Location: EventLocationRequest{
					Location: event.GetProperty(ics.ComponentPropertyLocation).Value,
				},
			})
			vevents = append(vevents, event)
		}
		if len(requests) == 0 {
			http.Error(w, "No events found in iCal file", http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "Content-Type must be application/json or text/calendar", http.StatusUnsupportedMediaType)
		return
	}

	if callerRole != RoleAdmin {
		checked := make(map[int]bool)
		for _, req := range requests {
			if req.OrganizationID == nil {
				http.Error(w, "organization_id is required", http.StatusBadRequest)
				return
			}
			orgID := *req.OrganizationID
			member, seen := checked[orgID]
			if !seen {
				member = isOrgMember(callerID, orgID)
				checked[orgID] = member
			}
			if !member {
				http.Error(w, "Forbidden: not a member of the specified organization", http.StatusForbidden)
				return
			}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var allCreatedEvents []Event
	allCreated := true
	for i, req := range requests {
		locationID, err := ensureLocation(tx, req.Location)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		createdEvents, created, err := createEventFromRequest(tx, req, locationID, isPublished)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !created {
			allCreated = false
		}
		allCreatedEvents = append(allCreatedEvents, createdEvents...)
		if i < len(vevents) {
			for _, ev := range createdEvents {
				attachImagesFromICalEvent(ev.ID, vevents[i])
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	if userRole != RoleAdmin && userRole != RoleUser {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]

	if userRole != RoleAdmin {
		callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
		var orgID sql.NullInt64
		db.QueryRow("SELECT organization_id FROM events WHERE id = ?", id).Scan(&orgID)
		if !orgID.Valid || !isOrgMember(callerID, int(orgID.Int64)) {
			http.Error(w, "Forbidden: not a member of the event's organization", http.StatusForbidden)
			return
		}
	}

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
	userRole := r.Header.Get("X-User-Role")
	if userRole != RoleAdmin && userRole != RoleUser {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]

	if userRole != RoleAdmin {
		callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
		var orgID sql.NullInt64
		db.QueryRow("SELECT organization_id FROM events WHERE id = ?", id).Scan(&orgID)
		if !orgID.Valid || !isOrgMember(callerID, int(orgID.Int64)) {
			http.Error(w, "Forbidden: not a member of the event's organization", http.StatusForbidden)
			return
		}
	}

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
		// short-code lookup — no cache headers, events can be updated
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

	// Cache fingerprint: total published event count + latest insertion time.
	// Filters are not reflected in the ETag, so clients may over-fetch — never under-fetch.
	if checkPublicCacheHeaders(w, r,
		"SELECT COUNT(*), MAX(created_at) FROM events WHERE is_published = 1") {
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
	now := time.Now().Unix()
	tag := r.URL.Query().Get("tag")
	loc := r.URL.Query().Get("location")

	cntQ := "SELECT COUNT(*), MAX(e.created_at) FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE e.is_published = 1 AND e.start_time >= ?"
	cntArgs := []interface{}{now}
	if tag != "" {
		cntQ += " AND EXISTS (SELECT 1 FROM json_each(e.tags) WHERE value = ?)"
		cntArgs = append(cntArgs, tag)
	}
	if loc != "" {
		cntQ += " AND l.location LIKE ?"
		cntArgs = append(cntArgs, "%"+loc+"%")
	}
	if checkPublicCacheHeaders(w, r, cntQ, cntArgs...) {
		return
	}

	query := eventListSelect + " WHERE e.is_published = 1 AND e.start_time >= ?"
	args := []interface{}{now}

	if tag != "" {
		query += " AND EXISTS (SELECT 1 FROM json_each(e.tags) WHERE value = ?)"
		args = append(args, tag)
	}
	if loc != "" {
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
	now := time.Now().Unix()
	if checkPublicCacheHeaders(w, r,
		"SELECT COUNT(*), MAX(created_at) FROM events WHERE is_published = 1 AND start_time >= ? AND EXISTS (SELECT 1 FROM json_each(tags) WHERE value = ?)",
		now, tag) {
		return
	}
	query := eventListSelect + " WHERE e.is_published = 1 AND e.start_time >= ? AND EXISTS (SELECT 1 FROM json_each(e.tags) WHERE value = ?) ORDER BY e.start_time ASC"
	rows, err := db.Query(query, now, tag)
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
	now := time.Now().Unix()
	if checkPublicCacheHeaders(w, r,
		"SELECT COUNT(*), MAX(e.created_at) FROM events e LEFT JOIN locations l ON e.location_id = l.id WHERE e.is_published = 1 AND e.start_time >= ? AND l.town LIKE ?",
		now, "%"+town+"%") {
		return
	}
	query := eventListSelect + " WHERE e.is_published = 1 AND e.start_time >= ? AND l.town LIKE ? ORDER BY e.start_time ASC"
	rows, err := db.Query(query, now, "%"+town+"%")
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

// checkPublicCacheHeaders runs cntQuery (must SELECT COUNT(*), MAX(created_at))
// and emits ETag/Last-Modified/Cache-Control headers. Returns true and writes
// 304 when the client's cached copy is still fresh; caller must return immediately.
func checkPublicCacheHeaders(w http.ResponseWriter, r *http.Request, cntQuery string, args ...interface{}) bool {
	var n int
	var modStr sql.NullString
	if err := db.QueryRow(cntQuery, args...).Scan(&n, &modStr); err != nil {
		return false
	}
	var lastMod time.Time
	if modStr.Valid && modStr.String != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, modStr.String); err == nil {
				lastMod = t
				break
			}
		}
	}
	etag := fmt.Sprintf(`"%d-%d"`, n, lastMod.Unix())
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "public, max-age=60")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil && !lastMod.After(t) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

// GET /api/v1/tags
func getTags(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userRole := r.Header.Get("X-User-Role")
	isAuthorizedAdmin := userRole == RoleUser || userRole == RoleAdmin || userRole == RolePublisher

	query := "SELECT DISTINCT j.value FROM events, json_each(events.tags) AS j WHERE 1=1"
	var args []interface{}
	if !isAuthorizedAdmin {
		query += " AND is_published = 1"
	}
	query += " ORDER BY j.value"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tags = append(tags, tag)
	}
	json.NewEncoder(w).Encode(tags)
}
