package caldav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// memBackend is a minimal in-memory CalDAV server backend.
type memBackend struct {
	mu   sync.Mutex
	objs map[string]caldav.CalendarObject
	n    int
}

const (
	principal = "/u/"
	homeSet   = "/u/calendars/"
	calPath   = "/u/calendars/work/"
)

func (b *memBackend) CurrentUserPrincipal(ctx context.Context) (string, error) { return principal, nil }
func (b *memBackend) CalendarHomeSetPath(ctx context.Context) (string, error)  { return homeSet, nil }
func (b *memBackend) CreateCalendar(ctx context.Context, c *caldav.Calendar) error {
	return nil
}
func (b *memBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	return []caldav.Calendar{{Path: calPath, Name: "Work", SupportedComponentSet: []string{"VEVENT"}}}, nil
}
func (b *memBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	return &caldav.Calendar{Path: calPath, Name: "Work", SupportedComponentSet: []string{"VEVENT"}}, nil
}
func (b *memBackend) GetCalendarObject(ctx context.Context, p string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.objs[p]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &o, nil
}
func (b *memBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var res []caldav.CalendarObject
	for _, o := range b.objs {
		res = append(res, o)
	}
	return res, nil
}
func (b *memBackend) QueryCalendarObjects(ctx context.Context, p string, q *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	all, _ := b.ListCalendarObjects(ctx, p, nil)
	return caldav.Filter(q, all)
}
func (b *memBackend) PutCalendarObject(ctx context.Context, p string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n++
	o := caldav.CalendarObject{Path: p, ETag: fmt.Sprintf("e%d", b.n), ModTime: time.Now(), Data: cal}
	b.objs[p] = o
	return &o, nil
}
func (b *memBackend) DeleteCalendarObject(ctx context.Context, p string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objs, p)
	return nil
}

func TestSyncRoundTrip(t *testing.T) { testSyncRoundTrip(t, false) }

// Some servers send unquoted/weak ETags, which go-webdav alone rejects ("failed to unquote ETag").
func TestSyncRoundTripSloppyETags(t *testing.T) { testSyncRoundTrip(t, true) }

var etagElemRe = regexp.MustCompile(`(<[^>]*getetag[^>]*>)(.*?)(</[^>]*getetag>)`)

// sloppyETags rewrites server responses to use unquoted ETags in XML and weak ETag headers.
func sloppyETags(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		if et := rec.Header().Get("ETag"); et != "" {
			w.Header().Set("ETag", "W/"+et)
		}
		body := etagElemRe.ReplaceAllStringFunc(rec.Body.String(), func(m string) string {
			p := etagElemRe.FindStringSubmatch(m)
			return p[1] + strings.NewReplacer("&#34;", "", "&quot;", "", `"`, "").Replace(p[2]) + p[3]
		})
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Code)
		io.WriteString(w, body)
	})
}

func testSyncRoundTrip(t *testing.T, sloppy bool) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	var authOK bool
	var handler http.Handler = &caldav.Handler{Backend: backend}
	if sloppy {
		handler = sloppyETags(handler)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		authOK = ok && u == "alice" && p == "secret"
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	// A remote event created by another client.
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	remote := &model.Event{UID: "remote-1", Title: "Remote meeting", StartTime: start, EndTime: start.Add(time.Hour)}
	backend.PutCalendarObject(context.Background(), calPath+"remote-1.ics", ics.FromEvents([]*model.Event{remote}), nil)

	store, err := storage.Open(t.TempDir() + "/cal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	src := &model.CalendarSource{Name: "Work", Type: model.SourceCalDAV, URL: srv.URL, Username: "alice", Password: "secret", Enabled: true}
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 1. Pull.
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !authOK {
		t.Fatal("basic auth not sent")
	}
	if src.CalendarPath != calPath {
		t.Fatalf("write path = %q", src.CalendarPath)
	}
	evs, _ := store.EventsInRange(start.Add(-time.Hour), start.Add(2*time.Hour))
	if len(evs) != 1 || evs[0].Title != "Remote meeting" || evs[0].Href == "" || evs[0].ETag == "" || strings.Contains(evs[0].ETag, `"`) {
		t.Fatalf("pulled: %+v", evs)
	}

	// 2. Local create is pushed.
	local := &model.Event{CalendarID: src.ID, UID: ics.NewUID(), Title: "Local plan", StartTime: start.Add(3 * time.Hour),
		EndTime: start.Add(4 * time.Hour), Dirty: true, Reminders: []model.ReminderOffset{model.MinutesOffset(10)}}
	if err := store.SaveEvent(local); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatalf("sync push: %v", err)
	}
	if len(backend.objs) != 2 {
		t.Fatalf("server has %d objects", len(backend.objs))
	}
	found := false
	for p, o := range backend.objs {
		if strings.HasPrefix(p, calPath) && strings.Contains(p, "agendling") {
			found = ics.ToEvents(o.Data, "", "")[0].Title == "Local plan"
		}
	}
	if !found {
		t.Fatal("local event not on server")
	}
	pending, _ := store.PendingEvents(src.ID)
	if len(pending) != 0 {
		t.Fatalf("still pending: %d", len(pending))
	}

	// 3. Local delete is pushed.
	evs, _ = store.EventsInRange(start, start.Add(5*time.Hour))
	for _, e := range evs {
		if e.Title == "Remote meeting" {
			e.Deleted = true
			store.SaveEvent(e)
		}
	}
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatalf("sync delete: %v", err)
	}
	if _, ok := backend.objs[calPath+"remote-1.ics"]; ok {
		t.Fatal("remote object not deleted")
	}
	evs, _ = store.EventsInRange(start.Add(-time.Hour), start.Add(5*time.Hour))
	if len(evs) != 1 || evs[0].Title != "Local plan" || len(evs[0].Reminders) != 1 {
		t.Fatalf("after delete: %+v", evs)
	}

	// 4. Bad credentials fail gracefully.
	bad := *src
	bad.Password = "wrong"
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, p, _ := r.BasicAuth(); p != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
	if _, err := SyncSource(ctx, store, &bad); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestSyncReportsNewEvents(t *testing.T) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	srv := httptest.NewServer(&caldav.Handler{Backend: backend})
	defer srv.Close()
	put := func(uid, title string, start time.Time, rrule string) {
		e := &model.Event{UID: uid, Title: title, StartTime: start, EndTime: start.Add(time.Hour), RRule: rrule}
		backend.PutCalendarObject(context.Background(), calPath+uid+".ics", ics.FromEvents([]*model.Event{e}), nil)
	}
	titles := func(evs []*model.Event) string {
		var s []string
		for _, e := range evs {
			s = append(s, e.Title)
		}
		return strings.Join(s, ",")
	}

	store, err := storage.Open(t.TempDir() + "/cal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	src := &model.CalendarSource{Name: "Work", Type: model.SourceCalDAV, URL: srv.URL, Enabled: true}
	store.SaveSource(src)
	ctx := context.Background()
	now := time.Now().Truncate(time.Hour)

	put("existing", "Existing", now.Add(48*time.Hour), "")
	added, err := SyncSource(ctx, store, src)
	if err != nil || len(added) != 0 {
		t.Fatalf("first sync must not report the initial import: %q %v", titles(added), err)
	}
	src.LastSync = time.Now()

	put("new-1", "Planning", now.Add(72*time.Hour), "")
	put("new-series", "Weekly sync", now.Add(-30*24*time.Hour), "FREQ=WEEKLY") // started in the past, repeats on
	put("new-past", "Already over", now.Add(-72*time.Hour), "")
	added, err = SyncSource(ctx, store, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(added); got != "Planning,Weekly sync" && got != "Weekly sync,Planning" {
		t.Fatalf("new events = %q", got)
	}

	added, _ = SyncSource(ctx, store, src)
	if len(added) != 0 {
		t.Fatalf("reported again: %q", titles(added))
	}
}
