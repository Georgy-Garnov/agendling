//go:build windows && uitour

// Command demoapp runs Agendling on Windows with the demo calendars (see internal/demo), kept in
// %LOCALAPPDATA%\agendling-demo away from the user's real calendars, to try changes by hand.
// Build with "make demo"; delete that folder to start over.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/Georgy-Garnov/agendling/internal/demo"
	"github.com/Georgy-Garnov/agendling/internal/storage"
	"github.com/Georgy-Garnov/agendling/internal/ui"
)

func main() {
	// A local Windows folder: SQLite cannot lock a database on a WSL share (\\wsl.localhost).
	data := filepath.Join(os.Getenv("LOCALAPPDATA"), "agendling-demo")
	if err := os.MkdirAll(data, 0o755); err != nil {
		log.Fatal(err)
	}
	os.Setenv("APPDATA", data) // appinfo.DataDir
	acc, err := demo.Start(5301, 5302)
	if err != nil {
		log.Fatal(err)
	}
	defer acc.Close()
	db := filepath.Join(data, "Agendling", "calendar.db")
	_, statErr := os.Stat(db)
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		log.Fatal(err)
	}
	store, err := storage.Open(db)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if os.IsNotExist(statErr) {
		if err := demo.Seed(store, acc); err != nil {
			log.Fatal(err)
		}
	}
	if err := ui.NewApp(store).Run(false); err != nil {
		log.Fatal(err)
	}
}
