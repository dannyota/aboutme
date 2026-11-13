package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type dataEnvelope struct {
	Data any `json:"data"`
}

func WriteData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, dataEnvelope{Data: data})
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("api: encode response body", "error", err)
	}
}
