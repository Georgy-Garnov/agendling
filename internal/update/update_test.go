package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v0.4.0", "v0.3.2", true},
		{"v0.3.10", "v0.3.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.3.2", "v0.3.2", false},
		{"v0.3.2", "v0.3.2-4-gabcdef-dirty", false},
		{"v0.3.1", "v0.3.2", false},
		{"v0.4.0", "dev", false},
		{"nightly", "v0.3.2", false},
	} {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no UA", http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"tag_name":"v0.4.0","html_url":"https://github.com/Georgy-Garnov/agendling/releases/tag/v0.4.0","assets":[]}`))
	}))
	defer srv.Close()
	old := LatestURL
	LatestURL = srv.URL
	defer func() { LatestURL = old }()

	rel, err := Check(context.Background(), "v0.3.2")
	if err != nil || !rel.Newer || rel.Tag != "v0.4.0" || rel.URL != "https://github.com/Georgy-Garnov/agendling/releases/tag/v0.4.0" {
		t.Fatalf("check: %+v, %v", rel, err)
	}
	if rel, _ := Check(context.Background(), "v0.4.0"); rel.Newer {
		t.Fatal("same version reported as newer")
	}
}

func TestCheckRetriesServerErrors(t *testing.T) {
	old := retryDelays
	retryDelays = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	defer func() { retryDelays = old }()
	calls, failures := 0, 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if failures > 0 {
			failures--
			http.Error(w, "busy", http.StatusGatewayTimeout)
			return
		}
		w.Write([]byte(`{"tag_name":"v0.4.0","html_url":"https://github.com/Georgy-Garnov/agendling/releases/tag/v0.4.0"}`))
	}))
	defer srv.Close()
	oldURL := LatestURL
	LatestURL = srv.URL
	defer func() { LatestURL = oldURL }()
	if rel, err := Check(context.Background(), "v0.3.2"); err != nil || !rel.Newer || calls != 2 {
		t.Fatalf("after one 504: %+v, %v, %d calls", rel, err, calls)
	}

	failures = 2 // recovers on the third attempt
	calls = 0
	if rel, err := Check(context.Background(), "v0.3.2"); err != nil || rel.Tag != "v0.4.0" || calls != 3 {
		t.Fatalf("after two 504s: %+v, %v, %d calls", rel, err, calls)
	}

	failures = 10 // keeps failing
	if _, err := Check(context.Background(), "v0.3.2"); err == nil || !strings.Contains(err.Error(), "not responding") {
		t.Fatalf("persistent 504: %v", err)
	}
}
