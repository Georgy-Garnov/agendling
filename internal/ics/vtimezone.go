package ics

import (
	"fmt"
	"time"

	"github.com/emersion/go-ical"
)

// VTimezone describes loc as an RFC 5545 VTIMEZONE, based on the UTC offset
// transitions of the year of ref. Each transition becomes a yearly STANDARD or
// DAYLIGHT rule ("2nd Sunday of March"); a zone without DST gets one STANDARD block.
func VTimezone(loc *time.Location, ref time.Time) *ical.Component {
	tz := ical.NewComponent(ical.CompTimezone)
	tz.Props.SetText(ical.PropTimezoneID, loc.String())

	year := ref.In(loc).Year()
	transitions := yearTransitions(loc, year)
	if len(transitions) == 0 {
		t := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
		name, off := t.Zone()
		tz.Children = append(tz.Children, observance(ical.CompTimezoneStandard, name, off, off,
			time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), ""))
		return tz
	}
	for _, tr := range transitions {
		kind := ical.CompTimezoneStandard
		if tr.to > tr.from {
			kind = ical.CompTimezoneDaylight
		}
		// DTSTART is the wall-clock time of the transition in the offset before it.
		wall := tr.at.UTC().Add(time.Duration(tr.from) * time.Second)
		tz.Children = append(tz.Children, observance(kind, tr.name, tr.from, tr.to, wall, yearlyRule(wall)))
	}
	return tz
}

type transition struct {
	at       time.Time
	from, to int
	name     string
}

func yearTransitions(loc *time.Location, year int) []transition {
	var res []transition
	t := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, loc)
	_, prev := t.Zone()
	for t.Before(end) {
		next := t.Add(time.Hour)
		if _, off := next.In(loc).Zone(); off != prev {
			// Narrow the change down to the minute.
			lo, hi := t, next
			for hi.Sub(lo) > time.Minute {
				mid := lo.Add(hi.Sub(lo) / 2)
				if _, o := mid.In(loc).Zone(); o == prev {
					lo = mid
				} else {
					hi = mid
				}
			}
			name, _ := hi.In(loc).Zone()
			res = append(res, transition{at: hi, from: prev, to: off, name: name})
			prev = off
		}
		t = next
	}
	return res
}

var weekdayCodes = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// yearlyRule expresses a date as "n-th weekday of the month" (or "last weekday").
func yearlyRule(t time.Time) string {
	n := (t.Day()-1)/7 + 1
	daysInMonth := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if t.Day()+7 > daysInMonth {
		n = -1
	}
	return fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(t.Month()), n, weekdayCodes[t.Weekday()])
}

func observance(kind, name string, from, to int, start time.Time, rule string) *ical.Component {
	c := ical.NewComponent(kind)
	dt := ical.NewProp(ical.PropDateTimeStart)
	dt.Value = start.Format(datetimeFormat) // local time without TZID, as RFC 5545 requires
	c.Props.Set(dt)
	c.Props.SetText(ical.PropTimezoneOffsetFrom, formatOffset(from))
	c.Props.SetText(ical.PropTimezoneOffsetTo, formatOffset(to))
	if name != "" {
		c.Props.SetText(ical.PropTimezoneName, name)
	}
	if rule != "" {
		r := ical.NewProp(ical.PropRecurrenceRule)
		r.SetValueType(ical.ValueRecurrence)
		r.Value = rule
		c.Props.Set(r)
	}
	return c
}

func formatOffset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%02d%02d", sign, sec/3600, sec%3600/60)
}
