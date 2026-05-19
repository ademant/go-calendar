package main

import (
	"encoding/json"
	"net/http"
	"time"
)

type ServiceInfo struct {
	Service         string `json:"service"`
	Version         string `json:"version"`
	BuildTime       string `json:"build_time"`
	TotalEvents     int    `json:"total_events"`
	PublishedEvents int    `json:"published_events"`
	UpcomingEvents  int    `json:"upcoming_events"`
}

// GET /api/v1/info
func getInfo(w http.ResponseWriter, r *http.Request) {
	var total, published, upcoming int

	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&total); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE is_published = 1`).Scan(&published); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE is_published = 1 AND start_time > ?`, now()).Scan(&upcoming); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	info := ServiceInfo{
		Service:         "dansal",
		Version:         Version,
		BuildTime:       BuildTime,
		TotalEvents:     total,
		PublishedEvents: published,
		UpcomingEvents:  upcoming,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func now() int64 {
	return time.Now().Unix()
}
