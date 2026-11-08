//go:build uitour

// Command demodata prepares Agendling's data directory with the demo calendars (see
// internal/demo) and keeps the demo CalDAV accounts running, so the app can be started against
// them for screenshots (used for the Linux/GTK shots):
//
//	go run -tags uitour ./tools/demodata ~/.config/Agendling
package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/Georgy-Garnov/agendling/internal/demo"
	"github.com/Georgy-Garnov/agendling/internal/secret"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: demodata DATA_DIR")
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}
	secret.UseKeyFileIn(dir) // no desktop keyring in a screenshot sandbox
	acc, err := demo.Start(5301, 5302)
	if err != nil {
		log.Fatal(err)
	}
	defer acc.Close()
	store, err := storage.Open(filepath.Join(dir, "calendar.db"))
	if err != nil {
		log.Fatal(err)
	}
	if n, _ := store.Sources(); len(n) > 0 {
		log.Printf("demo data already seeded")
	} else if err := demo.Seed(store, acc); err != nil {
		log.Fatal(err)
	}
	store.Close()
	log.Printf("demo data in %s; accounts on %s and %s; Ctrl+C to stop", dir, acc.Work.URL, acc.Cloud.URL)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
