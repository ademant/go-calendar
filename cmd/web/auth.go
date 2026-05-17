package main

import (
	"net/http"
	"time"
)

type LoginPageData struct {
	Error    string
	Username string
}

func loginPageHandler(cfg *Config, tmpls *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if getSessionUser(r) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		renderTemplate(w, tmpls.login, tmplData(r, cfg, "Login", LoginPageData{}))
	}
}

func loginHandler(cfg *Config, tmpls *Templates, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		username := r.FormValue("username")
		password := r.FormValue("password")

		lr, err := client.Login(r.Context(), username, password)
		if err != nil {
			renderTemplate(w, tmpls.login, tmplData(r, cfg, "Login", LoginPageData{
				Error:    "Invalid username or password.",
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
