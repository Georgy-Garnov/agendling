package caldav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"
)

func TestSameService(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"https://caldav.icloud.com/", "https://p34-caldav.icloud.com:443/1/calendars/", true},
		{"https://dav.example.org/", "https://dav.example.org:8443/x/", true},
		{"http://127.0.0.1:1/", "http://127.0.0.1:2/", true},
		{"https://caldav.icloud.com/", "http://p34-caldav.icloud.com/", false},
		{"https://caldav.icloud.com/", "https://evil.example.com/", false},
		{"https://example.com/", "https://evil.com/", false},
		{"https://example.com/", "https://dav.example.com/", true},
	} {
		from, _ := url.Parse(tc.from)
		to, _ := url.Parse(tc.to)
		if got := sameService(from, to); got != tc.want {
			t.Errorf("sameService(%s, %s) = %v", tc.from, tc.to, got)
		}
	}
}

// iCloud announces the calendar home set on an account partition host; the front host still
// lists the calendars but answers event queries with an empty multistatus.
func TestSyncFollowsHomeSetToPartitionHost(t *testing.T) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	var partitionReports atomic.Int32
	partition := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "REPORT" {
			partitionReports.Add(1)
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			if strings.Contains(string(body), "allcomp") {
				// iCloud strips the events from objects requested with <allprop/><allcomp/>.
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				w.WriteHeader(http.StatusMultiStatus)
				io.WriteString(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"/>`)
				return
			}
		}
		(&caldav.Handler{Backend: backend}).ServeHTTP(w, r)
	}))
	defer partition.Close()

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		switch {
		case r.Method == "PROPFIND" && strings.Contains(string(body), "calendar-home-set"):
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>`+principal+`</href><propstat><prop>`+
				`<calendar-home-set xmlns="urn:ietf:params:xml:ns:caldav"><href xmlns="DAV:">`+partition.URL+homeSet+`</href></calendar-home-set>`+
				`</prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`)
		case r.Method == "REPORT" || r.Method == "PUT":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"/>`)
		default:
			(&caldav.Handler{Backend: backend}).ServeHTTP(w, r)
		}
	}))
	defer front.Close()

	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	putEvent(backend, "a", "On partition", start)
	store, src := newTestSource(t, front.URL)

	if n, err := TestConnection(context.Background(), src); err != nil || n != 1 {
		t.Fatalf("test connection: %d, %v", n, err)
	}
	if added, err := SyncSource(context.Background(), store, src); err != nil || len(added) != 0 {
		t.Fatalf("first sync: added %d, err %v", len(added), err) // an initial import is not "new"
	}
	evs, _ := store.EventsInRange(start.Add(-time.Hour), start.Add(2*time.Hour))
	if len(evs) != 1 || evs[0].Title != "On partition" {
		t.Fatalf("events: %+v", evs)
	}
	if partitionReports.Load() != 1 {
		t.Fatalf("partition got %d REPORTs", partitionReports.Load())
	}
	if st := loadState(store, src.ID); st.Endpoint != partition.URL+"/" {
		t.Fatalf("endpoint = %q, want %q", st.Endpoint, partition.URL+"/")
	}

	// Cached partition endpoint is used by later (incremental) syncs, including pushes.
	putEvent(backend, "b", "Second", start.Add(time.Hour))
	added, err := SyncSource(context.Background(), store, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].Title != "Second" {
		t.Fatalf("new events: %+v", added)
	}
	if evs, _ := store.EventsInRange(start.Add(-time.Hour), start.Add(3*time.Hour)); len(evs) != 2 {
		t.Fatalf("after second sync: %+v", evs)
	}
}
