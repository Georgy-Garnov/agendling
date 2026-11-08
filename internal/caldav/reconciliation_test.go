package caldav

import (
	"context"
	"errors"
	"github.com/Georgy-Garnov/agendling/internal/storage"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	dav "github.com/emersion/go-webdav/caldav"
)

type changingBackend struct {
	*multiBackend
	mu        sync.Mutex
	calendars []dav.Calendar
}

func (b *changingBackend) ListCalendars(context.Context) ([]dav.Calendar, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]dav.Calendar(nil), b.calendars...), nil
}
func (b *changingBackend) setCalendars(cals []dav.Calendar) {
	b.mu.Lock()
	b.calendars = cals
	b.mu.Unlock()
}
func serverCalendars(paths ...string) []dav.Calendar {
	var cals []dav.Calendar
	for _, p := range paths {
		cals = append(cals, dav.Calendar{Path: p, Name: p, SupportedComponentSet: []string{"VEVENT"}})
	}
	return cals
}
func changingServer(t *testing.T) (*changingBackend, *httptest.Server) {
	t.Helper()
	b := &changingBackend{multiBackend: &multiBackend{&memBackend{objs: map[string]dav.CalendarObject{}}}, calendars: serverCalendars(calPath, sharedPath)}
	server := httptest.NewServer(&dav.Handler{Backend: b})
	t.Cleanup(server.Close)
	return b, server
}

func TestReconcileRemovedCalendarMustNotRetarget(t *testing.T) {
	backend, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	shared := sources[1]
	if shared.CalendarPath != sharedPath {
		t.Fatalf("unexpected target %s", shared.CalendarPath)
	}
	// Shared calendar is no longer advertised (e.g. revoked sharing); DAV principal
	// discovery is still available on that URL.
	backend.setCalendars(serverCalendars(calPath))
	if err := saveState(store, shared.ID, syncState{}); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(24 * time.Hour)
	event := &model.Event{CalendarID: shared.ID, UID: ics.NewUID(), Title: "Private shared entry", StartTime: start, EndTime: start.Add(time.Hour), Dirty: true}
	if err := store.SaveEvent(event); err != nil {
		t.Fatal(err)
	}
	_, err = SyncSource(context.Background(), store, shared)
	saved, _ := store.EventsByUID(shared.ID, event.UID)
	if len(saved) == 1 && strings.HasPrefix(saved[0].Href, calPath) {
		t.Fatalf("entry intended for %s was written to %s; sync error=%v", sharedPath, saved[0].Href, err)
	}
	if err == nil {
		t.Fatal("missing calendar was silently accepted")
	}
	if len(saved) != 1 || !saved[0].Dirty || saved[0].Href != "" {
		t.Fatal("pending entry was changed or lost")
	}
	backend.memBackend.mu.Lock()
	writes := len(backend.objs)
	backend.memBackend.mu.Unlock()
	if writes != 0 {
		t.Fatal("missing calendar caused a server write")
	}
	stored, err := store.Source(shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CalendarPath != sharedPath {
		t.Fatal("missing calendar changed binding")
	}
}

func TestReconcileNewCalendarDetectedAfterFullRefresh(t *testing.T) {
	backend, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	third := "/u/calendars/third/"
	backend.setCalendars(serverCalendars(calPath, sharedPath, third))
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, cal := range sources {
		if err := saveState(store, cal.ID, syncState{}); err != nil {
			t.Fatal(err)
		}
		if _, err := SyncSource(context.Background(), store, cal); err != nil {
			t.Fatal(err)
		}
	}
	sources, err = store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("new server calendar not discovered after full refresh: got %d sources, want 3", len(sources))
	}
}

func TestReconcileExistingSubscriptionNotDuplicated(t *testing.T) {
	_, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	child := *src
	child.ID = 0
	child.URL = server.URL + sharedPath
	child.CalendarPath = sharedPath
	child.Name = "Already subscribed"
	if err := store.SaveSource(&child); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, cal := range sources {
		if cal.URL == server.URL+sharedPath {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("same calendar now subscribed %d times after migration", count)
	}
}

func TestReconcileStaleSettingsMustNotDuplicate(t *testing.T) {
	_, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	settingsCopy := *src
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	// A settings dialog opened before background discovery saves its old source copy.
	settingsCopy.Color = "#123456"
	if err := store.SaveSource(&settingsCopy); !errors.Is(err, storage.ErrSourceChanged) {
		t.Fatalf("expected stale settings conflict, got %v", err)
	}
	fresh, err := store.Source(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Color = "#123456"
	if err := store.SaveSource(fresh); err != nil {
		t.Fatal(err)
	}
	settingsCopy = *fresh
	if _, err := SyncSource(context.Background(), store, &settingsCopy); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("saving settings during split caused %d subscriptions instead of 2", len(sources))
	}
}

func TestReconcileLegacySplitRetainsDiscovery(t *testing.T) {
	backend, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	src.URL, src.Name, src.CalendarPath = server.URL+calPath, "NAS / Personal Calendar", calPath
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	child := *src
	child.ID = 0
	child.URL, child.Name, child.CalendarPath = server.URL+sharedPath, "NAS / SchubboCal", sharedPath
	if err := store.SaveSource(&child); err != nil {
		t.Fatal(err)
	}
	for _, cal := range []*model.CalendarSource{src, &child} {
		if err := saveState(store, cal.ID, syncState{Version: 3, FullAt: time.Now(), Collections: []string{cal.CalendarPath}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	if src.DiscoveryURL != server.URL+homeSet {
		t.Fatalf("legacy account discovery not restored: %q", src.DiscoveryURL)
	}
	third := "/u/calendars/third/"
	backend.setCalendars(serverCalendars(calPath, sharedPath, third))
	if err := saveState(store, src.ID, syncState{}); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("expected three sources after legacy upgrade, got %d", len(sources))
	}
	owners := 0
	for _, cal := range sources {
		if cal.DiscoveryURL != "" {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("expected one discovery owner, got %d", owners)
	}
}

func TestReconcileDirectSubscriptionStaysDirect(t *testing.T) {
	_, server := changingServer(t)
	store, src := newTestSource(t, server.URL+sharedPath)
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || src.DiscoveryURL != "" || src.CalendarPath != sharedPath {
		t.Fatal("direct subscription unexpectedly subscribed to entire account")
	}
}

func TestReconcilePreservesAccountWithOneInitialCalendar(t *testing.T) {
	backend, server := changingServer(t)
	backend.setCalendars(serverCalendars(calPath))
	store, src := newTestSource(t, server.URL)
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	backend.setCalendars(serverCalendars(calPath, sharedPath))
	if err := saveState(store, src.ID, syncState{}); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil || len(sources) != 2 {
		t.Fatalf("new calendar not discovered for initially single-calendar account: %d, %v", len(sources), err)
	}
}

func TestReconcileReusesExistingDefaultSubscription(t *testing.T) {
	_, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	child := *src
	child.ID = 0
	child.URL, child.Name, child.Color, child.Enabled = server.URL+calPath, "My chosen name", "#abcdef", false
	if err := store.SaveSource(&child); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("duplicate default subscription: %d sources", len(sources))
	}
	if src.Name != child.Name || src.Color != child.Color || src.Enabled {
		t.Fatal("explicit subscription preferences were lost")
	}
	if src.DiscoveryURL != server.URL {
		t.Fatal("account discovery lost while merging duplicate default")
	}
}

func TestReconcileDisabledOwnerStillFindsNewCalendars(t *testing.T) {
	backend, server := changingServer(t)
	store, src := newTestSource(t, server.URL)
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	src.Enabled = false
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	third := "/u/calendars/third/"
	backend.setCalendars(serverCalendars(calPath, sharedPath, third))
	if err := saveState(store, src.ID, syncState{}); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	sources, err := store.Sources()
	if err != nil || len(sources) != 3 {
		t.Fatalf("disabled coordinator lost account discovery: %d, %v", len(sources), err)
	}
	if !sources[2].Enabled {
		t.Fatal("disabling one calendar disabled newly discovered siblings")
	}
}

func TestPinnedCalendarDoesNotPushForeignHref(t *testing.T) {
	backend, server := changingServer(t)
	store, src := newTestSource(t, server.URL+sharedPath)
	if _, err := SyncSource(context.Background(), store, src); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(24 * time.Hour)
	ev := &model.Event{CalendarID: src.ID, UID: "foreign", Href: calPath + "foreign.ics", StartTime: now, EndTime: now.Add(time.Hour), Dirty: true}
	if err := store.SaveEvent(ev); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(context.Background(), store, src); err == nil {
		t.Fatal("foreign resource was silently pushed")
	}
	backend.memBackend.mu.Lock()
	writes := len(backend.objs)
	backend.memBackend.mu.Unlock()
	if writes != 0 {
		t.Fatal("foreign resource caused a server write")
	}
	pending, err := store.PendingEvents(src.ID)
	if err != nil || len(pending) != 1 || !pending[0].Dirty {
		t.Fatal("blocked edit was lost")
	}
}
