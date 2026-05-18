package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type TemplateData struct {
	Title        string
	Domain       string
	User         *SessionUser
	Strings      I18nStrings
	LangCode     string
	Languages    []LangOption
	Contact      string
	ImpressumURL string
	Data         interface{}
}

func tmplData(r *http.Request, cfg *Config, i18n *I18n, title string, data interface{}) TemplateData {
	lang := i18n.detectLang(r)
	contact := cfg.pagesContent.ContactText(lang)
	impressumURL := ""
	if cfg.pagesContent.ImpressumText(lang) != "" {
		impressumURL = "/impressum"
	}
	return TemplateData{
		Title:        title,
		Domain:       cfg.Domain,
		User:         getSessionUser(r),
		Strings:      i18n.Strings(lang),
		LangCode:     lang,
		Languages:    i18n.Options(lang),
		Contact:      contact,
		ImpressumURL: impressumURL,
		Data:         data,
	}
}

type IndexData struct {
	Events []Event
	OrgMap map[int]Organization
}

type EventData struct {
	Event   Event
	Org     *Organization
	OrgSlug string
}

type OrgData struct {
	Org    Organization
	Events []Event
	Slug   string
	Handle string
}

//go:embed templates
var templateFS embed.FS

//go:embed static/favicon.svg
var faviconSVG []byte

//go:embed static/logo.svg
var logoSVG []byte

//go:embed static/banner.svg
var bannerSVG []byte

func svgHandler(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	}
}

var locMonths = map[string][12]string{
	"br": {"Gen.", "C'hwev.", "Meur.", "Ebr.", "Mae", "Mezh.", "Gouer.", "Eost", "Gwen.", "Here", "Du", "Kerz."},
	"de": {"Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"},
	"en": {"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	"fr": {"jan.", "fév.", "mar.", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
	"es": {"Ene", "Feb", "Mar", "Abr", "May", "Jun", "Jul", "Ago", "Sep", "Oct", "Nov", "Dic"},
	"it": {"Gen", "Feb", "Mar", "Apr", "Mag", "Giu", "Lug", "Ago", "Set", "Ott", "Nov", "Dic"},
	"nl": {"Jan", "Feb", "Mrt", "Apr", "Mei", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dec"},
}
var locWeekdays = map[string][7]string{
	"br": {"Sul.", "Lun.", "Meur.", "Merc'h.", "Yaou.", "Gwen.", "Sad."},
	"de": {"So.", "Mo.", "Di.", "Mi.", "Do.", "Fr.", "Sa."},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	"fr": {"Dim.", "Lun.", "Mar.", "Mer.", "Jeu.", "Ven.", "Sam."},
	"es": {"Dom", "Lun", "Mar", "Mié", "Jue", "Vie", "Sáb"},
	"it": {"Dom", "Lun", "Mar", "Mer", "Gio", "Ven", "Sab"},
	"nl": {"Zo", "Ma", "Di", "Wo", "Do", "Vr", "Za"},
}

func locMonth(lang string, m time.Month) string {
	if names, ok := locMonths[lang]; ok {
		return names[m-1]
	}
	return locMonths["en"][m-1]
}
func locWeekday(lang string, w time.Weekday) string {
	if names, ok := locWeekdays[lang]; ok {
		return names[w]
	}
	return locWeekdays["en"][w]
}

var parseLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"}

func parseTime(s string) (time.Time, bool) {
	for _, layout := range parseLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var tmplFuncMap = template.FuncMap{
	"formatTime": func(lang, s string) string {
		t, ok := parseTime(s)
		if !ok {
			return s
		}
		wd := locWeekday(lang, t.Weekday())
		mo := locMonth(lang, t.Month())
		if lang == "de" {
			return fmt.Sprintf("%s %02d. %s %d, %02d:%02d", wd, t.Day(), mo, t.Year(), t.Hour(), t.Minute())
		}
		return fmt.Sprintf("%s %02d %s %d, %02d:%02d", wd, t.Day(), mo, t.Year(), t.Hour(), t.Minute())
	},
	"formatDate": func(lang, s string) string {
		t, ok := parseTime(s)
		if !ok {
			return s
		}
		mo := locMonth(lang, t.Month())
		if lang == "de" {
			return fmt.Sprintf("%02d. %s %d", t.Day(), mo, t.Year())
		}
		return fmt.Sprintf("%02d %s %d", t.Day(), mo, t.Year())
	},
	"isoDate": func(s string) string {
		if t, ok := parseTime(s); ok {
			return t.Format("2006-01-02")
		}
		return s
	},
	"isoTime": func(s string) string {
		if t, ok := parseTime(s); ok {
			return t.Format("15:04")
		}
		return ""
	},
	"formatHourMin": func(s string) string {
		if t, ok := parseTime(s); ok {
			return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
		}
		return ""
	},
	"sameDate": func(s1, s2 string) bool {
		t1, ok1 := parseTime(s1)
		t2, ok2 := parseTime(s2)
		if !ok1 || !ok2 {
			return false
		}
		return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day()
	},
	"join": func(ss []string) string {
		return strings.Join(ss, ", ")
	},
	"jsStr": func(s string) template.JS {
		b, _ := json.Marshal(s)
		return template.JS(b)
	},
	"derefInt": func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	},
	// mastodonURL converts "@user@instance.tld" → "https://instance.tld/@user".
	// If the value already starts with "http", it is returned unchanged.
	"mastodonURL": func(handle string) string {
		if strings.HasPrefix(handle, "http") {
			return handle
		}
		// strip leading @
		h := strings.TrimPrefix(handle, "@")
		parts := strings.SplitN(h, "@", 2)
		if len(parts) == 2 {
			return "https://" + parts[1] + "/@" + parts[0]
		}
		return handle
	},
	"eventsGeoJSON": func(events []Event) template.JS {
		type geoEvent struct {
			ID        int     `json:"id"`
			Title     string  `json:"t"`
			Start     string  `json:"s"`
			Location  string  `json:"loc,omitempty"`
			Town      string  `json:"town,omitempty"`
			Country   string  `json:"c,omitempty"`
			Lat       float64 `json:"lat"`
			Lng       float64 `json:"lng"`
			URL       string  `json:"url,omitempty"`
			Ball      bool    `json:"ball,omitempty"`
			Workshop  bool    `json:"ws,omitempty"`
			Cancelled bool    `json:"x,omitempty"`
		}
		var geo []geoEvent
		for _, e := range events {
			lat, errLat := strconv.ParseFloat(e.LocationLat, 64)
			lng, errLng := strconv.ParseFloat(e.LocationLng, 64)
			if errLat != nil || errLng != nil || (lat == 0 && lng == 0) {
				continue
			}
			geo = append(geo, geoEvent{
				ID: e.ID, Title: e.Title, Start: e.StartTime,
				Location: e.Location, Town: e.LocationTown, Country: e.LocationCountry,
				Lat: lat, Lng: lng, URL: e.URL,
				Ball: e.HasBall, Workshop: e.HasWorkshop, Cancelled: e.IsCancelled,
			})
		}
		if geo == nil {
			return template.JS("[]")
		}
		b, _ := json.Marshal(geo)
		return template.JS(b)
	},
	"orgName": func(orgMap map[int]Organization, id *int) string {
		if id == nil {
			return ""
		}
		if o, ok := orgMap[*id]; ok {
			return o.Name
		}
		return ""
	},
	"orgSlug": orgSlug,
	"countryList": func(events []Event) []string {
		seen := make(map[string]bool)
		var out []string
		for _, e := range events {
			if e.LocationCountry != "" && !seen[e.LocationCountry] {
				seen[e.LocationCountry] = true
				out = append(out, e.LocationCountry)
			}
		}
		sort.Strings(out)
		return out
	},
}

type Templates struct {
	index              *template.Template
	event              *template.Template
	org                *template.Template
	login              *template.Template
	settings           *template.Template
	verify             *template.Template
	adminOrgs          *template.Template
	adminOrgEdit       *template.Template
	adminFetchurls     *template.Template
	adminFetchurlNew   *template.Template
	adminFetchurlEdit  *template.Template
	adminLocations     *template.Template
	adminLocationEdit  *template.Template
	musicians          *template.Template
	musician           *template.Template
	adminMusicians     *template.Template
	adminMusicianEdit  *template.Template
	adminEvents        *template.Template
	adminEventNew      *template.Template
	adminEventEdit     *template.Template
	impressum          *template.Template
}

func loadTemplates() *Templates {
	load := func(page string) *template.Template {
		t, err := template.New("base").Funcs(tmplFuncMap).ParseFS(templateFS,
			"templates/base.html", "templates/"+page+".html")
		if err != nil {
			log.Fatalf("load template %s: %v", page, err)
		}
		return t
	}
	return &Templates{
		index:             load("index"),
		event:             load("event"),
		org:               load("org"),
		login:             load("login"),
		settings:          load("settings"),
		verify:            load("verify"),
		adminOrgs:         load("admin_orgs"),
		adminOrgEdit:      load("admin_org_edit"),
		adminFetchurls:    load("admin_fetchurls"),
		adminFetchurlNew:  load("admin_fetchurl_new"),
		adminFetchurlEdit: load("admin_fetchurl_edit"),
		adminLocations:    load("admin_locations"),
		adminLocationEdit: load("admin_location_edit"),
		musicians:         load("musicians"),
		musician:          load("musician"),
		adminMusicians:    load("admin_musicians"),
		adminMusicianEdit: load("admin_musician_edit"),
		adminEvents:       load("admin_events"),
		adminEventNew:     load("admin_event_new"),
		adminEventEdit:    load("admin_event_edit"),
		impressum:         load("impressum"),
	}
}

func renderTemplate(w http.ResponseWriter, tmpl *template.Template, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func indexHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := client.GetEvents(r.Context(), "")
		if err != nil {
			http.Error(w, "could not load events", http.StatusBadGateway)
			return
		}
		orgMap := make(map[int]Organization)
		if orgs, err := client.GetOrganizations(r.Context()); err == nil {
			for _, o := range orgs {
				orgMap[o.ID] = o
			}
		}
		title := i18n.T(r, "events_title")
		renderTemplate(w, tmpls.index, tmplData(r, cfg, i18n, title, IndexData{Events: events, OrgMap: orgMap}))
	}
}

func eventHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := mux.Vars(r)["id"]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		event, err := client.GetEvent(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		var org *Organization
		var slug string
		if event.OrganizationID != nil {
			o, err := client.GetOrganization(r.Context(), *event.OrganizationID)
			if err == nil {
				org = &o
				slug = orgSlug(o.Name)
			}
		}

		renderTemplate(w, tmpls.event, tmplData(r, cfg, i18n, event.Title, EventData{Event: event, Org: org, OrgSlug: slug}))
	}
}

func orgFrontendHandler(cfg *Config, tmpls *Templates, db *sql.DB, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["name"]

		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			orgs, oErr := client.GetOrganizations(r.Context())
			if oErr != nil {
				http.Error(w, "upstream error", http.StatusBadGateway)
				return
			}
			for _, o := range orgs {
				if orgSlug(o.Name) == slug {
					actor, err = ensureActor(db, o.ID, slug)
					break
				}
			}
			if actor == nil {
				http.NotFound(w, r)
				return
			}
		} else if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		org, err := client.GetOrganization(r.Context(), actor.OrgID)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		events, err := client.GetEventsByOrg(r.Context(), actor.OrgID)
		if err != nil {
			events = nil
		}

		handle := "@" + slug + "@" + cfg.Domain
		renderTemplate(w, tmpls.org, tmplData(r, cfg, i18n, org.Name, OrgData{
			Org:    org,
			Events: events,
			Slug:   slug,
			Handle: handle,
		}))
	}
}

func actorOrFrontendHandler(cfg *Config, tmpls *Templates, db *sql.DB, client *DansalClient, i18n *I18n) http.HandlerFunc {
	frontendH := orgFrontendHandler(cfg, tmpls, db, client, i18n)
	apH := apActorHandler(cfg, db, client)
	return func(w http.ResponseWriter, r *http.Request) {
		if isAPRequest(r) {
			apH(w, r)
		} else {
			frontendH(w, r)
		}
	}
}

func apActorHandler(cfg *Config, db *sql.DB, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["name"]
		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			orgs, err := client.GetOrganizations(r.Context())
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "upstream error")
				return
			}
			for _, org := range orgs {
				if orgSlug(org.Name) == slug {
					actor, err = ensureActor(db, org.ID, slug)
					if err != nil {
						writeJSONError(w, http.StatusInternalServerError, "actor init error")
						return
					}
					break
				}
			}
			if actor == nil {
				writeJSONError(w, http.StatusNotFound, "actor not found")
				return
			}
		} else if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		org, err := client.GetOrganization(r.Context(), actor.OrgID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "org not found")
			return
		}

		a := actorFromOrg(cfg, org, actor)
		writeJSON(w, http.StatusOK, a)
	}
}

func impressumHandler(cfg *Config, tmpls *Templates, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pc := loadPagesContent(cfg.PagesFile)
		lang := i18n.detectLang(r)
		body := pc.ImpressumHTML(lang)
		if body == "" {
			http.NotFound(w, r)
			return
		}
		title := i18n.T(r, "nav_impressum")
		renderTemplate(w, tmpls.impressum, tmplData(r, cfg, i18n, title, body))
	}
}
