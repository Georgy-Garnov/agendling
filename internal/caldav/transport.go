package caldav

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-webdav"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
)

// userAgent identifies the app to servers; anti-abuse layers tend to throttle the anonymous Go default.
var userAgent = appinfo.Name + "/" + appinfo.Version + " (+" + appinfo.Homepage + ")"

// ThrottledError reports that the server asked the client to slow down (HTTP 429, or 503 with Retry-After).
type ThrottledError struct {
	Status     int
	RetryAfter time.Duration // 0 when the server did not say
}

func (e *ThrottledError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("server rate limit (HTTP %d), retry after %s", e.Status, e.RetryAfter.Round(time.Second))
	}
	return fmt.Sprintf("server rate limit (HTTP %d)", e.Status)
}

// politeClient sets the User-Agent, logs failed responses with their headers and turns
// rate-limit responses into ThrottledError so the engine can back off.
type politeClient struct {
	c webdav.HTTPClient
}

func (p politeClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	resp, err := p.c.Do(req)
	if err != nil || resp == nil || resp.StatusCode < 400 || resp.StatusCode == http.StatusNotFound {
		return resp, err
	}
	retryAfter, hasRetryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	throttled := resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusServiceUnavailable && hasRetryAfter
	if !throttled {
		log.Printf("caldav: %s %s -> %s", req.Method, req.URL.Path, resp.Status)
		return resp, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	log.Printf("caldav: %s %s -> %s; headers: %s; body: %q",
		req.Method, req.URL.Path, resp.Status, formatHeaders(resp.Header), strings.TrimSpace(string(body)))
	return nil, &ThrottledError{Status: resp.StatusCode, RetryAfter: retryAfter}
}

// parseRetryAfter accepts both forms allowed by RFC 9110: delay-seconds and an HTTP-date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(max(n, 0)) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

func formatHeaders(h http.Header) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		if k != "Set-Cookie" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + strings.Join(h[k], ",")
	}
	return strings.Join(parts, "; ")
}
