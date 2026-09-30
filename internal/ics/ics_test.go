package ics

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/recur"
)

const sample = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:test
BEGIN:VEVENT
UID:abc@example
DTSTART;TZID=Europe/Berlin:20260105T100000
DTEND;TZID=Europe/Berlin:20260105T110000
SUMMARY:Standup
DESCRIPTION:Join https://meet.example/x
RRULE:FREQ=DAILY;COUNT=5
EXDATE;TZID=Europe/Berlin:20260107T100000
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT15M
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:abc@example
RECURRENCE-ID;TZID=Europe/Berlin:20260108T100000
DTSTART;TZID=Europe/Berlin:20260108T120000
DTEND;TZID=Europe/Berlin:20260108T130000
SUMMARY:Standup (moved)
END:VEVENT
BEGIN:VEVENT
UID:allday@example
DTSTART;VALUE=DATE:20260110
SUMMARY:Holiday
END:VEVENT
END:VCALENDAR
`

func TestParseAndExpand(t *testing.T) {
	cal, err := ical.NewDecoder(strings.NewReader(strings.ReplaceAll(sample, "\n", "\r\n"))).Decode()
	if err != nil {
		t.Fatal(err)
	}
	evs := ToEvents(cal, "/cal/abc.ics", "etag")
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d", len(evs))
	}
	master := evs[0]
	if master.RRule != "FREQ=DAILY;COUNT=5" || len(master.ExDates) != 1 || len(master.Reminders) != 1 {
		t.Fatalf("bad master: %+v", master)
	}
	if master.Reminders[0].Minutes() != 15 {
		t.Fatalf("reminder = %d", master.Reminders[0].Minutes())
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if !master.StartTime.Equal(time.Date(2026, 1, 5, 10, 0, 0, 0, berlin)) {
		t.Fatalf("start = %v", master.StartTime)
	}
	if !evs[2].AllDay || evs[2].EndTime.Sub(evs[2].StartTime) != 24*time.Hour {
		t.Fatalf("bad all-day: %+v", evs[2])
	}

	occ := recur.Expand(evs, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	// 5 daily - 1 exdate - 1 overridden + 1 override + 1 all-day = 5
	if len(occ) != 5 {
		for _, o := range occ {
			t.Log(o.Event.Title, o.Start)
		}
		t.Fatalf("want 5 occurrences, got %d", len(occ))
	}
}

func TestRoundTrip(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 30, 0, 0, time.Local)
	e := &model.Event{UID: NewUID(), Title: "Review", Description: "line1\nline2", URL: "https://x.test/a?b=c",
		StartTime: start, EndTime: start.Add(time.Hour), RRule: "FREQ=WEEKLY;BYDAY=MO",
		ExDates: []time.Time{start.AddDate(0, 0, 7)}, Reminders: []model.ReminderOffset{model.MinutesOffset(60)}}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(FromEvents([]*model.Event{e})); err != nil {
		t.Fatal(err)
	}
	cal, err := ical.NewDecoder(&buf).Decode()
	if err != nil {
		t.Fatal(err)
	}
	got := ToEvents(cal, "", "")[0]
	if got.Title != e.Title || got.Description != e.Description || got.URL != e.URL || !got.StartTime.Equal(start) ||
		got.RRule != e.RRule || len(got.ExDates) != 1 || got.Reminders[0].Minutes() != 60 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
