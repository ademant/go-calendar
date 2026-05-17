package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// buildVerifyURL constructs the verification link. If base_url is configured
// it is used as prefix; otherwise the URL is inferred from the request.
func buildVerifyURL(r *http.Request, token string) string {
	base := strings.TrimRight(config.Server.BaseURL, "/")
	if base == "" {
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/api/v1/verify/" + token
}

func generateVerificationToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// POST /api/v1/users/{id}/verify — generate and send a verification link.
// Callers may only verify their own account unless they are admin.
func sendVerification(w http.ResponseWriter, r *http.Request) {
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	callerRole := r.Header.Get("X-User-Role")

	targetID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}
	if callerID != targetID && callerRole != RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Channel string `json:"channel"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Channel == "" {
		http.Error(w, "channel is required (email, telegram, matrix)", http.StatusBadRequest)
		return
	}
	if req.Channel != "email" && req.Channel != "telegram" && req.Channel != "matrix" {
		http.Error(w, "channel must be one of: email, telegram, matrix", http.StatusBadRequest)
		return
	}

	var user User
	var emailVer, telegramVer, matrixVer, disabled int
	err = db.QueryRow(
		"SELECT id, username, email, role, COALESCE(telegram,''), COALESCE(matrix,''), COALESCE(email_verified,0), COALESCE(telegram_verified,0), COALESCE(matrix_verified,0), COALESCE(disabled,0), created_at FROM users WHERE id=?",
		targetID,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.Telegram, &user.Matrix,
		&emailVer, &telegramVer, &matrixVer, &disabled, &user.CreatedAt)
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch req.Channel {
	case "email":
		if user.Email == "" {
			http.Error(w, "User has no email address", http.StatusBadRequest)
			return
		}
	case "telegram":
		if user.Telegram == "" {
			http.Error(w, "User has no Telegram handle", http.StatusBadRequest)
			return
		}
	case "matrix":
		if user.Matrix == "" {
			http.Error(w, "User has no Matrix ID", http.StatusBadRequest)
			return
		}
	}

	// Replace any existing pending token for this user+channel.
	db.Exec("DELETE FROM verification_tokens WHERE user_id=? AND channel=?", targetID, req.Channel)

	token, err := generateVerificationToken()
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}
	expiresAt := time.Now().UTC().Add(time.Duration(config.Server.VerificationExpiryHours) * time.Hour)
	_, err = db.Exec(
		"INSERT INTO verification_tokens (token, user_id, channel, expires_at) VALUES (?, ?, ?, ?)",
		token, targetID, req.Channel, expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		http.Error(w, "Failed to create verification token", http.StatusInternalServerError)
		return
	}

	var vURL string
	if req.BaseURL != "" {
		vURL = strings.TrimRight(req.BaseURL, "/") + "/api/v1/verify/" + token
	} else {
		vURL = buildVerifyURL(r, token)
	}

	var sendErr error
	switch req.Channel {
	case "email":
		sendErr = sendEmailVerification(user, vURL)
	case "telegram":
		sendErr = sendTelegramVerification(user, vURL)
	case "matrix":
		sendErr = sendMatrixVerification(user, vURL)
	}
	if sendErr != nil {
		db.Exec("DELETE FROM verification_tokens WHERE token=?", token)
		log.Printf("verify: send failed for user %d channel %s: %v", targetID, req.Channel, sendErr)
		http.Error(w, "Failed to send verification: "+sendErr.Error(), http.StatusBadGateway)
		return
	}

	log.Printf("verify: sent %s verification to user %d (%s)", req.Channel, targetID, user.Username)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/verify/{token} — public; marks the account verified and consumes the token.
func consumeVerification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	token := mux.Vars(r)["token"]

	var id, userID int
	var channel, expiresAt string
	err := db.QueryRow(
		"SELECT id, user_id, channel, expires_at FROM verification_tokens WHERE token=?", token,
	).Scan(&id, &userID, &channel, &expiresAt)
	if err == sql.ErrNoRows {
		http.Error(w, "Invalid or expired verification link", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	exp, err := parseTokenExpiration(expiresAt)
	if err != nil || time.Now().After(exp) {
		db.Exec("DELETE FROM verification_tokens WHERE id=?", id)
		http.Error(w, "Verification link has expired", http.StatusGone)
		return
	}

	col := map[string]string{
		"email":    "email_verified",
		"telegram": "telegram_verified",
		"matrix":   "matrix_verified",
	}[channel]
	if col == "" {
		http.Error(w, "Unknown channel", http.StatusInternalServerError)
		return
	}

	db.Exec(fmt.Sprintf("UPDATE users SET %s=1 WHERE id=?", col), userID)
	db.Exec("DELETE FROM verification_tokens WHERE id=?", id)
	log.Printf("verify: %s verified for user %d", channel, userID)

	json.NewEncoder(w).Encode(map[string]string{"channel": channel, "status": "verified"})
}

func sendEmailVerification(user User, verifyURL string) error {
	body := fmt.Sprintf(
		"Hello %s,\n\nplease verify your email address:\n\n  %s\n\nThis link expires in %d hours.\n",
		user.Username, verifyURL, config.Server.VerificationExpiryHours,
	)
	return SendEmail(user.Email, "Verify your email address", body)
}

func sendTelegramVerification(user User, verifyURL string) error {
	botToken := config.Server.TelegramBotToken
	if botToken == "" {
		return fmt.Errorf("telegram_bot_token not configured")
	}
	text := fmt.Sprintf(
		"Hello %s, please verify your Telegram account:\n\n%s\n\nThis link expires in %d hours.",
		user.Username, verifyURL, config.Server.VerificationExpiryHours,
	)
	payload, _ := json.Marshal(map[string]any{
		"chat_id": user.Telegram,
		"text":    text,
	})
	apiURL := "https://api.telegram.org/bot" + botToken + "/sendMessage"
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("Telegram API: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if !result.OK {
		return fmt.Errorf("Telegram API: %s", result.Description)
	}
	return nil
}

func sendMatrixVerification(user User, verifyURL string) error {
	homeserver := strings.TrimRight(config.Server.MatrixHomeserver, "/")
	accessToken := config.Server.MatrixAccessToken
	if homeserver == "" || accessToken == "" {
		return fmt.Errorf("matrix_homeserver or matrix_access_token not configured")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// Create a direct message room and invite the user.
	createBody, _ := json.Marshal(map[string]any{
		"is_direct": true,
		"invite":    []string{user.Matrix},
		"preset":    "trusted_private_chat",
	})
	req, _ := http.NewRequest("POST", homeserver+"/_matrix/client/v3/createRoom", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Matrix createRoom: %w", err)
	}
	defer resp.Body.Close()

	var roomResult struct {
		RoomID string `json:"room_id"`
		Error  string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&roomResult)
	if roomResult.RoomID == "" {
		return fmt.Errorf("Matrix createRoom failed: %s", roomResult.Error)
	}

	// Send the verification message.
	txnID := strconv.FormatInt(time.Now().UnixNano(), 10)
	sendURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		homeserver, url.PathEscape(roomResult.RoomID), txnID)
	msgBody, _ := json.Marshal(map[string]any{
		"msgtype": "m.text",
		"body": fmt.Sprintf(
			"Hello %s, please verify your Matrix account:\n\n%s\n\nThis link expires in %d hours.",
			user.Username, verifyURL, config.Server.VerificationExpiryHours,
		),
	})
	req2, _ := http.NewRequest("PUT", sendURL, bytes.NewReader(msgBody))
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := client.Do(req2)
	if err != nil {
		return fmt.Errorf("Matrix send message: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 300 {
		return fmt.Errorf("Matrix send message: HTTP %d", resp2.StatusCode)
	}
	return nil
}
