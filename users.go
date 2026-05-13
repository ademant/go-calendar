package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

const (
	RoleAdmin  = "admin"
	RoleUser   = "user"
	RoleViewer = "viewer"
)

type User struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type UserCreateRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type UserUpdateRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// hashPassword creates a SHA256 hash of the password
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%x", hash)
}

// validateRole checks if the role is valid
func validateRole(role string) bool {
	return role == RoleAdmin || role == RoleUser || role == RoleViewer
}

// generateRandomPassword generates a random password of specified length
func generateRandomPassword(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*"
	password := make([]byte, length)
	for i := range password {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		password[i] = charset[num.Int64()]
	}
	return string(password)
}

// adminUserExists checks if an admin user already exists
func adminUserExists(db *sql.DB) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = ?", RoleAdmin).Scan(&count)
	return err == nil && count > 0
}

// ensureAdminUser creates an admin user if none exists
func ensureAdminUser(db *sql.DB) {
	if adminUserExists(db) {
		log.Println("Admin user already exists")
		return
	}

	// Generate random password
	password := generateRandomPassword(16)
	passwordHash := hashPassword(password)

	// Create admin user
	_, err := db.Exec(
		"INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, ?)",
		"admin", "admin@localhost", passwordHash, RoleAdmin,
	)
	if err != nil {
		log.Printf("Warning: Could not create initial admin user: %v\n", err)
		return
	}

	log.Println("========================================")
	log.Println("INITIAL ADMIN USER CREATED")
	log.Println("========================================")
	log.Printf("Username: admin\n")
	log.Printf("Email: admin@localhost\n")
	log.Printf("Password: %s\n", password)
	log.Println("========================================")
	log.Println("Please save this password in a secure location.")
	log.Println("========================================")
}

// GET /api/v1/users - List all users
func getUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT id, username, email, role, created_at FROM users")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		users = append(users, user)
	}

	json.NewEncoder(w).Encode(users)
}

// POST /api/v1/users - Create a new user
func createUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req UserCreateRequest

	// Parse request body based on content type
	contentType := r.Header.Get("Content-Type")
	if contentType == "application/x-www-form-urlencoded" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		req.Username = r.FormValue("username")
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")
		req.Role = r.FormValue("role")
	} else {
		// Default to JSON parsing
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Validate input
	if req.Username == "" || req.Email == "" || req.Password == "" {
		http.Error(w, "Username, email, and password are required", http.StatusBadRequest)
		return
	}

	// Set default role if not provided
	if req.Role == "" {
		req.Role = RoleUser
	}

	if !validateRole(req.Role) {
		http.Error(w, "Invalid role. Allowed values: admin, user, viewer", http.StatusBadRequest)
		return
	}

	// Hash password
	passwordHash := hashPassword(req.Password)

	// Insert user
	result, err := db.Exec(
		"INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, ?)",
		req.Username, req.Email, passwordHash, req.Role,
	)
	if err != nil {
		http.Error(w, "Username or email already exists", http.StatusConflict)
		return
	}

	id, _ := result.LastInsertId()
	user := User{
		ID:       int(id),
		Username: req.Username,
		Email:    req.Email,
		Role:     req.Role,
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

// GET /api/v1/users/{id} - Get a specific user
func getUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	var user User
	err := db.QueryRow(
		"SELECT id, username, email, role, created_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(user)
}

// PUT /api/v1/users/{id} - Update user (email and role)
func updateUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]
	targetID, err := strconv.Atoi(id)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	requesterIDStr := r.Header.Get("X-User-ID")
	requesterRole := r.Header.Get("X-User-Role")
	requesterID, err := strconv.Atoi(requesterIDStr)
	if err != nil {
		http.Error(w, "Invalid requester information", http.StatusUnauthorized)
		return
	}

	var req UserUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate role if provided
	if req.Role != "" && !validateRole(req.Role) {
		http.Error(w, "Invalid role. Allowed values: admin, user, viewer", http.StatusBadRequest)
		return
	}

	// Only admin or the user themself can update user details
	if requesterRole != RoleAdmin && requesterID != targetID {
		http.Error(w, "Forbidden: only the user or an admin may update this account", http.StatusForbidden)
		return
	}

	// Regular users may not change their own role
	if req.Role != "" && requesterRole != RoleAdmin {
		http.Error(w, "Forbidden: only admin may change role", http.StatusForbidden)
		return
	}

	// Check if user exists
	var user User
	err = db.QueryRow("SELECT id, username, email, role, created_at FROM users WHERE id = ?", id).
		Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.CreatedAt)
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update email if provided
	if req.Email != "" {
		user.Email = req.Email
	}

	// Update role if provided
	if req.Role != "" {
		user.Role = req.Role
	}

	// Execute update
	_, err = db.Exec(
		"UPDATE users SET email = ?, role = ? WHERE id = ?",
		user.Email, user.Role, id,
	)
	if err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(user)
}

// DELETE /api/v1/users/{id} - Delete a user
func deleteUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	if requesterRole != RoleAdmin {
		http.Error(w, "Forbidden: only admins may delete users", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	// Check if user exists and prevent deletion of admin accounts
	var userID int
	var userRole string
	err := db.QueryRow("SELECT id, role FROM users WHERE id = ?", id).Scan(&userID, &userRole)
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if userRole == RoleAdmin {
		http.Error(w, "Forbidden: admin users may not be deleted", http.StatusForbidden)
		return
	}

	// Delete user
	result, err := db.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
