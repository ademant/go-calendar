package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/gorilla/mux"
)

// feedMainHandler serves all upcoming events.
func feedMainHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := client.GetEvents(r.Context(), "")
		if err != nil {
			http.Error(w, "could not load events", http.StatusBadGateway)
			return
		}
		serveEventFeed(w, r, cfg, cfg.Domain+" events", events)
	}
}

// feedOrgHandler serves events for one organisation, identified by its AP slug.
func feedOrgHandler(cfg *Config, db *sql.DB, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["slug"]
		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		org, err := client.GetOrganization(r.Context(), actor.OrgID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		events, _ := client.GetEventsByOrg(r.Context(), actor.OrgID)
		serveEventFeed(w, r, cfg, org.Name, events)
	}
}

// feedMusicianHandler serves events for one musician, identified by slug.
func feedMusicianHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["slug"]
		musicians, err := client.GetMusicians(r.Context())
		if err != nil {
			http.Error(w, "could not load musicians", http.StatusBadGateway)
			return
		}
		var found *Musician
		for i := range musicians {
			if orgSlug(musicians[i].Bandname) == slug {
				found = &musicians[i]
				break
			}
		}
		if found == nil {
			http.NotFound(w, r)
			return
		}
		events, _ := client.GetPublicEventsByMusician(r.Context(), found.ID)
		serveEventFeed(w, r, cfg, found.Bandname, events)
	}
}

// feedLocationHandler serves events at one location, identified by slug.
func feedLocationHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["slug"]
		locs, err := client.GetLocations(r.Context())
		if err != nil {
			http.Error(w, "could not load locations", http.StatusBadGateway)
			return
		}
		var found *Location
		for i := range locs {
			if orgSlug(locs[i].Location) == slug {
				found = &locs[i]
				break
			}
		}
		if found == nil {
			http.NotFound(w, r)
			return
		}
		all, _ := client.GetEvents(r.Context(), "")
		var events []Event
		for _, e := range all {
			if e.Location == found.Location {
				events = append(events, e)
			}
		}
		label := found.Location
		if found.Town != "" {
			label += ", " + found.Town
		}
		serveEventFeed(w, r, cfg, label, events)
	}
}

// feedTypeHandler serves events filtered by has_ball / has_workshop / neither.
func feedTypeHandler(cfg *Config, client *DansalClient, feedType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, _ := client.GetEvents(r.Context(), "")
		var events []Event
		for _, e := range all {
			switch feedType {
			case "ball":
				if e.HasBall {
					events = append(events, e)
				}
			case "workshop":
				if e.HasWorkshop {
					events = append(events, e)
				}
			case "festival":
				if !e.HasBall && !e.HasWorkshop {
					events = append(events, e)
				}
			}
		}
		serveEventFeed(w, r, cfg, feedType+" events", events)
	}
}

// serveEventFeed dispatches to the right format renderer based on the {format} route variable.
func serveEventFeed(w http.ResponseWriter, r *http.Request, cfg *Config, title string, events []Event) {
	if events == nil {
		events = []Event{}
	}
	selfURL := "https://" + cfg.Domain + r.URL.Path
	switch mux.Vars(r)["format"] {
	case "ical", "ics":
		serveICalFeed(w, cfg, events)
	case "json":
		serveJSONFeed(w, events)
	case "rss":
		serveRSSFeed(w, cfg, title, selfURL, events)
	default:
		http.NotFound(w, r)
	}
}

// serveICalFeed writes a text/calendar (iCal) response.
func serveICalFeed(w http.ResponseWriter, cfg *Config, events []Event) {
	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	cal.SetName(cfg.Domain)
	for _, e := range events {
		feedAddEventToCalendar(cal, cfg.Domain, e)
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="events.ics"`)
	w.Write([]byte(cal.Serialize()))
}

func feedAddEventToCalendar(cal *ics.Calendar, domain string, e Event) {
	vevent := cal.AddEvent(fmt.Sprintf("event-%d@%s", e.ID, domain))
	vevent.SetSummary(e.Title)
	if e.Description != "" {
		vevent.SetDescription(e.Description)
	}
	if t, err := time.Parse(time.RFC3339, e.StartTime); err == nil {
		vevent.SetProperty(ics.ComponentPropertyDtStart, t.UTC().Format("20060102T150405Z"))
	}
	if t, err := time.Parse(time.RFC3339, e.EndTime); err == nil {
		vevent.SetProperty(ics.ComponentPropertyDtEnd, t.UTC().Format("20060102T150405Z"))
	}
	loc := e.Location
	if loc == "" {
		loc = e.LocationTown
	}
	if loc != "" {
		vevent.SetLocation(loc)
	}
	if e.URL != "" {
		vevent.SetProperty(ics.ComponentPropertyUrl, e.URL)
	}
	if len(e.Tags) > 0 {
		vevent.SetProperty(ics.ComponentPropertyCategories, strings.Join(e.Tags, ","))
	}
}

// serveJSONFeed writes events as an application/json array.
func serveJSONFeed(w http.ResponseWriter, events []Event) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(events)
}

// RSS 2.0 output types (local to dansal_web, no conflict with dansal's input types).
type feedRSSRoot struct {
	XMLName xml.Name       `xml:"rss"`
	Version string         `xml:"version,attr"`
	Channel feedRSSChannel `xml:"channel"`
}

type feedRSSChannel struct {
	Title       string        `xml:"title"`
	Link        string        `xml:"link"`
	Description string        `xml:"description"`
	Items       []feedRSSItem `xml:"item"`
}

type feedRSSItem struct {
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	GUID       string   `xml:"guid"`
	PubDate    string   `xml:"pubDate"`
	Desc       string   `xml:"description,omitempty"`
	EventStart string   `xml:"eventStart,omitempty"`
	EventEnd   string   `xml:"eventEnd,omitempty"`
	Location   string   `xml:"location,omitempty"`
	Categories []string `xml:"category"`
}

// serveRSSFeed writes an RSS 2.0 feed with eventStart/eventEnd extension elements.
func serveRSSFeed(w http.ResponseWriter, cfg *Config, title, selfURL string, events []Event) {
	items := make([]feedRSSItem, 0, len(events))
	for _, e := range events {
		link := e.URL
		if link == "" {
			link = fmt.Sprintf("https://%s/events/%d", cfg.Domain, e.ID)
		}
		var pubDate string
		if t, err := time.Parse(time.RFC3339, e.StartTime); err == nil {
			pubDate = t.UTC().Format(time.RFC1123Z)
		}
		loc := e.Location
		if loc == "" {
			loc = e.LocationTown
		}
		items = append(items, feedRSSItem{
			Title:      e.Title,
			Link:       link,
			GUID:       fmt.Sprintf("https://%s/events/%d", cfg.Domain, e.ID),
			PubDate:    pubDate,
			Desc:       e.Description,
			EventStart: e.StartTime,
			EventEnd:   e.EndTime,
			Location:   loc,
			Categories: e.Tags,
		})
	}

	root := feedRSSRoot{
		Version: "2.0",
		Channel: feedRSSChannel{
			Title:       title,
			Link:        selfURL,
			Description: title,
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(root)
}

// feedURL builds the canonical feed URL for a given path and format extension.
func feedURL(cfg *Config, path, format string) string {
	return "https://" + cfg.Domain + path + "/events." + format
}
