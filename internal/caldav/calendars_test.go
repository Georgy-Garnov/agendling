package caldav

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// The scenarios behind the per-calendar sources of pull request #5, on the account model of
// v0.4.0: an account keeps all its calendars, and each event is bound to its own calendar.

// liveBackend is an account whose calendars can be added and deleted while the app syncs; a
// write to a calendar that does not exist fails like on real servers (409 Conflict).
type liveBackend struct {
	multiBackend
	calMu      sync.Mutex
	cals       []caldav.Calendar
	failWrites bool // the server refuses writes for a while
}

func newLiveBackend(names ...string) *liveBackend {
	b := &liveBackend{multiBackend: multiBackend{memBackend: memBackend{objs: map[string]caldav.CalendarObject{}}, queried: map[string]int{}}}
	for _, n := range names {
		b.add(n)
	}
	return b
}

func liveCal(name string) string { return "/u/calendars/" + strings.ToLower(name) + "/" }

func (b *liveBackend) add(name string) {
	b.calMu.Lock()
	defer b.calMu.Unlock()
	b.cals = append(b.cals, caldav.Calendar{Path: liveCal(name), Name: name, SupportedComponentSet: []string{"VEVENT"}})
}

// remove deletes a calendar with its events, as when it is deleted in the web interface.
func (b *liveBackend) remove(name string) {
	b.calMu.Lock()
	for i, c := range b.cals {
		if c.Name == name {
			b.cals = append(b.cals[:i], b.cals[i+1:]...)
			break
		}
	}
	b.calMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	for p := range b.objs {
		if strings.HasPrefix(p, liveCal(name)) {
			delete(b.objs, p)
		}
	}
}

func (b *liveBackend) setFailWrites(v bool) {
	b.calMu.Lock()
	defer b.calMu.Unlock()
	b.failWrites = v
}

func (b *liveBackend) has(coll string) bool {
	b.calMu.Lock()
	defer b.calMu.Unlock()
	for _, c := range b.cals {
		if c.Path == coll {
			return true
		}
	}
	return false
}

func (b *liveBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	b.calMu.Lock()
	defer b.calMu.Unlock()
	return append([]caldav.Calendar(nil), b.cals...), nil
}

func (b *liveBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	cals, _ := b.ListCalendars(ctx)
	for _, c := range cals {
		if c.Path == p {
			return &c, nil
		}
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no calendar %s", p))
}

func (b *liveBackend) PutCalendarObject(ctx context.Context, p string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	if !b.has(CollectionOf(p)) {
		return nil, webdav.NewHTTPError(http.StatusConflict, fmt.Errorf("no calendar for %s", p))
	}
	b.calMu.Lock()
	fail := b.failWrites
	b.calMu.Unlock()
	if fail {
		return nil, webdav.NewHTTPError(http.StatusServiceUnavailable, fmt.Errorf("maintenance"))
	}
	return b.memBackend.PutCalendarObject(ctx, p, cal, opts)
}

func (b *liveBackend) count(coll string) int {
	objs, _ := b.ListCalendarObjects(context.Background(), coll, nil)
	return len(objs)
}

type liveAccount struct {
	t       *testing.T
	backend *liveBackend
	store   *storage.Store
	src     *model.CalendarSource
	start   time.Time
}

func newLiveAccount(t *testing.T, names ...string) *liveAccount {
	backend := newLiveBackend(names...)
	srv := httptest.NewServer(&caldav.Handler{Backend: backend})
	t.Cleanup(srv.Close)
	store, src := newTestSource(t, srv.URL)
	return &liveAccount{t: t, backend: backend, store: store, src: src, start: time.Now().Add(24 * time.Hour).Truncate(time.Hour)}
}

func (a *liveAccount) sync() ([]*model.Event, error) {
	return SyncSource(context.Background(), a.store, a.src)
}

func (a *liveAccount) mustSync() {
	a.t.Helper()
	if _, err := a.sync(); err != nil {
		a.t.Fatal(err)
	}
}

// fullRefresh makes the next sync rediscover the calendars, as it does once a day.
func (a *liveAccount) fullRefresh() {
	st := loadState(a.store, a.src.ID)
	st.FullAt = time.Time{}
	if err := saveState(a.store, a.src.ID, st); err != nil {
		a.t.Fatal(err)
	}
}

func (a *liveAccount) event(uid string) *model.Event {
	a.t.Helper()
	evs, err := a.store.EventsByUID(a.src.ID, uid)
	if err != nil || len(evs) == 0 {
		a.t.Fatalf("event %s: %v", uid, err)
	}
	return evs[0]
}

// A calendar deleted on the server must not take the local changes made to it into another
// calendar of the account: they stay here, and the sync says so.
func TestDeletedCalendarKeepsItsChanges(t *testing.T) {
	a := newLiveAccount(t, "Home", "Work")
	home, work := liveCal("Home"), liveCal("Work")
	a.backend.put(home, "edited", "Dentist", a.start)
	a.backend.put(home, "removed", "Old plan", a.start.Add(time.Hour))
	a.backend.put(work, "w", "Standup", a.start.Add(2*time.Hour))
	a.mustSync()

	// Offline: edit one Home event, delete another, and add a new one to Home.
	ed := a.event("edited")
	ed.Title, ed.Dirty = "Dentist (moved)", true
	rm := a.event("removed")
	rm.Deleted, rm.Dirty = true, true
	added := &model.Event{CalendarID: a.src.ID, UID: "added", Title: "New at home", StartTime: a.start.Add(3 * time.Hour),
		EndTime: a.start.Add(4 * time.Hour), Href: ObjectHref(home, "added"), Dirty: true}
	for _, e := range []*model.Event{ed, rm, added} {
		if err := a.store.SaveEvent(e); err != nil {
			t.Fatal(err)
		}
	}

	a.backend.remove("Home")
	a.fullRefresh()
	_, err := a.sync()
	var gone *CalendarGoneError
	if !errors.As(err, &gone) || gone.Calendar != "Home" || gone.Pending != 2 {
		t.Fatalf("sync error: %v", err)
	}
	if !strings.Contains(err.Error(), `calendar "Home" is no longer on the server`) {
		t.Fatalf("message: %v", err)
	}
	if n := a.backend.count(work); n != 1 {
		t.Fatalf("changes moved to Work: it has %d events", n)
	}
	for _, uid := range []string{"edited", "added"} {
		if e := a.event(uid); !e.Dirty || CollectionOf(e.Href) != home {
			t.Fatalf("%s: dirty=%v href=%s", uid, e.Dirty, e.Href)
		}
	}
	if evs, _ := a.store.EventsByUID(a.src.ID, "removed"); len(evs) != 0 {
		t.Fatal("a deletion in the deleted calendar stays pending")
	}
	if a.event("w").Title != "Standup" {
		t.Fatal("the other calendar stopped syncing")
	}
}

// Between daily refreshes the calendar list is cached; a write that finds its calendar gone
// makes the next sync rediscover, which then reports the calendar by name.
func TestCalendarGoneBetweenRefreshes(t *testing.T) {
	a := newLiveAccount(t, "Home", "Work")
	a.mustSync()
	added := &model.Event{CalendarID: a.src.ID, UID: "late", Title: "Late addition", StartTime: a.start, EndTime: a.start.Add(time.Hour),
		Href: ObjectHref(liveCal("Home"), "late"), Dirty: true}
	if err := a.store.SaveEvent(added); err != nil {
		t.Fatal(err)
	}
	a.backend.remove("Home")
	if _, err := a.sync(); !errors.Is(err, errCollectionMissing) {
		t.Fatalf("first sync: %v", err)
	}
	_, err := a.sync()
	var gone *CalendarGoneError
	if !errors.As(err, &gone) || gone.Calendar != "Home" {
		t.Fatalf("second sync: %v", err)
	}
	if n := a.backend.count(liveCal("Work")); n != 0 {
		t.Fatalf("the event went to Work: %d", n)
	}
}

// A calendar created on the server later is found at the next refresh and synced, shown by
// default, without announcing its events as new meetings; the user's choices stay as they are.
func TestCalendarAddedLaterIsSynced(t *testing.T) {
	a := newLiveAccount(t, "Home", "Old")
	a.src.HiddenCollections = []string{liveCal("Old")}
	if err := a.store.SaveSource(a.src); err != nil {
		t.Fatal(err)
	}
	a.mustSync()
	if got := KnownCalendars(a.store, a.src.ID); len(got) != 2 {
		t.Fatalf("known: %+v", got)
	}

	a.backend.add("Team")
	a.backend.put(liveCal("Team"), "t", "Team offsite", a.start)
	a.fullRefresh()
	added, err := a.sync()
	if err != nil {
		t.Fatal(err)
	}
	if got := KnownCalendars(a.store, a.src.ID); len(got) != 3 || got[2].Name != "Team" {
		t.Fatalf("known after refresh: %+v", got)
	}
	if a.event("t").Title != "Team offsite" || len(added) != 0 {
		t.Fatalf("new calendar's event: %d announced", len(added))
	}
	if src, _ := a.store.Source(a.src.ID); len(src.HiddenCollections) != 1 || src.HiddenCollections[0] != liveCal("Old") {
		t.Fatalf("choices changed: %v", src.HiddenCollections)
	}
}

// A local edit waiting to be sent survives a refresh that brings the server's older version.
func TestPendingEditSurvivesRefresh(t *testing.T) {
	a := newLiveAccount(t, "Home")
	a.backend.put(liveCal("Home"), "e", "Server title", a.start)
	a.mustSync()
	e := a.event("e")
	e.Title, e.Dirty = "Local title", true
	if err := a.store.SaveEvent(e); err != nil {
		t.Fatal(err)
	}
	// The server refuses writes for a while: the push fails, the pull of its version still runs.
	a.backend.setFailWrites(true)
	a.fullRefresh()
	if _, err := a.sync(); err == nil {
		t.Fatal("the push did not fail")
	}
	if got := a.event("e"); got.Title != "Local title" || !got.Dirty {
		t.Fatalf("local edit lost: %q", got.Title)
	}
	a.backend.setFailWrites(false)
	a.mustSync()
	if got := a.event("e"); got.Dirty || got.Title != "Local title" {
		t.Fatalf("after push: %+v", got)
	}
	objs, _ := a.backend.ListCalendarObjects(context.Background(), liveCal("Home"), nil)
	if len(objs) != 1 || !strings.Contains(fmt.Sprint(ics.ToEvents(objs[0].Data, "", "")[0].Title), "Local title") {
		t.Fatal("server does not have the local edit")
	}
}
