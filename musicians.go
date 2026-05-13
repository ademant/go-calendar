package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type Musician struct {
	ID           int    `json:"id"`
	Bandname     string `json:"bandname"`
	Internetsite string `json:"internetsite"`
	CreatedAt    string `json:"created_at"`
}

type MusicianCreateRequest struct {
	Bandname     string `json:"bandname"`
	Internetsite string `json:"internetsite"`
}

type MusicianUpdateRequest struct {
	Bandname     string `json:"bandname"`
	Internetsite string `json:"internetsite"`
}

// GET /api/v1/musicians - List all musicians
func getMusicians(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT id, bandname, internetsite, created_at FROM musicians")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	musicians := []Musician{}
	for rows.Next() {
		var musician Musician
		if err := rows.Scan(&musician.ID, &musician.Bandname, &musician.Internetsite, &musician.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		musicians = append(musicians, musician)
	}

	json.NewEncoder(w).Encode(musicians)
}

// GET /api/v1/musicians/{id} - Get single musician
func getMusician(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	var musician Musician
	err := db.QueryRow(
		"SELECT id, bandname, internetsite, created_at FROM musicians WHERE id = ?",
		id,
	).Scan(&musician.ID, &musician.Bandname, &musician.Internetsite, &musician.CreatedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "Musician not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(musician)
}

// POST /api/v1/musicians - Create musician
func createMusician(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req MusicianCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		"INSERT INTO musicians (bandname, internetsite) VALUES (?, ?)",
		req.Bandname, req.Internetsite,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	musician := Musician{
		ID:           int(id),
		Bandname:     req.Bandname,
		Internetsite: req.Internetsite,
	}

	err = db.QueryRow("SELECT created_at FROM musicians WHERE id = ?", id).Scan(&musician.CreatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(musician)
}

// PUT /api/v1/musicians/{id} - Update musician
func updateMusician(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	var req MusicianUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		"UPDATE musicians SET bandname = ?, internetsite = ? WHERE id = ?",
		req.Bandname, req.Internetsite, id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Musician not found", http.StatusNotFound)
		return
	}

	var musician Musician
	err = db.QueryRow(
		"SELECT id, bandname, internetsite, created_at FROM musicians WHERE id = ?",
		id,
	).Scan(&musician.ID, &musician.Bandname, &musician.Internetsite, &musician.CreatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(musician)
}

// DELETE /api/v1/musicians/{id} - Delete musician
func deleteMusician(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	result, err := db.Exec("DELETE FROM musicians WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Musician not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
