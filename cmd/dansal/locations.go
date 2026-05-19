package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

type Location struct {
	ID             int    `json:"id"`
	Location       string `json:"location"`
	ShortName      string `json:"short_name,omitempty"`
	Address        string `json:"address"`
	Zipcode        string `json:"zipcode"`
	Town           string `json:"town"`
	Country        string `json:"country,omitempty"`
	Latitude       string `json:"latitude"`
	Longitude      string `json:"longitude"`
	Internetsite   string `json:"internetsite"`
	CreatedAt      string `json:"created_at"`
	OrganizationID *int   `json:"organization_id,omitempty"`
}

type LocationCreateRequest struct {
	Location       string `json:"location"`
	ShortName      string `json:"short_name"`
	Address        string `json:"address"`
	Zipcode        string `json:"zipcode"`
	Town           string `json:"town"`
	Country        string `json:"country"`
	Latitude       string `json:"latitude"`
	Longitude      string `json:"longitude"`
	Internetsite   string `json:"internetsite"`
	OrganizationID *int   `json:"organization_id,omitempty"`
}

type LocationCreateResponse struct {
	Location         Location   `json:"location"`
	SimilarLocations []Location `json:"similar_locations,omitempty"`
}

// Address parsing — same patterns as dansal_admin fill-location-fields.
var (
	locPatternFull    = regexp.MustCompile(`^[^,]+,\s*(.+?),\s*(\d{5})\s+(.+)$`)
	locPatternNoZip   = regexp.MustCompile(`^[^,]+,\s*(.+?\s+\d+\w*),\s*([A-ZÄÖÜ].+)$`)
	locPatternZipOnly = regexp.MustCompile(`^[^,]+,\s*(\d{5})\s+(.+)$`)
	trailingNr        = regexp.MustCompile(`\s+\d+\w*$`)
)

type locationParsed struct{ street, town string }

func parseLocationNameServer(name string) (locationParsed, bool) {
	if m := locPatternFull.FindStringSubmatch(name); m != nil {
		return locationParsed{street: strings.TrimSpace(m[1]), town: strings.TrimSpace(m[3])}, true
	}
	if m := locPatternNoZip.FindStringSubmatch(name); m != nil {
		return locationParsed{street: strings.TrimSpace(m[1]), town: strings.TrimSpace(m[2])}, true
	}
	if m := locPatternZipOnly.FindStringSubmatch(name); m != nil {
		return locationParsed{town: strings.TrimSpace(m[2])}, true
	}
	return locationParsed{}, false
}

func streetBase(address string) string {
	return strings.TrimSpace(trailingNr.ReplaceAllString(address, ""))
}

// similarLocations returns locations on the same street (ignoring house number)
// in the same town whose name differs from the one being created.
func similarLocations(name, street, town string) []Location {
	if town == "" {
		return nil
	}
	base := streetBase(street)
	var rows *sql.Rows
	var err error
	if base != "" {
		rows, err = db.Query(`
			SELECT id, location, COALESCE(short_name,''), address, COALESCE(zipcode,''), town,
			       COALESCE(country,''), COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(internetsite,''),
			       created_at, organization_id
			FROM locations
			WHERE town = ?
			  AND address != ''
			  AND (address = ? OR address LIKE ?)
			  AND location != ?`,
			town, base, base+" %", name,
		)
	} else {
		rows, err = db.Query(`
			SELECT id, location, COALESCE(short_name,''), address, COALESCE(zipcode,''), town,
			       COALESCE(country,''), COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(internetsite,''),
			       created_at, organization_id
			FROM locations
			WHERE town = ?
			  AND location != ?`,
			town, name,
		)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []Location
	for rows.Next() {
		var loc Location
		var orgID sql.NullInt64
		if err := rows.Scan(&loc.ID, &loc.Location, &loc.ShortName, &loc.Address,
			&loc.Zipcode, &loc.Town, &loc.Country, &loc.Latitude, &loc.Longitude,
			&loc.Internetsite, &loc.CreatedAt, &orgID); err != nil {
			continue
		}
		if orgID.Valid {
			v := int(orgID.Int64)
			loc.OrganizationID = &v
		}
		result = append(result, loc)
	}
	return result
}

type LocationUpdateRequest struct {
	ShortName    string `json:"short_name"`
	Address      string `json:"address"`
	Zipcode      string `json:"zipcode"`
	Town         string `json:"town"`
	Country      string `json:"country"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
	Internetsite string `json:"internetsite"`
}

// GET /api/v1/locations - List all locations
func getLocations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := db.Query("SELECT id, location, COALESCE(short_name,''), address, COALESCE(zipcode,''), town, COALESCE(country,''), COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(internetsite,''), created_at, organization_id FROM locations")
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	locations := []Location{}
	for rows.Next() {
		var location Location
		var orgID sql.NullInt64
		if err := rows.Scan(&location.ID, &location.Location, &location.ShortName, &location.Address, &location.Zipcode, &location.Town, &location.Country, &location.Latitude, &location.Longitude, &location.Internetsite, &location.CreatedAt, &orgID); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if orgID.Valid {
			v := int(orgID.Int64)
			location.OrganizationID = &v
		}
		locations = append(locations, location)
	}

	json.NewEncoder(w).Encode(locations)
}

// POST /api/v1/locations - Create one or more locations
func createLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.As(err, new(*http.MaxBytesError)) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, err.Error(), status)
		return
	}

	var reqs []LocationCreateRequest
	if json.Unmarshal(body, &reqs) != nil || len(reqs) == 0 || reqs[0].Location == "" {
		var single LocationCreateRequest
		if err := json.Unmarshal(body, &single); err != nil {
			writeError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		reqs = []LocationCreateRequest{single}
	}

	// Validate all items before inserting any.
	for _, req := range reqs {
		if req.Location == "" {
			writeError(w, "location is required", http.StatusBadRequest)
			return
		}
	}
	if requesterRole != RoleAdmin {
		if requesterRole != RoleUser && requesterRole != RolePublisher {
			writeError(w, "Forbidden", http.StatusForbidden)
			return
		}
		checked := make(map[int]bool)
		for _, req := range reqs {
			if req.OrganizationID == nil {
				writeError(w, "organization_id is required", http.StatusBadRequest)
				return
			}
			orgID := *req.OrganizationID
			member, seen := checked[orgID]
			if !seen {
				member = isOrgMember(callerID, orgID)
				checked[orgID] = member
			}
			if !member {
				writeError(w, "Forbidden: not a member of the specified organization", http.StatusForbidden)
				return
			}
		}
	}

	results := make([]LocationCreateResponse, 0, len(reqs))
	for _, req := range reqs {
		// Derive street and town for the duplicate-street check.
		// Prefer explicit request fields; fall back to parsing the location name.
		street, town := req.Address, req.Town
		if street == "" || town == "" {
			if parsed, ok := parseLocationNameServer(req.Location); ok {
				if street == "" {
					street = parsed.street
				}
				if town == "" {
					town = parsed.town
				}
			}
		}
		similar := similarLocations(req.Location, street, town)

		var orgIDArg any
		if req.OrganizationID != nil {
			orgIDArg = *req.OrganizationID
		}
		result, err := db.Exec(
			"INSERT INTO locations (location, short_name, address, zipcode, town, country, latitude, longitude, internetsite, organization_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			req.Location, req.ShortName, req.Address, req.Zipcode, req.Town, req.Country, req.Latitude, req.Longitude, req.Internetsite, orgIDArg,
		)
		if err != nil {
			writeError(w, "Failed to create location", http.StatusInternalServerError)
			return
		}
		id, _ := result.LastInsertId()
		loc := Location{
			ID:             int(id),
			Location:       req.Location,
			ShortName:      req.ShortName,
			Address:        req.Address,
			Zipcode:        req.Zipcode,
			Town:           req.Town,
			Country:        req.Country,
			Latitude:       req.Latitude,
			Longitude:      req.Longitude,
			Internetsite:   req.Internetsite,
			OrganizationID: req.OrganizationID,
		}
		results = append(results, LocationCreateResponse{Location: loc, SimilarLocations: similar})
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(results)
}

// GET /api/v1/locations/{id} - Get a specific location
func getLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]

	var location Location
	var orgID sql.NullInt64
	err := db.QueryRow(
		"SELECT id, location, COALESCE(short_name,''), address, COALESCE(zipcode,''), town, COALESCE(country,''), COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(internetsite,''), created_at, organization_id FROM locations WHERE id = ?",
		id,
	).Scan(&location.ID, &location.Location, &location.ShortName, &location.Address, &location.Zipcode, &location.Town, &location.Country, &location.Latitude, &location.Longitude, &location.Internetsite, &location.CreatedAt, &orgID)
	if orgID.Valid {
		v := int(orgID.Int64)
		location.OrganizationID = &v
	}

	if err == sql.ErrNoRows {
		writeError(w, "Location not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(location)
}

// PATCH /api/v1/locations/{id} - Full update including organization_id
func patchLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	id := mux.Vars(r)["id"]

	if requesterRole != RoleAdmin {
		if requesterRole != RoleUser && requesterRole != RolePublisher {
			writeError(w, "Forbidden", http.StatusForbidden)
			return
		}
		callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
		var orgID sql.NullInt64
		err := db.QueryRow("SELECT organization_id FROM locations WHERE id = ?", id).Scan(&orgID)
		if err == sql.ErrNoRows {
			writeError(w, "Location not found", http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !orgID.Valid || !isOrgMember(callerID, int(orgID.Int64)) {
			writeError(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Location       string `json:"location"`
		ShortName      string `json:"short_name"`
		Address        string `json:"address"`
		Zipcode        string `json:"zipcode"`
		Town           string `json:"town"`
		Country        string `json:"country"`
		Latitude       string `json:"latitude"`
		Longitude      string `json:"longitude"`
		Internetsite   string `json:"internetsite"`
		OrganizationID *int   `json:"organization_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var loc Location
	var orgID sql.NullInt64
	err := db.QueryRow(
		"SELECT id, location, COALESCE(short_name,''), address, COALESCE(zipcode,''), town, COALESCE(country,''), COALESCE(latitude,''), COALESCE(longitude,''), COALESCE(internetsite,''), created_at, organization_id FROM locations WHERE id = ?", id,
	).Scan(&loc.ID, &loc.Location, &loc.ShortName, &loc.Address, &loc.Zipcode, &loc.Town, &loc.Country, &loc.Latitude, &loc.Longitude, &loc.Internetsite, &loc.CreatedAt, &orgID)
	if err == sql.ErrNoRows {
		writeError(w, "Location not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Location != "" {
		loc.Location = req.Location
	}
	loc.ShortName = req.ShortName
	loc.Address = req.Address
	loc.Zipcode = req.Zipcode
	if req.Town != "" {
		loc.Town = req.Town
	}
	loc.Country = req.Country
	loc.Latitude = req.Latitude
	loc.Longitude = req.Longitude
	loc.Internetsite = req.Internetsite
	loc.OrganizationID = req.OrganizationID

	var orgVal any
	if loc.OrganizationID != nil {
		orgVal = *loc.OrganizationID
	}
	if _, err := db.Exec(
		"UPDATE locations SET location=?, short_name=?, address=?, zipcode=?, town=?, country=?, latitude=?, longitude=?, internetsite=?, organization_id=? WHERE id=?",
		loc.Location, loc.ShortName, loc.Address, loc.Zipcode, loc.Town, loc.Country, loc.Latitude, loc.Longitude, loc.Internetsite, orgVal, loc.ID,
	); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(loc)
}

// POST /api/v1/locations/bulk-assign-org - Admin bulk-assign organization to locations
func bulkAssignLocationOrg(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Role") != RoleAdmin {
		writeError(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	var req struct {
		IDs            []int `json:"ids"`
		OrganizationID *int  `json:"organization_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	var orgVal any
	if req.OrganizationID != nil {
		orgVal = *req.OrganizationID
	}
	for _, id := range req.IDs {
		db.Exec("UPDATE locations SET organization_id=? WHERE id=?", orgVal, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/locations/{id} - Delete a location
func deleteLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requesterRole := r.Header.Get("X-User-Role")
	vars := mux.Vars(r)
	id := vars["id"]

	if requesterRole != RoleAdmin {
		if requesterRole != RoleUser {
			writeError(w, "Forbidden", http.StatusForbidden)
			return
		}
		callerID, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
		var orgID sql.NullInt64
		err := db.QueryRow("SELECT organization_id FROM locations WHERE id = ?", id).Scan(&orgID)
		if err == sql.ErrNoRows {
			writeError(w, "Location not found", http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !orgID.Valid || !isOrgMember(callerID, int(orgID.Int64)) {
			writeError(w, "Forbidden: not a member of the location's organization", http.StatusForbidden)
			return
		}
	}

	// Check if location exists
	var locationID int
	err := db.QueryRow("SELECT id FROM locations WHERE id = ?", id).Scan(&locationID)
	if err == sql.ErrNoRows {
		writeError(w, "Location not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Delete location
	result, err := db.Exec("DELETE FROM locations WHERE id = ?", id)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		writeError(w, "Location not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
