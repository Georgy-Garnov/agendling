//go:build windows

package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// TestRenderViews paints every view offscreen. Set RENDER_DIR to keep the PNGs for inspection.
func TestRenderViews(t *testing.T) {
	// walk creates its stock GDI objects on first window creation. Without a comctl v6
	// manifest (test binaries have none) the window itself fails later, which is fine here.
	if mw, err := walk.NewMainWindow(); err == nil {
		defer mw.Dispose()
	}

	store, err := storage.Open(filepath.Join(t.TempDir(), "cal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	work := &model.CalendarSource{Name: "Work", Type: model.SourceCalDAV, URL: "https://x", Color: "#0b8043", Enabled: true}
	home := &model.CalendarSource{Name: "Home", Type: model.SourceLocal, Color: "#8e24aa", Enabled: true}
	for _, s := range []*model.CalendarSource{work, home} {
		if err := store.SaveSource(s); err != nil {
			t.Fatal(err)
		}
	}
	anchor := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local)
	at := func(day, h, m int) time.Time {
		return anchor.AddDate(0, 0, day).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	events := []*model.Event{
		{CalendarID: work.ID, UID: "1", Title: "Daily standup", StartTime: at(-2, 10, 0), EndTime: at(-2, 10, 15), RRule: "FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR"},
		{CalendarID: work.ID, UID: "2", Title: "Design review", Location: "Room 4", StartTime: at(0, 11, 0), EndTime: at(0, 12, 30)},
		{CalendarID: home.ID, UID: "3", Title: "Dentist", StartTime: at(0, 11, 30), EndTime: at(0, 12, 0)},
		{CalendarID: work.ID, UID: "4", Title: "1:1", StartTime: at(0, 12, 0), EndTime: at(0, 12, 30)},
		{CalendarID: home.ID, UID: "5", Title: "Vacation", StartTime: at(1, 0, 0), EndTime: at(4, 0, 0), AllDay: true},
		{CalendarID: work.ID, UID: "6", Title: "Release", StartTime: at(2, 0, 0), EndTime: at(3, 0, 0), AllDay: true},
		{CalendarID: home.ID, UID: "7", Title: "Gym", StartTime: at(-1, 18, 0), EndTime: at(-1, 19, 30), RRule: "FREQ=WEEKLY;BYDAY=TU,TH"},
		{CalendarID: work.ID, UID: "8", Title: "Planning", StartTime: at(2, 14, 0), EndTime: at(2, 16, 0)},
		{CalendarID: work.ID, UID: "9", Title: "Late sync", StartTime: at(2, 14, 0), EndTime: at(2, 14, 45)},
	}
	for _, e := range events {
		if err := store.SaveEvent(e); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp(store)
	v := app.view
	v.anchor = anchor
	out := os.Getenv("RENDER_DIR")

	for mode, name := range viewNames {
		v.mode = viewMode(mode)
		v.Reload()
		bmp, err := walk.NewBitmapForDPI(walk.Size{Width: 1100, Height: 700}, 96)
		if err != nil {
			t.Fatal(err)
		}
		c, err := walk.NewCanvasFromImage(bmp)
		if err != nil {
			t.Fatal(err)
		}
		if err := v.render(c, walk.Rectangle{Width: 1100, Height: 700}); err != nil {
			t.Fatal(err)
		}
		c.Dispose()
		hits := 0
		for _, h := range v.hits {
			if h.kind == hitEvent {
				hits++
			}
		}
		if hits == 0 {
			t.Errorf("%s: no events painted", name)
		}
		if out != "" {
			img, err := bmp.ToImage()
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(out, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, img)
			f.Close()
		}
		bmp.Dispose()
	}
}
