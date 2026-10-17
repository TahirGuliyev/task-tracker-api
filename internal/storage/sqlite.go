// Package storage saves tasks in a SQLite database.
// It turns tasks into table rows and rows back into tasks.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/TahirGuliyev/task-tracker-api/internal/task"

	_ "modernc.org/sqlite" // registers the "sqlite" driver with database/sql
)

// ErrNotFound is returned when no task has the requested id.
// Callers check for it with errors.Is.
var ErrNotFound = errors.New("task not found")

// taskColumns lists the columns in the order scanTask reads them.
// Every query that returns tasks uses it, so the order always matches.
const taskColumns = "id, title, status, due_date, created_at"

// Store keeps tasks in a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens the SQLite database file at path, creating the file if needed,
// and makes sure the tasks table exists.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database %q: %w", path, err)
	}

	// SQLite allows one writer at a time. With one connection, requests wait
	// for each other instead of failing with "database is locked".
	db.SetMaxOpenConns(1)

	// AUTOINCREMENT never reuses the id of a deleted task,
	// so a bigger id always belongs to a newer task.
	const createTable = `CREATE TABLE IF NOT EXISTS tasks (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		title      TEXT NOT NULL,
		status     TEXT NOT NULL,
		due_date   TEXT,
		created_at TEXT NOT NULL
	)`

	// sql.Open does not connect yet. The first statement does,
	// so this is where a bad path fails.
	if _, err := db.Exec(createTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %q: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// scanner is anything with a Scan method. Both *sql.Row and *sql.Rows
// have this method, so scanTask can read from either.
type scanner interface {
	Scan(dest ...any) error
}

// scanTask reads one row, with the columns in taskColumns order, into a task.
// It is the only place that calls Scan.
func scanTask(row scanner) (task.Task, error) {
	var t task.Task
	var createdAt string

	// due_date goes into a *string, so a NULL due_date becomes nil.
	err := row.Scan(&t.ID, &t.Title, &t.Status, &t.DueDate, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, ErrNotFound
	}
	if err != nil {
		return task.Task{}, err
	}

	// created_at is stored as text, so it is turned back into a time.Time here.
	t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return task.Task{}, err
	}
	return t, nil
}

// Create saves a new task and returns it with the id the database gave it.
func (s *Store) Create(ctx context.Context, t task.Task) (task.Task, error) {
	// created_at is written as text such as "2026-05-08T14:30:00Z".
	// A time.Time argument would be stored in Go's String() format instead.
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO tasks (title, status, due_date, created_at) VALUES (?, ?, ?, ?)",
		t.Title, t.Status, t.DueDate, t.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return task.Task{}, err
	}

	t.ID, err = result.LastInsertId()
	if err != nil {
		return task.Task{}, err
	}
	return t, nil
}

// Get returns the task with the given id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (task.Task, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id)
	return scanTask(row)
}
