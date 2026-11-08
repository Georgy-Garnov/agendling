// Package update asks GitHub for the latest release. It runs only when the user presses
// "Check for updates"; nothing is downloaded or installed.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
)

// LatestURL is the GitHub API endpoint of the latest published release.
var LatestURL = "https://api.github.com/repos/Georgy-Garnov/agendling/releases/latest"

// ReleasesPage lists all releases, to look for updates by hand when the check fails.
const ReleasesPage = "https://github.com/Georgy-Garnov/agendling/releases"

// Release describes the latest published release.
type Release struct {
	Tag   string // e.g. "v0.4.0"
	URL   string // the release page
	Newer bool   // newer than the running version
}

// Check fetches the latest release and compares it with current (appinfo.Version).
func Check(ctx context.Context, current string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < len(retryDelays)+1; attempt++ {
		if attempt > 0 {
			// GitHub has spells of timeouts for anonymous requests; try again after a pause.
			select {
			case <-time.After(retryDelays[attempt-1]):
			case <-ctx.Done():
				return Release{}, lastErr
			}
		}
		rel, err, retry := fetchLatest(ctx, current)
		if err == nil || !retry {
			return rel, err
		}
		lastErr = err
	}
	return Release{}, lastErr
}

// retryDelays are the pauses before the second and third attempt.
var retryDelays = []time.Duration{2 * time.Second, 4 * time.Second}

// fetchLatest makes one request; retry tells whether the failure may be temporary.
func fetchLatest(ctx context.Context, current string) (rel Release, err error, retry bool) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		return Release{}, err, false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", appinfo.Name+"/"+appinfo.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("GitHub could not be reached; try again later"), true
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return Release{}, fmt.Errorf("GitHub is not responding right now (%s); try again later", resp.Status), true
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status), false
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("unexpected answer from GitHub: %w", err), false
	}
	if body.TagName == "" || !strings.HasPrefix(body.HTMLURL, "https://") {
		return Release{}, fmt.Errorf("unexpected answer from GitHub"), false
	}
	return Release{Tag: body.TagName, URL: body.HTMLURL, Newer: Newer(body.TagName, current)}, nil, false
}

// Newer reports whether release version a is newer than b ("v1.2.3", optionally followed by a
// git-describe suffix). Unparsable versions, like "dev" builds, are never newer.
func Newer(a, b string) bool {
	va, okA := parse(a)
	vb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var res [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return res, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return res, false
		}
		res[i] = n
	}
	return res, true
}
