package main

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func initDB(path string) *sql.DB {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS actors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id INTEGER UNIQUE NOT NULL,
    org_slug TEXT UNIQUE NOT NULL,
    public_key_pem TEXT NOT NULL,
    private_key_pem TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS followers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id INTEGER NOT NULL,
    actor_uri TEXT NOT NULL,
    inbox_url TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(org_id, actor_uri)
);
CREATE TABLE IF NOT EXISTS delivered (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id INTEGER NOT NULL,
    org_id INTEGER NOT NULL,
    delivered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(event_id, org_id)
);
`); err != nil {
		log.Fatalf("init db schema: %v", err)
	}
	return db
}

type ActorRecord struct {
	ID            int
	OrgID         int
	OrgSlug       string
	PublicKeyPEM  string
	PrivateKeyPEM string
}

func getActorBySlug(db *sql.DB, slug string) (*ActorRecord, error) {
	var a ActorRecord
	err := db.QueryRow(
		"SELECT id, org_id, org_slug, public_key_pem, private_key_pem FROM actors WHERE org_slug = ?",
		slug,
	).Scan(&a.ID, &a.OrgID, &a.OrgSlug, &a.PublicKeyPEM, &a.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func getActorByOrgID(db *sql.DB, orgID int) (*ActorRecord, error) {
	var a ActorRecord
	err := db.QueryRow(
		"SELECT id, org_id, org_slug, public_key_pem, private_key_pem FROM actors WHERE org_id = ?",
		orgID,
	).Scan(&a.ID, &a.OrgID, &a.OrgSlug, &a.PublicKeyPEM, &a.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ensureRelayActor creates (or fetches) the special relay actor with org_id=0.
func ensureRelayActor(db *sql.DB) (*ActorRecord, error) {
	return ensureActor(db, 0, "relay")
}

func ensureActor(db *sql.DB, orgID int, orgSlug string) (*ActorRecord, error) {
	a, err := getActorByOrgID(db, orgID)
	if err == nil {
		return a, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	pub, priv, err := generateRSAKeyPair()
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(
		"INSERT INTO actors (org_id, org_slug, public_key_pem, private_key_pem) VALUES (?, ?, ?, ?)",
		orgID, orgSlug, pub, priv,
	)
	if err != nil {
		return nil, err
	}
	return getActorByOrgID(db, orgID)
}

func addFollower(db *sql.DB, orgID int, actorURI, inboxURL string) error {
	_, err := db.Exec(
		"INSERT OR IGNORE INTO followers (org_id, actor_uri, inbox_url) VALUES (?, ?, ?)",
		orgID, actorURI, inboxURL,
	)
	return err
}

func removeFollower(db *sql.DB, orgID int, actorURI string) error {
	_, err := db.Exec(
		"DELETE FROM followers WHERE org_id = ? AND actor_uri = ?",
		orgID, actorURI,
	)
	return err
}

func countFollowers(db *sql.DB, orgID int) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM followers WHERE org_id = ?", orgID).Scan(&n)
	return n, err
}

func listFollowers(db *sql.DB, orgID int) ([]struct{ ActorURI, InboxURL string }, error) {
	rows, err := db.Query(
		"SELECT actor_uri, inbox_url FROM followers WHERE org_id = ? ORDER BY created_at",
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fs []struct{ ActorURI, InboxURL string }
	for rows.Next() {
		var f struct{ ActorURI, InboxURL string }
		if err := rows.Scan(&f.ActorURI, &f.InboxURL); err != nil {
			return nil, err
		}
		fs = append(fs, f)
	}
	return fs, nil
}

func markDelivered(db *sql.DB, eventID, orgID int) error {
	_, err := db.Exec(
		"INSERT OR IGNORE INTO delivered (event_id, org_id) VALUES (?, ?)",
		eventID, orgID,
	)
	return err
}

func isDelivered(db *sql.DB, eventID, orgID int) bool {
	var n int
	db.QueryRow(
		"SELECT COUNT(*) FROM delivered WHERE event_id = ? AND org_id = ?",
		eventID, orgID,
	).Scan(&n)
	return n > 0
}
