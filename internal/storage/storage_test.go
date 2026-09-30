package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

func TestStoreRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "cal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	src := &model.CalendarSource{Name: "Work", Type: model.SourceCalDAV, URL: "https://caldav.example/", Username: "u", Password: "p", Color: "#ff0000", SyncInterval: 15, Enabled: true}
	if err := st.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	got, err := st.Source(src.ID)
	if err != nil || got.Password != "p" {
		t.Fatalf("source: %+v %v", got, err)
	}

	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	allDay := &model.Event{CalendarID: src.ID, UID: "a", Title: "Day", StartTime: start, EndTime: start.AddDate(0, 0, 1), AllDay: true}
	dirty := &model.Event{CalendarID: src.ID, UID: "b", Title: "Local edit", StartTime: start.Add(10 * time.Hour), EndTime: start.Add(11 * time.Hour), Dirty: true,
		Reminders: []model.ReminderOffset{model.MinutesOffset(5), model.MinutesOffset(60)}}
	for _, e := range []*model.Event{allDay, dirty} {
		if err := st.SaveEvent(e); err != nil {
			t.Fatal(err)
		}
	}

	// Remote refresh must keep the pending local edit and replace the rest.
	remote := []*model.Event{
		{UID: "b", Title: "Remote version", StartTime: start, EndTime: start.Add(time.Hour)},
		{UID: "c", Title: "New", StartTime: start, EndTime: start.Add(time.Hour)},
	}
	if err := st.ReplaceRemoteEvents(src.ID, remote); err != nil {
		t.Fatal(err)
	}
	evs, err := st.EventsInRange(start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]*model.Event{}
	for _, e := range evs {
		titles[e.Title] = e
	}
	if len(evs) != 2 || titles["Local edit"] == nil || titles["New"] == nil {
		t.Fatalf("unexpected events: %v", titles)
	}
	if r := titles["Local edit"].Reminders; len(r) != 2 || r[1].Minutes() != 60 {
		t.Fatalf("reminders: %v", r)
	}

	s := st.Settings()
	s.CustomSoundPath = `C:\sound.mp3`
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if st.Settings().CustomSoundPath != `C:\sound.mp3` {
		t.Fatal("settings not persisted")
	}
}
