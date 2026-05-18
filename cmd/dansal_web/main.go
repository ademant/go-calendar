package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

func main() {
	cfg := loadConfig()
	db := initDB(cfg.DBPath)
	client := &DansalClient{
		BaseURL: cfg.DansalURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}

	tmpls := loadTemplates()
	i18n := loadI18n(cfg.I18nFile)

	r := mux.NewRouter()

	r.HandleFunc("/.well-known/webfinger", webfingerHandler(cfg, db, client)).Methods("GET")
	r.HandleFunc("/.well-known/nodeinfo", nodeinfoIndexHandler(cfg)).Methods("GET")
	r.HandleFunc("/nodeinfo/2.0", nodeinfoHandler(cfg)).Methods("GET")

	r.HandleFunc("/org/{name}", actorOrFrontendHandler(cfg, tmpls, db, client, i18n)).Methods("GET")
	r.HandleFunc("/org/{name}/outbox", outboxHandler(cfg, db, client)).Methods("GET")
	r.HandleFunc("/org/{name}/followers", followersHandler(cfg, db)).Methods("GET")
	r.HandleFunc("/org/{name}/inbox", inboxHandler(cfg, db, client)).Methods("POST")

	r.HandleFunc("/favicon.svg", svgHandler(faviconSVG)).Methods("GET")
	r.HandleFunc("/logo.svg", svgHandler(logoSVG)).Methods("GET")
	r.HandleFunc("/banner.svg", svgHandler(bannerSVG)).Methods("GET")
	r.HandleFunc("/", indexHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/events/{id}", eventHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/musicians", musiciansHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/musicians/{id}", musicianHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/login", loginPageHandler(cfg, tmpls, i18n)).Methods("GET")
	r.HandleFunc("/login", loginHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/logout", logoutHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/lang", langHandler(i18n)).Methods("GET")
	r.HandleFunc("/settings", settingsPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/settings", settingsUpdateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/settings/verify", settingsSendVerifyHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/magic", magicRequestHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/api/v1/login/magic/{token}", magicLoginHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/api/v1/verify/{token}", verifyEmailHandler(cfg, tmpls, client, i18n)).Methods("GET")

	r.HandleFunc("/admin/events", adminEventsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/events/new", adminEventNewPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/events/new", adminEventCreateHandler(cfg, tmpls, client, i18n)).Methods("POST")

	r.HandleFunc("/admin/organizations", adminOrgsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/organizations/new", adminOrgNewPageHandler(cfg, tmpls, i18n)).Methods("GET")
	r.HandleFunc("/admin/organizations/new", adminOrgCreateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/edit", adminOrgEditPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/organizations/{id}/edit", adminOrgSaveHandler(cfg, tmpls, client, i18n)).Methods("POST")

	r.HandleFunc("/admin/fetchurls", adminFetchurlsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/fetchurls/new", adminFetchurlNewPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/fetchurls/new", adminFetchurlNewPostHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/fetchurls/bulk", adminFetchurlBulkHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/fetchurls/{id}/edit", adminFetchurlEditPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/fetchurls/{id}/edit", adminFetchurlSaveHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/fetchurls/{id}/delete", adminFetchurlDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/fetchurls/{id}/run", adminFetchurlRunHandler(cfg, client)).Methods("POST")

	r.HandleFunc("/admin/musicians", adminMusiciansHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/musicians/new", adminMusicianNewPageHandler(cfg, tmpls, i18n)).Methods("GET")
	r.HandleFunc("/admin/musicians/new", adminMusicianCreateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/musicians/{id}/edit", adminMusicianEditPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/musicians/{id}/edit", adminMusicianSaveHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/musicians/{id}/delete", adminMusicianDeleteHandler(cfg, client)).Methods("POST")

	r.HandleFunc("/admin/locations", adminLocationsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/locations/new", adminLocationNewPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/locations/new", adminLocationCreateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/locations/bulk-assign", adminLocationBulkAssignHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/locations/{id}/edit", adminLocationEditPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/locations/{id}/edit", adminLocationSaveHandler(cfg, tmpls, client, i18n)).Methods("POST")

	go startDelivery(cfg, db, client)

	log.Printf("web server listening on %s (domain: %s)", cfg.Listen, cfg.Domain)
	if err := http.ListenAndServe(cfg.Listen, r); err != nil {
		log.Fatal(err)
	}
}
