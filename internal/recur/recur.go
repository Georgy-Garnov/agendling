// Package recur expands stored events (including RRULE series with EXDATE and
// RECURRENCE-ID overrides) into concrete occurrences for a time range.
package recur

import (
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

// maxOccurrences guards against pathological rules (e.g. FREQ=SECONDLY).
const maxOccurrences = 5000

// Expand returns occurrences overlapping [from, to), sorted by start time.
func Expand(events []*model.Event, from, to time.Time) []model.Occurrence {
	// Overrides replace the occurrence of their master identified by RECURRENCE-ID.
	overridden := map[string]map[int64]bool{}
	for _, e := range events {
		if !e.RecurrenceID.IsZero() {
			m := overridden[e.UID]
			if m == nil {
				m = map[int64]bool{}
				overridden[e.UID] = m
			}
			m[e.RecurrenceID.Unix()] = true
		}
	}

	var res []model.Occurrence
	for _, e := range events {
		if e.Deleted {
			continue
		}
		if e.RRule == "" || !e.RecurrenceID.IsZero() {
			if overlaps(e.StartTime, e.EndTime, e.AllDay, from, to) {
				res = append(res, model.Occurrence{Event: e, Start: e.StartTime, End: e.EndTime})
			}
			continue
		}
		for _, start := range Starts(e, from.Add(-e.Duration()), to) {
			start = start.In(time.Local)
			if overridden[e.UID][start.Unix()] {
				continue
			}
			end := start.Add(e.Duration())
			if e.AllDay {
				// Keep all-day spans in whole calendar days across DST changes.
				end = addDays(start, daysBetween(e.StartTime, e.EndTime))
			}
			if overlaps(start, end, e.AllDay, from, to) {
				res = append(res, model.Occurrence{Event: e, Start: start, End: end})
			}
		}
	}
	sort.Slice(res, func(i, j int) bool {
		if !res[i].Start.Equal(res[j].Start) {
			return res[i].Start.Before(res[j].Start)
		}
		if res[i].Event.AllDay != res[j].Event.AllDay {
			return res[i].Event.AllDay
		}
		return res[i].End.After(res[j].End)
	})
	return res
}

// Starts lists occurrence start times of a recurring event within [from, to].
func Starts(e *model.Event, from, to time.Time) []time.Time {
	set, err := RuleSet(e)
	if err != nil {
		// A rule we cannot parse degrades to a single occurrence.
		if !e.StartTime.Before(from) && e.StartTime.Before(to) {
			return []time.Time{e.StartTime}
		}
		return nil
	}
	var res []time.Time
	it := set.Iterator()
	for i := 0; i < maxOccurrences; i++ {
		t, ok := it()
		if !ok || !t.Before(to) {
			break
		}
		if !t.Before(from) {
			res = append(res, t)
		}
	}
	return res
}

// RuleSet builds the recurrence set in the event's own time zone (RFC 5545 §3.3.10):
// a series defined as 10:00 in a zone without DST stays at 10:00 there even when the
// local zone changes its UTC offset for daylight saving time.
func RuleSet(e *model.Event) (*rrule.Set, error) {
	zone := e.Zone()
	opt, err := rrule.StrToROptionInLocation(strings.TrimPrefix(e.RRule, "RRULE:"), zone)
	if err != nil {
		return nil, err
	}
	opt.Dtstart = e.StartTime.In(zone)
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, err
	}
	set := &rrule.Set{}
	set.RRule(r)
	for _, ex := range e.ExDates {
		set.ExDate(ex)
	}
	return set, nil
}

func overlaps(start, end time.Time, allDay bool, from, to time.Time) bool {
	if !end.After(start) {
		// Zero-length events still occupy their instant.
		end = start.Add(time.Minute)
		if allDay {
			end = addDays(start, 1)
		}
	}
	return start.Before(to) && end.After(from)
}

func addDays(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+n, 0, 0, 0, 0, t.Location())
}

func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	ua := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	ub := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	n := int(ub.Sub(ua).Hours() / 24)
	if n < 1 {
		n = 1
	}
	return n
}
