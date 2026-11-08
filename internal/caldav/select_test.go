package caldav

import (
	"context"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

const (
	homeCal = "/u/calendars/home/"
	workCal = "/u/calendars/work/"
)

// multiBackend serves two event calendars; objects belong to the calendar their path is under.
type multiBackend struct {
	memBackend
	mu      sync.Mutex
	queried map[string]int
}

func (b *multiBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	return []caldav.Calendar{
		{Path: homeCal, Name: "Home", SupportedComponentSet: []string{"VEVENT"}},
		{Path: workCal, Name: "Work", SupportedComponentSet: []string{"VEVENT"}},
	}, nil
}
func (b *multiBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	cals, _ := b.ListCalendars(ctx)
	for _, c := range cals {
		if c.Path == p {
			return &c, nil
		}
	}
	return nil, caldav.NewPreconditionError(caldav.PreconditionNoUIDConflict)
}
func (b *multiBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	all, _ := b.memBackend.ListCalendarObjects(ctx, p, req)
	var res []caldav.CalendarObject
	for _, o := range all {
		if strings.HasPrefix(o.Path, p) {
			res = append(res, o)
		}
	}
	return res, nil
}
func (b *multiBackend) QueryCalendarObjects(ctx context.Context, p string, q *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	b.mu.Lock()
	b.queried[p]++
	b.mu.Unlock()
	objs, _ := b.ListCalendarObjects(ctx, p, nil)
	return caldav.Filter(q, objs)
}

func (b *multiBackend) put(coll, uid, title string, start time.Time) {
	e := &model.Event{UID: uid, Title: title, StartTime: start, EndTime: start.Add(time.Hour)}
	b.PutCalendarObject(context.Background(), coll+uid+".ics", ics.FromEvents([]*model.Event{e}), nil)
}

func TestSyncHonoursCalendarSelection(t *testing.T) {
	backend := &multiBackend{memBackend: memBackend{objs: map[string]caldav.CalendarObject{}}, queried: map[string]int{}}
	srv := httptest.NewServer(&caldav.Handler{Backend: backend})
	defer srv.Close()
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	backend.put(homeCal, "h1", "Home event", start)
	backend.put(workCal, "w1", "Work event", start.Add(time.Hour))
	store, src := newTestSource(t, srv.URL)
	titles := func() string {
		evs, _ := store.EventsInRange(start.Add(-time.Hour), start.Add(10*time.Hour))
		var s []string
		for _, e := range evs {
			s = append(s, e.Title)
		}
		sort.Strings(s)
		return strings.Join(s, ",")
	}
	sync := func() {
		t.Helper()
		if _, err := SyncSource(ctx, store, src); err != nil {
			t.Fatal(err)
		}
	}

	cals, err := TestConnection(ctx, src)
	if err != nil || len(cals) != 2 || cals[0] != (CalendarInfo{homeCal, "Home"}) || cals[1] != (CalendarInfo{workCal, "Work"}) {
		t.Fatalf("calendars: %+v, %v", cals, err)
	}
	if len(KnownCalendars(store, src.ID)) != 0 {
		t.Fatal("calendars known before the first sync")
	}
	sync()
	if got := titles(); got != "Home event,Work event" {
		t.Fatalf("all calendars: %q", got)
	}
	if got := KnownCalendars(store, src.ID); len(got) != 2 || got[0].Name != "Home" {
		t.Fatalf("known calendars: %+v", got)
	}

	// Hiding a calendar drops its events and stops querying it; new events go to a shown one
	// even though the hidden calendar is listed first.
	src.HiddenCollections = []string{homeCal}
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	backend.queried = map[string]int{}
	sync()
	if got := titles(); got != "Work event" {
		t.Fatalf("home hidden: %q", got)
	}
	if backend.queried[homeCal] != 0 || backend.queried[workCal] != 1 {
		t.Fatalf("queried: %v", backend.queried)
	}
	if src.CalendarPath != workCal {
		t.Fatalf("write path = %q", src.CalendarPath)
	}
	local := &model.Event{CalendarID: src.ID, UID: ics.NewUID(), Title: "Local", StartTime: start.Add(3 * time.Hour),
		EndTime: start.Add(4 * time.Hour), Dirty: true}
	if err := store.SaveEvent(local); err != nil {
		t.Fatal(err)
	}
	sync()
	if objs, _ := backend.ListCalendarObjects(ctx, workCal, nil); len(objs) != 2 {
		t.Fatalf("local event not pushed to the shown calendar: %d objects", len(objs))
	}
	if got := titles(); got != "Local,Work event" {
		t.Fatalf("after push: %q", got)
	}

	// Showing it again brings its events back without announcing them as new meetings, while a
	// genuinely new event elsewhere still is.
	src.HiddenCollections = nil
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	backend.put(workCal, "w2", "New work event", start.Add(5*time.Hour))
	added, err := SyncSource(ctx, store, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(); got != "Home event,Local,New work event,Work event" {
		t.Fatalf("home shown again: %q", got)
	}
	if len(added) != 1 || added[0].Title != "New work event" {
		t.Fatalf("reported as new: %+v", added)
	}

	// An event moved to another calendar of the account is stored there, then removed from the old one.
	ws, _ := store.EventsByUID(src.ID, "w1")
	ws[0].MovedFrom, ws[0].Href, ws[0].Dirty = ws[0].Href, ObjectHref(homeCal, "w1"), true
	if err := store.SaveEvent(ws[0]); err != nil {
		t.Fatal(err)
	}
	sync()
	if _, ok := backend.objs[workCal+"w1.ics"]; ok {
		t.Fatal("old copy left in the work calendar")
	}
	if _, ok := backend.objs[homeCal+"w1.ics"]; !ok {
		t.Fatal("moved event not in the home calendar")
	}
	if ws, _ = store.EventsByUID(src.ID, "w1"); len(ws) != 1 || ws[0].MovedFrom != "" || ws[0].Dirty || ws[0].Href != homeCal+"w1.ics" {
		t.Fatalf("after move: %+v", ws)
	}

	// Hiding everything is reported instead of silently showing nothing.
	src.HiddenCollections = []string{homeCal, workCal}
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(ctx, store, src); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("all hidden: %v", err)
	}
}
