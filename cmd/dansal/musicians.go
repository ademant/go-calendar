package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

type Musician struct {
	ID           int    `json:"id"`
	Bandname     string `json:"bandname"`
	ShortName    string `json:"short_name,omitempty"`
	Internetsite string `json:"internetsite,omitempty"`
	Description  string `json:"description,omitempty"`
	MBID         string `json:"mbid,omitempty"`
	Mastodon     string `json:"mastodon,omitempty"`
	Instagram    string `json:"instagram,omitempty"`
	Facebook     string `json:"facebook,omitempty"`
	Soundcloud   string `json:"soundcloud,omitempty"`
	CreatedAt    string `json:"created_at"`
}

type MusicianCreateRequest struct {
	Bandname     string `json:"bandname"`
	ShortName    string `json:"short_name"`
	Internetsite string `json:"internetsite"`
	Description  string `json:"description"`
	MBID         string `json:"mbid"`
	Mastodon     string `json:"mastodon"`
	Instagram    string `json:"instagram"`
	Facebook     string `json:"facebook"`
	Soundcloud   string `json:"soundcloud"`
}

type MusicianUpdateRequest struct {
	Bandname     string `json:"bandname"`
	ShortName    string `json:"short_name"`
	Internetsite string `json:"internetsite"`
	Description  string `json:"description"`
	MBID         string `json:"mbid"`
	Mastodon     string `json:"mastodon"`
	Instagram    string `json:"instagram"`
	Facebook     string `json:"facebook"`
	Soundcloud   string `json:"soundcloud"`
}

const musicianCols = `id, bandname,
	COALESCE(short_name,''), COALESCE(internetsite,''), COALESCE(description,''),
	COALESCE(mbid,''), COALESCE(mastodon,''), COALESCE(instagram,''),
	COALESCE(facebook,''), COALESCE(soundcloud,''), created_at`

func scanMusician(row interface{ Scan(...any) error }) (Musician, error) {
	var m Musician
	return m, row.Scan(&m.ID, &m.Bandname, &m.ShortName, &m.Internetsite, &m.Description,
		&m.MBID, &m.Mastodon, &m.Instagram, &m.Facebook, &m.Soundcloud, &m.CreatedAt)
}

// GET /api/v1/musicians - List all musicians
func getMusicians(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT " + musicianCols + " FROM musicians ORDER BY bandname")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	musicians := []Musician{}
	for rows.Next() {
		m, err := scanMusician(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		musicians = append(musicians, m)
	}

	json.NewEncoder(w).Encode(musicians)
}

// GET /api/v1/musicians/{id} - Get single musician
func getMusician(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := mux.Vars(r)["id"]
	musician, err := scanMusician(db.QueryRow("SELECT "+musicianCols+" FROM musicians WHERE id = ?", id))
	if err == sql.ErrNoRows {
		http.Error(w, "Musician not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(musician)
}

// POST /api/v1/musicians - Create one or more musicians
func createMusician(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	if requesterRole != RoleAdmin && requesterRole != RoleUser {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.As(err, new(*http.MaxBytesError)) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}

	var reqs []MusicianCreateRequest
	if json.Unmarshal(body, &reqs) != nil || len(reqs) == 0 || reqs[0].Bandname == "" {
		var single MusicianCreateRequest
		if err := json.Unmarshal(body, &single); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		reqs = []MusicianCreateRequest{single}
	}

	musicians := make([]Musician, 0, len(reqs))
	for _, req := range reqs {
		m, err := scanMusician(db.QueryRow(
			`INSERT INTO musicians (bandname, short_name, internetsite, description, mbid, mastodon, instagram, facebook, soundcloud)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING `+musicianCols,
			req.Bandname, req.ShortName, req.Internetsite, req.Description, req.MBID,
			req.Mastodon, req.Instagram, req.Facebook, req.Soundcloud,
		))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		musicians = append(musicians, m)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(musicians)
}

// PUT /api/v1/musicians/{id} - Update musician
func updateMusician(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	if requesterRole != RoleAdmin && requesterRole != RoleUser {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]

	var req MusicianUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		`UPDATE musicians SET bandname=?, short_name=?, internetsite=?, description=?, mbid=?,
		 mastodon=?, instagram=?, facebook=?, soundcloud=? WHERE id=?`,
		req.Bandname, req.ShortName, req.Internetsite, req.Description, req.MBID,
		req.Mastodon, req.Instagram, req.Facebook, req.Soundcloud, id,
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

	musician, err := scanMusician(db.QueryRow("SELECT "+musicianCols+" FROM musicians WHERE id = ?", id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(musician)
}

// DELETE /api/v1/musicians/{id} - Delete musician
func deleteMusician(w http.ResponseWriter, r *http.Request) {
	requesterRole := r.Header.Get("X-User-Role")
	if requesterRole != RoleAdmin && requesterRole != RoleUser {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]

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
