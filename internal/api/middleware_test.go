package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLog sends the log output to a buffer until the test ends,
// so the test can check what was logged.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

// TestRecoverPanics checks that a panic becomes a JSON 500 and is logged.
func TestRecoverPanics(t *testing.T) {
	logs := captureLog(t)
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})
	rec := httptest.NewRecorder()

	recoverPanics(panicking).ServeHTTP(rec, httptest.NewRequest("GET", "/tasks", nil))

	checkError(t, rec, http.StatusInternalServerError, "internal server error")
	if !strings.Contains(logs.String(), "panic: something went wrong") {
		t.Errorf("log = %q, want the panic message", logs.String())
	}
}

// TestLogRequests checks that the response passes through unchanged
// and that the log line has the real status code.
func TestLogRequests(t *testing.T) {
	logs := captureLog(t)
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "task not found")
	})
	rec := httptest.NewRecorder()

	logRequests(notFound).ServeHTTP(rec, httptest.NewRequest("GET", "/tasks/999", nil))

	checkError(t, rec, http.StatusNotFound, "task not found")
	if !strings.Contains(logs.String(), "GET /tasks/999 404 ") {
		t.Errorf("log = %q, want a line with GET /tasks/999 404", logs.String())
	}
}

// TestLogRequestsImplicitOK checks the log line for a handler that only calls
// Write. Write then sends 200 on its own, without calling our WriteHeader.
func TestLogRequestsImplicitOK(t *testing.T) {
	logs := captureLog(t)
	writeOnly := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	rec := httptest.NewRecorder()

	logRequests(writeOnly).ServeHTTP(rec, httptest.NewRequest("GET", "/tasks", nil))

	if !strings.Contains(logs.String(), "GET /tasks 200 ") {
		t.Errorf("log = %q, want a line with GET /tasks 200", logs.String())
	}
}
