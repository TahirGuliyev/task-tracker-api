// Package task defines a task and the rules every task must follow.
// It knows nothing about HTTP or the database, so the rules are easy to test.
package task

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxTitleLength is the maximum title length, counted in characters (not bytes).
const MaxTitleLength = 200

// ErrValidation marks every error caused by breaking a rule.
// Those errors wrap it, so callers check for it with errors.Is.
var ErrValidation = errors.New("validation failed")

// validationError returns ErrValidation together with a message for the client.
func validationError(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, message)
}

// Status is the state of a task.
type Status string

// The only allowed statuses.
const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
)

// InvalidStatusMessage is the error text for a status that is not allowed.
const InvalidStatusMessage = `status must be "pending" or "done"`

// IsValid reports whether s is one of the allowed statuses.
func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusDone:
		return true
	default:
		return false
	}
}

// Task is one item in the to-do list.
// The fields are in the same order as the sample JSON response.
type Task struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	// DueDate is kept as "YYYY-MM-DD" text: a time.Time would be written with
	// a time part. nil means "no due date" and is written as null.
	DueDate   *string   `json:"due_date"`
	CreatedAt time.Time `json:"created_at"`
}

// Changes holds the values a request wants to set.
// A nil field means "keep the current value".
type Changes struct {
	Title        *string
	Status       *Status
	DueDate      *string
	ClearDueDate bool // set when a PATCH request sends "due_date": null
}

// New creates a task from the given changes and checks the rules.
// A new task is pending unless the changes say otherwise.
// now is a parameter, so tests can pass a fixed time.
func New(changes Changes, now time.Time) (Task, error) {
	t := Task{
		Status:    StatusPending,
		CreatedAt: now.UTC().Truncate(time.Second), // UTC and whole seconds, like the sample
	}
	return t.Apply(changes)
}

// Apply returns a copy of t with the changes applied.
// It has a value receiver, so it works on a copy and the original task
// never changes, even when the result breaks a rule.
func (t Task) Apply(changes Changes) (Task, error) {
	if changes.Title != nil {
		t.Title = *changes.Title
	}
	if changes.Status != nil {
		t.Status = *changes.Status
	}
	if changes.ClearDueDate {
		t.DueDate = nil
	} else if changes.DueDate != nil {
		t.DueDate = changes.DueDate
	}

	// Check the final state, so the rules hold no matter which fields were sent.
	if err := t.validate(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// validate returns the first rule the task breaks, or nil.
func (t Task) validate() error {
	hasTitle := strings.TrimSpace(t.Title) != ""

	// Checked first, so this rule always gets its own clear message.
	if t.Status == StatusDone && !hasTitle {
		return validationError(`a task with status "done" must have a title`)
	}
	if !hasTitle {
		return validationError("title is required")
	}
	if utf8.RuneCountInString(t.Title) > MaxTitleLength {
		return validationError(fmt.Sprintf("title must be at most %d characters", MaxTitleLength))
	}
	if !t.Status.IsValid() {
		return validationError(InvalidStatusMessage)
	}
	if t.DueDate != nil {
		// time.DateOnly is the layout "2006-01-02", so only YYYY-MM-DD is accepted.
		if _, err := time.Parse(time.DateOnly, *t.DueDate); err != nil {
			return validationError("due_date must be a valid date in YYYY-MM-DD format")
		}
	}
	return nil
}
