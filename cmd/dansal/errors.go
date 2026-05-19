package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func writeError(w http.ResponseWriter, msg string, code int) {
	if code >= 500 {
		log.Printf("error %d: %s", code, msg)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
