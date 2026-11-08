//go:build windows && uitour

// Command uishots opens Agendling with demo data (see internal/demo), saves screenshots of its
// windows into the given directory, and exits; the screenshots in docs/ are made with it. Build
// with "make uishots" and run "uishots.exe DIR" on Windows.
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
	if len(os.Args) != 2 {
		log.Fatal("usage: uishots DIR")
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	data, err := os.MkdirTemp("", "agendling-uishots")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(data)
	os.Setenv("APPDATA", data) // appinfo.DataDir: keep away from the user's real calendars

	acc, err := demo.Start(0, 0)
	if err != nil {
		log.Fatal(err)
	}
	defer acc.Close()
	store, err := storage.Open(filepath.Join(data, "cal.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := demo.Seed(store, acc); err != nil {
		log.Fatal(err)
	}
	if err := ui.RunTour(store, dir); err != nil {
		log.Fatal(err)
	}
}
