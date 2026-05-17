package main

import (
	"encoding/json"
	"net/http"
	"sort"
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

func orgFromForm(r *http.Request) Organization {
	return Organization{
		Name:         strings.TrimSpace(r.FormValue("name")),
		Description:  strings.TrimSpace(r.FormValue("description")),
		ActorName:    strings.TrimSpace(r.FormValue("actor_name")),
		Website:      strings.TrimSpace(r.FormValue("website")),
		Instagram:    strings.TrimSpace(r.FormValue("instagram")),
		Mastodon:     strings.TrimSpace(r.FormValue("mastodon")),
		Facebook:     strings.TrimSpace(r.FormValue("facebook")),
		ContactEmail: strings.TrimSpace(r.FormValue("contact_email")),
	}
}

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

func adminOrgNewPageHandler(cfg *Config, tmpls *Templates, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		title := i18n.T(r, "admin_new")
		renderTemplate(w, tmpls.adminOrgEdit, tmplData(r, cfg, i18n, title, AdminOrgEditData{}))
	}
}

func adminOrgCreateHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		org := orgFromForm(r)
		token := getSessionToken(r)
		if _, err := client.CreateOrganization(r.Context(), org, token); err != nil {
			title := i18n.T(r, "admin_new")
			renderTemplate(w, tmpls.adminOrgEdit, tmplData(r, cfg, i18n, title, AdminOrgEditData{
				Org:      org,
				ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/organizations", http.StatusSeeOther)
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
		org := orgFromForm(r)
		token := getSessionToken(r)
		if err := client.UpdateOrganization(r.Context(), id, org, token); err != nil {
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

// ── Musicians ─────────────────────────────────────────────────────────────────

type AdminMusiciansData struct {
	Musicians []Musician
}

type AdminMusicianEditData struct {
	Musician Musician
	IsNew    bool
	ErrorKey string
}

func musicianFromForm(r *http.Request) Musician {
	return Musician{
		Bandname:     strings.TrimSpace(r.FormValue("bandname")),
		ShortName:    strings.TrimSpace(r.FormValue("short_name")),
		Internetsite: strings.TrimSpace(r.FormValue("internetsite")),
		Description:  strings.TrimSpace(r.FormValue("description")),
		MBID:         strings.TrimSpace(r.FormValue("mbid")),
		Mastodon:     strings.TrimSpace(r.FormValue("mastodon")),
		Instagram:    strings.TrimSpace(r.FormValue("instagram")),
		Facebook:     strings.TrimSpace(r.FormValue("facebook")),
		Soundcloud:   strings.TrimSpace(r.FormValue("soundcloud")),
	}
}

func adminMusiciansHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		musicians, err := client.GetMusicians(r.Context())
		if err != nil {
			http.Error(w, "could not load musicians", http.StatusBadGateway)
			return
		}
		title := i18n.T(r, "admin_musicians_title")
		renderTemplate(w, tmpls.adminMusicians, tmplData(r, cfg, i18n, title, AdminMusiciansData{Musicians: musicians}))
	}
}

func adminMusicianNewPageHandler(cfg *Config, tmpls *Templates, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		title := i18n.T(r, "admin_new")
		renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{IsNew: true}))
	}
}

func adminMusicianCreateHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m := musicianFromForm(r)
		if _, err := client.CreateMusician(r.Context(), m, getSessionToken(r)); err != nil {
			title := i18n.T(r, "admin_new")
			renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{
				Musician: m, IsNew: true, ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/musicians", http.StatusSeeOther)
	}
}

func adminMusicianEditPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
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
		musician, err := client.GetMusician(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		title := i18n.T(r, "admin_edit")
		renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{Musician: musician}))
	}
}

func adminMusicianSaveHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
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
		m := musicianFromForm(r)
		if err := client.UpdateMusician(r.Context(), id, m, getSessionToken(r)); err != nil {
			title := i18n.T(r, "admin_edit")
			renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{
				Musician: m, ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/musicians", http.StatusSeeOther)
	}
}

func adminMusicianDeleteHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
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
		_ = client.DeleteMusician(r.Context(), id, getSessionToken(r))
		http.Redirect(w, r, "/admin/musicians", http.StatusSeeOther)
	}
}

// ── Fetch sources ─────────────────────────────────────────────────────────────

type AdminFetchurlsData struct {
	Sources []FetchSource
	OrgMap  map[int]Organization
	Orgs    []Organization
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
			Orgs:    orgs,
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

func adminFetchurlDeleteHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
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
		_ = client.DeleteFetchSource(r.Context(), id, getSessionToken(r))
		http.Redirect(w, r, "/admin/fetchurls", http.StatusSeeOther)
	}
}

func adminFetchurlRunHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
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
		count, runErr := client.RunFetchSource(r.Context(), id, getSessionToken(r))
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			if runErr != nil {
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]string{"error": runErr.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"count": count})
			return
		}
		http.Redirect(w, r, "/admin/fetchurls", http.StatusSeeOther)
	}
}

func adminFetchurlBulkHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var ids []int
		for _, s := range r.Form["src_ids"] {
			if n, err := strconv.Atoi(s); err == nil {
				ids = append(ids, n)
			}
		}
		if len(ids) == 0 {
			http.Redirect(w, r, "/admin/fetchurls", http.StatusSeeOther)
			return
		}
		token := getSessionToken(r)
		action := r.FormValue("bulk_action")
		switch action {
		case "delete":
			if user.Role != "admin" {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			_ = client.BulkDeleteFetchSources(r.Context(), ids, token)
		case "run":
			_ = client.BulkRunFetchSources(r.Context(), ids, token)
		case "assign-org":
			if user.Role != "admin" {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			var orgID *int
			if v := r.FormValue("organization_id"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					orgID = &n
				}
			}
			_ = client.BulkAssignFetchSourceOrg(r.Context(), ids, orgID, token)
		}
		http.Redirect(w, r, "/admin/fetchurls", http.StatusSeeOther)
	}
}

// ── Locations ─────────────────────────────────────────────────────────────────

type AdminLocationsData struct {
	Locations []Location
	OrgMap    map[int]Organization
	Orgs      []Organization
	IsAdmin   bool
}

type AdminLocationEditData struct {
	Location Location
	Orgs     []Organization
	ErrorKey string
}

func buildOrgMap(orgs []Organization) map[int]Organization {
	m := make(map[int]Organization, len(orgs))
	for _, o := range orgs {
		m[o.ID] = o
	}
	return m
}

func adminLocationsHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		locs, err := client.GetLocations(r.Context())
		if err != nil {
			http.Error(w, "could not load locations", http.StatusBadGateway)
			return
		}
		sort.Slice(locs, func(i, j int) bool {
			if locs[i].Town != locs[j].Town {
				return locs[i].Town < locs[j].Town
			}
			return locs[i].Location < locs[j].Location
		})
		orgs, _ := client.GetOrganizations(r.Context())
		title := i18n.T(r, "admin_locations_title")
		renderTemplate(w, tmpls.adminLocations, tmplData(r, cfg, i18n, title, AdminLocationsData{
			Locations: locs,
			OrgMap:    buildOrgMap(orgs),
			Orgs:      orgs,
			IsAdmin:   user.Role == "admin",
		}))
	}
}

func adminLocationBulkAssignHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var ids []int
		for _, s := range r.Form["loc_ids"] {
			if n, err := strconv.Atoi(s); err == nil {
				ids = append(ids, n)
			}
		}
		var orgID *int
		if v := r.FormValue("organization_id"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				orgID = &n
			}
		}
		if len(ids) > 0 {
			client.BulkAssignLocationOrg(r.Context(), ids, orgID, getSessionToken(r))
		}
		http.Redirect(w, r, "/admin/locations", http.StatusSeeOther)
	}
}

func adminLocationNewPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		title := i18n.T(r, "admin_new")
		renderTemplate(w, tmpls.adminLocationEdit, tmplData(r, cfg, i18n, title, AdminLocationEditData{Orgs: orgs}))
	}
}

func adminLocationCreateHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var orgID *int
		if v := r.FormValue("organization_id"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				orgID = &n
			}
		}
		loc := Location{
			Location:     strings.TrimSpace(r.FormValue("location")),
			ShortName:    strings.TrimSpace(r.FormValue("short_name")),
			Address:      strings.TrimSpace(r.FormValue("address")),
			Zipcode:      strings.TrimSpace(r.FormValue("zipcode")),
			Town:         strings.TrimSpace(r.FormValue("town")),
			Country:      strings.TrimSpace(r.FormValue("country")),
			Latitude:     strings.TrimSpace(r.FormValue("latitude")),
			Longitude:    strings.TrimSpace(r.FormValue("longitude")),
			Internetsite: strings.TrimSpace(r.FormValue("internetsite")),
			OrganizationID: orgID,
		}
		token := getSessionToken(r)
		if _, err := client.CreateLocation(r.Context(), loc, token); err != nil {
			orgs, _ := client.GetOrganizations(r.Context())
			title := i18n.T(r, "admin_new")
			renderTemplate(w, tmpls.adminLocationEdit, tmplData(r, cfg, i18n, title, AdminLocationEditData{
				Location: loc,
				Orgs:     orgs,
				ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/locations", http.StatusSeeOther)
	}
}

func adminLocationEditPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
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
		loc, err := client.GetLocation(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		title := i18n.T(r, "admin_edit")
		renderTemplate(w, tmpls.adminLocationEdit, tmplData(r, cfg, i18n, title, AdminLocationEditData{
			Location: loc,
			Orgs:     orgs,
		}))
	}
}

func adminLocationSaveHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
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
		var orgID *int
		if v := r.FormValue("organization_id"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				orgID = &n
			}
		}
		loc := Location{
			ID:             id,
			Location:       strings.TrimSpace(r.FormValue("location")),
			ShortName:      strings.TrimSpace(r.FormValue("short_name")),
			Address:        strings.TrimSpace(r.FormValue("address")),
			Zipcode:        strings.TrimSpace(r.FormValue("zipcode")),
			Town:           strings.TrimSpace(r.FormValue("town")),
			Country:        strings.TrimSpace(r.FormValue("country")),
			Latitude:       strings.TrimSpace(r.FormValue("latitude")),
			Longitude:      strings.TrimSpace(r.FormValue("longitude")),
			Internetsite:   strings.TrimSpace(r.FormValue("internetsite")),
			OrganizationID: orgID,
		}
		token := getSessionToken(r)
		if err := client.UpdateLocation(r.Context(), id, loc, token); err != nil {
			orgs, _ := client.GetOrganizations(r.Context())
			title := i18n.T(r, "admin_edit")
			renderTemplate(w, tmpls.adminLocationEdit, tmplData(r, cfg, i18n, title, AdminLocationEditData{
				Location: loc,
				Orgs:     orgs,
				ErrorKey: "admin_save_error",
			}))
			return
		}
		http.Redirect(w, r, "/admin/locations", http.StatusSeeOther)
	}
}
