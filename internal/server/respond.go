package server

import (
	"encoding/json"
	"net/http"
)

// This file defines the standard response shape of the WHOLE /api/v1 API.
// It is the reference that the OpenAPI generator formalizes. Success: the resource
// body directly. Error: always the envelope
//
//	{ "error": { "code": "<stable slug>", "message": "<human>" } }
//
// The `code` is a stable (non-localized) slug for the client to branch on; the
// `message` is the readable text (pt-BR).

// errorBody is the standard error envelope.
type errorBody struct {
	Error errorDetail `json:"error"`
}

// errorDetail carries the code (stable slug) and the message (human).
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON serializes v as JSON with the given status. Internal use of the
// handlers for success responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the standard error envelope with the given status, code and
// message.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
