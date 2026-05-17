package main

import (
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

type TemplateData struct {
	Title  string
	Domain string
	Data   interface{}
}

type IndexData struct {
	Events []Event
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
	"derefInt": func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	},
}

type Templates struct {
	index *template.Template
	event *template.Template
	org   *template.Template
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
		index: load("index"),
		event: load("event"),
		org:   load("org"),
	}
}

func renderTemplate(w http.ResponseWriter, tmpl *template.Template, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func indexHandler(cfg *Config, tmpls *Templates, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := client.GetEvents(r.Context(), "")
		if err != nil {
			http.Error(w, "could not load events", http.StatusBadGateway)
			return
		}
		renderTemplate(w, tmpls.index, TemplateData{
			Title:  "Upcoming Events",
			Domain: cfg.Domain,
			Data:   IndexData{Events: events},
		})
	}
}

func eventHandler(cfg *Config, tmpls *Templates, client *DansalClient) http.HandlerFunc {
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

		renderTemplate(w, tmpls.event, TemplateData{
			Title:  event.Title,
			Domain: cfg.Domain,
			Data:   EventData{Event: event, Org: org, OrgSlug: slug},
		})
	}
}

func orgFrontendHandler(cfg *Config, tmpls *Templates, db *sql.DB, client *DansalClient) http.HandlerFunc {
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
		renderTemplate(w, tmpls.org, TemplateData{
			Title:  org.Name,
			Domain: cfg.Domain,
			Data: OrgData{
				Org:    org,
				Events: events,
				Slug:   slug,
				Handle: handle,
			},
		})
	}
}

func actorOrFrontendHandler(cfg *Config, tmpls *Templates, db *sql.DB, client *DansalClient) http.HandlerFunc {
	frontendH := orgFrontendHandler(cfg, tmpls, db, client)
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
