package caldav

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	dav "github.com/emersion/go-webdav/caldav"
)

const sharedPath = "/u/calendars/shared/"

type multiBackend struct{ *memBackend }

func (b *multiBackend) ListCalendars(context.Context) ([]dav.Calendar, error) {
	return []dav.Calendar{{Path: calPath, Name: "Personal Calendar", SupportedComponentSet: []string{"VEVENT"}}, {Path: sharedPath, Name: "SchubboCal", SupportedComponentSet: []string{"VEVENT"}}}, nil
}
func (b *multiBackend) GetCalendar(ctx context.Context, p string) (*dav.Calendar, error) {
	cals, _ := b.ListCalendars(ctx)
	for _, cal := range cals {
		if cleanDir(p) == cal.Path {
			return &cal, nil
		}
	}
	return nil, fmt.Errorf("unknown calendar")
}
func (b *multiBackend) ListCalendarObjects(ctx context.Context, p string, req *dav.CalendarCompRequest) ([]dav.CalendarObject, error) {
	all, err := b.memBackend.ListCalendarObjects(ctx, p, req)
	var result []dav.CalendarObject
	for _, obj := range all {
		if strings.HasPrefix(obj.Path, cleanDir(p)) {
			result = append(result, obj)
		}
	}
	return result, err
}
func (b *multiBackend) QueryCalendarObjects(ctx context.Context, p string, q *dav.CalendarQuery) ([]dav.CalendarObject, error) {
	all, err := b.ListCalendarObjects(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	return dav.Filter(q, all)
}

func TestSeparateCalendarsReadAndWrite(t *testing.T) {
	ctx := context.Background()
	backend := &multiBackend{&memBackend{objs: map[string]dav.CalendarObject{}}}
	server := httptest.NewServer(&dav.Handler{Backend: backend})
	defer server.Close()
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	// Identical UIDs in different calendars must remain independent.
	for _, p := range []string{calPath, sharedPath} {
		ev := &model.Event{UID: "same-uid", Title: p, StartTime: start, EndTime: start.Add(time.Hour)}
		backend.PutCalendarObject(ctx, p+"same.ics", ics.FromEvents([]*model.Event{ev}), nil)
	}
	store, src := newTestSource(t, server.URL)
	src.Name = "NAS"
	src.CalendarPath = calPath
	src.SkipTLSVerify = true
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	// A mixed account from an earlier version is separated automatically on startup.
	if err := saveState(store, src.ID, syncState{Version: 2, Collections: []string{calPath, sharedPath}, FullAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(store, nil)
	finished := make(chan Status, 10)
	engine.onStatus = func(s Status) {
		if !s.Running {
			finished <- s
		}
	}
	engine.Restart()
	defer engine.Stop()
	seen := map[int64]bool{}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for len(seen) < 2 {
		select {
		case status := <-finished:
			if status.Err != nil {
				t.Fatal(status.Err)
			}
			seen[status.SourceID] = true
		case <-timeout.C:
			t.Fatal("workers did not synchronize both discovered calendars")
		}
	}
	engine.Stop()
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %d", len(sources))
	}
	if sources[0].Name != "NAS / Personal Calendar" || sources[1].Name != "NAS / SchubboCal" {
		t.Fatalf("names: %q, %q", sources[0].Name, sources[1].Name)
	}
	if sources[0].Color == sources[1].Color {
		t.Fatal("calendar colors are identical")
	}
	for _, src := range sources {
		if !src.SkipTLSVerify {
			t.Fatal("TLS preference lost")
		}
		events, err := store.EventsByUID(src.ID, "same-uid")
		if err != nil || len(events) != 1 {
			t.Fatalf("calendar %s: events %d, error %v", src.Name, len(events), err)
		}
		if events[0].Title != src.CalendarPath {
			t.Fatalf("wrong event in %s: %s", src.Name, events[0].Title)
		}
		ev := &model.Event{CalendarID: src.ID, UID: "new-" + fmt.Sprint(src.ID), Title: src.Name, StartTime: start, EndTime: start.Add(time.Hour), Dirty: true}
		if err := store.SaveEvent(ev); err != nil {
			t.Fatal(err)
		}
		if _, err := SyncSource(ctx, store, src); err != nil {
			t.Fatal(err)
		}
		savedEvents, err := store.EventsByUID(src.ID, ev.UID)
		if err != nil || len(savedEvents) != 1 {
			t.Fatalf("written event missing: %d, %v", len(savedEvents), err)
		}
		saved := savedEvents[0]
		if !strings.HasPrefix(saved.Href, src.CalendarPath) {
			t.Fatalf("write went to wrong calendar: %s", saved.Href)
		}
		if _, err := backend.GetCalendarObject(ctx, saved.Href, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Repeated synchronization must not create additional calendar sources.
	sources, err = store.Sources()
	if err != nil || len(sources) != 2 {
		t.Fatalf("sources after repeated sync: %d, %v", len(sources), err)
	}
}
