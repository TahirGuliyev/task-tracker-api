# Task Tracker API

## How to run

Requires Go 1.26+ (the SQLite driver needs it; with the default `GOTOOLCHAIN=auto`, Go 1.21+ downloads it automatically) or Docker. The first build compiles the SQLite driver (up to a minute).

Install:

```sh
git clone https://github.com/TahirGuliyev/task-tracker-api.git && cd task-tracker-api
go mod download
```

Run (listens on port 8080 and creates `./tasks.db`):

```sh
go run .
```

Environment variables: `PORT` (default `8080`) and `DB_PATH` (default `tasks.db`):

```sh
PORT=9090 DB_PATH=/tmp/tasks.db go run .
```

Docker (data persists in the `task-data` volume):

```sh
docker build -t task-tracker . && docker run --rm -p 8080:8080 -v task-data:/data task-tracker
```

Test:

```sh
go test ./...
```

Examples:

```sh
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d '{"title": "Buy groceries", "due_date": "2026-05-15"}'
curl -X PATCH http://localhost:8080/tasks/1 -H "Content-Type: application/json" -d '{"status": "done"}'
curl "http://localhost:8080/tasks?status=done&limit=10&offset=0"
```

## Technology choices

- **Standard library `net/http`**: Go 1.22+ routing handles methods and path parameters, so no framework is needed.
- **SQLite** (`modernc.org/sqlite`, plain SQL via `database/sql`): pure Go (no C compiler needed); the database is one file with no server to install.

## What I would improve

- Server timeouts and graceful shutdown.
- JSON bodies for the router's 404 and 405 responses.
- Schema migrations.
- More than one database connection (WAL mode and a busy timeout), so reads do not wait behind writes.
- A non-root Docker user (the `/data` volume needs a matching owner).
- A total count for pagination.

## Assumptions

- Title is required, but the "done" rule mentions tasks with no title. I chose to require a non-blank title on create and update, which satisfies both; the "done" rule is checked first, so it still returns its own 400.
- "200 chars" counts Unicode characters, not bytes; whitespace-only titles count as empty; titles are stored as sent.
- `status` defaults to `pending`, is case-sensitive, can be `done` at creation and can be reopened.
- `due_date` is `YYYY-MM-DD` (past dates allowed); omitted or `null` means none; in PATCH, `null` clears it.
- PATCH is partial; `{}` returns 400. Unknown fields, including the server-set `id` and `created_at`, return 400.
- Newest first means `created_at` descending, then `id` descending.
- `limit` and `offset` are optional, so `GET /tasks` still lists all tasks. An invalid `status`, `limit` or `offset` returns 400; unknown query parameters are ignored.
- DELETE returns 200 with the deleted task, as the PDF lists no 204. A malformed id returns 400, a missing task 404.
