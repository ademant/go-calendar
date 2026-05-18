package main

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type MusiciansPageData struct {
	Musicians []Musician
}

type MusicianPageData struct {
	Musician Musician
	Events   []Event
	Slug     string
}

func musiciansHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		musicians, err := client.GetMusicians(r.Context())
		if err != nil {
			http.Error(w, "could not load musicians", http.StatusBadGateway)
			return
		}
		title := i18n.T(r, "musicians_title")
		renderTemplate(w, tmpls.musicians, tmplData(r, cfg, i18n, title, MusiciansPageData{Musicians: musicians}))
	}
}

func musicianHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		events, _ := client.GetPublicEventsByMusician(r.Context(), id)
		title := musician.Bandname
		renderTemplate(w, tmpls.musician, tmplData(r, cfg, i18n, title, MusicianPageData{
			Musician: musician,
			Events:   events,
			Slug:     orgSlug(musician.Bandname),
		}))
	}
}
