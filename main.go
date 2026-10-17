// Command task-tracker-api runs a small REST API for a personal task list.
// The tasks are kept in a SQLite database file.
package main

import (
	"log"
	"net/http"

	"github.com/TahirGuliyev/task-tracker-api/internal/api"
	"github.com/TahirGuliyev/task-tracker-api/internal/storage"
)

func main() {
	// log.Fatal stops the program at once and skips deferred calls,
	// so it is only used here. run returns errors, so its defers still run.
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run opens the database and serves HTTP requests.
func run() error {
	store, err := storage.Open("tasks.db")
	if err != nil {
		return err
	}
	defer store.Close()

	addr := ":8080"
	log.Printf("starting server on %s", addr)

	// ListenAndServe only returns when the server fails to run,
	// for example when the port is already in use.
	return http.ListenAndServe(addr, api.NewRouter(store))
}
