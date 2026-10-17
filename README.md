# Task Tracker API

A REST API for a personal task list, built with Go and SQLite.

## How to run

Requires Go 1.26+ (the SQLite driver needs it; with the default `GOTOOLCHAIN=auto`, Go 1.21+ downloads it automatically). No C compiler is needed. The first build compiles the SQLite driver (up to a minute).

Install:

```sh
git clone https://github.com/TahirGuliyev/task-tracker-api.git && cd task-tracker-api
go mod download
```

Run (listens on http://localhost:8080 and creates `tasks.db` in the current directory):

```sh
go run .
```

Test:

```sh
go test ./...
```

Examples:

```sh
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d '{"title": "Buy groceries", "due_date": "2026-05-15"}'
curl -X PATCH http://localhost:8080/tasks/1 -H "Content-Type: application/json" -d '{"status": "done"}'
curl "http://localhost:8080/tasks?status=done"
```

## Technology choices

- **Go standard library (`net/http`)**: Go 1.22+ routing handles methods and path parameters, so no framework is needed.
- **SQLite with `modernc.org/sqlite`**: pure Go (no C compiler needed); the database is one file with no server to install.
- **`database/sql`** with plain SQL.

## What I would improve

- Server timeouts and graceful shutdown.
- JSON bodies for the router's own 404 and 405 responses.
- Schema migrations instead of `CREATE TABLE IF NOT EXISTS`.
- More than one database connection (WAL mode and a busy timeout), so reads do not wait behind writes.

## Assumptions

- Title is required, but the "done" rule mentions tasks with no title. I chose to require a non-blank title on create and update, which satisfies both; the "done" rule is checked first, so it still returns its own 400.
- "200 chars" means Unicode characters, not bytes; a whitespace-only title counts as empty; titles are stored as sent.
- `status` defaults to `pending`, is case-sensitive, can be `done` at creation and can go back to `pending`.
- `due_date` is `YYYY-MM-DD` (past dates allowed); omitted or `null` means none; in PATCH, `null` clears it.
- PATCH is partial and `{}` returns 400. Unknown fields return 400, including `id` and `created_at`, which the server sets.
- Newest first means `created_at` descending, then `id` descending.
- An invalid `status` filter returns 400, not an empty list; unknown query parameters are ignored.
- DELETE returns 200 with the deleted task (the PDF lists 200, 201, 400 and 404). A malformed id returns 400, a missing task 404.
