package main

import (
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

type TemplateData struct {
	Title     string
	Domain    string
	User      *SessionUser
	Strings   I18nStrings
	LangCode  string
	Languages []LangOption
	Data      interface{}
}

func tmplData(r *http.Request, cfg *Config, i18n *I18n, title string, data interface{}) TemplateData {
	lang := i18n.detectLang(r)
	return TemplateData{
		Title:     title,
		Domain:    cfg.Domain,
		User:      getSessionUser(r),
		Strings:   i18n.Strings(lang),
		LangCode:  lang,
		Languages: i18n.Options(lang),
		Data:      data,
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

var tmplFuncMap = template.FuncMap{
	"formatTime": func(s string) string {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.Format("Mon 02 Jan 2006, 15:04")
			}
		}
		return s
	},
	"formatDate": func(s string) string {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.Format("02 Jan 2006")
			}
		}
		return s
	},
	"isoDate": func(s string) string {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.Format("2006-01-02")
			}
		}
		return s
	},
	"derefInt": func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
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
	adminFetchurlEdit  *template.Template
	adminLocations     *template.Template
	adminLocationEdit  *template.Template
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
		adminFetchurlEdit: load("admin_fetchurl_edit"),
		adminLocations:    load("admin_locations"),
		adminLocationEdit: load("admin_location_edit"),
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
			http.NotFound(w, r)
			return
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
