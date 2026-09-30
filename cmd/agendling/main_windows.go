//go:build windows

// Command agendling is a lightweight tray calendar with CalDAV sync and reminders.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	_ "time/tzdata" // IANA zones for TZIDs in CalDAV data; Windows has no zoneinfo

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/storage"
	"github.com/Georgy-Garnov/agendling/internal/ui"
)

func main() {
	minimized := flag.Bool("minimized", false, "start hidden in the system tray")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		fmt.Println(appinfo.Name, appinfo.Version)
		return
	}

	dir, err := appinfo.DataDir()
	if err != nil {
		fatal("Cannot determine data directory: " + err.Error())
	}
	if !ui.AcquireSingleInstance(dir) {
		// Bring the running instance's window up instead of starting a second copy.
		if !ui.ActivateRunningInstance() {
			walk.MsgBox(nil, appinfo.Name, appinfo.Name+" is already running — look for its icon in the system tray.", walk.MsgBoxIconInformation)
		}
		return
	}

	if f, err := os.OpenFile(filepath.Join(dir, appinfo.ID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}

	store, err := storage.Open(filepath.Join(dir, "calendar.db"))
	if err != nil {
		fatal("Cannot open database: " + err.Error())
	}
	defer store.Close()

	settings := store.Settings()
	ui.MigrateStartupEntry(settings.RunAtStartup)

	app := ui.NewApp(store)
	if err := app.Run(*minimized || settings.StartMinimized); err != nil {
		fatal(err.Error())
	}
	store.Close()
	// Don't wait for background sync goroutines; exit as soon as the UI is gone.
	os.Exit(0)
}

func fatal(msg string) {
	log.Print(msg)
	walk.MsgBox(nil, appinfo.Name, msg, walk.MsgBoxIconError)
	os.Exit(1)
}
