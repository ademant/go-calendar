package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin     = "admin"
	RoleUser      = "user"
	RolePublisher = "publisher"
	RoleViewer    = "viewer"
)

type User struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Telegram  string `json:"telegram,omitempty"`
	Matrix    string `json:"matrix,omitempty"`
	CreatedAt string `json:"created_at"`
}

type UserCreateRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Telegram string `json:"telegram"`
	Matrix   string `json:"matrix"`
}

type UserUpdateRequest struct {
	Email    string `json:"email"`
	Role     string `json:"role"`
	Telegram string `json:"telegram"`
	Matrix   string `json:"matrix"`
}

func isReservedUsername(username string) bool {
	lower := strings.ToLower(username)
	for _, r := range config.Server.ReservedUsernames {
		if strings.ToLower(r) == lower {
			return true
		}
	}
	return false
}

// passwordBytes returns the bcrypt input for password.
// SHA-256 collapses the input to 32 bytes, preventing bcrypt's silent 72-byte truncation.
func passwordBytes(password string) []byte {
	h := sha256.Sum256([]byte(password))
	return h[:]
}

// hashPassword hashes password with bcrypt (DefaultCost) over a SHA-256 digest.
func hashPassword(password string) string {
	h, err := bcrypt.GenerateFromPassword(passwordBytes(password), bcrypt.DefaultCost)
	if err != nil {
		sum := sha256.Sum256([]byte(password))
		return fmt.Sprintf("%x", sum)
	}
	return string(h)
}

// checkPassword verifies password against stored hash. Three cases are handled:
//   - bcrypt with SHA-256 pre-hash (current format, migrate=false)
//   - bcrypt with raw password (previous format, migrate=true so caller re-hashes)
//   - legacy hex SHA-256 (migrate=true)
func checkPassword(password, stored string) (ok, migrate bool) {
	if strings.HasPrefix(stored, "$2") {
		if bcrypt.CompareHashAndPassword([]byte(stored), passwordBytes(password)) == nil {
			return true, false
		}
		// Older bcrypt hash created before SHA-256 pre-hashing was introduced.
		if bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) == nil {
			return true, true
		}
		return false, false
	}
	// Legacy hex SHA-256 hash.
	sum := sha256.Sum256([]byte(password))
	if fmt.Sprintf("%x", sum) == stored {
		return true, true
	}
	return false, false
}

// validateRole checks if the role is valid
func validateRole(role string) bool {
	return role == RoleAdmin || role == RoleUser || role == RolePublisher || role == RoleViewer
}


// GET /api/v1/users - List all users
func getUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT id, username, email, role, COALESCE(telegram,''), COALESCE(matrix,''), created_at FROM users")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.Telegram, &user.Matrix, &user.CreatedAt); err != nil {
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

	if isReservedUsername(req.Username) {
		http.Error(w, "Username is reserved", http.StatusBadRequest)
		return
	}

	// Set default role if not provided
	if req.Role == "" {
		req.Role = RoleUser
	}

	if !validateRole(req.Role) {
		http.Error(w, "Invalid role. Allowed values: admin, user, publisher, viewer", http.StatusBadRequest)
		return
	}

	// Hash password
	passwordHash := hashPassword(req.Password)

	// Insert user
	result, err := db.Exec(
		"INSERT INTO users (username, email, password_hash, role, telegram, matrix) VALUES (?, ?, ?, ?, ?, ?)",
		req.Username, req.Email, passwordHash, req.Role, req.Telegram, req.Matrix,
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
		Telegram: req.Telegram,
		Matrix:   req.Matrix,
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
		"SELECT id, username, email, role, COALESCE(telegram,''), COALESCE(matrix,''), created_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.Telegram, &user.Matrix, &user.CreatedAt)

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
		http.Error(w, "Invalid role. Allowed values: admin, user, publisher, viewer", http.StatusBadRequest)
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
	err = db.QueryRow("SELECT id, username, email, role, COALESCE(telegram,''), COALESCE(matrix,''), created_at FROM users WHERE id = ?", id).
		Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.Telegram, &user.Matrix, &user.CreatedAt)
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Telegram != "" {
		user.Telegram = req.Telegram
	}
	if req.Matrix != "" {
		user.Matrix = req.Matrix
	}

	_, err = db.Exec(
		"UPDATE users SET email = ?, role = ?, telegram = ?, matrix = ? WHERE id = ?",
		user.Email, user.Role, user.Telegram, user.Matrix, id,
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
