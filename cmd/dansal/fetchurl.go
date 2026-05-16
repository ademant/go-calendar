package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/gorilla/mux"
)

type FetchURLRequest struct {
	URL  string   `json:"url"`
	Type string   `json:"type"`
	Tags []string `json:"tags"`
}

type FetchSource struct {
	ID            int      `json:"id"`
	URL           string   `json:"url"`
	Type          string   `json:"type"`
	Tags          []string `json:"tags"`
	LastFetchedAt string   `json:"last_fetched_at,omitempty"`
	CreatedAt     string   `json:"created_at"`
}

var fetchClient = &http.Client{Timeout: 30 * time.Second}

// ensureLocation returns the id of an existing location matching loc.Location,
// or inserts a new one. Returns 0 with no error when loc.Location is empty.
func ensureLocation(q querier, loc EventLocationRequest) (int64, error) {
	if loc.Location == "" {
		return 0, nil
	}
	var id int64
	err := q.QueryRow("SELECT id FROM locations WHERE location = ?", loc.Location).Scan(&id)
	if err == nil {
		if loc.Latitude != "" || loc.Longitude != "" {
			q.Exec("UPDATE locations SET latitude=?, longitude=? WHERE id=? AND latitude IS NULL AND longitude IS NULL",
				loc.Latitude, loc.Longitude, id)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	result, err := q.Exec(
		"INSERT INTO locations (location, address, zipcode, town, latitude, longitude, internetsite) VALUES (?, ?, ?, ?, ?, ?, ?)",
		loc.Location, loc.Address, loc.Zipcode, loc.Town, loc.Latitude, loc.Longitude, loc.Eventsite,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// parseICalGeo splits a GEO property value ("lat;lon") into its components.
func parseICalGeo(s string) (lat, lon string) {
	if i := strings.IndexByte(s, ';'); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "", ""
}

// parseICalDuration parses an RFC 5545 DURATION value (e.g. "PT1H30M", "P1D")
// into a time.Duration.
func parseICalDuration(s string) (time.Duration, error) {
	orig := s
	neg := false
	s = strings.TrimPrefix(s, "+")
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") {
		return 0, fmt.Errorf("invalid DURATION %q", orig)
	}
	s = s[1:]

	var total time.Duration
	for len(s) > 0 {
		if s[0] == 'T' {
			s = s[1:]
			continue
		}
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i >= len(s) {
			return 0, fmt.Errorf("invalid DURATION %q", orig)
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, err
		}
		unit := s[i]
		s = s[i+1:]
		switch unit {
		case 'W':
			total += time.Duration(n) * 7 * 24 * time.Hour
		case 'D':
			total += time.Duration(n) * 24 * time.Hour
		case 'H':
			total += time.Duration(n) * time.Hour
		case 'M':
			total += time.Duration(n) * time.Minute
		case 'S':
			total += time.Duration(n) * time.Second
		default:
			return 0, fmt.Errorf("unknown DURATION unit %q in %q", string(unit), orig)
		}
	}
	if neg {
		total = -total
	}
	return total, nil
}

// parseICalCategories extracts all CATEGORIES values from a vevent,
// splitting comma-separated entries and deduplicating.
func parseICalCategories(event *ics.VEvent) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, prop := range event.GetProperties(ics.ComponentPropertyCategories) {
		for _, cat := range strings.Split(prop.Value, ",") {
			cat = strings.TrimSpace(cat)
			if cat != "" && !seen[cat] {
				seen[cat] = true
				tags = append(tags, cat)
			}
		}
	}
	return tags
}


func scanFetchSource(s scanner) (FetchSource, error) {
	var src FetchSource
	var tagsJSON string
	var lastFetched sql.NullString
	if err := s.Scan(&src.ID, &src.URL, &src.Type, &tagsJSON, &lastFetched, &src.CreatedAt); err != nil {
		return FetchSource{}, err
	}
	if tagsJSON != "" {
		json.Unmarshal([]byte(tagsJSON), &src.Tags)
	}
	if lastFetched.Valid {
		src.LastFetchedAt = lastFetched.String
	}
	return src, nil
}

// upsertFetchSource inserts or updates a fetch source by URL, returning its id.
func upsertFetchSource(rawURL, typ string, tags []string) (int64, error) {
	tagsJSON, _ := json.Marshal(tags)
	var id int64
	err := db.QueryRow("SELECT id FROM fetch_sources WHERE url = ?", rawURL).Scan(&id)
	if err == sql.ErrNoRows {
		result, err := db.Exec(
			"INSERT INTO fetch_sources (url, type, tags) VALUES (?, ?, ?)",
			rawURL, typ, string(tagsJSON),
		)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	_, err = db.Exec("UPDATE fetch_sources SET type = ?, tags = ? WHERE id = ?", typ, string(tagsJSON), id)
	return id, err
}

// GET /api/v1/fetchurl
func getFetchSources(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, url, type, tags, last_fetched_at, created_at FROM fetch_sources ORDER BY id ASC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sources []FetchSource
	for rows.Next() {
		src, err := scanFetchSource(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sources = append(sources, src)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources)
}

// GET /api/v1/fetchurl/{id}
func getFetchSource(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	src, err := scanFetchSource(db.QueryRow(
		"SELECT id, url, type, tags, last_fetched_at, created_at FROM fetch_sources WHERE id = ?", id,
	))
	if err == sql.ErrNoRows {
		http.Error(w, "Fetch source not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(src)
}

// importFromSource dispatches to the correct importer based on src.Type.
func importFromSource(src FetchSource) ([]Event, bool, error) {
	if src.Type == "folkdance-json" {
		return importFromFolkdanceJSON(src)
	}
	return importFromICalSource(src)
}

// importFromICalSource fetches an iCal URL and imports its events into the DB.
func importFromICalSource(src FetchSource) ([]Event, bool, error) {
	resp, err := fetchClient.Get(src.URL)
	if err != nil {
		return nil, false, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("remote returned %d", resp.StatusCode)
	}

	cal, err := ics.ParseCalendar(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("parse iCal: %w", err)
	}

	db.Exec("UPDATE fetch_sources SET last_fetched_at = CURRENT_TIMESTAMP WHERE id = ?", src.ID)

	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var allEvents []Event
	allCreated := true

	for _, vevent := range cal.Events() {
		prop := func(p ics.ComponentProperty) string {
			if v := vevent.GetProperty(p); v != nil {
				return v.Value
			}
			return ""
		}

		startT, err := vevent.GetStartAt()
		if err != nil {
			continue
		}
		endT := startT
		if et, err := vevent.GetEndAt(); err == nil {
			endT = et
		} else if durStr := prop(ics.ComponentPropertyDuration); durStr != "" {
			if d, err := parseICalDuration(durStr); err == nil {
				endT = startT.Add(d)
			}
		}

		title := prop(ics.ComponentPropertySummary)
		if title == "" {
			continue
		}

		tags := parseICalCategories(vevent)
		seen := make(map[string]bool)
		for _, t := range tags {
			seen[t] = true
		}
		for _, t := range src.Tags {
			if !seen[t] {
				tags = append(tags, t)
			}
		}

		baseUID := prop(ics.ComponentPropertyUniqueId)
		sourceLastModified := icalLastModified(vevent)

		// Expand RRULE if present; fall back to the single base occurrence.
		occs, _ := expandRRuleOccurrences(vevent, startT, endT)
		if occs == nil {
			occs = [][2]time.Time{{startT, endT}}
		}

		for _, occ := range occs {
			// Recurring occurrences after the base get a timestamp-qualified UID
			// so each instance deduplicates independently across re-imports.
			uid := baseUID
			if len(occs) > 1 && !occ[0].Equal(startT) {
				uid = fmt.Sprintf("%s_%d", baseUID, occ[0].UTC().Unix())
			}

			eventReq := EventCreateRequest{
				UID:                uid,
				Title:              title,
				Description:        prop(ics.ComponentPropertyDescription),
				StartTime:          occ[0].UTC().Format(time.RFC3339),
				EndTime:            occ[1].UTC().Format(time.RFC3339),
				IsCancelled:        prop(ics.ComponentPropertyStatus) == "CANCELLED",
				Tags:               tags,
				URL:                attachURL(vevent),
				Source:             src.URL,
				OrganizationID:     ensureOrgFromOrganizer(vevent),
				Location: func() EventLocationRequest {
				lat, lon := parseICalGeo(prop(ics.ComponentPropertyGeo))
				return EventLocationRequest{
					Location:  prop(ics.ComponentPropertyLocation),
					Latitude:  lat,
					Longitude: lon,
				}
			}(),
				SourceLastModified: sourceLastModified,
			}

			locationID, err := ensureLocation(tx, eventReq.Location)
			if err != nil {
				return nil, false, err
			}

			events, created, err := createEventFromRequest(tx, eventReq, locationID, true)
			if err != nil {
				return nil, false, err
			}
			if !created {
				allCreated = false
			}
			for _, ev := range events {
				attachImagesFromICalEvent(ev.ID, vevent)
			}
			allEvents = append(allEvents, events...)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return allEvents, allCreated, nil
}

// POST /api/v1/fetchurl
func fetchURL(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	if userRole != RoleAdmin && userRole != RoleUser && userRole != RolePublisher {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req FetchURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if !strings.Contains(req.URL, "://") {
		req.URL = "https://" + req.URL
	}
	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "URL must use http or https scheme", http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		req.Type = detectFetchType(req.URL)
	}
	if !validFetchType(req.Type) {
		http.Error(w, "Unsupported type; use 'ical' or 'folkdance-json'", http.StatusBadRequest)
		return
	}

	sourceID, err := upsertFetchSource(req.URL, req.Type, req.Tags)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	src := FetchSource{ID: int(sourceID), URL: req.URL, Type: req.Type, Tags: req.Tags}
	allEvents, allCreated, err := importFromSource(src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if allCreated && len(allEvents) > 0 {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(allEvents)
}

// POST /api/v1/fetchurl/fetch-all
func fetchAllURLs(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	if userRole != RoleAdmin && userRole != RoleUser && userRole != RolePublisher {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	rows, err := db.Query("SELECT id, url, type, tags, last_fetched_at, created_at FROM fetch_sources")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sources []FetchSource
	for rows.Next() {
		src, err := scanFetchSource(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sources = append(sources, src)
	}

	type sourceResult struct {
		SourceID   int    `json:"source_id"`
		URL        string `json:"url"`
		Events     int    `json:"events"`
		AllCreated bool   `json:"all_created"`
		Error      string `json:"error,omitempty"`
	}

	// Fan out: fetch all sources in parallel. Each goroutine writes to its own
	// index so no mutex is needed for the slice itself.
	results := make([]sourceResult, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src FetchSource) {
			defer wg.Done()
			events, allCreated, err := importFromSource(src)
			if err != nil {
				results[i] = sourceResult{SourceID: src.ID, URL: src.URL, Error: err.Error()}
				return
			}
			results[i] = sourceResult{SourceID: src.ID, URL: src.URL, Events: len(events), AllCreated: allCreated}
		}(i, src)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// POST /api/v1/fetchurl/{id}/fetch
func fetchURLByID(w http.ResponseWriter, r *http.Request) {
	userRole := r.Header.Get("X-User-Role")
	if userRole != RoleAdmin && userRole != RoleUser && userRole != RolePublisher {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]
	src, err := scanFetchSource(db.QueryRow(
		"SELECT id, url, type, tags, last_fetched_at, created_at FROM fetch_sources WHERE id = ?", id,
	))
	if err == sql.ErrNoRows {
		http.Error(w, "Fetch source not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !validFetchType(src.Type) {
		http.Error(w, "Unsupported type; use 'ical' or 'folkdance-json'", http.StatusBadRequest)
		return
	}

	allEvents, allCreated, err := importFromSource(src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if allCreated && len(allEvents) > 0 {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(allEvents)
}
