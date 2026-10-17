package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TahirGuliyev/task-tracker-api/internal/storage"
	"github.com/TahirGuliyev/task-tracker-api/internal/task"
)

// doneRuleMessage is the error for any request that would leave
// a "done" task without a title.
const doneRuleMessage = `validation failed: a task with status "done" must have a title`

// atLeastOneMessage is the error for a PATCH body without any known field.
const atLeastOneMessage = "request body must contain at least one of: title, status, due_date"

// createdAtFormat matches a created_at like the sample: "2026-05-08T14:30:00Z".
var createdAtFormat = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

// newTestStore opens a new, empty database in a temporary folder.
func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Cleanups run last-in, first-out, so the store is closed
	// before the temporary folder is deleted.
	t.Cleanup(func() { store.Close() })
	return store
}

// newTestRouter returns the real router with a new, empty database.
// Requests go through the router, so routing and PathValue are tested too.
func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	return NewRouter(newTestStore(t))
}

// doRequest sends a request through the router and returns the recorded
// response. body may be empty.
func doRequest(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// postTask creates a task through the API and returns it.
func postTask(t *testing.T, router http.Handler, body string) task.Task {
	t.Helper()
	rec := doRequest(t, router, "POST", "/tasks", body)
	checkStatus(t, rec, http.StatusCreated)
	return decodeTask(t, rec)
}

// checkStatus stops the test unless the response has the wanted status code
// and a JSON Content-Type.
func checkStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

// checkError checks the status code and the message of an error response.
func checkError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()
	checkStatus(t, rec, wantStatus)
	if got := decodeError(t, rec); got != wantMessage {
		t.Errorf("error = %q, want %q", got, wantMessage)
	}
}

// checkSameTask fails the test if the two tasks differ. They are compared
// as JSON, because DueDate is a pointer and == would compare addresses.
func checkSameTask(t *testing.T, got, want task.Task) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode task: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode task: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("got %s\nwant %s", gotJSON, wantJSON)
	}
}

// decodeTask reads one task from the response body.
func decodeTask(t *testing.T, rec *httptest.ResponseRecorder) task.Task {
	t.Helper()
	var got task.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode task from %s: %v", rec.Body.String(), err)
	}
	return got
}

// decodeList reads a list of tasks from the response body.
func decodeList(t *testing.T, rec *httptest.ResponseRecorder) []task.Task {
	t.Helper()
	var got []task.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode list from %s: %v", rec.Body.String(), err)
	}
	return got
}

// decodeError reads the message from an {"error": "..."} response body.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error from %s: %v", rec.Body.String(), err)
	}
	return got.Error
}

// ids returns the ids of the tasks in order, so lists are easy to compare.
func ids(tasks []task.Task) []int64 {
	var result []int64
	for _, tk := range tasks {
		result = append(result, tk.ID)
	}
	return result
}

// TestCreateTask checks the 201 response and every field of the new task.
func TestCreateTask(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantTaskStatus task.Status
		wantDueDate    string // due_date exactly as it appears in the response JSON
	}{
		{"title only", `{"title":"Buy groceries"}`, task.StatusPending, `null`},
		{"with a due date", `{"title":"Buy groceries","due_date":"2026-05-15"}`, task.StatusPending, `"2026-05-15"`},
		{"null status and due date", `{"title":"Buy groceries","status":null,"due_date":null}`, task.StatusPending, `null`},
		{"done at creation", `{"title":"Buy groceries","status":"done"}`, task.StatusDone, `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(t)
			before := time.Now().UTC().Truncate(time.Second)

			rec := doRequest(t, router, "POST", "/tasks", tt.body)

			checkStatus(t, rec, http.StatusCreated)
			got := decodeTask(t, rec)
			if got.ID != 1 || got.Title != "Buy groceries" || got.Status != tt.wantTaskStatus {
				t.Errorf("got %+v", got)
			}
			if got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now()) {
				t.Errorf("created_at = %v, want the current time", got.CreatedAt)
			}

			// Decode the body again into raw fields to check the exact JSON text.
			var raw struct {
				DueDate   json.RawMessage `json:"due_date"`
				CreatedAt string          `json:"created_at"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if string(raw.DueDate) != tt.wantDueDate {
				t.Errorf("due_date = %s, want %s", raw.DueDate, tt.wantDueDate)
			}
			if !createdAtFormat.MatchString(raw.CreatedAt) {
				t.Errorf("created_at = %q, want UTC with whole seconds, like 2026-05-08T14:30:00Z", raw.CreatedAt)
			}
		})
	}
}

// TestCreateTaskRules checks each rule over HTTP: values at the limit pass,
// and a broken rule is 400 with its own message.
func TestCreateTaskRules(t *testing.T) {
	const dateMessage = "validation failed: due_date must be a valid date in YYYY-MM-DD format"

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string // not checked for 201
	}{
		{"title at the limit", `{"title":"` + strings.Repeat("ə", task.MaxTitleLength) + `"}`, http.StatusCreated, ""},
		{"title over the limit", `{"title":"` + strings.Repeat("ə", task.MaxTitleLength+1) + `"}`, http.StatusBadRequest, fmt.Sprintf("validation failed: title must be at most %d characters", task.MaxTitleLength)},
		{"missing title", `{}`, http.StatusBadRequest, "validation failed: title is required"},
		{"blank title", `{"title":" \t "}`, http.StatusBadRequest, "validation failed: title is required"},
		{"unknown status", `{"title":"a","status":"archived"}`, http.StatusBadRequest, "validation failed: " + task.InvalidStatusMessage},
		{"upper-case status", `{"title":"a","status":"DONE"}`, http.StatusBadRequest, "validation failed: " + task.InvalidStatusMessage},
		{"past due date", `{"title":"a","due_date":"2001-01-01"}`, http.StatusCreated, ""},
		{"impossible date", `{"title":"a","due_date":"2026-02-30"}`, http.StatusBadRequest, dateMessage},
		{"empty date", `{"title":"a","due_date":""}`, http.StatusBadRequest, dateMessage},
		{"date with a time", `{"title":"a","due_date":"2026-05-15T10:00:00Z"}`, http.StatusBadRequest, dateMessage},
	}

	router := newTestRouter(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "POST", "/tasks", tt.body)
			if tt.wantStatus == http.StatusCreated {
				checkStatus(t, rec, http.StatusCreated)
				return
			}
			checkError(t, rec, tt.wantStatus, tt.wantError)
		})
	}
}

// TestCreateTaskWithFormContentType checks that the body is read as JSON
// whatever the Content-Type says. curl -d without -H sends a form type.
func TestCreateTaskWithFormContentType(t *testing.T) {
	router := newTestRouter(t)
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(`{"title":"Buy groceries"}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	checkStatus(t, rec, http.StatusCreated)
}

// TestGetTask checks that a saved task comes back unchanged.
func TestGetTask(t *testing.T) {
	router := newTestRouter(t)
	created := postTask(t, router, `{"title":"Buy groceries","due_date":"2026-05-15"}`)

	rec := doRequest(t, router, "GET", "/tasks/1", "")

	checkStatus(t, rec, http.StatusOK)
	checkSameTask(t, decodeTask(t, rec), created)
}

// TestGetTaskErrors checks that a malformed id is 400 and a missing task is 404.
func TestGetTaskErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantError  string
	}{
		{"missing task", "/tasks/999", http.StatusNotFound, "task not found"},
		{"not a number", "/tasks/abc", http.StatusBadRequest, "invalid task id"},
		{"zero", "/tasks/0", http.StatusBadRequest, "invalid task id"},
		{"negative", "/tasks/-1", http.StatusBadRequest, "invalid task id"},
		{"too big for int64", "/tasks/99999999999999999999", http.StatusBadRequest, "invalid task id"},
	}

	router := newTestRouter(t)
	postTask(t, router, `{"title":"Buy groceries"}`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "GET", tt.path, "")
			checkError(t, rec, tt.wantStatus, tt.wantError)
		})
	}
}

// TestListTasksEmpty checks that an empty list is [], not null.
func TestListTasksEmpty(t *testing.T) {
	router := newTestRouter(t)

	rec := doRequest(t, router, "GET", "/tasks", "")

	checkStatus(t, rec, http.StatusOK)
	// After decoding, [] and null both give a slice of length 0,
	// so the raw body is checked here.
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}

// TestListTasksNewestFirst checks the order, including two tasks created
// in the same second.
func TestListTasksNewestFirst(t *testing.T) {
	store := newTestStore(t)
	router := NewRouter(store)

	// The API always uses the current time, so these tasks are saved through
	// the store with fixed times. That makes the same-second case certain.
	morning := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	noon := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	saveTaskAt(t, store, "Buy groceries", morning) // id 1
	saveTaskAt(t, store, "Call mom", noon)         // id 2
	saveTaskAt(t, store, "Pay rent", morning)      // id 3, same second as id 1

	rec := doRequest(t, router, "GET", "/tasks", "")

	checkStatus(t, rec, http.StatusOK)
	// Noon is the newest. In the same second, the bigger id is the newer task.
	want := []int64{2, 3, 1}
	if got := ids(decodeList(t, rec)); !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

// saveTaskAt saves a task with the given creation time straight through the store.
func saveTaskAt(t *testing.T, store *storage.Store, title string, createdAt time.Time) {
	t.Helper()
	tk, err := task.New(task.Changes{Title: &title}, createdAt)
	if err != nil {
		t.Fatalf("new task: %v", err)
	}
	if _, err := store.Create(context.Background(), tk); err != nil {
		t.Fatalf("save task: %v", err)
	}
}

// TestListTasksStatusFilter checks ?status= and that other parameters are ignored.
func TestListTasksStatusFilter(t *testing.T) {
	router := newTestRouter(t)
	postTask(t, router, `{"title":"Buy groceries"}`)            // id 1, pending
	postTask(t, router, `{"title":"Call mom","status":"done"}`) // id 2, done
	postTask(t, router, `{"title":"Pay rent"}`)                 // id 3, pending

	tests := []struct {
		name    string
		path    string
		wantIDs []int64
	}{
		{"no filter", "/tasks", []int64{3, 2, 1}},
		{"done", "/tasks?status=done", []int64{2}},
		{"pending", "/tasks?status=pending", []int64{3, 1}},
		{"empty status means no filter", "/tasks?status=", []int64{3, 2, 1}},
		{"unknown parameters are ignored", "/tasks?page=2", []int64{3, 2, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "GET", tt.path, "")
			checkStatus(t, rec, http.StatusOK)
			if got := ids(decodeList(t, rec)); !slices.Equal(got, tt.wantIDs) {
				t.Errorf("ids = %v, want %v", got, tt.wantIDs)
			}
		})
	}
}

// TestListTasksBadQuery checks that a bad filter is 400, not an empty list.
func TestListTasksBadQuery(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantError string
	}{
		{"unknown status", "/tasks?status=archived", task.InvalidStatusMessage},
		{"upper-case status", "/tasks?status=DONE", task.InvalidStatusMessage},
		{"broken escape", "/tasks?status=%zz", "invalid query string"},
		{"semicolon", "/tasks?status=done;x=1", "invalid query string"},
	}

	router := newTestRouter(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "GET", tt.path, "")
			checkError(t, rec, http.StatusBadRequest, tt.wantError)
		})
	}
}

// TestUpdateTask checks that PATCH changes only the fields it sends.
func TestUpdateTask(t *testing.T) {
	tests := []struct {
		name           string
		create         string // body of the POST that creates the task
		patch          string
		wantTitle      string
		wantTaskStatus task.Status
		wantDueDate    string // "" means no due date
	}{
		{"status only", `{"title":"Buy groceries","due_date":"2026-05-15"}`, `{"status":"done"}`, "Buy groceries", task.StatusDone, "2026-05-15"},
		{"title only", `{"title":"Buy groceries","due_date":"2026-05-15"}`, `{"title":"Buy milk"}`, "Buy milk", task.StatusPending, "2026-05-15"},
		{"title is stored as sent", `{"title":"Buy groceries"}`, `{"title":"  Buy milk  "}`, "  Buy milk  ", task.StatusPending, ""},
		{"set a due date", `{"title":"Buy groceries"}`, `{"due_date":"2026-06-01"}`, "Buy groceries", task.StatusPending, "2026-06-01"},
		{"null clears the due date", `{"title":"Buy groceries","due_date":"2026-05-15"}`, `{"due_date":null}`, "Buy groceries", task.StatusPending, ""},
		{"done back to pending", `{"title":"Buy groceries","status":"done"}`, `{"status":"pending"}`, "Buy groceries", task.StatusPending, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(t)
			created := postTask(t, router, tt.create)

			rec := doRequest(t, router, "PATCH", "/tasks/1", tt.patch)

			checkStatus(t, rec, http.StatusOK)
			got := decodeTask(t, rec)
			if got.ID != created.ID || !got.CreatedAt.Equal(created.CreatedAt) {
				t.Errorf("id or created_at changed: got %+v, created %+v", got, created)
			}
			if got.Title != tt.wantTitle || got.Status != tt.wantTaskStatus {
				t.Errorf("title, status = %q, %q; want %q, %q", got.Title, got.Status, tt.wantTitle, tt.wantTaskStatus)
			}
			gotDueDate := ""
			if got.DueDate != nil {
				gotDueDate = *got.DueDate
			}
			if gotDueDate != tt.wantDueDate {
				t.Errorf("due_date = %q, want %q", gotDueDate, tt.wantDueDate)
			}

			// The change is saved, not only returned.
			rec = doRequest(t, router, "GET", "/tasks/1", "")
			checkStatus(t, rec, http.StatusOK)
			checkSameTask(t, decodeTask(t, rec), got)
		})
	}
}

// TestUpdateTaskErrors checks the PATCH errors and that a rejected PATCH
// changes nothing.
func TestUpdateTaskErrors(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{"empty object", `{}`, atLeastOneMessage},
		{"null body", `null`, atLeastOneMessage},
		{"null title", `{"title":null}`, "title must be a string"},
		{"null status", `{"status":null}`, "status must be a string"},
		{"null title with done", `{"title":null,"status":"done"}`, "title must be a string"},
		{"status is a boolean", `{"status":true}`, "status must be a string"},
		{"due date is a number", `{"due_date":5}`, "due_date must be a string"},
		{"blank title", `{"title":"   "}`, "validation failed: title is required"},
		{"unknown status", `{"status":"archived"}`, "validation failed: " + task.InvalidStatusMessage},
		{"empty due date", `{"due_date":""}`, "validation failed: due_date must be a valid date in YYYY-MM-DD format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A new database for each case, so a PATCH that wrongly succeeds
			// cannot change the task the next cases compare against.
			router := newTestRouter(t)
			created := postTask(t, router, `{"title":"Buy groceries","due_date":"2026-05-15"}`)

			rec := doRequest(t, router, "PATCH", "/tasks/1", tt.body)
			checkError(t, rec, http.StatusBadRequest, tt.wantError)

			rec = doRequest(t, router, "GET", "/tasks/1", "")
			checkStatus(t, rec, http.StatusOK)
			checkSameTask(t, decodeTask(t, rec), created)
		})
	}
}

// TestUpdateTaskCheckOrder checks which error wins when a PATCH has several
// problems: a bad id, then a bad body, then a missing task, then a broken rule.
func TestUpdateTaskCheckOrder(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
		wantError  string
	}{
		{"bad id before bad body", "/tasks/abc", `{}`, http.StatusBadRequest, "invalid task id"},
		{"bad body before missing task", "/tasks/999", `{}`, http.StatusBadRequest, atLeastOneMessage},
		{"missing task before broken rule", "/tasks/999", `{"title":""}`, http.StatusNotFound, "task not found"},
		{"missing task", "/tasks/999", `{"title":"Buy milk"}`, http.StatusNotFound, "task not found"},
	}

	router := newTestRouter(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "PATCH", tt.path, tt.body)
			checkError(t, rec, tt.wantStatus, tt.wantError)
		})
	}
}

// TestDeleteTask checks that DELETE answers 200 with the task and that the
// task is gone afterwards.
func TestDeleteTask(t *testing.T) {
	router := newTestRouter(t)
	created := postTask(t, router, `{"title":"Buy groceries","due_date":"2026-05-15"}`)

	rec := doRequest(t, router, "DELETE", "/tasks/1", "")

	checkStatus(t, rec, http.StatusOK)
	checkSameTask(t, decodeTask(t, rec), created)

	checkError(t, doRequest(t, router, "DELETE", "/tasks/1", ""), http.StatusNotFound, "task not found")
	checkError(t, doRequest(t, router, "GET", "/tasks/1", ""), http.StatusNotFound, "task not found")
	checkError(t, doRequest(t, router, "DELETE", "/tasks/abc", ""), http.StatusBadRequest, "invalid task id")

	// AUTOINCREMENT never gives the id of a deleted task to a new one.
	if next := postTask(t, router, `{"title":"Call mom"}`); next.ID != 2 {
		t.Errorf("new id = %d, want 2", next.ID)
	}
}

// TestDoneRule checks the done rule on every path that can break it.
func TestDoneRule(t *testing.T) {
	tests := []struct {
		name       string
		existing   string // body of a POST that runs first; "" means none
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"create done with an empty title", "", "POST", "/tasks", `{"title":"","status":"done"}`, http.StatusBadRequest},
		{"create done without a title", "", "POST", "/tasks", `{"status":"done"}`, http.StatusBadRequest},
		{"create done with a blank title", "", "POST", "/tasks", `{"title":"   ","status":"done"}`, http.StatusBadRequest},
		{"mark done and empty the title", `{"title":"Buy groceries"}`, "PATCH", "/tasks/1", `{"title":"","status":"done"}`, http.StatusBadRequest},
		{"empty the title of a done task", `{"title":"Buy groceries","status":"done"}`, "PATCH", "/tasks/1", `{"title":""}`, http.StatusBadRequest},
		{"mark a task with a title done", `{"title":"Buy groceries"}`, "PATCH", "/tasks/1", `{"status":"done"}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(t)
			if tt.existing != "" {
				postTask(t, router, tt.existing)
			}

			rec := doRequest(t, router, tt.method, tt.path, tt.body)

			if tt.wantStatus == http.StatusOK {
				checkStatus(t, rec, http.StatusOK)
				if got := decodeTask(t, rec); got.Status != task.StatusDone {
					t.Errorf("status = %q, want done", got.Status)
				}
				return
			}
			checkError(t, rec, tt.wantStatus, doneRuleMessage)
		})
	}
}

// TestBadBody checks that a bad body gets a 400 with our own message on both
// POST and PATCH, because both read the body with decodeJSON.
func TestBadBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{"empty", "", "request body must not be empty"},
		{"only spaces", "   ", "request body must not be empty"},
		{"array", `[]`, "request body must be a valid JSON object"},
		{"cut off", `{"title":"Buy`, "request body must be a valid JSON object"},
		{"syntax error", `{"title" "Buy groceries"}`, "request body must be a valid JSON object"},
		{"form data", `title=Buy+groceries`, "request body must be a valid JSON object"},
		{"two objects", `{"title":"a"}{"title":"b"}`, "request body must contain a single JSON object"},
		{"trailing data", `{"title":"a"} x`, "request body must contain a single JSON object"},
		{"title is a number", `{"title":5}`, "title must be a string"},
		{"unknown field", `{"title":"a","id":1}`, `unknown field "id" (allowed fields: title, status, due_date)`},
		{"over 1 MB", `{"title":"` + strings.Repeat("a", maxBodyBytes) + `"}`, "request body must not be larger than 1 MB"},
	}

	router := newTestRouter(t)
	postTask(t, router, `{"title":"Buy groceries"}`) // task 1, the target of the PATCH requests

	for _, tt := range tests {
		t.Run("POST "+tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "POST", "/tasks", tt.body)
			checkError(t, rec, http.StatusBadRequest, tt.wantError)
		})
		t.Run("PATCH "+tt.name, func(t *testing.T) {
			rec := doRequest(t, router, "PATCH", "/tasks/1", tt.body)
			checkError(t, rec, http.StatusBadRequest, tt.wantError)
		})
	}
}

// TestWrongMethod checks the mux's own 405 answer and its Allow header.
func TestWrongMethod(t *testing.T) {
	tests := []struct {
		method    string
		path      string
		wantAllow string
	}{
		{"PUT", "/tasks/1", "DELETE, GET, HEAD, PATCH"},
		{"DELETE", "/tasks", "GET, HEAD, POST"},
	}

	router := newTestRouter(t)
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := doRequest(t, router, tt.method, tt.path, "")
			// The mux writes this response itself, as plain text,
			// so only the code and the header are checked.
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}

// TestCanceledRequestContext checks that the endpoints still work when the
// request context is already canceled. net/http cancels it when the client
// closes its sending side, so the store calls must not depend on it.
func TestCanceledRequestContext(t *testing.T) {
	router := newTestRouter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The requests run in this order: the POST creates task 1 for the others.
	tests := []struct {
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"POST", "/tasks", `{"title":"Buy groceries"}`, http.StatusCreated},
		{"GET", "/tasks", "", http.StatusOK},
		{"GET", "/tasks/1", "", http.StatusOK},
		{"PATCH", "/tasks/1", `{"status":"done"}`, http.StatusOK},
		{"DELETE", "/tasks/1", "", http.StatusOK},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)).WithContext(ctx)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		checkStatus(t, rec, tt.wantStatus)
	}
}
