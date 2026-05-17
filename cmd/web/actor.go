package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var slugRe = regexp.MustCompile(`[^a-z0-9\-]`)

func orgSlug(name string) string {
	s := strings.ToLower(name)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func actorURL(cfg *Config, slug string) string {
	return "https://" + cfg.Domain + "/org/" + slug
}

func actorFromOrg(cfg *Config, org Organization, actor *ActorRecord) Actor {
	base := actorURL(cfg, actor.OrgSlug)
	return Actor{
		Context:           APContext,
		Type:              "Organization",
		ID:                base,
		Name:              org.Name,
		Summary:           org.Description,
		URL:               base,
		PreferredUsername: actor.OrgSlug,
		Inbox:             base + "/inbox",
		Outbox:            base + "/outbox",
		Followers:         base + "/followers",
		PublicKey: PublicKey{
			ID:           base + "#main-key",
			Owner:        base,
			PublicKeyPem: actor.PublicKeyPEM,
		},
	}
}

func isAPRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/activity+json") ||
		strings.Contains(accept, "application/ld+json")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/activity+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func webfingerHandler(cfg *Config, db *sql.DB, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := r.URL.Query().Get("resource")
		if resource == "" {
			writeJSONError(w, http.StatusBadRequest, "resource parameter required")
			return
		}
		prefix := "acct:"
		if !strings.HasPrefix(resource, prefix) {
			writeJSONError(w, http.StatusBadRequest, "only acct: resources supported")
			return
		}
		account := strings.TrimPrefix(resource, prefix)
		parts := strings.SplitN(account, "@", 2)
		if len(parts) != 2 || parts[1] != cfg.Domain {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		slug := parts[0]
		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			orgs, err := client.GetOrganizations(r.Context())
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "upstream error")
				return
			}
			for _, org := range orgs {
				if orgSlug(org.Name) == slug {
					actor, err = ensureActor(db, org.ID, slug)
					if err != nil {
						writeJSONError(w, http.StatusInternalServerError, "actor init error")
						return
					}
					break
				}
			}
			if actor == nil {
				writeJSONError(w, http.StatusNotFound, "user not found")
				return
			}
		} else if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		base := actorURL(cfg, actor.OrgSlug)
		wf := WebFinger{
			Subject: resource,
			Aliases: []string{base},
			Links: []WebFingerLink{
				{
					Rel:  "self",
					Type: "application/activity+json",
					Href: base,
				},
			},
		}
		w.Header().Set("Content-Type", "application/jrd+json")
		json.NewEncoder(w).Encode(wf)
	}
}

func nodeinfoIndexHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"links": []map[string]string{
				{
					"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.0",
					"href": "https://" + cfg.Domain + "/nodeinfo/2.0",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func nodeinfoHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ni := NodeInfo{
			Version: "2.0",
			Software: NodeInfoSoftware{
				Name:    "dansal-web",
				Version: "1.0.0",
			},
			Protocols:         []string{"activitypub"},
			OpenRegistrations: false,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ni)
	}
}

func outboxHandler(cfg *Config, db *sql.DB, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["name"]
		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "actor not found")
			return
		} else if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		base := actorURL(cfg, slug)
		outboxURL := base + "/outbox"

		if r.URL.Query().Get("page") != "true" {
			events, err := client.GetEventsByOrg(r.Context(), actor.OrgID)
			if err != nil {
				events = nil
			}
			col := OrderedCollection{
				Context:    APContext,
				Type:       "OrderedCollection",
				ID:         outboxURL,
				TotalItems: len(events),
				First:      outboxURL + "?page=true",
			}
			writeJSON(w, http.StatusOK, col)
			return
		}

		events, err := client.GetEventsByOrg(r.Context(), actor.OrgID)
		if err != nil {
			events = nil
		}

		items := make([]interface{}, 0, len(events))
		for _, e := range events {
			items = append(items, buildCreateActivity(cfg, actor.OrgSlug, e))
		}

		page := OrderedCollectionPage{
			Context:      APContext,
			Type:         "OrderedCollectionPage",
			ID:           outboxURL + "?page=true",
			PartOf:       outboxURL,
			TotalItems:   len(items),
			OrderedItems: items,
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func followersHandler(cfg *Config, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["name"]
		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "actor not found")
			return
		} else if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		fs, err := listFollowers(db, actor.OrgID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		uris := make([]string, len(fs))
		for i, f := range fs {
			uris[i] = f.ActorURI
		}

		base := actorURL(cfg, slug)
		col := OrderedCollection{
			Context:    APContext,
			Type:       "OrderedCollection",
			ID:         base + "/followers",
			TotalItems: len(uris),
			Items:      uris,
		}
		writeJSON(w, http.StatusOK, col)
	}
}

func inboxHandler(cfg *Config, db *sql.DB, client *DansalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := mux.Vars(r)["name"]
		actor, err := getActorBySlug(db, slug)
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "actor not found")
			return
		} else if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "read error")
			return
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		activityType, _ := raw["type"].(string)
		actorField, _ := raw["actor"].(string)

		switch activityType {
		case "Follow":
			if actorField == "" {
				writeJSONError(w, http.StatusBadRequest, "missing actor")
				return
			}
			inboxURL, err := resolveInboxURL(r.Context(), client, actorField)
			if err != nil {
				log.Printf("inbox: resolve actor %s: %v", actorField, err)
				inboxURL = ""
			}
			if inboxURL == "" {
				writeJSONError(w, http.StatusBadRequest, "could not resolve actor inbox")
				return
			}
			if err := addFollower(db, actor.OrgID, actorField, inboxURL); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			go sendAccept(cfg, actor, raw, actorField, inboxURL)
			w.WriteHeader(http.StatusAccepted)

		case "Undo":
			obj, _ := raw["object"].(map[string]interface{})
			if obj == nil {
				writeJSONError(w, http.StatusBadRequest, "missing object")
				return
			}
			objType, _ := obj["type"].(string)
			if objType != "Follow" {
				writeJSONError(w, http.StatusBadRequest, "only Undo{Follow} supported")
				return
			}
			undoActor := actorField
			if undoActor == "" {
				undoActor, _ = obj["actor"].(string)
			}
			if undoActor == "" {
				writeJSONError(w, http.StatusBadRequest, "missing actor")
				return
			}
			if err := removeFollower(db, actor.OrgID, undoActor); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.WriteHeader(http.StatusAccepted)

		default:
			writeJSONError(w, http.StatusBadRequest, "unsupported activity type")
		}
	}
}

func resolveInboxURL(ctx context.Context, client *DansalClient, actorURI string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, actorURI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/activity+json")
	resp, err := client.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var actor map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&actor); err != nil {
		return "", err
	}
	inbox, _ := actor["inbox"].(string)
	return inbox, nil
}

func sendAccept(cfg *Config, actor *ActorRecord, followActivity map[string]interface{}, followerURI, inboxURL string) {
	base := actorURL(cfg, actor.OrgSlug)
	accept := Activity{
		Context: APContext,
		Type:    "Accept",
		ID:      base + "#accept-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Actor:   base,
		Object:  followActivity,
		To:      []string{followerURI},
	}
	body, err := json.Marshal(accept)
	if err != nil {
		log.Printf("sendAccept marshal: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, inboxURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("sendAccept new request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/activity+json")
	keyID := base + "#main-key"
	if err := SignRequest(req, keyID, actor.PrivateKeyPEM, body); err != nil {
		log.Printf("sendAccept sign: %v", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("sendAccept post: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("sendAccept: remote returned %d for %s", resp.StatusCode, inboxURL)
	}
}

func buildCreateActivity(cfg *Config, slug string, e Event) Activity {
	base := actorURL(cfg, slug)
	eventID := fmt.Sprintf("https://%s/events/%d", cfg.Domain, e.ID)
	apEvent := APEvent{
		Type:      "Event",
		ID:        eventID,
		Name:      e.Title,
		Content:   e.Description,
		StartTime: e.StartTime,
		EndTime:   e.EndTime,
		URL:       e.URL,
	}
	if e.Location != "" {
		apEvent.Location = &APPlace{
			Type: "Place",
			Name: e.Location,
		}
	} else if e.LocationTown != "" {
		apEvent.Location = &APPlace{
			Type: "Place",
			Name: e.LocationTown,
		}
	}
	for _, tag := range e.Tags {
		apEvent.Tag = append(apEvent.Tag, APHashtag{
			Type: "Hashtag",
			Name: "#" + tag,
			Href: fmt.Sprintf("https://%s/?tag=%s", cfg.Domain, tag),
		})
	}

	return Activity{
		Type:  "Create",
		ID:    eventID + "/activity",
		Actor: base,
		Object: apEvent,
		To:    []string{"https://www.w3.org/ns/activitystreams#Public"},
		CC:    []string{base + "/followers"},
	}
}
