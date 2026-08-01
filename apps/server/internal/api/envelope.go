// Package api implements the HTTP router, middleware, and response
// envelope shared by every endpoint on the Go API server.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorBody is the "error" object inside an error envelope.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorEnvelope is the response shape for every failed request:
// {"error":{"code":"...","message":"..."}}.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// dataEnvelope is the response shape for every successful request:
// {"data":...}.
type dataEnvelope struct {
	Data any `json:"data"`
}

// WriteData writes a success envelope {"data":data} with the given status
// code.
func WriteData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, dataEnvelope{Data: data})
}

// WriteError writes an error envelope {"error":{"code":code,"message":message}}
// with the given status code.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line and headers are already sent at this point, so all
		// we can do is record the failure; the client sees a truncated body.
		slog.Error("api: encode response body", "error", err)
	}
}
