package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// requireLogin redirects to /login if no session user, returning false when redirect was sent.
func requireLogin(w http.ResponseWriter, r *http.Request) (*SessionUser, bool) {
	u := getSessionUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, false
	}
	return u, true
}

// ── Organizations ─────────────────────────────────────────────────────────────

type AdminOrgsData struct {
	Orgs    []Organization
	CanEdit bool
}

type AdminOrgEditData struct {
	Org      Organization
	ErrorKey string
}

func adminOrgsHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		orgs, err := client.GetOrganizations(r.Context())
		if err != nil {
			http.Error(w, "could not load organizations", http.StatusBadGateway)
			return
		}
		title := i18n.T(r, "admin_orgs_title")
		renderTemplate(w, tmpls.adminOrgs, tmplData(r, cfg, i18n, title, AdminOrgsData{
			Orgs:    orgs,
			CanEdit: user.Role == "admin",
		}))
	}
}

func adminOrgEditPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		id, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		org, err := client.GetOrganization(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		title := i18n.T(r, "admin_edit")
		renderTemplate(w, tmpls.adminOrgEdit, tmplData(r, cfg, i18n, title, AdminOrgEditData{Org: org}))
	}
}

func adminOrgSaveHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		id, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		description := strings.TrimSpace(r.FormValue("description"))
		token := getSessionToken(r)

		if err := client.UpdateOrganization(r.Context(), id, name, description, token); err != nil {
			org, _ := client.GetOrganization(r.Context(), id)
			org.Name = name
			org.Description = description
			title := i18n.T(r, "admin_edit")
			renderTemplate(w, tmpls.adminOrgEdit, tmplData(r, cfg, i18n, title, AdminOrgEditData{
				Org:      org,
				ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/organizations", http.StatusSeeOther)
	}
}

// ── Fetch sources ─────────────────────────────────────────────────────────────

type AdminFetchurlsData struct {
	Sources []FetchSource
	OrgMap  map[int]Organization
}

type AdminFetchurlEditData struct {
	Source   FetchSource
	Orgs     []Organization
	OrgMap   map[int]Organization
	ErrorKey string
}

func adminFetchurlsHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		token := getSessionToken(r)
		sources, err := client.GetFetchSources(r.Context(), token)
		if err != nil {
			http.Error(w, "could not load feed sources", http.StatusBadGateway)
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		orgMap := make(map[int]Organization, len(orgs))
		for _, o := range orgs {
			orgMap[o.ID] = o
		}
		title := i18n.T(r, "admin_fetchurls_title")
		renderTemplate(w, tmpls.adminFetchurls, tmplData(r, cfg, i18n, title, AdminFetchurlsData{
			Sources: sources,
			OrgMap:  orgMap,
		}))
	}
}

func adminFetchurlEditPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		id, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		token := getSessionToken(r)
		src, err := client.GetFetchSource(r.Context(), id, token)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		orgMap := make(map[int]Organization, len(orgs))
		for _, o := range orgs {
			orgMap[o.ID] = o
		}
		title := i18n.T(r, "admin_edit")
		renderTemplate(w, tmpls.adminFetchurlEdit, tmplData(r, cfg, i18n, title, AdminFetchurlEditData{
			Source: src,
			Orgs:   orgs,
			OrgMap: orgMap,
		}))
	}
}

func adminFetchurlSaveHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		id, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		typ := r.FormValue("type")
		rawTags := strings.TrimSpace(r.FormValue("tags"))
		var tags []string
		for _, t := range strings.FieldsFunc(rawTags, func(r rune) bool { return r == ',' || r == ' ' }) {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		var orgID *int
		if v := r.FormValue("organization_id"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				orgID = &n
			}
		}

		token := getSessionToken(r)
		if err := client.UpdateFetchSource(r.Context(), id, typ, tags, orgID, token); err != nil {
			src, _ := client.GetFetchSource(r.Context(), id, token)
			orgs, _ := client.GetOrganizations(r.Context())
			orgMap := make(map[int]Organization, len(orgs))
			for _, o := range orgs {
				orgMap[o.ID] = o
			}
			title := i18n.T(r, "admin_edit")
			renderTemplate(w, tmpls.adminFetchurlEdit, tmplData(r, cfg, i18n, title, AdminFetchurlEditData{
				Source:   src,
				Orgs:     orgs,
				OrgMap:   orgMap,
				ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/fetchurls", http.StatusSeeOther)
	}
}
