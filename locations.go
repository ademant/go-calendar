package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type Location struct {
	ID           int    `json:"id"`
	Location     string `json:"location"`
	Address      string `json:"address"`
	Zipcode      string `json:"zipcode"`
	Town         string `json:"town"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
	Internetsite string `json:"internetsite"`
	CreatedAt    string `json:"created_at"`
}

type LocationCreateRequest struct {
	Location     string `json:"location"`
	Address      string `json:"address"`
	Zipcode      string `json:"zipcode"`
	Town         string `json:"town"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
	Internetsite string `json:"internetsite"`
}

type LocationUpdateRequest struct {
	Address      string `json:"address"`
	Zipcode      string `json:"zipcode"`
	Town         string `json:"town"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
	Internetsite string `json:"internetsite"`
}

// GET /api/v1/locations - List all locations
func getLocations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT id, location, address, zipcode, town, latitude, longitude, internetsite, created_at FROM locations")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	locations := []Location{}
	for rows.Next() {
		var location Location
		if err := rows.Scan(&location.ID, &location.Location, &location.Address, &location.Zipcode, &location.Town, &location.Latitude, &location.Longitude, &location.Internetsite, &location.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		locations = append(locations, location)
	}

	json.NewEncoder(w).Encode(locations)
}

// POST /api/v1/locations - Create a new location
func createLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req LocationCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate: location is mandatory
	if req.Location == "" {
		http.Error(w, "Location is required", http.StatusBadRequest)
		return
	}

	// Insert location
	result, err := db.Exec(
		"INSERT INTO locations (location, address, zipcode, town, latitude, longitude, internetsite) VALUES (?, ?, ?, ?, ?, ?, ?)",
		req.Location, req.Address, req.Zipcode, req.Town, req.Latitude, req.Longitude, req.Internetsite,
	)
	if err != nil {
		http.Error(w, "Failed to create location", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	location := Location{
		ID:           int(id),
		Location:     req.Location,
		Address:      req.Address,
		Zipcode:      req.Zipcode,
		Town:         req.Town,
		Latitude:     req.Latitude,
		Longitude:    req.Longitude,
		Internetsite: req.Internetsite,
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(location)
}

// GET /api/v1/locations/{id} - Get a specific location
func getLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	var location Location
	err := db.QueryRow(
		"SELECT id, location, address, zipcode, town, latitude, longitude, internetsite, created_at FROM locations WHERE id = ?",
		id,
	).Scan(&location.ID, &location.Location, &location.Address, &location.Zipcode, &location.Town, &location.Latitude, &location.Longitude, &location.Internetsite, &location.CreatedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "Location not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(location)
}

// PUT /api/v1/locations/{id} - Update location
func updateLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	if requesterRole != RoleAdmin {
		http.Error(w, "Forbidden: only admins may update locations", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var req LocationUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if location exists
	var location Location
	err := db.QueryRow("SELECT id, location, address, zipcode, town, latitude, longitude, internetsite, created_at FROM locations WHERE id = ?", id).
		Scan(&location.ID, &location.Location, &location.Address, &location.Zipcode, &location.Town, &location.Latitude, &location.Longitude, &location.Internetsite, &location.CreatedAt)
	if err == sql.ErrNoRows {
		http.Error(w, "Location not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update fields if provided
	if req.Address != "" {
		location.Address = req.Address
	}
	if req.Zipcode != "" {
		location.Zipcode = req.Zipcode
	}
	if req.Town != "" {
		location.Town = req.Town
	}
	if req.Latitude != "" {
		location.Latitude = req.Latitude
	}
	if req.Longitude != "" {
		location.Longitude = req.Longitude
	}
	if req.Internetsite != "" {
		location.Internetsite = req.Internetsite
	}

	// Execute update
	_, err = db.Exec(
		"UPDATE locations SET address = ?, zipcode = ?, town = ?, latitude = ?, longitude = ?, internetsite = ? WHERE id = ?",
		location.Address, location.Zipcode, location.Town, location.Latitude, location.Longitude, location.Internetsite, id,
	)
	if err != nil {
		http.Error(w, "Failed to update location", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(location)
}

// DELETE /api/v1/locations/{id} - Delete a location
func deleteLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	if requesterRole != RoleAdmin {
		http.Error(w, "Forbidden: only admins may delete locations", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	// Check if location exists
	var locationID int
	err := db.QueryRow("SELECT id FROM locations WHERE id = ?", id).Scan(&locationID)
	if err == sql.ErrNoRows {
		http.Error(w, "Location not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Delete location
	result, err := db.Exec("DELETE FROM locations WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Location not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
