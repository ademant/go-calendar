package main

import (
	"encoding/json"
	"net/http"
)

type AdminConfigResponse struct {
	TelegramBotToken  string `json:"telegram_bot_token"`
	TelegramBotName   string `json:"telegram_bot_name"`
	MatrixHomeserver  string `json:"matrix_homeserver"`
	MatrixAccessToken string `json:"matrix_access_token"`
}

type AdminConfigPatch struct {
	TelegramBotToken  *string `json:"telegram_bot_token"`
	TelegramBotName   *string `json:"telegram_bot_name"`
	MatrixHomeserver  *string `json:"matrix_homeserver"`
	MatrixAccessToken *string `json:"matrix_access_token"`
}

func getAdminConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-User-Role") != RoleAdmin {
		writeError(w, "forbidden", http.StatusForbidden)
		return
	}
	json.NewEncoder(w).Encode(AdminConfigResponse{
		TelegramBotToken:  config.Server.TelegramBotToken,
		TelegramBotName:   config.Server.TelegramBotName,
		MatrixHomeserver:  config.Server.MatrixHomeserver,
		MatrixAccessToken: config.Server.MatrixAccessToken,
	})
}

func patchAdminConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("X-User-Role") != RoleAdmin {
		writeError(w, "forbidden", http.StatusForbidden)
		return
	}
	var req AdminConfigPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.TelegramBotToken != nil {
		config.Server.TelegramBotToken = *req.TelegramBotToken
	}
	if req.TelegramBotName != nil {
		config.Server.TelegramBotName = *req.TelegramBotName
	}
	if req.MatrixHomeserver != nil {
		config.Server.MatrixHomeserver = *req.MatrixHomeserver
	}
	if req.MatrixAccessToken != nil {
		config.Server.MatrixAccessToken = *req.MatrixAccessToken
	}
	if configFilePath != "" {
		if err := saveConfig(configFilePath); err != nil {
			writeError(w, "failed to save config", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
