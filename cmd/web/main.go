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

	r := mux.NewRouter()

	r.HandleFunc("/.well-known/webfinger", webfingerHandler(cfg, db, client)).Methods("GET")
	r.HandleFunc("/.well-known/nodeinfo", nodeinfoIndexHandler(cfg)).Methods("GET")
	r.HandleFunc("/nodeinfo/2.0", nodeinfoHandler(cfg)).Methods("GET")

	r.HandleFunc("/org/{name}", actorOrFrontendHandler(cfg, tmpls, db, client)).Methods("GET")
	r.HandleFunc("/org/{name}/outbox", outboxHandler(cfg, db, client)).Methods("GET")
	r.HandleFunc("/org/{name}/followers", followersHandler(cfg, db)).Methods("GET")
	r.HandleFunc("/org/{name}/inbox", inboxHandler(cfg, db, client)).Methods("POST")

	r.HandleFunc("/", indexHandler(cfg, tmpls, client)).Methods("GET")
	r.HandleFunc("/events/{id}", eventHandler(cfg, tmpls, client)).Methods("GET")
	r.HandleFunc("/login", loginPageHandler(cfg, tmpls)).Methods("GET")
	r.HandleFunc("/login", loginHandler(cfg, tmpls, client)).Methods("POST")
	r.HandleFunc("/logout", logoutHandler(cfg, client)).Methods("POST")

	go startDelivery(cfg, db, client)

	log.Printf("web server listening on %s (domain: %s)", cfg.Listen, cfg.Domain)
	if err := http.ListenAndServe(cfg.Listen, r); err != nil {
		log.Fatal(err)
	}
}
