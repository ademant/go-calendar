package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type folkdanceEvent struct {
	Name         string   `json:"name"`
	Links        []string `json:"links"`
	Start        string   `json:"start"`      // RFC3339 with offset, e.g. "2026-05-13T20:00:00+02:00"
	End          string   `json:"end"`         // RFC3339, optional
	StartDate    string   `json:"start_date"`  // date-only fallback, e.g. "2026-05-22"
	EndDate      string   `json:"end_date"`    // date-only fallback
	Country      string   `json:"country"`
	City         string   `json:"city"`
	State        string   `json:"state"` // AU/US region
	Styles       []string `json:"styles"`
	Workshop     bool     `json:"workshop"`
	Social       bool     `json:"social"`
	Bands        []string `json:"bands"`
	Callers      []string `json:"callers"`
	Organisation string   `json:"organisation"`
	Price        string   `json:"price"`
	Details      string   `json:"details"`
	Cancelled    bool     `json:"cancelled"`
	Source       string   `json:"source"` // internal YAML path, ignored
}

func parseFolkdanceTime(datetime, date string) (string, error) {
	if datetime != "" {
		t, err := time.Parse(time.RFC3339, datetime)
		if err != nil {
			return "", err
		}
		return t.UTC().Format(time.RFC3339), nil
	}
	if date != "" {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			return "", err
		}
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("no time value")
}

func folkdanceLocationString(city, state, country string) string {
	parts := []string{city}
	if state != "" {
		parts = append(parts, state)
	}
	if country != "" {
		parts = append(parts, country)
	}
	return strings.Join(parts, ", ")
}

// ensureMusician returns the id of an existing musician matching bandname,
// or inserts a new one with just the bandname set.
func ensureMusician(q querier, bandname string) (int64, error) {
	if bandname == "" {
		return 0, nil
	}
	var id int64
	err := q.QueryRow("SELECT id FROM musicians WHERE bandname = ?", bandname).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	result, err := q.Exec("INSERT INTO musicians (bandname) VALUES (?)", bandname)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func importFromFolkdanceJSON(src FetchSource) ([]Event, bool, error) {
	resp, err := fetchClient.Get(src.URL)
	if err != nil {
		return nil, false, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("remote returned %d", resp.StatusCode)
	}

	var payload struct {
		Events []folkdanceEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("parse JSON: %w", err)
	}

	db.Exec("UPDATE fetch_sources SET last_fetched_at = CURRENT_TIMESTAMP WHERE id = ?", src.ID)

	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var allEvents []Event
	allCreated := true

	for _, fe := range payload.Events {
		if fe.Name == "" {
			continue
		}

		startTime, err := parseFolkdanceTime(fe.Start, fe.StartDate)
		if err != nil {
			continue
		}

		// For date-only end values use end-of-day so multi-day events have a
		// sensible duration even when start and end fall on the same calendar date.
		endTime := startTime
		if fe.End != "" {
			endTime, _ = parseFolkdanceTime(fe.End, "")
		} else if fe.EndDate != "" {
			if t, err := time.Parse("2006-01-02", fe.EndDate); err == nil {
				endTime = t.Add(24*time.Hour - time.Second).UTC().Format(time.RFC3339)
			}
		}

		// Merge feed styles with any tags configured on the source.
		tags := make([]string, 0, len(fe.Styles)+len(src.Tags))
		seen := make(map[string]bool)
		for _, s := range fe.Styles {
			if s != "" && !seen[s] {
				seen[s] = true
				tags = append(tags, s)
			}
		}
		for _, s := range src.Tags {
			if s != "" && !seen[s] {
				seen[s] = true
				tags = append(tags, s)
			}
		}

		var eventURL string
		if len(fe.Links) > 0 {
			eventURL = fe.Links[0]
		}

		locStr := folkdanceLocationString(fe.City, fe.State, fe.Country)

		var musicianIDs []int
		for _, band := range fe.Bands {
			id, err := ensureMusician(tx, band)
			if err != nil {
				return nil, false, fmt.Errorf("ensureMusician %q: %w", band, err)
			}
			if id > 0 {
				musicianIDs = append(musicianIDs, int(id))
			}
		}

		eventReq := EventCreateRequest{
			Title:          fe.Name,
			StartTime:      startTime,
			EndTime:        endTime,
			HasBall:        fe.Social,
			HasWorkshop:    fe.Workshop,
			IsCancelled:    fe.Cancelled,
			Tags:           tags,
			URL:            eventURL,
			Source:         src.URL,
			OrganizationID: ensureOrgByName(fe.Organisation),
			Musicians:      musicianIDs,
			Location: EventLocationRequest{
				Location: locStr,
				Town:     fe.City,
				Country:  fe.Country,
			},
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
		allEvents = append(allEvents, events...)
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return allEvents, allCreated, nil
}

// folkdanceJSONProbe returns true when the URL looks like a folkdance.page JSON feed.
func folkdanceJSONProbe(rawURL string) bool {
	return strings.Contains(rawURL, "folkdance.page") && strings.Contains(rawURL, ".json")
}

// httpContentType fetches only the Content-Type header of a URL.
func httpContentType(rawURL string) string {
	resp, err := fetchClient.Head(rawURL)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(ct)
}

// detectFetchType infers the fetch type from the URL and a HEAD request when
// the caller has not supplied an explicit type.
func detectFetchType(rawURL string) string {
	if folkdanceJSONProbe(rawURL) {
		return "folkdance-json"
	}
	ct := httpContentType(rawURL)
	switch ct {
	case "application/json":
		return "folkdance-json"
	default:
		return "ical"
	}
}

// validFetchType returns true for recognised fetch type strings.
func validFetchType(t string) bool {
	return t == "ical" || t == "folkdance-json"
}
