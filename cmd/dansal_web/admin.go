package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// requireLogin redirects to /login if no session user, returning false when redirect was sent.
func requireLogin(w http.ResponseWriter, r *http.Request) (*SessionUser, bool) {
	u := getSessionUser(r)
	if u == nil {
		next := r.URL.RequestURI()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
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

type OrgStats struct {
	Org           Organization
	Slug          string
	EventCount    int
	LocationCount int
	FetchSources  []FetchSource
}

type AdminOrgsData struct {
	Stats   []OrgStats
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
		token := getSessionToken(r)
		orgs, err := client.GetOrganizations(r.Context())
		if err != nil {
			http.Error(w, "could not load organizations", http.StatusBadGateway)
			return
		}
		events, _ := client.GetEvents(r.Context(), "")
		locs, _ := client.GetLocations(r.Context())
		sources, _ := client.GetFetchSources(r.Context(), token)

		evtCount := map[int]int{}
		for _, e := range events {
			if e.OrganizationID != nil {
				evtCount[*e.OrganizationID]++
			}
		}
		locCount := map[int]int{}
		for _, l := range locs {
			if l.OrganizationID != nil {
				locCount[*l.OrganizationID]++
			}
		}
		srcsByOrg := map[int][]FetchSource{}
		for _, s := range sources {
			if s.OrganizationID != nil {
				srcsByOrg[*s.OrganizationID] = append(srcsByOrg[*s.OrganizationID], s)
			}
		}

		stats := make([]OrgStats, len(orgs))
		for i, o := range orgs {
			stats[i] = OrgStats{
				Org:           o,
				Slug:          orgSlug(o.Name),
				EventCount:    evtCount[o.ID],
				LocationCount: locCount[o.ID],
				FetchSources:  srcsByOrg[o.ID],
			}
		}

		title := i18n.T(r, "admin_orgs_title")
		renderTemplate(w, tmpls.adminOrgs, tmplData(r, cfg, i18n, title, AdminOrgsData{
			Stats:   stats,
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
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
		}
		org := orgFromForm(r)
		token := getSessionToken(r)
		created, err := client.CreateOrganization(r.Context(), org, token)
		if err != nil {
			title := i18n.T(r, "admin_new")
			renderTemplate(w, tmpls.adminOrgEdit, tmplData(r, cfg, i18n, title, AdminOrgEditData{
				Org:      org,
				ErrorKey: "admin_save_error",
			}))
			return
		}
		if file, header, ferr := r.FormFile("image"); ferr == nil {
			data, _ := io.ReadAll(file)
			file.Close()
			if uerr := client.UploadOrgImage(r.Context(), created.ID, data, header.Filename, token); uerr != nil {
				log.Printf("upload org image error: %v", uerr)
			}
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
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
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
		if file, header, ferr := r.FormFile("image"); ferr == nil {
			data, _ := io.ReadAll(file)
			file.Close()
			if uerr := client.UploadOrgImage(r.Context(), id, data, header.Filename, token); uerr != nil {
				log.Printf("upload org image error: %v", uerr)
			}
		}
		http.Redirect(w, r, "/admin/organizations", http.StatusSeeOther)
	}
}

func adminOrgDeleteHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
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
		_ = client.DeleteOrganization(r.Context(), id, getSessionToken(r))
		http.Redirect(w, r, "/admin/organizations", http.StatusSeeOther)
	}
}

func adminOrgRunFeedsHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
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
		sources, err := client.GetFetchSources(r.Context(), token)
		if err == nil {
			var ids []int
			for _, s := range sources {
				if s.OrganizationID != nil && *s.OrganizationID == id {
					ids = append(ids, s.ID)
				}
			}
			if len(ids) > 0 {
				_ = client.BulkRunFetchSources(r.Context(), ids, token)
			}
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
	Events   []Event
	IsNew    bool
	ErrorKey string
}

func musicianFromForm(r *http.Request) Musician {
	beginYear, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("begin_year")))
	return Musician{
		Bandname:     strings.TrimSpace(r.FormValue("bandname")),
		ShortName:    strings.TrimSpace(r.FormValue("short_name")),
		Internetsite: strings.TrimSpace(r.FormValue("internetsite")),
		Description:  strings.TrimSpace(r.FormValue("description")),
		MBID:         strings.TrimSpace(r.FormValue("mbid")),
		WikidataID:   strings.TrimSpace(r.FormValue("wikidata_id")),
		Country:      strings.TrimSpace(r.FormValue("country")),
		BeginYear:    beginYear,
		Biography:    strings.TrimSpace(r.FormValue("biography")),
		MembersJSON:  linesToJSON(r.FormValue("members")),
		AlbumsJSON:   linesToJSON(r.FormValue("albums")),
		Mastodon:     strings.TrimSpace(r.FormValue("mastodon")),
		Instagram:    strings.TrimSpace(r.FormValue("instagram")),
		Facebook:     strings.TrimSpace(r.FormValue("facebook")),
		Soundcloud:   strings.TrimSpace(r.FormValue("soundcloud")),
	}
}

// linesToJSON converts a newline-separated text input to a JSON string array.
func linesToJSON(s string) string {
	var items []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	if len(items) == 0 {
		return ""
	}
	b, _ := json.Marshal(items)
	return string(b)
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
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
		}
		m := musicianFromForm(r)
		created, err := client.CreateMusician(r.Context(), m, getSessionToken(r))
		if err != nil {
			title := i18n.T(r, "admin_new")
			renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{
				Musician: m, IsNew: true, ErrorKey: "admin_save_error",
			}))
			return
		}
		if file, header, ferr := r.FormFile("image"); ferr == nil {
			data, _ := io.ReadAll(file)
			file.Close()
			if uerr := client.UploadMusicianImage(r.Context(), created.ID, data, header.Filename, getSessionToken(r)); uerr != nil {
				log.Printf("upload musician image error: %v", uerr)
			}
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
		events, _ := client.GetEventsByMusician(r.Context(), id, getSessionToken(r))
		title := i18n.T(r, "admin_edit")
		renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{
			Musician: musician,
			Events:   events,
		}))
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
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
		}
		m := musicianFromForm(r)
		if err := client.UpdateMusician(r.Context(), id, m, getSessionToken(r)); err != nil {
			title := i18n.T(r, "admin_edit")
			renderTemplate(w, tmpls.adminMusicianEdit, tmplData(r, cfg, i18n, title, AdminMusicianEditData{
				Musician: m, ErrorKey: "admin_save_error",
			}))
			return
		}
		if file, header, ferr := r.FormFile("image"); ferr == nil {
			data, _ := io.ReadAll(file)
			file.Close()
			if uerr := client.UploadMusicianImage(r.Context(), id, data, header.Filename, getSessionToken(r)); uerr != nil {
				log.Printf("upload musician image error: %v", uerr)
			}
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

type AdminFetchurlNewData struct {
	Orgs     []Organization
	ErrorKey string
	URL      string
}

func adminFetchurlNewPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		title := i18n.T(r, "fetch_new_title")
		renderTemplate(w, tmpls.adminFetchurlNew, tmplData(r, cfg, i18n, title, AdminFetchurlNewData{Orgs: orgs}))
	}
}

func adminFetchurlNewPostHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		rawURL := strings.TrimSpace(r.FormValue("url"))
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
		if _, err := client.CreateFetchSource(r.Context(), rawURL, typ, tags, orgID, token); err != nil {
			orgs, _ := client.GetOrganizations(r.Context())
			title := i18n.T(r, "fetch_new_title")
			renderTemplate(w, tmpls.adminFetchurlNew, tmplData(r, cfg, i18n, title, AdminFetchurlNewData{
				Orgs:     orgs,
				ErrorKey: "fetch_add_error",
				URL:      rawURL,
			}))
			return
		}
		http.Redirect(w, r, "/admin/fetchurls", http.StatusSeeOther)
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

func adminLocationDeleteHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
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
		_ = client.DeleteLocation(r.Context(), id, getSessionToken(r))
		http.Redirect(w, r, "/admin/locations", http.StatusSeeOther)
	}
}

// ── Events ────────────────────────────────────────────────────────────────────

type AdminEventsData struct {
	Events            []Event
	Organizations     []Organization
	Musicians         []Musician
	FilterIncludePast bool
	FilterOrgID       int
	FilterDateFrom    string
	FilterDateTo      string
	FilterMusicianID  int
}

type AdminEventNewData struct {
	Organizations []Organization
	Locations     []Location
	Musicians     []Musician
	ErrorKey      string
}

// ── Users & Invites ───────────────────────────────────────────────────────────

type AdminUsersData struct {
	IsAdmin        bool
	Users          []UserInfo
	Orgs           []Organization
	OrgMap         map[int]Organization
	UserOrgs       map[int][]int
	Invites        []InviteLink
	BaseURL        string
	NewInviteToken string
}

func adminUsersHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireLogin(w, r)
		if !ok {
			return
		}
		token := getSessionToken(r)
		isAdmin := su.Role == "admin"

		orgs, _ := client.GetOrganizations(r.Context())
		orgMap := make(map[int]Organization, len(orgs))
		for _, o := range orgs {
			orgMap[o.ID] = o
		}

		userOrgs := make(map[int][]int)
		for _, o := range orgs {
			members, err := client.GetOrganizationMembers(r.Context(), o.ID, token)
			if err != nil {
				continue
			}
			for _, m := range members {
				userOrgs[m.UserID] = append(userOrgs[m.UserID], o.ID)
			}
		}

		var users []UserInfo
		if isAdmin {
			users, _ = client.GetAllUsers(r.Context(), token)
		} else {
			seen := make(map[int]bool)
			for _, orgID := range userOrgs[su.ID] {
				members, _ := client.GetOrganizationMembers(r.Context(), orgID, token)
				for _, m := range members {
					if !seen[m.UserID] {
						seen[m.UserID] = true
						users = append(users, UserInfo{ID: m.UserID, Username: m.Username})
					}
				}
			}
		}

		invites, _ := client.ListInvites(r.Context(), token)
		active := make([]InviteLink, 0, len(invites))
		for _, inv := range invites {
			if inv.UsedAt == "" {
				active = append(active, inv)
			}
		}

		title := i18n.T(r, "admin_users_title")
		renderTemplate(w, tmpls.adminUsers, tmplData(r, cfg, i18n, title, AdminUsersData{
			IsAdmin:        isAdmin,
			Users:          users,
			Orgs:           orgs,
			OrgMap:         orgMap,
			UserOrgs:       userOrgs,
			Invites:        active,
			BaseURL:        cfg.publicBaseURL(),
			NewInviteToken: r.URL.Query().Get("new_invite"),
		}))
	}
}

func adminUserDeleteHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if su.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		id, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_ = client.DeleteUser(r.Context(), id, getSessionToken(r))
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func adminUserRoleHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if su.Role != "admin" {
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
		_ = client.UpdateUser(r.Context(), id, map[string]string{"role": r.FormValue("role")}, getSessionToken(r))
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func adminUserOrgHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if su.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		userID, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		token := getSessionToken(r)
		action := r.FormValue("action")
		orgID, err := strconv.Atoi(r.FormValue("org_id"))
		if err != nil {
			http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			return
		}
		if action == "remove" {
			_ = client.RemoveOrgMember(r.Context(), orgID, userID, token)
		} else {
			_ = client.AddOrgMember(r.Context(), orgID, userID, token)
		}
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func adminUsersBulkHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if su.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		token := getSessionToken(r)
		action := r.FormValue("action")
		for _, idStr := range r.Form["user_ids"] {
			id, err := strconv.Atoi(idStr)
			if err != nil {
				continue
			}
			switch action {
			case "delete":
				_ = client.DeleteUser(r.Context(), id, token)
			case "org":
				orgID, err := strconv.Atoi(r.FormValue("org_id"))
				if err == nil {
					_ = client.AddOrgMember(r.Context(), orgID, id, token)
				}
			}
		}
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func adminInviteCreateHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		token := getSessionToken(r)
		role := r.FormValue("role")
		if role == "" {
			role = "user"
		}
		var orgID *int
		if s := r.FormValue("org_id"); s != "" {
			if id, err := strconv.Atoi(s); err == nil {
				orgID = &id
			}
		}
		link, err := client.CreateInvite(r.Context(), role, orgID, token)
		if err != nil {
			http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/users?new_invite="+link.Token, http.StatusSeeOther)
	}
}

func adminInviteRevokeHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		invToken := mux.Vars(r)["token"]
		_ = client.RevokeInvite(r.Context(), invToken, getSessionToken(r))
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func adminEventsHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}

		q := r.URL.Query()
		includePast := q.Get("include_past") == "1"
		orgID, _ := strconv.Atoi(q.Get("org_id"))
		musicianID, _ := strconv.Atoi(q.Get("musician_id"))
		dateFrom := q.Get("date_from")
		dateTo := q.Get("date_to")

		params := url.Values{}
		if includePast {
			params.Set("include_past", "true")
		}
		if dateFrom != "" {
			if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
				params.Set("start_time_after", strconv.FormatInt(t.Unix()-1, 10))
			}
		}
		if dateTo != "" {
			if t, err := time.Parse("2006-01-02", dateTo); err == nil {
				params.Set("start_time_before", strconv.FormatInt(t.Add(24*time.Hour).Unix(), 10))
			}
		}
		if musicianID != 0 {
			params.Set("musician_id", strconv.Itoa(musicianID))
		}

		token := getSessionToken(r)
		events, err := client.GetAdminEvents(r.Context(), token, params)
		if err != nil {
			http.Error(w, "could not load events", http.StatusBadGateway)
			return
		}
		if orgID != 0 {
			filtered := events[:0]
			for _, e := range events {
				if e.OrganizationID != nil && *e.OrganizationID == orgID {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}

		orgs, _ := client.GetOrganizations(r.Context())
		musicians, _ := client.GetMusicians(r.Context())

		title := i18n.T(r, "admin_events_title")
		renderTemplate(w, tmpls.adminEvents, tmplData(r, cfg, i18n, title, AdminEventsData{
			Events:            events,
			Organizations:     orgs,
			Musicians:         musicians,
			FilterIncludePast: includePast,
			FilterOrgID:       orgID,
			FilterDateFrom:    dateFrom,
			FilterDateTo:      dateTo,
			FilterMusicianID:  musicianID,
		}))
	}
}

func adminEventNewPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		locs, _ := client.GetLocations(r.Context())
		musicians, _ := client.GetMusicians(r.Context())
		title := i18n.T(r, "admin_event_new_title")
		renderTemplate(w, tmpls.adminEventNew, tmplData(r, cfg, i18n, title, AdminEventNewData{
			Organizations: orgs,
			Locations:     locs,
			Musicians:     musicians,
		}))
	}
}

func adminEventCreateHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireLogin(w, r)
		if !ok {
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		renderErr := func(errKey string) {
			orgs, _ := client.GetOrganizations(r.Context())
			locs, _ := client.GetLocations(r.Context())
			musicians, _ := client.GetMusicians(r.Context())
			title := i18n.T(r, "admin_event_new_title")
			renderTemplate(w, tmpls.adminEventNew, tmplData(r, cfg, i18n, title, AdminEventNewData{
				Organizations: orgs,
				Locations:     locs,
				Musicians:     musicians,
				ErrorKey:      errKey,
			}))
		}

		date := r.FormValue("date")
		startT := r.FormValue("start_time")
		endT := r.FormValue("end_time")
		startTime, endTime := "", ""
		if date != "" && startT != "" {
			startTime = date + "T" + startT + ":00"
		}
		if date != "" && endT != "" {
			endTime = date + "T" + endT + ":00"
		}

		var orgID *int
		switch r.FormValue("org_choice") {
		case "existing":
			if v := r.FormValue("org_id"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					orgID = &n
				}
			}
		case "new":
			newOrg := Organization{Name: strings.TrimSpace(r.FormValue("new_org_name"))}
			if newOrg.Name != "" {
				created, err := client.CreateOrganization(r.Context(), newOrg, getSessionToken(r))
				if err != nil {
					renderErr("admin_save_error")
					return
				}
				orgID = &created.ID
			}
		}

		var locReq EventLocReq
		switch r.FormValue("loc_choice") {
		case "existing":
			if v := r.FormValue("loc_id"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					locs, _ := client.GetLocations(r.Context())
					for _, l := range locs {
						if l.ID == n {
							locReq = EventLocReq{
								Location:  l.Location,
								Address:   l.Address,
								Town:      l.Town,
								Country:   l.Country,
								Latitude:  l.Latitude,
								Longitude: l.Longitude,
							}
							break
						}
					}
				}
			}
		case "new":
			locReq = EventLocReq{
				Location:  strings.TrimSpace(r.FormValue("new_loc_name")),
				Address:   strings.TrimSpace(r.FormValue("new_loc_address")),
				Zipcode:   strings.TrimSpace(r.FormValue("new_loc_zip")),
				Town:      strings.TrimSpace(r.FormValue("new_loc_town")),
				Country:   strings.TrimSpace(r.FormValue("new_loc_country")),
				Latitude:  strings.TrimSpace(r.FormValue("new_loc_lat")),
				Longitude: strings.TrimSpace(r.FormValue("new_loc_lng")),
			}
		}

		var pricing *Pricing
		if pt := r.FormValue("pricing_type"); pt != "" && pt != "none" {
			p := &Pricing{Type: pt}
			switch pt {
			case "single":
				if amt := r.FormValue("pricing_amount"); amt != "" {
					if f, err := strconv.ParseFloat(amt, 64); err == nil {
						p.Amount = f
					}
				}
				p.Currency = strings.TrimSpace(r.FormValue("pricing_currency"))
			case "multiple":
				labels := r.MultipartForm.Value["pl_label"]
				amounts := r.MultipartForm.Value["pl_amount"]
				for i, lbl := range labels {
					lbl = strings.TrimSpace(lbl)
					if lbl == "" {
						continue
					}
					var amt float64
					if i < len(amounts) {
						if f, err := strconv.ParseFloat(strings.TrimSpace(amounts[i]), 64); err == nil {
							amt = f
						}
					}
					p.Prices = append(p.Prices, Price{Label: lbl, Amount: amt})
				}
				if len(p.Prices) == 0 {
					p = nil
				}
			}
			pricing = p
		}

		var tags []string
		if t := strings.TrimSpace(r.FormValue("tags")); t != "" {
			for _, tag := range strings.Split(t, ",") {
				if tag = strings.TrimSpace(tag); tag != "" {
					tags = append(tags, tag)
				}
			}
		}

		req := EventCreateReq{
			Title:              strings.TrimSpace(r.FormValue("title")),
			Description:        strings.TrimSpace(r.FormValue("description")),
			StartTime:          startTime,
			EndTime:            endTime,
			HasBall:            r.FormValue("has_ball") == "on",
			HasWorkshop:        r.FormValue("has_workshop") == "on",
			HasFestival:        r.FormValue("has_festival") == "on",
			WorkshopDifficulty: r.FormValue("workshop_difficulty"),
			BookingURL:         strings.TrimSpace(r.FormValue("booking_url")),
			Tags:               tags,
			URL:                strings.TrimSpace(r.FormValue("url")),
			OrganizationID:     orgID,
			Pricing:            pricing,
			Location:           locReq,
		}

		if req.Title == "" {
			renderErr("evt_title_required")
			return
		}

		event, err := client.CreateEvent(r.Context(), req, getSessionToken(r))
		if err != nil {
			log.Printf("create event error: %v", err)
			renderErr("admin_save_error")
			return
		}

		if file, header, ferr := r.FormFile("image"); ferr == nil {
			defer file.Close()
			data, rerr := io.ReadAll(file)
			if rerr == nil {
				if uerr := client.UploadEventImage(r.Context(), event.ID, data, header.Filename, getSessionToken(r)); uerr != nil {
					log.Printf("upload image error: %v", uerr)
				}
			}
		}

		starts := r.MultipartForm.Value["tt_start"]
		ends := r.MultipartForm.Value["tt_end"]
		titles := r.MultipartForm.Value["tt_title"]
		descs := r.MultipartForm.Value["tt_desc"]
		rooms := r.MultipartForm.Value["tt_room"]
		var ttEntries []TimetableEntryReq
		for i, s := range starts {
			s = strings.TrimSpace(s)
			if i >= len(titles) {
				break
			}
			t := strings.TrimSpace(titles[i])
			if s == "" && t == "" {
				continue
			}
			entry := TimetableEntryReq{StartTime: s, Title: t}
			if i < len(ends) {
				entry.EndTime = strings.TrimSpace(ends[i])
			}
			if i < len(descs) {
				entry.Description = strings.TrimSpace(descs[i])
			}
			if i < len(rooms) {
				entry.Room = strings.TrimSpace(rooms[i])
			}
			ttEntries = append(ttEntries, entry)
		}
		if len(ttEntries) > 0 {
			if terr := client.AddTimetableEntries(r.Context(), event.ID, ttEntries, getSessionToken(r)); terr != nil {
				log.Printf("add timetable error: %v", terr)
			}
		}

		http.Redirect(w, r, "/admin/events", http.StatusSeeOther)
	}
}

type AdminEventEditData struct {
	Event         Event
	Organizations []Organization
	Locations     []Location
	Musicians     []Musician
	ErrorKey      string
}

func adminEventEditPageHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
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
		event, err := client.GetEvent(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		orgs, _ := client.GetOrganizations(r.Context())
		locs, _ := client.GetLocations(r.Context())
		musicians, _ := client.GetMusicians(r.Context())
		title := i18n.T(r, "admin_event_edit_title")
		renderTemplate(w, tmpls.adminEventEdit, tmplData(r, cfg, i18n, title, AdminEventEditData{
			Event:         event,
			Organizations: orgs,
			Locations:     locs,
			Musicians:     musicians,
		}))
	}
}

func adminEventSaveHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
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
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		renderErr := func(errKey string) {
			event, _ := client.GetEvent(r.Context(), id)
			orgs, _ := client.GetOrganizations(r.Context())
			locs, _ := client.GetLocations(r.Context())
			musicians, _ := client.GetMusicians(r.Context())
			title := i18n.T(r, "admin_event_edit_title")
			renderTemplate(w, tmpls.adminEventEdit, tmplData(r, cfg, i18n, title, AdminEventEditData{
				Event:         event,
				Organizations: orgs,
				Locations:     locs,
				Musicians:     musicians,
				ErrorKey:      errKey,
			}))
		}

		date := r.FormValue("date")
		startT := r.FormValue("start_time")
		endT := r.FormValue("end_time")
		startTime, endTime := "", ""
		if date != "" && startT != "" {
			startTime = date + "T" + startT + ":00"
		}
		if date != "" && endT != "" {
			endTime = date + "T" + endT + ":00"
		}

		var orgID *int
		switch r.FormValue("org_choice") {
		case "existing":
			if v := r.FormValue("org_id"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					orgID = &n
				}
			}
		case "new":
			newOrg := Organization{Name: strings.TrimSpace(r.FormValue("new_org_name"))}
			if newOrg.Name != "" {
				created, err := client.CreateOrganization(r.Context(), newOrg, getSessionToken(r))
				if err != nil {
					renderErr("admin_save_error")
					return
				}
				orgID = &created.ID
			}
		}

		var locReq EventLocReq
		switch r.FormValue("loc_choice") {
		case "existing":
			if v := r.FormValue("loc_id"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					locs, _ := client.GetLocations(r.Context())
					for _, l := range locs {
						if l.ID == n {
							locReq = EventLocReq{
								Location:  l.Location,
								Address:   l.Address,
								Town:      l.Town,
								Country:   l.Country,
								Latitude:  l.Latitude,
								Longitude: l.Longitude,
							}
							break
						}
					}
				}
			}
		case "new":
			locReq = EventLocReq{
				Location:  strings.TrimSpace(r.FormValue("new_loc_name")),
				Address:   strings.TrimSpace(r.FormValue("new_loc_address")),
				Zipcode:   strings.TrimSpace(r.FormValue("new_loc_zip")),
				Town:      strings.TrimSpace(r.FormValue("new_loc_town")),
				Country:   strings.TrimSpace(r.FormValue("new_loc_country")),
				Latitude:  strings.TrimSpace(r.FormValue("new_loc_lat")),
				Longitude: strings.TrimSpace(r.FormValue("new_loc_lng")),
			}
		}

		var pricing *Pricing
		if pt := r.FormValue("pricing_type"); pt != "" && pt != "none" {
			p := &Pricing{Type: pt}
			switch pt {
			case "single":
				if amt := r.FormValue("pricing_amount"); amt != "" {
					if f, err := strconv.ParseFloat(amt, 64); err == nil {
						p.Amount = f
					}
				}
				p.Currency = strings.TrimSpace(r.FormValue("pricing_currency"))
			case "multiple":
				labels := r.MultipartForm.Value["pl_label"]
				amounts := r.MultipartForm.Value["pl_amount"]
				for i, lbl := range labels {
					lbl = strings.TrimSpace(lbl)
					if lbl == "" {
						continue
					}
					var amt float64
					if i < len(amounts) {
						if f, err := strconv.ParseFloat(strings.TrimSpace(amounts[i]), 64); err == nil {
							amt = f
						}
					}
					p.Prices = append(p.Prices, Price{Label: lbl, Amount: amt})
				}
				if len(p.Prices) == 0 {
					p = nil
				}
			}
			pricing = p
		}

		var tags []string
		if t := strings.TrimSpace(r.FormValue("tags")); t != "" {
			for _, tag := range strings.Split(t, ",") {
				if tag = strings.TrimSpace(tag); tag != "" {
					tags = append(tags, tag)
				}
			}
		}

		var musicianIDs []int
		for _, v := range r.MultipartForm.Value["musician_ids"] {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				musicianIDs = append(musicianIDs, n)
			}
		}

		ticketsTotal, _ := strconv.Atoi(r.FormValue("tickets_total"))
		req := EventUpdateReq{
			Title:              strings.TrimSpace(r.FormValue("title")),
			Description:        strings.TrimSpace(r.FormValue("description")),
			StartTime:          startTime,
			EndTime:            endTime,
			HasBall:            r.FormValue("has_ball") == "on",
			HasWorkshop:        r.FormValue("has_workshop") == "on",
			HasFestival:        r.FormValue("has_festival") == "on",
			WorkshopDifficulty: r.FormValue("workshop_difficulty"),
			BookingURL:         strings.TrimSpace(r.FormValue("booking_url")),
			IsCancelled:        r.FormValue("is_cancelled") == "on",
			Availability:       r.FormValue("availability"),
			TicketsTotal:       ticketsTotal,
			BookingEnabled:     r.FormValue("booking_enabled") == "on",
			IsPublished:        r.FormValue("is_published") == "on",
			Tags:               tags,
			URL:                strings.TrimSpace(r.FormValue("url")),
			OrganizationID:     orgID,
			Pricing:            pricing,
			Location:           locReq,
			Musicians:          musicianIDs,
		}

		if req.Title == "" {
			renderErr("evt_title_required")
			return
		}

		if _, err := client.UpdateEvent(r.Context(), id, req, getSessionToken(r)); err != nil {
			log.Printf("update event error: %v", err)
			renderErr("admin_save_error")
			return
		}

		if file, header, ferr := r.FormFile("image"); ferr == nil {
			defer file.Close()
			data, rerr := io.ReadAll(file)
			if rerr == nil {
				if uerr := client.UploadEventImage(r.Context(), id, data, header.Filename, getSessionToken(r)); uerr != nil {
					log.Printf("upload image error: %v", uerr)
				}
			}
		}

		starts := r.MultipartForm.Value["tt_start"]
		ends := r.MultipartForm.Value["tt_end"]
		titles := r.MultipartForm.Value["tt_title"]
		descs := r.MultipartForm.Value["tt_desc"]
		rooms := r.MultipartForm.Value["tt_room"]
		var ttEntries []TimetableEntryReq
		for i, s := range starts {
			s = strings.TrimSpace(s)
			if i >= len(titles) {
				break
			}
			t := strings.TrimSpace(titles[i])
			if s == "" && t == "" {
				continue
			}
			entry := TimetableEntryReq{StartTime: s, Title: t}
			if i < len(ends) {
				entry.EndTime = strings.TrimSpace(ends[i])
			}
			if i < len(descs) {
				entry.Description = strings.TrimSpace(descs[i])
			}
			if i < len(rooms) {
				entry.Room = strings.TrimSpace(rooms[i])
			}
			ttEntries = append(ttEntries, entry)
		}
		if err := client.ReplaceTimetable(r.Context(), id, ttEntries, getSessionToken(r)); err != nil {
			log.Printf("replace timetable error: %v", err)
		}

		http.Redirect(w, r, "/admin/events", http.StatusSeeOther)
	}
}
