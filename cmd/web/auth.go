package main

import (
	"net/http"
	"time"
)

type LoginPageData struct {
	ErrorKey string
	Username string
}

func loginPageHandler(cfg *Config, tmpls *Templates, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if getSessionUser(r) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		title := i18n.T(r, "login_title")
		renderTemplate(w, tmpls.login, tmplData(r, cfg, i18n, title, LoginPageData{}))
	}
}

func loginHandler(cfg *Config, tmpls *Templates, client *DansalClient, i18n *I18n) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		username := r.FormValue("username")
		password := r.FormValue("password")

		lr, err := client.Login(r.Context(), username, password)
		if err != nil {
			title := i18n.T(r, "login_title")
			renderTemplate(w, tmpls.login, tmplData(r, cfg, i18n, title, LoginPageData{
				ErrorKey: "login_error_invalid",
				Username: username,
			}))
			return
		}

		expiresAt, err := time.Parse(time.RFC3339, lr.ExpiresAt)
		if err != nil {
			expiresAt = time.Now().Add(24 * time.Hour)
		}

		setSession(w, lr.Token, SessionUser{
			ID:       lr.User.ID,
			Username: lr.User.Username,
			Role:     lr.User.Role,
		}, expiresAt)

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func logoutHandler(cfg *Config, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token := getSessionToken(r); token != "" {
			_ = client.Logout(r.Context(), token)
		}
		clearSession(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}
