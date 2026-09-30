//go:build linux

// Command agendling is a lightweight tray calendar with CalDAV sync and reminders (GTK 3 on Linux).
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/gtkui"
	"github.com/Georgy-Garnov/agendling/internal/storage"
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
		log.Fatalf("data directory: %v", err)
	}
	activations, ok := singleInstance(dir)
	if !ok {
		return // the running instance was asked to show its window
	}
	if f, err := os.OpenFile(filepath.Join(dir, appinfo.ID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}

	store, err := storage.Open(filepath.Join(dir, "calendar.db"))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	app := gtkui.NewApp(store)
	if err := app.Run(*minimized || store.Settings().StartMinimized, activations); err != nil {
		log.Fatal(err)
	}
	store.Close()
	os.Exit(0) // don't wait for background sync goroutines
}

// singleInstance takes an exclusive lock in dir. If another instance holds it, that
// instance is asked (over a Unix socket) to show its window and false is returned.
// The returned channel delivers such requests from later launches.
func singleInstance(dir string) (<-chan struct{}, bool) {
	sock := filepath.Join(dir, "instance.sock")
	lock, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, true // can't coordinate; just run
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
		}
		return nil, false
	}
	// The lock is held until the process exits (the file stays open).
	os.Remove(sock)
	ch := make(chan struct{}, 1)
	l, err := net.Listen("unix", sock)
	if err != nil {
		return ch, true
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ch, true
}
