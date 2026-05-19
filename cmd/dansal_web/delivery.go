package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func startDelivery(cfg *Config, db *sql.DB, client *DansalClient, relayActor *ActorRecord) {
	ticker := time.NewTicker(time.Duration(cfg.PollSecs) * time.Second)
	lastPoll := time.Now().Add(-time.Duration(cfg.PollSecs) * time.Second)

	for range ticker.C {
		pollAndDeliver(cfg, db, client, relayActor, lastPoll)
		lastPoll = time.Now()
	}
}

func pollAndDeliver(cfg *Config, db *sql.DB, client *DansalClient, relayActor *ActorRecord, since time.Time) {
	after := since.UTC().Format(time.RFC3339)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	events, err := client.GetEvents(ctx, after)
	if err != nil {
		log.Printf("delivery poll: %v", err)
		return
	}

	for _, e := range events {
		if !e.IsPublished || e.OrganizationID == nil {
			continue
		}
		orgID := *e.OrganizationID

		// Deliver to org followers
		if !isDelivered(db, e.ID, orgID) {
			actor, err := getActorByOrgID(db, orgID)
			if err == nil {
				activity := buildCreateActivity(cfg, actor.OrgSlug, e)
				if err := deliverToFollowers(cfg, db, actor, activity); err != nil {
					log.Printf("delivery event %d org %d: %v", e.ID, orgID, err)
				} else {
					if err := markDelivered(db, e.ID, orgID); err != nil {
						log.Printf("mark delivered event %d org %d: %v", e.ID, orgID, err)
					}
				}
			}
		}

		// Additionally deliver to relay followers (org_id=0 as sentinel)
		if relayActor != nil && !isDelivered(db, e.ID, 0) {
			activity := buildCreateActivity(cfg, relayActor.OrgSlug, e)
			if err := deliverToFollowers(cfg, db, relayActor, activity); err != nil {
				log.Printf("relay delivery event %d: %v", e.ID, err)
			} else {
				if err := markDelivered(db, e.ID, 0); err != nil {
					log.Printf("mark relay delivered event %d: %v", e.ID, err)
				}
			}
		}
	}
}

func deliverToFollowers(cfg *Config, db *sql.DB, actor *ActorRecord, activity Activity) error {
	activity.Context = APContext

	followers, err := listFollowers(db, actor.OrgID)
	if err != nil {
		return err
	}
	if len(followers) == 0 {
		return nil
	}

	body, err := json.Marshal(activity)
	if err != nil {
		return err
	}

	base := actorURL(cfg, actor.OrgSlug)
	keyID := base + "#main-key"

	for _, f := range followers {
		if err := postToInbox(f.InboxURL, keyID, actor.PrivateKeyPEM, body); err != nil {
			log.Printf("deliver to %s: %v", f.InboxURL, err)
		}
	}
	return nil
}

func deliverEventToFollowers(cfg *Config, db *sql.DB, orgID int, event Event) {
	actor, err := getActorByOrgID(db, orgID)
	if err != nil {
		return
	}
	activity := buildCreateActivity(cfg, actor.OrgSlug, event)
	deliverToFollowers(cfg, db, actor, activity)
}

func postToInbox(inboxURL, keyID, privateKeyPEM string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, inboxURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/activity+json")
	if err := SignRequest(req, keyID, privateKeyPEM, body); err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("remote returned %d", resp.StatusCode)
	}
	return nil
}
