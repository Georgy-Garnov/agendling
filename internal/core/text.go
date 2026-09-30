// Package core holds UI-independent application logic shared by the Windows and Linux
// front ends: event operations, formatting and parsing helpers, and view geometry.
package core

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/scheduler"
)

// URLRegexp matches web links in free text.
var URLRegexp = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"'()\[\]]+[^\s<>"'()\[\].,;:!?]`)

// ExtractURLs finds distinct web links in free text.
func ExtractURLs(texts ...string) []string {
	seen := map[string]bool{}
	var res []string
	for _, t := range texts {
		for _, u := range URLRegexp.FindAllString(t, -1) {
			if !seen[u] {
				seen[u] = true
				res = append(res, u)
			}
		}
	}
	return res
}

// NormalizeURL adds a scheme to bare "www." links.
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u != "" && !strings.Contains(u, "://") && !strings.HasPrefix(u, "mailto:") {
		u = "https://" + u
	}
	return u
}

var (
	htmlTagRe    = regexp.MustCompile(`(?is)<[a-z/!][^>]*>`)
	htmlBreakRe  = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`)
	htmlAnchorRe = regexp.MustCompile(`(?is)<a\s[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
)

// PlainText converts an HTML description (some servers send one) into plain text,
// keeping link targets. Plain text is returned unchanged apart from line-ending normalization.
func PlainText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !htmlTagRe.MatchString(s) {
		return s
	}
	s = htmlAnchorRe.ReplaceAllStringFunc(s, func(m string) string {
		p := htmlAnchorRe.FindStringSubmatch(m)
		text := strings.TrimSpace(htmlTagRe.ReplaceAllString(p[2], ""))
		if text == "" || text == p[1] || strings.Contains(p[1], text) {
			return p[1]
		}
		return text + " (" + p[1] + ")"
	})
	s = htmlBreakRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

// Shorten truncates s to n runes with an ellipsis.
func Shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func HumanDuration(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 60:
		return fmt.Sprintf("%d min", m)
	case m < 24*60:
		if m%60 == 0 {
			return fmt.Sprintf("%d h", m/60)
		}
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	default:
		return fmt.Sprintf("%d d", m/(24*60))
	}
}

// WhenText describes an alert's time, e.g. "Wed, 30 Sep 2026  10:00 – 11:00  ·  starts in 15 min".
func WhenText(al scheduler.Alert) string {
	var when string
	if al.Event.AllDay {
		when = al.Start.Format("Monday, 2 January 2006") + " (all day)"
	} else {
		when = al.Start.Format("Mon, 2 Jan 2006  15:04") + " – " + al.End.Format("15:04")
		if !SameDay(al.Start, al.End) && al.End.After(al.Start) {
			when = al.Start.Format("Mon 2 Jan 15:04") + " – " + al.End.Format("Mon 2 Jan 15:04")
		}
	}
	d := time.Until(al.Start).Round(time.Minute)
	switch {
	case al.Event.AllDay:
	case d > 0:
		when += "  ·  starts in " + HumanDuration(d)
	case d > -time.Minute:
		when += "  ·  starting now"
	default:
		when += "  ·  started " + HumanDuration(-d) + " ago"
	}
	return when
}

// ParseReminders accepts "15, 1h, 1d, 90m" style lists and returns minutes.
func ParseReminders(s string) ([]int, error) {
	var res []int
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		mult := 1
		switch {
		case strings.HasSuffix(part, "d"):
			mult, part = 24*60, strings.TrimSuffix(part, "d")
		case strings.HasSuffix(part, "h"):
			mult, part = 60, strings.TrimSuffix(part, "h")
		case strings.HasSuffix(part, "w"):
			mult, part = 7*24*60, strings.TrimSuffix(part, "w")
		case strings.HasSuffix(part, "m"):
			part = strings.TrimSuffix(part, "m")
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("invalid reminder %q", part)
		}
		res = append(res, v*mult)
	}
	return res, nil
}

func FormatReminders(mins []int) string {
	parts := make([]string, len(mins))
	for i, m := range mins {
		switch {
		case m > 0 && m%(7*24*60) == 0:
			parts[i] = strconv.Itoa(m/(7*24*60)) + "w"
		case m > 0 && m%(24*60) == 0:
			parts[i] = strconv.Itoa(m/(24*60)) + "d"
		case m > 0 && m%60 == 0:
			parts[i] = strconv.Itoa(m/60) + "h"
		default:
			parts[i] = strconv.Itoa(m) + "m"
		}
	}
	return strings.Join(parts, ", ")
}

// ParseClock parses "9:30", "09:30", "0930" or "9".
func ParseClock(s string) (h, m int, err error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"15:04", "1504", "15"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t.Hour(), t.Minute(), nil
		}
	}
	return 0, 0, fmt.Errorf("invalid time %q (use HH:MM)", s)
}

// Combine joins a calendar date with an "HH:MM" clock in the local zone.
func Combine(date time.Time, clock string) (time.Time, error) {
	h, m, err := ParseClock(clock)
	if err != nil {
		return time.Time{}, err
	}
	y, mo, d := date.Date()
	return time.Date(y, mo, d, h, m, 0, 0, time.Local), nil
}

func DayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// WeekStart returns the Monday that starts t's week.
func WeekStart(t time.Time) time.Time {
	d := DayStart(t)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func SameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func ContainsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
