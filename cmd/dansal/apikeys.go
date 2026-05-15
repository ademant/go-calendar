package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type APIKey struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Name      string `json:"name"`
	Key       string `json:"key,omitempty"`
	CreatedAt string `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Name   string `json:"name"`
	UserID *int   `json:"user_id,omitempty"`
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ak_" + base64.URLEncoding.EncodeToString(b), nil
}

func validateAPIKey(key string) (int, string, error) {
	var userID int
	var userRole string

	err := db.QueryRow(
		"SELECT users.id, users.role FROM api_keys JOIN users ON api_keys.user_id = users.id WHERE api_keys.api_key = ?",
		key,
	).Scan(&userID, &userRole)

	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("invalid api key")
	}
	if err != nil {
		return 0, "", err
	}

	return userID, userRole, nil
}

func listAPIKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	callerRole := r.Header.Get("X-User-Role")
	callerIDStr := r.Header.Get("X-User-ID")
	callerID, _ := strconv.Atoi(callerIDStr)

	var rows *sql.Rows
	var err error

	if callerRole == RoleAdmin {
		rows, err = db.Query("SELECT id, user_id, name, created_at FROM api_keys ORDER BY id")
	} else {
		rows, err = db.Query("SELECT id, user_id, name, created_at FROM api_keys WHERE user_id = ? ORDER BY id", callerID)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to fetch API keys"})
		return
	}
	defer rows.Close()

	keys := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.CreatedAt); err != nil {
			continue
		}
		keys = append(keys, k)
	}

	json.NewEncoder(w).Encode(keys)
}

func createAPIKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	callerRole := r.Header.Get("X-User-Role")
	callerIDStr := r.Header.Get("X-User-ID")
	callerID, _ := strconv.Atoi(callerIDStr)

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	targetUserID := callerID
	if req.UserID != nil {
		if callerRole != RoleAdmin && *req.UserID != callerID {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "Only admins can create API keys for other users"})
			return
		}
		targetUserID = *req.UserID
	}

	key, err := generateAPIKey()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate API key"})
		return
	}

	var id int
	var createdAt string
	err = db.QueryRow(
		"INSERT INTO api_keys (user_id, name, api_key) VALUES (?, ?, ?) RETURNING id, created_at",
		targetUserID, req.Name, key,
	).Scan(&id, &createdAt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create API key"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(APIKey{
		ID:        id,
		UserID:    targetUserID,
		Name:      req.Name,
		Key:       key,
		CreatedAt: createdAt,
	})
}

func deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	callerRole := r.Header.Get("X-User-Role")
	callerIDStr := r.Header.Get("X-User-ID")
	callerID, _ := strconv.Atoi(callerIDStr)

	vars := mux.Vars(r)
	keyID, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid API key ID"})
		return
	}

	var ownerID int
	err = db.QueryRow("SELECT user_id FROM api_keys WHERE id = ?", keyID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "API key not found"})
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	if callerRole != RoleAdmin && callerID != ownerID {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Cannot delete another user's API key"})
		return
	}

	if _, err = db.Exec("DELETE FROM api_keys WHERE id = ?", keyID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to delete API key"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
