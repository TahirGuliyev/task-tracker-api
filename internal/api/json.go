package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// maxBodyBytes is the largest request body the API reads.
const maxBodyBytes = 1024 * 1024 // 1 MB

// allowedFields lists the fields a request body may have.
// It is part of the error message for an unknown field.
const allowedFields = "title, status, due_date"

// decodeJSON reads one JSON object from the request body into dst,
// which must be a pointer. Its errors are safe to show the client,
// so the caller answers 400 Bad Request with err.Error().
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	// Stop reading after 1 MB, so a huge body cannot use up the memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	// A field we do not know, such as "id" or a typo like "titel",
	// is an error instead of being silently ignored.
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		// errors.As checks err and the errors it wraps for one of the variable's
		// type, and stores it in the variable, so it gets the variable's address.
		var maxBytesErr *http.MaxBytesError
		var typeErr *json.UnmarshalTypeError

		switch {
		case errors.Is(err, io.EOF):
			// io.EOF means there was nothing to read: the body was empty or only spaces.
			return errors.New("request body must not be empty")
		case errors.As(err, &maxBytesErr):
			return errors.New("request body must not be larger than 1 MB")
		case errors.As(err, &typeErr) && typeErr.Field != "":
			// Every field in our requests is a string. Field is empty when the
			// whole body has the wrong type, such as [], so that case goes to default.
			return fmt.Errorf("%s must be a string", typeErr.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			// encoding/json has no error type for this, so its text is checked.
			// If that text ever changes, the default case below still gives a 400.
			// The rest of the text is the quoted field name, such as "id".
			field := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("unknown field %s (allowed fields: %s)", field, allowedFields)
		default:
			// Syntax errors, cut-off JSON and bodies such as [] or 123 end up here.
			// Never send encoding/json's own text: it differs between Go versions.
			return errors.New("request body must be a valid JSON object")
		}
	}

	// A second Decode, into a throwaway empty struct, must reach the end of
	// the body (io.EOF). This catches {...}{...} and trailing data such as {...} x.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	// Headers must be set before WriteHeader: changes made after it are not sent.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status code is already sent, so the error can only be logged.
		log.Printf("write response: %v", err)
	}
}

// writeError writes an error response such as {"error": "task not found"}.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
