// Package api serves the task endpoints over HTTP.
// It reads JSON requests, applies the task rules, calls the store
// and writes JSON responses.
package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/TahirGuliyev/task-tracker-api/internal/storage"
	"github.com/TahirGuliyev/task-tracker-api/internal/task"
)

// taskHandlers holds what the handlers need.
type taskHandlers struct {
	store *storage.Store
}

// NewRouter returns the HTTP handler that serves every endpoint.
func NewRouter(store *storage.Store) http.Handler {
	h := &taskHandlers{store: store}
	mux := http.NewServeMux()

	// The method in each pattern makes the mux answer any other method with
	// 405 Method Not Allowed and an Allow header. Do not register "/":
	// a catch-all route would turn every 405 into a 404.
	mux.HandleFunc("POST /tasks", h.createTask)
	mux.HandleFunc("GET /tasks/{id}", h.getTask)

	return mux
}

// createTaskRequest is the JSON body of POST /tasks. It is separate from
// task.Task, so a client cannot set id or created_at.
// A missing field stays nil; on create, null means the same as missing.
type createTaskRequest struct {
	Title   *string      `json:"title"`
	Status  *task.Status `json:"status"`
	DueDate *string      `json:"due_date"`
}

// createTask handles POST /tasks.
func (h *taskHandlers) createTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	changes := task.Changes{Title: req.Title, Status: req.Status, DueDate: req.DueDate}
	t, err := task.New(changes, time.Now())
	if err != nil {
		respondWithError(w, err)
		return
	}

	// context.Background() is never canceled. net/http cancels r.Context() when
	// the client closes its sending side (macOS nc does that right after the
	// request), and the task could then be saved but answered with a 500.
	t, err = h.store.Create(context.Background(), t)
	if err != nil {
		respondWithError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// getTask handles GET /tasks/{id}.
func (h *taskHandlers) getTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := h.store.Get(context.Background(), id)
	if err != nil {
		respondWithError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// parseID reads the {id} part of the path. Valid ids are whole numbers from 1 up.
func parseID(r *http.Request) (int64, error) {
	// Base 10, and the number must fit in an int64, the type of Task.ID.
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)

	// ids start at 1, so 0 and negative numbers are malformed, not missing.
	if err != nil || id < 1 {
		return 0, errors.New("invalid task id")
	}
	return id, nil
}

// respondWithError writes the response for an error from the task rules
// or the store. Internal details are logged, never sent to the client.
func respondWithError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, task.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "task not found")
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
