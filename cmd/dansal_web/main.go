package main

import (
	"log"
	"log/syslog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
)

func main() {
	if w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "dansal_web"); err == nil {
		log.SetOutput(w)
		log.SetFlags(0)
	}

	cfg := loadConfig()
	cfg.pagesContent = loadPagesContent(cfg.PagesFile)
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
	r.HandleFunc("/nodeinfo/2.1", nodeinfo21Handler(cfg)).Methods("GET")

	r.HandleFunc("/org/{name}", actorOrFrontendHandler(cfg, tmpls, db, client, i18n)).Methods("GET")
	r.HandleFunc("/org/{name}/outbox", outboxHandler(cfg, db, client)).Methods("GET")
	r.HandleFunc("/org/{name}/followers", followersHandler(cfg, db)).Methods("GET")
	r.HandleFunc("/org/{name}/inbox", inboxHandler(cfg, db, client)).Methods("POST")

	faviconData, logoData, bannerData := faviconSVG, logoSVG, bannerSVG
	if cfg.ImagesDir != "" {
		if b, err := os.ReadFile(filepath.Join(cfg.ImagesDir, "favicon.svg")); err == nil {
			faviconData = b
		}
		if b, err := os.ReadFile(filepath.Join(cfg.ImagesDir, "logo.svg")); err == nil {
			logoData = b
		}
		if b, err := os.ReadFile(filepath.Join(cfg.ImagesDir, "banner.svg")); err == nil {
			bannerData = b
		}
	}
	r.HandleFunc("/favicon.svg", svgHandler(faviconData)).Methods("GET")
	r.HandleFunc("/logo.svg", svgHandler(logoData)).Methods("GET")
	r.HandleFunc("/banner.svg", svgHandler(bannerData)).Methods("GET")
	r.HandleFunc("/", indexHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/events/{id}", eventHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/events/{id}/board", contactBoardPostHandler(cfg, client, i18n)).Methods("POST")
	r.HandleFunc("/events/{id}/board/{post_id}/delete", contactBoardDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/events/{id}/board/{post_id}/contact", contactBoardContactHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/contact-posts/verify/{token}", contactBoardVerifyHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/checkin/{qr_token}", checkinGetHandler(cfg, tmpls, i18n)).Methods("GET")
	r.HandleFunc("/checkin/{qr_token}", checkinPostHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/events/{id}/book", bookingPostHandler(cfg, client, i18n)).Methods("POST")
	r.HandleFunc("/bookings/verify/{token}", bookingVerifyHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/events/{id}.ics", feedEventICSHandler(cfg, client)).Methods("GET")
	r.HandleFunc("/musicians", musiciansHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/musicians/{id}", musicianHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/organizations", orgsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/impressum", impressumHandler(cfg, tmpls, i18n)).Methods("GET")

	// Feed exports — order: specific before generic
	r.HandleFunc("/feed/org/{slug}/events.{format}", feedOrgHandler(cfg, db, client)).Methods("GET")
	r.HandleFunc("/feed/musician/{slug}/events.{format}", feedMusicianHandler(cfg, client)).Methods("GET")
	r.HandleFunc("/feed/location/{slug}/events.{format}", feedLocationHandler(cfg, client)).Methods("GET")
	r.HandleFunc("/feed/ball/events.{format}", feedTypeHandler(cfg, client, "ball")).Methods("GET")
	r.HandleFunc("/feed/workshop/events.{format}", feedTypeHandler(cfg, client, "workshop")).Methods("GET")
	r.HandleFunc("/feed/festival/events.{format}", feedTypeHandler(cfg, client, "festival")).Methods("GET")
	r.HandleFunc("/feed/events.{format}", feedMainHandler(cfg, client)).Methods("GET")

	r.HandleFunc("/login", loginPageHandler(cfg, tmpls, i18n)).Methods("GET")
	r.HandleFunc("/login", loginHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/logout", logoutHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/lang", langHandler(i18n)).Methods("GET")
	r.HandleFunc("/settings", settingsPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/settings", settingsUpdateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/settings/verify", settingsSendVerifyHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/settings/verify-telegram", settingsTelegramVerifyHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/magic", magicRequestHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/api/v1/login/magic/{token}", magicLoginHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/api/v1/verify/{token}", verifyEmailHandler(cfg, tmpls, client, i18n)).Methods("GET")

	r.HandleFunc("/admin/users", adminUsersHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/users/bulk", adminUsersBulkHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/users/{id}/delete", adminUserDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/users/{id}/role", adminUserRoleHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/users/{id}/org", adminUserOrgHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/invites/new", adminInviteCreateHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/invites/{token}/revoke", adminInviteRevokeHandler(cfg, client)).Methods("POST")

	r.HandleFunc("/admin/events/{id}/bookings", adminBookingsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/bookings/{id}/approve", adminBookingApproveHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/bookings/{id}/cancel", adminBookingCancelHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/bookings/{id}/delete", adminBookingDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/events/{id}/delete", adminEventDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/events/{id}/image/delete", adminEventImageDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/musicians/{id}/image/delete", adminMusicianImageDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/image/delete", adminOrgImageDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/events", adminEventsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/events/new", adminEventNewPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/events/new", adminEventCreateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/events/{id}/edit", adminEventEditPageHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/events/{id}/edit", adminEventSaveHandler(cfg, tmpls, client, i18n)).Methods("POST")

	r.HandleFunc("/admin/organizations", adminOrgsHandler(cfg, tmpls, client, i18n)).Methods("GET")
	r.HandleFunc("/admin/organizations/check-actor-name", adminOrgCheckActorNameHandler(cfg, client)).Methods("GET")
	r.HandleFunc("/admin/organizations/new", adminOrgNewPageHandler(cfg, tmpls, i18n)).Methods("GET")
	r.HandleFunc("/admin/organizations/new", adminOrgCreateHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/edit", adminOrgEditPageHandler(cfg, tmpls, client, i18n, db)).Methods("GET")
	r.HandleFunc("/admin/organizations/{id}/edit", adminOrgSaveHandler(cfg, tmpls, client, i18n)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/delete", adminOrgDeleteHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/run-feeds", adminOrgRunFeedsHandler(cfg, client)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/follow", adminOrgFollowHandler(cfg, db, client)).Methods("POST")
	r.HandleFunc("/admin/organizations/{id}/unfollow", adminOrgUnfollowHandler(cfg, db, client)).Methods("POST")

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
	r.HandleFunc("/admin/locations/{id}/delete", adminLocationDeleteHandler(cfg, client)).Methods("POST")

	relayActor, err := ensureRelayActor(db)
	if err != nil {
		log.Printf("relay actor init: %v", err)
	}
	go startDelivery(cfg, db, client, relayActor)

	log.Printf("web server listening on %s (domain: %s)", cfg.Listen, cfg.Domain)
	if err := http.ListenAndServe(cfg.Listen, r); err != nil {
		log.Fatal(err)
	}
}
