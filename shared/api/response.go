// shared/api/response.go
package api

import (
	"encoding/json"
	"log" // For logging unexpected JSON errors
	"net/http"
)

// JSONErrorResponse defines a standard structure for API error responses.
type JSONErrorResponse struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`    // Optional: for custom application-specific error codes
	Details string `json:"details,omitempty"` // Optional: for more detailed error info
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

// WriteError writes a JSON error response with the given status code and message.
func WriteError(w http.ResponseWriter, status int, message string) {
	errResp := JSONErrorResponse{
		Message: message,
		Code:    status, // Using HTTP status code as the error code
	}
	// Attempt to write JSON, fall back to plain text if JSON encoding fails
	if err := WriteJSON(w, status, errResp); err != nil {
		log.Printf("ERROR: Failed to write JSON error response: %v. Falling back to plain text.", err)
		http.Error(w, message, status) // Fallback
	}
}

// WriteBadRequest convenience function
func WriteBadRequest(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusBadRequest, message)
}

// WriteNotFound convenience function
func WriteNotFound(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusNotFound, message)
}

// WriteInternalServerError convenience function
func WriteInternalServerError(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusInternalServerError, message)
}

// Add more convenience functions for common error types (e.g., WriteUnauthorized, WriteForbidden)
