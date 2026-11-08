package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

func TestSplitPreservesExistingEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "split.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	src := &model.CalendarSource{Name: "NAS", Type: model.SourceCalDAV, URL: "https://nas/caldav/", Username: "user", Password: "secret", Enabled: true, SkipTLSVerify: true}
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	events := []*model.Event{
		{CalendarID: src.ID, UID: "personal", Href: "/personal/event.ics", StartTime: now, EndTime: now.Add(time.Hour)},
		{CalendarID: src.ID, UID: "shared", Href: "https://nas/shared/event.ics", StartTime: now, EndTime: now.Add(time.Hour), Dirty: true},
		{CalendarID: src.ID, UID: "shared", Href: "/shared/event.ics", StartTime: now, EndTime: now.Add(time.Hour), RecurrenceID: now, Deleted: true},
		{CalendarID: src.ID, UID: "pending", StartTime: now, EndTime: now.Add(time.Hour), Dirty: true},
	}
	for _, ev := range events {
		if err := store.SaveEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	first, second := *src, *src
	first.Name, first.URL, first.CalendarPath, first.Color = "Personal", "https://nas/personal/", "/personal/", "#4285f4"
	second.ID = 0
	second.Name, second.URL, second.CalendarPath, second.Color = "Shared", "https://nas/shared/", "/shared/", "#e67c73"
	calendars := []*model.CalendarSource{&first, &second}
	for _, cal := range calendars {
		cal.CollectionURL = cal.URL
	}

	if err := store.ReconcileCalDAVSource(src, calendars); err != nil {
		t.Fatal(err)
	}
	for i, ev := range events {
		got, err := store.Event(ev.ID)
		if err != nil {
			t.Fatal(err)
		}
		wantID := src.ID
		if i == 1 || i == 2 {
			wantID = calendars[1].ID
		}
		if got.CalendarID != wantID || got.Dirty != ev.Dirty || got.Deleted != ev.Deleted || !got.RecurrenceID.Equal(ev.RecurrenceID) {
			t.Fatalf("event changed unexpectedly: %+v", got)
		}
	}
	child, err := store.Source(calendars[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Password != "secret" || !child.SkipTLSVerify || !child.Enabled || child.Username != "user" {
		t.Fatal("account preferences were not retained")
	}
}

func TestReconcileDuplicatePendingEvents(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending-wins", true: "two-pending-rollback"}[conflict], func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "merge.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			src := &model.CalendarSource{Name: "NAS", Type: model.SourceCalDAV, URL: "https://nas/", Username: "user", Enabled: true}
			if err := store.SaveSource(src); err != nil {
				t.Fatal(err)
			}
			existing := *src
			existing.ID = 0
			existing.Name, existing.URL, existing.CalendarPath = "Shared custom", "https://nas/shared/", "/shared/"
			if err := store.SaveSource(&existing); err != nil {
				t.Fatal(err)
			}
			now := time.Now().Truncate(time.Second)
			old := &model.Event{CalendarID: src.ID, UID: "same", Href: "/shared/event.ics", Title: "Mixed copy", StartTime: now, EndTime: now.Add(time.Hour), Dirty: conflict}
			pending := *old
			pending.ID, pending.CalendarID, pending.Dirty, pending.Title = 0, existing.ID, true, "Pending edit"
			for _, ev := range []*model.Event{old, &pending} {
				if err := store.SaveEvent(ev); err != nil {
					t.Fatal(err)
				}
			}
			first, second := *src, *src
			first.CollectionURL, first.CalendarPath = "https://nas/personal/", "/personal/"
			first.DiscoveryURL, first.DiscoveryName = src.URL, src.Name
			second.CollectionURL, second.CalendarPath, second.URL = "https://nas/shared/", "/shared/", "https://nas/shared/"
			err = store.ReconcileCalDAVSource(src, []*model.CalendarSource{&first, &second})
			if conflict {
				if err == nil {
					t.Fatal("two pending copies were silently merged")
				}
				sources, _ := store.Sources()
				if len(sources) != 2 || sources[0].CollectionURL != "" {
					t.Fatal("failed merge did not roll back configuration")
				}
				for _, id := range []int64{old.ID, pending.ID} {
					if _, err := store.Event(id); err != nil {
						t.Fatal("failed merge lost an event")
					}
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				events, err := store.EventsByUID(existing.ID, "same")
				if err != nil || len(events) != 1 || !events[0].Dirty || events[0].Title != "Pending edit" {
					t.Fatal("pending duplicate edit was not preserved")
				}
				reused, err := store.Source(existing.ID)
				if err != nil || reused.Name != "Shared custom" {
					t.Fatal("existing subscription preferences were overwritten")
				}
			}
		})
	}
}

func TestMigrateCollectionMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := schema
	for _, line := range []string{"collection_url TEXT NOT NULL DEFAULT '',", "discovery_url TEXT NOT NULL DEFAULT '',", "discovery_name TEXT NOT NULL DEFAULT '',", "revision INTEGER NOT NULL DEFAULT 1,"} {
		legacy = strings.ReplaceAll(legacy, line, "")
	}
	if _, err := db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sources(name,type,url,username) VALUES('Old','caldav','https://nas/','user')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sources, err := store.Sources()
	if err != nil || len(sources) != 1 {
		t.Fatalf("legacy source missing: %v", err)
	}
	src := sources[0]
	if src.Revision != 1 || src.CollectionURL != "" || src.DiscoveryURL != "" {
		t.Fatal("unexpected legacy metadata")
	}
	stale := *src
	src.Color = "#123456"
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(&stale); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("stale settings accepted: %v", err)
	}
}
