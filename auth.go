package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type Token struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      User   `json:"user"`
}

type TokenError struct {
	Error string `json:"error"`
}

// generateToken creates a secure random token
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// createTokenInDB stores the token in the database
func createTokenInDB(userID int) (string, time.Time, error) {
	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, err
	}

	// Use configured token expiration time
	expirationHours := 24 // default
	if config != nil && config.Server.TokenExpirationHours > 0 {
		expirationHours = config.Server.TokenExpirationHours
	}
	expiresAt := time.Now().UTC().Add(time.Duration(expirationHours) * time.Hour)
	expiresAtStr := expiresAt.Format(time.RFC3339Nano)

	_, err = db.Exec(
		"INSERT INTO tokens (user_id, token, expires_at) VALUES (?, ?, ?)",
		userID, token, expiresAtStr,
	)
	if err != nil {
		return "", time.Time{}, err
	}

	return token, expiresAt, nil
}

// validateToken checks if a token is valid and not expired
func validateToken(token string) (int, string, error) {
	var userID int
	var userRole string
	var expiresAt string

	err := db.QueryRow(
		"SELECT users.id, users.role, tokens.expires_at FROM tokens JOIN users ON tokens.user_id = users.id WHERE tokens.token = ?",
		token,
	).Scan(&userID, &userRole, &expiresAt)

	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("invalid token")
	}
	if err != nil {
		return 0, "", err
	}

	// Check if token is expired
	expTime, err := parseTokenExpiration(expiresAt)
	if err != nil {
		return 0, "", fmt.Errorf("invalid token expiration format")
	}

	if time.Now().After(expTime) {
		return 0, "", fmt.Errorf("token expired")
	}

	return userID, userRole, nil
}

func parseTokenExpiration(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported expiration format")
}

// GET /api/v1/login - Login endpoint to get OAuth token
func login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req LoginRequest

	// Parse request body based on content type
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TokenError{Error: "Invalid form data"})
			return
		}
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
	} else {
		// Default to JSON parsing
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TokenError{Error: "Invalid request body"})
			return
		}
	}

	// Validate input
	if req.Username == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TokenError{Error: "Username and password are required"})
		return
	}

	// Verify user credentials
	var user User
	var passwordHash string

	err := db.QueryRow(
		"SELECT id, username, email, role, created_at, password_hash FROM users WHERE username = ?",
		req.Username,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &passwordHash)

	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(TokenError{Error: "Invalid username or password"})
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TokenError{Error: "Internal server error"})
		return
	}

	// Verify password
	if hashPassword(req.Password) != passwordHash {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(TokenError{Error: "Invalid username or password"})
		return
	}

	// Generate token
	token, expiresAt, err := createTokenInDB(user.ID)
	if err != nil {
		log.Printf("Error creating token: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TokenError{Error: "Failed to create token"})
		return
	}

	// Return token and user info
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(LoginResponse{
		Token:     token,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		User:      user,
	})
}

// TokenMiddleware validates the token in the Authorization header
func TokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(TokenError{Error: "Authorization header missing"})
			return
		}

		// Extract token from "Bearer <token>" format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(TokenError{Error: "Invalid authorization header format. Use 'Bearer <token>'"})
			return
		}

		token := parts[1]

		// Validate token
		userID, userRole, err := validateToken(token)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(TokenError{Error: fmt.Sprintf("Invalid token: %v", err)})
			return
		}

		// Store userID and role in request header for later use
		r.Header.Set("X-User-ID", fmt.Sprintf("%d", userID))
		r.Header.Set("X-User-Role", userRole)

		next.ServeHTTP(w, r)
	})
}
