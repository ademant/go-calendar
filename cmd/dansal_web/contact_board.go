package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// POST /events/{id}/board
func contactBoardPostHandler(cfg *Config, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eventID, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/events/%d?board_error=board_form_error", eventID), http.StatusSeeOther)
			return
		}

		persons, _ := strconv.Atoi(r.FormValue("persons"))
		if persons < 1 {
			persons = 1
		}

		post := map[string]interface{}{
			"type":     r.FormValue("type"),
			"city":     r.FormValue("city"),
			"persons":  persons,
			"message":  r.FormValue("message"),
			"nickname": r.FormValue("nickname"),
			"email":    r.FormValue("email"),
		}

		if err := client.CreateContactPost(r.Context(), eventID, post); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/events/%d?board_error=board_post_error", eventID), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/events/%d?board_posted=1", eventID), http.StatusSeeOther)
	}
}

// POST /events/{id}/board/{post_id}/delete
func contactBoardDeleteHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eventID, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		postID, err := strconv.Atoi(mux.Vars(r)["post_id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		su, ok := requireLogin(w, r)
		if !ok {
			return
		}
		_ = su
		token := getSessionToken(r)

		if err := client.DeleteContactPost(r.Context(), postID, token); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/events/%d?board_error=board_delete_error", eventID), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/events/%d", eventID), http.StatusSeeOther)
	}
}

// POST /events/{id}/board/{post_id}/contact
func contactBoardContactHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eventID, err := strconv.Atoi(mux.Vars(r)["id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		postID, err := strconv.Atoi(mux.Vars(r)["post_id"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/events/%d?board_error=board_form_error", eventID), http.StatusSeeOther)
			return
		}

		email := r.FormValue("email")
		message := r.FormValue("message")

		if err := client.ContactPoster(r.Context(), postID, email, message); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/events/%d?board_error=board_contact_error", eventID), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/events/%d?board_contacted=1", eventID), http.StatusSeeOther)
	}
}

// GET /contact-posts/verify/{token}
func contactBoardVerifyHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := mux.Vars(r)["token"]
		if err := client.VerifyContactPost(r.Context(), token); err != nil {
			title := i18n.T(r, "verify_title")
			renderTemplate(w, tmpls.verify, tmplData(r, cfg, i18n, title, VerifyData{ErrorKey: "verify_error_invalid"}))
			return
		}
		title := i18n.T(r, "board_verify_title")
		renderTemplate(w, tmpls.verify, tmplData(r, cfg, i18n, title, VerifyData{Success: true}))
	}
}
