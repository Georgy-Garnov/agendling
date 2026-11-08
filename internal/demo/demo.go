// Package demo provides demo calendars for screenshots: two in-memory CalDAV accounts and a
// local calendar with a believable week of events around today. Used by tools/uishots and
// tools/demodata only.
package demo

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// demoBackend is an in-memory CalDAV account with several calendars.
type demoBackend struct {
	mu   sync.Mutex
	cals []caldav.Calendar
	objs map[string]caldav.CalendarObject
}

func (b *demoBackend) CurrentUserPrincipal(context.Context) (string, error) { return "/u/", nil }
func (b *demoBackend) CalendarHomeSetPath(context.Context) (string, error)  { return "/u/cal/", nil }
func (b *demoBackend) CreateCalendar(context.Context, *caldav.Calendar) error {
	return nil
}
func (b *demoBackend) ListCalendars(context.Context) ([]caldav.Calendar, error) { return b.cals, nil }
func (b *demoBackend) GetCalendar(_ context.Context, p string) (*caldav.Calendar, error) {
	for _, c := range b.cals {
		if c.Path == p {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("no calendar %s", p)
}
func (b *demoBackend) GetCalendarObject(_ context.Context, p string, _ *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o, ok := b.objs[p]; ok {
		return &o, nil
	}
	return nil, fmt.Errorf("not found")
}
func (b *demoBackend) ListCalendarObjects(_ context.Context, p string, _ *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var res []caldav.CalendarObject
	for k, o := range b.objs {
		if strings.HasPrefix(k, p) {
			res = append(res, o)
		}
	}
	return res, nil
}
func (b *demoBackend) QueryCalendarObjects(ctx context.Context, p string, q *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	all, _ := b.ListCalendarObjects(ctx, p, nil)
	return caldav.Filter(q, all)
}
func (b *demoBackend) PutCalendarObject(_ context.Context, p string, cal *ical.Calendar, _ *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := caldav.CalendarObject{Path: p, ETag: fmt.Sprintf("e%d", len(b.objs)+1), ModTime: time.Now(), Data: cal}
	b.objs[p] = o
	return &o, nil
}
func (b *demoBackend) DeleteCalendarObject(_ context.Context, p string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objs, p)
	return nil
}

// Accounts are the running demo CalDAV servers.
type Accounts struct {
	Work, Cloud *httptest.Server
}

func (a *Accounts) Close() {
	a.Work.Close()
	a.Cloud.Close()
}

// Start launches the demo accounts. With ports > 0 they listen on 127.0.0.1:port so another
// process (the app under test) can reach them.
func Start(workPort, cloudPort int) (*Accounts, error) {
	monday := time.Now().Truncate(24 * time.Hour)
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, -1)
	}
	at := func(day, h, m int) time.Time {
		return time.Date(monday.Year(), monday.Month(), monday.Day()+day, h, m, 0, 0, time.Local)
	}
	backend := func(names ...string) *demoBackend {
		b := &demoBackend{objs: map[string]caldav.CalendarObject{}}
		for _, name := range names {
			b.cals = append(b.cals, caldav.Calendar{Path: "/u/cal/" + strings.ToLower(name) + "/", Name: name,
				SupportedComponentSet: []string{"VEVENT"}})
		}
		return b
	}
	put := func(b *demoBackend, cal string, e *model.Event) {
		e.UID = strings.ToLower(strings.NewReplacer(" ", "-", ":", "").Replace(cal + "-" + e.Title + "-" + e.StartTime.Format("0102")))
		if e.TZID == "" && !e.AllDay {
			e.TZID = core.LocalTZID
		}
		b.PutCalendarObject(context.Background(), "/u/cal/"+cal+"/"+e.UID+".ics", ics.FromEvents([]*model.Event{e}), nil)
	}
	timed := func(title string, start time.Time, mins int) *model.Event {
		return &model.Event{Title: title, StartTime: start, EndTime: start.Add(time.Duration(mins) * time.Minute)}
	}

	work := backend("Work")
	standup := timed("Team standup", at(-7, 9, 30), 15)
	standup.RRule = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
	put(work, "work", standup)
	put(work, "work", timed("Sprint planning", at(1, 14, 0), 90))
	review := timed("Design review", at(2, 11, 0), 60)
	review.Location = "Room 4"
	put(work, "work", review)
	put(work, "work", timed("1:1 with Alex", at(3, 10, 0), 30))
	put(work, "work", timed("Sprint demo", at(4, 11, 0), 60))
	put(work, "work", timed("Release 2.4", at(4, 16, 0), 60))
	put(work, "work", &model.Event{Title: "DevConf", AllDay: true, StartTime: at(3, 0, 0), EndTime: at(5, 0, 0)})
	put(work, "work", timed("Quarterly planning", at(7, 10, 0), 120))
	put(work, "work", timed("Customer call", at(9, 15, 0), 45))
	put(work, "work", &model.Event{Title: "Team offsite", AllDay: true, StartTime: at(11, 0, 0), EndTime: at(12, 0, 0)})

	cloud := backend("Home", "Family", "Sport")
	put(cloud, "family", timed("Lunch with the design team", at(2, 13, 0), 60))
	put(cloud, "family", timed("Dinner with parents", at(4, 19, 0), 120))
	put(cloud, "family", timed("Brunch", at(6, 11, 0), 90))
	put(cloud, "family", timed("Call with Mom", at(6, 17, 0), 30))
	put(cloud, "home", timed("Plumber visit", at(0, 17, 0), 60))
	put(cloud, "home", timed("Car service", at(8, 8, 30), 60))
	put(cloud, "sport", timed("Football", at(5, 10, 0), 90))
	put(cloud, "sport", timed("Swimming", at(0, 7, 30), 60))
	put(cloud, "sport", timed("Swimming", at(7, 7, 30), 60))

	serve := func(b *demoBackend, port int) (*httptest.Server, error) {
		srv := httptest.NewUnstartedServer(&caldav.Handler{Backend: b})
		if port > 0 {
			l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return nil, err
			}
			srv.Listener.Close()
			srv.Listener = l
		}
		srv.Start()
		return srv, nil
	}
	ws, err := serve(work, workPort)
	if err != nil {
		return nil, err
	}
	cs, err := serve(cloud, cloudPort)
	if err != nil {
		ws.Close()
		return nil, err
	}
	return &Accounts{Work: ws, Cloud: cs}, nil
}

// Seed adds the demo accounts and a local calendar with a few events to an empty store, and
// mutes sounds (screenshot tours open reminder popups).
func Seed(store *storage.Store, acc *Accounts) error {
	monday := time.Now().Truncate(24 * time.Hour)
	for monday.Weekday() != time.Monday {
		monday = monday.AddDate(0, 0, -1)
	}
	at := func(day, h, m int) time.Time {
		return time.Date(monday.Year(), monday.Month(), monday.Day()+day, h, m, 0, 0, time.Local)
	}
	personal := &model.CalendarSource{Name: "Personal", Type: model.SourceLocal, Color: "#8e24aa", Enabled: true}
	for _, src := range []*model.CalendarSource{
		personal,
		{Name: "Work", Type: model.SourceCalDAV, URL: acc.Work.URL, Username: "alex", Password: "demo", Color: "#3b78e7", Enabled: true, SyncInterval: 15},
		{Name: "iCloud", Type: model.SourceCalDAV, URL: acc.Cloud.URL, Username: "alex@icloud.com", Password: "demo", Color: "#0b8043", Enabled: true, SyncInterval: 15},
	} {
		if err := store.SaveSource(src); err != nil {
			return err
		}
	}
	for i, day := range []int{1, 3, 8, 10} {
		start := at(day, 18, 30)
		e := &model.Event{CalendarID: personal.ID, UID: fmt.Sprintf("yoga-%d", i), Title: "Yoga", StartTime: start,
			EndTime: start.Add(time.Hour), TZID: core.LocalTZID}
		if err := store.SaveEvent(e); err != nil {
			return err
		}
	}
	st := store.Settings()
	st.Muted = true
	return store.SaveSettings(st)
}
