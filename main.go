// Command task-tracker-api runs a small REST API for a personal task list.
// The tasks are kept in a SQLite database file.
package main

import (
	"log"
	"net/http"
	"os"

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

// run reads the settings, opens the database and serves HTTP requests.
func run() error {
	port := getEnv("PORT", "8080")
	dbPath := getEnv("DB_PATH", "tasks.db")

	store, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	addr := ":" + port
	log.Printf("starting server on %s, database %s", addr, dbPath)

	// ListenAndServe only returns when the server fails to run,
	// for example when the port is already in use.
	return http.ListenAndServe(addr, api.NewRouter(store))
}

// getEnv returns the value of the environment variable key, or fallback.
// os.Getenv returns "" for a variable that is not set, so an empty
// variable also gets the fallback.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
