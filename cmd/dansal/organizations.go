package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	ics "github.com/arran4/golang-ical"
	"github.com/gorilla/mux"
)

type Organization struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ActorName    string `json:"actor_name,omitempty"`
	Website      string `json:"website,omitempty"`
	Instagram    string `json:"instagram,omitempty"`
	Mastodon     string `json:"mastodon,omitempty"`
	Facebook     string `json:"facebook,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	CreatedAt    string `json:"created_at"`
	ImageURL     string `json:"image_url,omitempty"`
}

type OrganizationMember struct {
	OrganizationID int    `json:"organization_id"`
	UserID         int    `json:"user_id"`
	Username       string `json:"username,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type CreateOrganizationRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ActorName    string `json:"actor_name"`
	Website      string `json:"website"`
	Instagram    string `json:"instagram"`
	Mastodon     string `json:"mastodon"`
	Facebook     string `json:"facebook"`
	ContactEmail string `json:"contact_email"`
}

type AddMemberRequest struct {
	UserID int `json:"user_id"`
}

// ensureOrgFromOrganizer finds or creates an organization from a vevent's ORGANIZER property.
// Prefers the CN parameter as the org name; falls back to the value with "mailto:" stripped.
// Returns nil when no usable ORGANIZER is present or on any DB error.
func ensureOrgFromOrganizer(vevent *ics.VEvent) *int {
	prop := vevent.GetProperty(ics.ComponentPropertyOrganizer)
	if prop == nil {
		return nil
	}

	name := ""
	if cn := prop.ICalParameters[string(ics.ParameterCn)]; len(cn) > 0 {
		name = strings.TrimSpace(cn[0])
	}
	if name == "" {
		name = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(prop.Value), "mailto:"))
	}
	if name == "" {
		return nil
	}

	var id int
	err := db.QueryRow("SELECT id FROM organizations WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		err = db.QueryRow(
			"INSERT INTO organizations (name) VALUES (?) RETURNING id", name,
		).Scan(&id)
	}
	if err != nil {
		return nil
	}
	return &id
}

// ensureOrgByName finds or creates an organization by name.
// Returns nil when name is empty or on any DB error.
func ensureOrgByName(name string) *int {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var id int
	err := db.QueryRow("SELECT id FROM organizations WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		err = db.QueryRow("INSERT INTO organizations (name) VALUES (?) RETURNING id", name).Scan(&id)
	}
	if err != nil {
		return nil
	}
	return &id
}

// isOrgMember returns true if userID is a member of orgID.
func isOrgMember(userID, orgID int) bool {
	var n int
	db.QueryRow(
		"SELECT COUNT(*) FROM organization_members WHERE user_id = ? AND organization_id = ?",
		userID, orgID,
	).Scan(&n)
	return n > 0
}

const orgSelectCols = `id, name, COALESCE(description,''), COALESCE(actor_name,''), COALESCE(website,''), COALESCE(instagram,''), COALESCE(mastodon,''), COALESCE(facebook,''), COALESCE(contact_email,''), created_at`

func scanOrg(row interface{ Scan(...any) error }) (Organization, error) {
	var o Organization
	if err := row.Scan(&o.ID, &o.Name, &o.Description, &o.ActorName, &o.Website, &o.Instagram, &o.Mastodon, &o.Facebook, &o.ContactEmail, &o.CreatedAt); err != nil {
		return o, err
	}
	o.ImageURL = orgImageURL(o.ID)
	return o, nil
}

// GET /api/v1/organizations
func getOrganizations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rows, err := db.Query("SELECT " + orgSelectCols + " FROM organizations ORDER BY id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	orgs := []Organization{}
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		orgs = append(orgs, o)
	}
	json.NewEncoder(w).Encode(orgs)
}

// POST /api/v1/organizations
func createOrganization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-User-Role") != RoleAdmin {
		http.Error(w, "Forbidden: only admins may create organizations", http.StatusForbidden)
		return
	}
	var req CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	o, err := scanOrg(db.QueryRow(
		"INSERT INTO organizations (name, description, actor_name, website, instagram, mastodon, facebook, contact_email) VALUES (?,?,?,?,?,?,?,?) RETURNING "+orgSelectCols,
		req.Name, req.Description, req.ActorName, req.Website, req.Instagram, req.Mastodon, req.Facebook, req.ContactEmail,
	))
	if err != nil {
		http.Error(w, "Failed to create organization", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(o)
}

// GET /api/v1/organizations/{id}
func getOrganization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := mux.Vars(r)["id"]
	o, err := scanOrg(db.QueryRow("SELECT "+orgSelectCols+" FROM organizations WHERE id = ?", id))
	if err == sql.ErrNoRows {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(o)
}

// PUT /api/v1/organizations/{id}
func updateOrganization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-User-Role") != RoleAdmin {
		http.Error(w, "Forbidden: only admins may update organizations", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]
	o, err := scanOrg(db.QueryRow("SELECT "+orgSelectCols+" FROM organizations WHERE id = ?", id))
	if err == sql.ErrNoRows {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var req CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name != "" {
		o.Name = req.Name
	}
	o.Description = req.Description
	o.ActorName = req.ActorName
	o.Website = req.Website
	o.Instagram = req.Instagram
	o.Mastodon = req.Mastodon
	o.Facebook = req.Facebook
	o.ContactEmail = req.ContactEmail
	if _, err := db.Exec(
		"UPDATE organizations SET name=?, description=?, actor_name=?, website=?, instagram=?, mastodon=?, facebook=?, contact_email=? WHERE id=?",
		o.Name, o.Description, o.ActorName, o.Website, o.Instagram, o.Mastodon, o.Facebook, o.ContactEmail, id,
	); err != nil {
		http.Error(w, "Failed to update organization", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(o)
}

// DELETE /api/v1/organizations/{id}
func deleteOrganization(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Role") != RoleAdmin {
		http.Error(w, "Forbidden: only admins may delete organizations", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]
	result, err := db.Exec("DELETE FROM organizations WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/organizations/{id}/members
func getOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := mux.Vars(r)["id"]
	rows, err := db.Query(`
		SELECT om.organization_id, om.user_id, u.username, om.created_at
		FROM organization_members om
		JOIN users u ON om.user_id = u.id
		WHERE om.organization_id = ?
		ORDER BY om.created_at`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	members := []OrganizationMember{}
	for rows.Next() {
		var m OrganizationMember
		if err := rows.Scan(&m.OrganizationID, &m.UserID, &m.Username, &m.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}
	json.NewEncoder(w).Encode(members)
}

// POST /api/v1/organizations/{id}/members
func addOrganizationMember(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-User-Role") != RoleAdmin {
		http.Error(w, "Forbidden: only admins may manage organization members", http.StatusForbidden)
		return
	}
	orgID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid organization ID", http.StatusBadRequest)
		return
	}
	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	var n int
	db.QueryRow("SELECT COUNT(*) FROM organizations WHERE id = ?", orgID).Scan(&n)
	if n == 0 {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}
	db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", req.UserID).Scan(&n)
	if n == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if _, err := db.Exec(
		"INSERT OR IGNORE INTO organization_members (organization_id, user_id) VALUES (?, ?)",
		orgID, req.UserID,
	); err != nil {
		http.Error(w, "Failed to add member", http.StatusInternalServerError)
		return
	}

	var m OrganizationMember
	db.QueryRow(
		"SELECT organization_id, user_id, created_at FROM organization_members WHERE organization_id = ? AND user_id = ?",
		orgID, req.UserID,
	).Scan(&m.OrganizationID, &m.UserID, &m.CreatedAt)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(m)
}

// DELETE /api/v1/organizations/{id}/members/{user_id}
func removeOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Role") != RoleAdmin {
		http.Error(w, "Forbidden: only admins may manage organization members", http.StatusForbidden)
		return
	}
	vars := mux.Vars(r)
	result, err := db.Exec(
		"DELETE FROM organization_members WHERE organization_id = ? AND user_id = ?",
		vars["id"], vars["user_id"],
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
