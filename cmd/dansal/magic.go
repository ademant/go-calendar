package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// POST /api/v1/login/magic — request a magic login link.
// Body: {"username":"..."} or {"email":"..."}.
// Always returns 204 to prevent user enumeration.
// Returns 429 when the per-user rate limit is active.
func requestMagicLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Username == "" && req.Email == "" {
		http.Error(w, "username or email is required", http.StatusBadRequest)
		return
	}

	var user User
	var emailVerified int
	var lastMagicSentAt string

	var err error
	if req.Username != "" {
		err = db.QueryRow(
			"SELECT id, username, email, role, created_at, COALESCE(email_verified,0), COALESCE(last_magic_sent_at,'') FROM users WHERE username=?",
			req.Username,
		).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &emailVerified, &lastMagicSentAt)
	} else {
		err = db.QueryRow(
			"SELECT id, username, email, role, created_at, COALESCE(email_verified,0), COALESCE(last_magic_sent_at,'') FROM users WHERE email=?",
			req.Email,
		).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &emailVerified, &lastMagicSentAt)
	}

	if err == sql.ErrNoRows || emailVerified == 0 {
		// Anti-enumeration: do not reveal whether the user exists or has a verified email.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Per-user rate limit: enforce minimum seconds between magic link requests.
	rateSecs := config.Server.MagicLoginRateSecs
	if lastMagicSentAt != "" {
		if last, err := parseTokenExpiration(lastMagicSentAt); err == nil {
			retryAfter := int(time.Until(last.Add(time.Duration(rateSecs) * time.Second)).Seconds())
			if retryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				http.Error(w, "Too many magic link requests", http.StatusTooManyRequests)
				return
			}
		}
	}

	token, err := generateVerificationToken()
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(config.Server.MagicLoginExpirySecs) * time.Second)

	// Replace any existing unused magic token for this user.
	db.Exec("DELETE FROM magic_login_tokens WHERE user_id=?", user.ID)

	_, err = db.Exec(
		"INSERT INTO magic_login_tokens (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, user.ID, expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		http.Error(w, "Failed to create magic token", http.StatusInternalServerError)
		return
	}

	db.Exec("UPDATE users SET last_magic_sent_at=? WHERE id=?", now.Format(time.RFC3339), user.ID)

	magicURL := buildMagicBase(r) + "/api/v1/login/magic/" + token

	body := fmt.Sprintf(
		"Hello %s,\n\nclick the link below to log in without a password:\n\n  %s\n\nThis link expires in %d minutes and can only be used once.\n",
		user.Username, magicURL, config.Server.MagicLoginExpirySecs/60,
	)
	if err := SendEmail(user.Email, "Your login link", body); err != nil {
		db.Exec("DELETE FROM magic_login_tokens WHERE token=?", token)
		log.Printf("magic: send failed for user %d (%s): %v", user.ID, user.Username, err)
		http.Error(w, "Failed to send login link: "+err.Error(), http.StatusBadGateway)
		return
	}

	log.Printf("magic: sent login link to user %d (%s)", user.ID, user.Username)
	w.WriteHeader(http.StatusNoContent)
}

// buildMagicBase returns the base URL (scheme + host) for magic link construction.
func buildMagicBase(r *http.Request) string {
	base := config.Server.BaseURL
	if base != "" {
		return base
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// GET /api/v1/login/magic/{token} — consume a magic login token and issue a session.
func useMagicLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	token := mux.Vars(r)["token"]

	var id, userID int
	var expiresAt string
	err := db.QueryRow(
		"SELECT id, user_id, expires_at FROM magic_login_tokens WHERE token=?", token,
	).Scan(&id, &userID, &expiresAt)
	if err == sql.ErrNoRows {
		http.Error(w, "Invalid or expired login link", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	exp, err := parseTokenExpiration(expiresAt)
	if err != nil || time.Now().After(exp) {
		db.Exec("DELETE FROM magic_login_tokens WHERE id=?", id)
		http.Error(w, "Login link has expired", http.StatusGone)
		return
	}

	// Consume the token immediately to prevent replay.
	db.Exec("DELETE FROM magic_login_tokens WHERE id=?", id)

	// Load user; re-enable if disabled (magic link proves email ownership).
	var user User
	db.Exec("UPDATE users SET disabled=0, failed_login_count=0, failed_login_since=NULL WHERE id=?", userID)
	credentials.pruneByUserID(userID)

	err = db.QueryRow(
		"SELECT id, username, email, role, created_at FROM users WHERE id=?", userID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	clientIP := getClientIP(r)
	sessionToken, sessionExpiry, err := createTokenInDB(user.ID, r.UserAgent(), clientIP, "")
	if err != nil {
		log.Printf("magic: failed to create session for user %d: %v", user.ID, err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	log.Printf("magic: user %d (%s) logged in via magic link from %s", user.ID, user.Username, clientIP)
	json.NewEncoder(w).Encode(LoginResponse{
		Token:     sessionToken,
		ExpiresAt: sessionExpiry.Format(time.RFC3339),
		User:      user,
	})
}
