package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
)

type adminRequest struct {
	Cmd      string `json:"cmd"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role,omitempty"`
	OrgID    int    `json:"org_id,omitempty"`
}

type adminResponse struct {
	OK    bool        `json:"ok"`
	Error string      `json:"error,omitempty"`
	Data  interface{} `json:"data,omitempty"`
}

func startAdminSocket(path string) net.Listener {
	if path == "" {
		path = "./dansal.sock"
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("Admin socket error: %v", err)
		return nil
	}
	if err := os.Chmod(path, 0600); err != nil {
		log.Printf("Admin socket chmod error: %v", err)
		ln.Close()
		return nil
	}
	log.Printf("Admin socket listening on %s", path)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleAdminConn(conn)
		}
	}()
	return ln
}

func handleAdminConn(conn net.Conn) {
	defer conn.Close()
	var req adminRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(adminResponse{OK: false, Error: "invalid request"})
		return
	}
	json.NewEncoder(conn).Encode(dispatchAdminCmd(req))
}

func dispatchAdminCmd(req adminRequest) adminResponse {
	switch req.Cmd {
	case "list-users":
		return adminListUsers()
	case "create-user":
		return adminCreateUser(req)
	case "delete-user":
		return adminDeleteUser(req)
	case "set-password":
		return adminSetPassword(req)
	case "set-role":
		return adminSetRole(req)
	case "list-orgs":
		return adminListOrgs()
	case "list-members":
		return adminListMembers(req)
	case "add-member":
		return adminAddMember(req)
	case "remove-member":
		return adminRemoveMember(req)
	default:
		return adminResponse{OK: false, Error: "unknown command: " + req.Cmd}
	}
}

func adminListUsers() adminResponse {
	rows, err := db.Query("SELECT id, username, email, role, created_at FROM users ORDER BY id")
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return adminResponse{OK: false, Error: err.Error()}
		}
		users = append(users, u)
	}
	return adminResponse{OK: true, Data: users}
}

func adminCreateUser(req adminRequest) adminResponse {
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return adminResponse{OK: false, Error: "username, email and password are required"}
	}
	role := req.Role
	if role == "" {
		role = RoleUser
	}
	if !validateRole(role) {
		return adminResponse{OK: false, Error: "invalid role: use admin, user, publisher or viewer"}
	}
	result, err := db.Exec(
		"INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, ?)",
		req.Username, req.Email, hashPassword(req.Password), role,
	)
	if err != nil {
		return adminResponse{OK: false, Error: "username or email already exists"}
	}
	id, _ := result.LastInsertId()
	return adminResponse{OK: true, Data: User{ID: int(id), Username: req.Username, Email: req.Email, Role: role}}
}

func adminDeleteUser(req adminRequest) adminResponse {
	if req.Username == "" {
		return adminResponse{OK: false, Error: "username is required"}
	}
	var userID int
	var role string
	if err := db.QueryRow("SELECT id, role FROM users WHERE username = ?", req.Username).Scan(&userID, &role); err != nil {
		return adminResponse{OK: false, Error: "user not found"}
	}
	if role == RoleAdmin {
		return adminResponse{OK: false, Error: "cannot delete admin users"}
	}
	db.Exec("DELETE FROM users WHERE id = ?", userID)
	return adminResponse{OK: true}
}

func adminSetPassword(req adminRequest) adminResponse {
	if req.Username == "" || req.Password == "" {
		return adminResponse{OK: false, Error: "username and password are required"}
	}
	result, err := db.Exec("UPDATE users SET password_hash = ? WHERE username = ?",
		hashPassword(req.Password), req.Username)
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return adminResponse{OK: false, Error: "user not found"}
	}
	return adminResponse{OK: true}
}

func adminSetRole(req adminRequest) adminResponse {
	if req.Username == "" || req.Role == "" {
		return adminResponse{OK: false, Error: "username and role are required"}
	}
	if !validateRole(req.Role) {
		return adminResponse{OK: false, Error: "invalid role: use admin, user, publisher or viewer"}
	}
	result, err := db.Exec("UPDATE users SET role = ? WHERE username = ?", req.Role, req.Username)
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return adminResponse{OK: false, Error: "user not found"}
	}
	return adminResponse{OK: true}
}

func adminListOrgs() adminResponse {
	rows, err := db.Query("SELECT id, name, description, created_at FROM organizations ORDER BY id")
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	defer rows.Close()
	orgs := []Organization{}
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Description, &o.CreatedAt); err != nil {
			return adminResponse{OK: false, Error: err.Error()}
		}
		orgs = append(orgs, o)
	}
	return adminResponse{OK: true, Data: orgs}
}

func adminListMembers(req adminRequest) adminResponse {
	if req.OrgID == 0 {
		return adminResponse{OK: false, Error: "org_id is required"}
	}
	rows, err := db.Query(`
		SELECT om.organization_id, om.user_id, u.username, om.created_at
		FROM organization_members om
		JOIN users u ON om.user_id = u.id
		WHERE om.organization_id = ?
		ORDER BY om.created_at`, req.OrgID)
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	defer rows.Close()
	members := []OrganizationMember{}
	for rows.Next() {
		var m OrganizationMember
		if err := rows.Scan(&m.OrganizationID, &m.UserID, &m.Username, &m.CreatedAt); err != nil {
			return adminResponse{OK: false, Error: err.Error()}
		}
		members = append(members, m)
	}
	return adminResponse{OK: true, Data: members}
}

func adminAddMember(req adminRequest) adminResponse {
	if req.OrgID == 0 || req.Username == "" {
		return adminResponse{OK: false, Error: "org_id and username are required"}
	}
	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = ?", req.Username).Scan(&userID); err != nil {
		return adminResponse{OK: false, Error: "user not found"}
	}
	if _, err := db.Exec(
		"INSERT OR IGNORE INTO organization_members (organization_id, user_id) VALUES (?, ?)",
		req.OrgID, userID,
	); err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	return adminResponse{OK: true}
}

func adminRemoveMember(req adminRequest) adminResponse {
	if req.OrgID == 0 || req.Username == "" {
		return adminResponse{OK: false, Error: "org_id and username are required"}
	}
	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = ?", req.Username).Scan(&userID); err != nil {
		return adminResponse{OK: false, Error: "user not found"}
	}
	result, err := db.Exec(
		"DELETE FROM organization_members WHERE organization_id = ? AND user_id = ?",
		req.OrgID, userID,
	)
	if err != nil {
		return adminResponse{OK: false, Error: err.Error()}
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return adminResponse{OK: false, Error: "member not found in organization"}
	}
	return adminResponse{OK: true}
}
