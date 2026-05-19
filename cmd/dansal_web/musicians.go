package main

import (
	"encoding/json"
	"net/http"
	"strconv"

)

type MusiciansPageData struct {
	Musicians []Musician
}

type MusicianPageData struct {
	Musician Musician
	Events   []Event
	Slug     string
	Members  []string
	Albums   []string
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
		id, err := strconv.Atoi(r.PathValue("id"))
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
		var members, albums []string
		json.Unmarshal([]byte(musician.MembersJSON), &members)
		json.Unmarshal([]byte(musician.AlbumsJSON), &albums)
		title := musician.Bandname
		renderTemplate(w, tmpls.musician, tmplData(r, cfg, i18n, title, MusicianPageData{
			Musician: musician,
			Events:   events,
			Slug:     orgSlug(musician.Bandname),
			Members:  members,
			Albums:   albums,
		}))
	}
}
