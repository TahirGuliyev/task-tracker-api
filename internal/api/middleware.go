package api

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

// statusRecorder wraps an http.ResponseWriter and remembers the status code.
// Embedding gives it all the methods of the wrapped writer, and only
// WriteHeader is replaced. This is not inheritance: the wrapped writer never
// calls this WriteHeader, not even for the 200 that Write sends on its own.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader remembers the status code, then sends it as usual.
func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// logRequests logs one line per request: the method, the path with the
// query string, the status code and how long the request took.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Start at 200, because a handler that never calls WriteHeader sends 200.
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// The handler writes to rec instead of w, so rec sees the status code.
		next.ServeHTTP(rec, r)

		// Logged after ServeHTTP returns, so the status and the duration are final.
		// A deferred log.Printf would read its arguments at once, before the
		// handler runs.
		log.Printf("%s %s %d %s", r.Method, r.URL.RequestURI(), rec.status, time.Since(start))
	})
}

// recoverPanics turns a panic in a handler into a 500 response.
// net/http already keeps the server running after a panic, but the client
// would only see a dropped connection. This sends a clean JSON 500 and logs
// the stack trace.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if panicValue := recover(); panicValue != nil {
				log.Printf("panic: %v\n%s", panicValue, debug.Stack())
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
