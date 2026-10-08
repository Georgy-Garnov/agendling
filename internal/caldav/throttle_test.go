package caldav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"", 0, false},
		{"120", 2 * time.Minute, true},
		{"Thu, 08 Oct 2026 12:05:00 GMT", 5 * time.Minute, true},
		{"Thu, 08 Oct 2026 11:00:00 GMT", 0, true},
		{"soon", 0, false},
	} {
		got, ok := parseRetryAfter(tc.in, now)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// recorder counts requests by method and can make the server answer 429.
type recorder struct {
	mu        sync.Mutex
	h         http.Handler
	methods   map[string]int
	agents    map[string]bool
	ctag      string // served as getctag for the calendar collection when non-empty
	limitLeft int    // when > 0 every request is answered with 429 and decrements it
}

func newRecorder(h http.Handler) *recorder {
	return &recorder{h: h, methods: map[string]int{}, agents: map[string]bool{}}
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = map[string]int{}
}

func (r *recorder) count(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.methods[method]
}

func (r *recorder) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.methods {
		n += c
	}
	return n
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.methods[req.Method]++
	r.agents[req.UserAgent()] = true
	limited := r.limitLeft > 0
	if limited {
		r.limitLeft--
	}
	ctag := r.ctag
	r.mu.Unlock()

	if limited {
		w.Header().Set("Retry-After", "120")
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	if req.Method == "PROPFIND" && ctag != "" {
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(string(body), "getctag") {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">`+
				`<d:response><d:href>%s</d:href><d:propstat><d:prop/><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+
				`<d:response><d:href>%s</d:href><d:propstat><d:prop><cs:getctag>%s</cs:getctag></d:prop>`+
				`<d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, homeSet, calPath, ctag)
			return
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
	}
	r.h.ServeHTTP(w, req)
}

func newTestSource(t *testing.T, url string) (*storage.Store, *model.CalendarSource) {
	t.Helper()
	store, err := storage.Open(t.TempDir() + "/cal.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	src := &model.CalendarSource{Name: "Work", Type: model.SourceCalDAV, URL: url, Username: "alice", Enabled: true}
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	return store, src
}

func putEvent(b *memBackend, uid, title string, start time.Time) {
	e := &model.Event{UID: uid, Title: title, StartTime: start, EndTime: start.Add(time.Hour)}
	b.PutCalendarObject(context.Background(), calPath+uid+".ics", ics.FromEvents([]*model.Event{e}), nil)
}

func TestSyncSkipsUnchangedCollections(t *testing.T) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	rec := newRecorder(&caldav.Handler{Backend: backend})
	rec.ctag = "1"
	srv := httptest.NewServer(rec)
	defer srv.Close()
	store, src := newTestSource(t, srv.URL)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	inRange := func() []string {
		evs, _ := store.EventsInRange(start.Add(-time.Hour), start.Add(10*time.Hour))
		var titles []string
		for _, e := range evs {
			titles = append(titles, e.Title)
		}
		return titles
	}

	putEvent(backend, "a", "First", start)
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatal(err)
	}
	if rec.count("REPORT") != 1 || len(inRange()) != 1 {
		t.Fatalf("first sync: %d REPORTs, events %v", rec.count("REPORT"), inRange())
	}
	if !rec.agents[userAgent] || !strings.HasPrefix(userAgent, "Agendling/") {
		t.Fatalf("user agents seen: %v", rec.agents)
	}

	// Unchanged collection: discovery is cached and the ctag check is the only request.
	rec.reset()
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatal(err)
	}
	if rec.total() != 1 || rec.count("PROPFIND") != 1 {
		t.Fatalf("unchanged sync made requests: %v", rec.methods)
	}
	if got := inRange(); len(got) != 1 {
		t.Fatalf("events lost on skipped pull: %v", got)
	}

	// Changed collection is pulled again.
	putEvent(backend, "b", "Second", start.Add(2*time.Hour))
	rec.mu.Lock()
	rec.ctag = "2"
	rec.mu.Unlock()
	rec.reset()
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatal(err)
	}
	if rec.count("REPORT") != 1 || len(inRange()) != 2 {
		t.Fatalf("changed sync: %v, events %v", rec.methods, inRange())
	}

	// A stale cache forces rediscovery and a full pull.
	st := loadState(store, src.ID)
	st.FullAt = time.Now().Add(-fullRefreshEvery - time.Minute)
	if err := saveState(store, src.ID, st); err != nil {
		t.Fatal(err)
	}
	rec.reset()
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatal(err)
	}
	if rec.count("REPORT") != 1 || rec.count("PROPFIND") < 4 || len(inRange()) != 2 {
		t.Fatalf("full refresh: %v, events %v", rec.methods, inRange())
	}

	// Pointing the source to another server drops the cache.
	src.URL = srv.URL + "/"
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if st := loadState(store, src.ID); len(st.Collections) != 0 {
		t.Fatalf("cache kept after URL change: %+v", st)
	}
}

func TestSyncRateLimited(t *testing.T) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	rec := newRecorder(&caldav.Handler{Backend: backend})
	rec.limitLeft = 1
	srv := httptest.NewServer(rec)
	defer srv.Close()
	store, src := newTestSource(t, srv.URL)

	_, err := SyncSource(context.Background(), store, src)
	var te *ThrottledError
	if !errors.As(err, &te) || te.Status != http.StatusTooManyRequests || te.RetryAfter != 2*time.Minute {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineBacksOffAfterRateLimit(t *testing.T) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	rec := newRecorder(&caldav.Handler{Backend: backend})
	rec.limitLeft = 1
	srv := httptest.NewServer(rec)
	defer srv.Close()
	store, src := newTestSource(t, srv.URL)
	// A second source on the same account shares the pause.
	other := &model.CalendarSource{Name: "Home", Type: model.SourceCalDAV, URL: srv.URL + calPath, Username: "alice", Enabled: true}
	if err := store.SaveSource(other); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var statuses []Status
	e := NewEngine(store, func(s Status) {
		mu.Lock()
		statuses = append(statuses, s)
		mu.Unlock()
	})
	ctx := context.Background()

	e.runOnce(ctx, src.ID)
	if rec.total() != 1 {
		t.Fatalf("requests after 429: %v", rec.methods)
	}
	e.runOnce(ctx, src.ID)
	e.runOnce(ctx, other.ID)
	if rec.total() != 1 {
		t.Fatalf("engine did not back off: %v", rec.methods)
	}
	mu.Lock()
	last := statuses[len(statuses)-1]
	mu.Unlock()
	if last.Err == nil || !strings.Contains(last.Err.Error(), "paused until") {
		t.Fatalf("last status: %+v", last)
	}

	// The pause is persisted, so a fresh engine (app restart) honours it too.
	if b := loadState(store, src.ID).BlockedUntil; time.Until(b) < time.Minute {
		t.Fatalf("blocked until %v", b)
	}
	NewEngine(store, nil).runOnce(ctx, src.ID)
	if rec.total() != 1 {
		t.Fatalf("restarted engine did not back off: %v", rec.methods)
	}

	// Once the pause is over, sync resumes and clears it.
	st := loadState(store, src.ID)
	st.BlockedUntil = time.Time{}
	saveState(store, src.ID, st)
	e2 := NewEngine(store, nil)
	e2.runOnce(ctx, src.ID)
	if rec.count("REPORT") != 1 {
		t.Fatalf("sync did not resume: %v", rec.methods)
	}
	if got, _ := store.Source(src.ID); got.LastError != "" {
		t.Fatalf("last error = %q", got.LastError)
	}
}
