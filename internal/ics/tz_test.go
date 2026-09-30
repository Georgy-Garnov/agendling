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

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func withLocal(t *testing.T, loc *time.Location) {
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

// Regression: a series defined in a zone without DST that started in winter was shown an
// hour off during summer on a machine whose zone observes DST, because the recurrence was
// expanded in the local zone instead of the event's own zone.
func TestRecurrenceKeepsEventZone(t *testing.T) {
	berlin, lagos := mustLoad(t, "Europe/Berlin"), mustLoad(t, "Africa/Lagos") // Lagos: UTC+1 all year
	withLocal(t, berlin)

	src := strings.ReplaceAll(`BEGIN:VCALENDAR
VERSION:2.0
PRODID:test
BEGIN:VEVENT
UID:daily@example.com
DTSTART;TZID=Africa/Lagos:20260114T100000
DTEND;TZID=Africa/Lagos:20260114T103000
SUMMARY:Daily sync
RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR;INTERVAL=1
END:VEVENT
END:VCALENDAR
`, "\n", "\r\n")
	cal, err := ical.NewDecoder(strings.NewReader(src)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	evs := ToEvents(cal, "", "")
	if evs[0].TZID != "Africa/Lagos" {
		t.Fatalf("TZID = %q", evs[0].TZID)
	}

	check := func(from time.Time, wantLocalHour int) {
		occ := recur.Expand(evs, from, from.AddDate(0, 0, 7))
		if len(occ) != 3 {
			t.Fatalf("week of %s: %d occurrences", from.Format("2006-01-02"), len(occ))
		}
		for _, o := range occ {
			if h := o.Start.In(lagos).Hour(); h != 10 {
				t.Errorf("%s: %02d:00 in the event zone, want 10:00", o.Start, h)
			}
			if h := o.Start.In(berlin).Hour(); h != wantLocalHour {
				t.Errorf("%s: %02d:00 local, want %02d:00", o.Start, h, wantLocalHour)
			}
		}
	}
	check(time.Date(2026, 2, 2, 0, 0, 0, 0, berlin), 10) // local winter time = event zone offset
	check(time.Date(2026, 7, 6, 0, 0, 0, 0, berlin), 11) // local summer time is one hour ahead
}

func TestVTimezone(t *testing.T) {
	berlin := VTimezone(mustLoad(t, "Europe/Berlin"), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	var rules []string
	for _, c := range berlin.Children {
		rules = append(rules, c.Name+" "+c.Props.Get(ical.PropRecurrenceRule).Value+" "+
			c.Props.Get(ical.PropTimezoneOffsetFrom).Value+">"+c.Props.Get(ical.PropTimezoneOffsetTo).Value+
			" "+c.Props.Get(ical.PropDateTimeStart).Value)
	}
	want := []string{
		"DAYLIGHT FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU +0100>+0200 20260329T020000",
		"STANDARD FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU +0200>+0100 20261025T030000",
	}
	if strings.Join(rules, "|") != strings.Join(want, "|") {
		t.Fatalf("Berlin VTIMEZONE:\n got %q\nwant %q", rules, want)
	}

	tokyo := VTimezone(mustLoad(t, "Asia/Tokyo"), time.Now())
	if len(tokyo.Children) != 1 || tokyo.Children[0].Name != ical.CompTimezoneStandard ||
		tokyo.Children[0].Props.Get(ical.PropTimezoneOffsetTo).Value != "+0900" {
		t.Fatalf("Tokyo VTIMEZONE: %+v", tokyo.Children)
	}
}

func TestZonedRoundTrip(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	withLocal(t, mustLoad(t, "America/New_York"))
	start := time.Date(2026, 1, 12, 10, 0, 0, 0, berlin)
	e := &model.Event{UID: "r", Title: "Weekly", StartTime: start, EndTime: start.Add(time.Hour),
		TZID: "Europe/Berlin", RRule: "FREQ=WEEKLY;BYDAY=MO", ExDates: []time.Time{start.AddDate(0, 0, 7)}}

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(FromEvents([]*model.Event{e})); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"BEGIN:VTIMEZONE", "TZID:Europe/Berlin", "DTSTART;TZID=Europe/Berlin:20260112T100000", "EXDATE;TZID=Europe/Berlin:20260119T100000"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	cal, err := ical.NewDecoder(&buf).Decode()
	if err != nil {
		t.Fatal(err)
	}
	got := ToEvents(cal, "", "")
	if len(got) != 1 || got[0].TZID != "Europe/Berlin" {
		t.Fatalf("decoded: %+v", got)
	}
	// Winter and summer occurrences stay at 10:00 Berlin time.
	for _, from := range []time.Time{time.Date(2026, 1, 12, 0, 0, 0, 0, berlin), time.Date(2026, 7, 6, 0, 0, 0, 0, berlin)} {
		occ := recur.Expand(got, from, from.AddDate(0, 0, 1))
		if len(occ) != 1 || occ[0].Start.In(berlin).Hour() != 10 {
			t.Fatalf("occurrences from %s: %+v", from, occ)
		}
	}
	// The excluded date is honoured after the round trip.
	if occ := recur.Expand(got, start.AddDate(0, 0, 7), start.AddDate(0, 0, 8)); len(occ) != 0 {
		t.Fatalf("EXDATE ignored: %+v", occ)
	}
}
