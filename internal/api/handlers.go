// Package api serves the task endpoints over HTTP.
// It reads JSON requests, applies the task rules, calls the store
// and writes JSON responses.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
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
	// The query string is not part of a pattern, so this route also serves
	// GET /tasks?status=done. A pattern such as "GET /tasks?status=done"
	// would never match.
	mux.HandleFunc("GET /tasks", h.listTasks)
	mux.HandleFunc("GET /tasks/{id}", h.getTask)
	mux.HandleFunc("PATCH /tasks/{id}", h.updateTask)

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

// listTasks handles GET /tasks, with an optional ?status= filter.
func (h *taskHandlers) listTasks(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tasks, err := h.store.List(context.Background(), opts)
	if err != nil {
		respondWithError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
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

// updateTaskRequest is the JSON body of PATCH /tasks/{id}.
// RawMessage keeps the raw JSON of a field, so a missing field (nil)
// can be told apart from null ("null").
type updateTaskRequest struct {
	Title   json.RawMessage `json:"title"`
	Status  json.RawMessage `json:"status"`
	DueDate json.RawMessage `json:"due_date"`
}

// toChanges checks that the sent fields are strings and turns them into
// task.Changes. The task rules are checked later, on the merged task.
func (req updateTaskRequest) toChanges() (task.Changes, error) {
	// {} and a null body both leave every field nil.
	if req.Title == nil && req.Status == nil && req.DueDate == nil {
		return task.Changes{}, errors.New("request body must contain at least one of: " + allowedFields)
	}

	var changes task.Changes

	if req.Title != nil {
		s, err := stringField(req.Title, "title")
		if err != nil {
			return task.Changes{}, err
		}
		changes.Title = &s
	}

	if req.Status != nil {
		s, err := stringField(req.Status, "status")
		if err != nil {
			return task.Changes{}, err
		}
		st := task.Status(s)
		changes.Status = &st
	}

	if req.DueDate != nil {
		// due_date is optional, so null means "remove the due date".
		// title and status are required, so for them null is an error.
		if string(req.DueDate) == "null" {
			changes.ClearDueDate = true
		} else {
			s, err := stringField(req.DueDate, "due_date")
			if err != nil {
				return task.Changes{}, err
			}
			changes.DueDate = &s
		}
	}

	return changes, nil
}

// updateTask handles PATCH /tasks/{id}. Only the sent fields change.
// The checks run in this order: a bad id gives 400, a bad body gives 400,
// a missing task gives 404, and a rule broken after merging gives 400.
func (h *taskHandlers) updateTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req updateTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	changes, err := req.toChanges()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Update returns ErrNotFound for a missing task (404)
	// and a validation error for a broken rule (400).
	t, err := h.store.Update(context.Background(), id, changes)
	if err != nil {
		respondWithError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// parseListOptions reads the query string of GET /tasks.
// Unknown parameters are ignored, and an empty status means no filter.
func parseListOptions(r *http.Request) (storage.ListOptions, error) {
	// r.URL.Query() silently drops broken pairs such as %zz
	// and would return every task with 200.
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return storage.ListOptions{}, errors.New("invalid query string")
	}

	var opts storage.ListOptions
	// Get returns the first value, so ?status=done&status=pending means done.
	status := values.Get("status")
	if status != "" {
		s := task.Status(status)
		if !s.IsValid() {
			return storage.ListOptions{}, errors.New(task.InvalidStatusMessage)
		}
		opts.Status = s
	}
	return opts, nil
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
