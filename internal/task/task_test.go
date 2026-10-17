package task

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The fields of Changes are pointers, and Go cannot take the address of a
// literal such as &"a", so these helpers return a pointer to a copy.
func strPtr(s string) *string { return &s }

func statusPtr(s Status) *Status { return &s }

func TestNew(t *testing.T) {
	now := time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		changes Changes
		wantErr string // "" means the task must be valid
	}{
		{"title only", Changes{Title: strPtr("Buy groceries")}, ""},
		{"done with a title", Changes{Title: strPtr("Buy groceries"), Status: statusPtr(StatusDone)}, ""},
		{"done without a title", Changes{Status: statusPtr(StatusDone)}, `a task with status "done" must have a title`},
		{"done with an empty title", Changes{Title: strPtr(""), Status: statusPtr(StatusDone)}, `a task with status "done" must have a title`},
		{"done with a blank title", Changes{Title: strPtr("   "), Status: statusPtr(StatusDone)}, `a task with status "done" must have a title`},
		{"no title", Changes{}, "title is required"},
		{"blank title", Changes{Title: strPtr(" \t\n")}, "title is required"},
		{"title at the limit", Changes{Title: strPtr(strings.Repeat("ə", MaxTitleLength))}, ""},
		{"title over the limit", Changes{Title: strPtr(strings.Repeat("ə", MaxTitleLength+1))}, fmt.Sprintf("title must be at most %d characters", MaxTitleLength)},
		{"unknown status", Changes{Title: strPtr("a"), Status: statusPtr("archived")}, InvalidStatusMessage},
		{"upper-case status", Changes{Title: strPtr("a"), Status: statusPtr("DONE")}, InvalidStatusMessage},
		{"due date", Changes{Title: strPtr("a"), DueDate: strPtr("2026-05-15")}, ""},
		{"leap day", Changes{Title: strPtr("a"), DueDate: strPtr("2024-02-29")}, ""},
		{"past due date", Changes{Title: strPtr("a"), DueDate: strPtr("2001-01-01")}, ""},
		{"impossible date", Changes{Title: strPtr("a"), DueDate: strPtr("2026-02-30")}, "due_date must be a valid date in YYYY-MM-DD format"},
		{"date with a time", Changes{Title: strPtr("a"), DueDate: strPtr("2026-05-15T10:00:00Z")}, "due_date must be a valid date in YYYY-MM-DD format"},
		{"empty due date", Changes{Title: strPtr("a"), DueDate: strPtr("")}, "due_date must be a valid date in YYYY-MM-DD format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.changes, now)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("got error %v, want a validation error", err)
			}
			if !strings.HasSuffix(err.Error(), tt.wantErr) {
				t.Fatalf("got %q, want it to end with %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNewSetsDefaults(t *testing.T) {
	baku := time.FixedZone("Baku", 4*60*60)
	now := time.Date(2026, 5, 8, 18, 30, 0, 123456789, baku)

	got, err := New(Changes{Title: strPtr("Buy groceries")}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != StatusPending {
		t.Errorf("status = %q, want %q", got.Status, StatusPending)
	}
	if got.DueDate != nil {
		t.Errorf("due_date = %q, want nil", *got.DueDate)
	}
	// UTC and whole seconds, like the sample response. RFC3339Nano would show
	// any leftover fraction of a second, so this also checks Truncate.
	if s := got.CreatedAt.Format(time.RFC3339Nano); s != "2026-05-08T14:30:00Z" {
		t.Errorf("created_at = %s, want 2026-05-08T14:30:00Z", s)
	}
}

func TestApplyKeepsUnchangedFields(t *testing.T) {
	original := Task{ID: 1, Title: "Buy groceries", Status: StatusPending, DueDate: strPtr("2026-05-15")}

	got, err := original.Apply(Changes{Status: statusPtr(StatusDone)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 1 || got.Title != "Buy groceries" || got.Status != StatusDone {
		t.Errorf("got %+v", got)
	}
	if got.DueDate == nil || *got.DueDate != "2026-05-15" {
		t.Errorf("due_date was not kept")
	}
}

func TestApplyClearsDueDate(t *testing.T) {
	original := Task{Title: "Buy groceries", Status: StatusPending, DueDate: strPtr("2026-05-15")}

	got, err := original.Apply(Changes{ClearDueDate: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DueDate != nil {
		t.Errorf("due_date = %q, want nil", *got.DueDate)
	}
}

func TestApplyRejectsEmptyTitleOnDoneTask(t *testing.T) {
	original := Task{Title: "Buy groceries", Status: StatusDone}

	_, err := original.Apply(Changes{Title: strPtr("")})
	if !errors.Is(err, ErrValidation) || !strings.HasSuffix(err.Error(), `a task with status "done" must have a title`) {
		t.Fatalf("got %v, want the done rule error", err)
	}
}

func TestApplyDoesNotChangeOriginalOnError(t *testing.T) {
	original := Task{Title: "Buy groceries", Status: StatusPending}

	_, err := original.Apply(Changes{Title: strPtr(""), Status: statusPtr(StatusDone)})
	if err == nil {
		t.Fatal("expected an error")
	}
	if original.Title != "Buy groceries" || original.Status != StatusPending {
		t.Errorf("original changed: %+v", original)
	}
}
