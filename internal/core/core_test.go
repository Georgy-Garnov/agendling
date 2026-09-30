package core

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

func TestParseReminders(t *testing.T) {
	got, err := ParseReminders("5, 15m; 1h 2d 1w")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{5, 15, 60, 2880, 10080}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if FormatReminders(want) != "5m, 15m, 1h, 2d, 1w" {
		t.Fatalf("format: %q", FormatReminders(want))
	}
	if _, err := ParseReminders("soon"); err == nil {
		t.Fatal("expected error")
	}
	if h, m, err := ParseClock("9:05"); err != nil || h != 9 || m != 5 {
		t.Fatalf("clock: %d:%d %v", h, m, err)
	}
}

func occ(start string, mins int, allDay bool) model.Occurrence {
	s, _ := time.ParseInLocation("2006-01-02 15:04", start, time.Local)
	return model.Occurrence{Event: &model.Event{AllDay: allDay}, Start: s, End: s.Add(time.Duration(mins) * time.Minute)}
}

func TestLayoutLanes(t *testing.T) {
	items := []model.Occurrence{
		occ("2026-09-30 11:00", 90, false), // lane 0 of 2
		occ("2026-09-30 11:30", 30, false), // lane 1
		occ("2026-09-30 12:00", 30, false), // lane 1 again (after 11:30-12:00)
		occ("2026-09-30 15:00", 60, false), // separate cluster, 1 lane
	}
	got := LayoutLanes(items)
	want := [][2]int{{0, 2}, {1, 2}, {1, 2}, {0, 1}}
	for i, w := range want {
		if got[i].Lane != w[0] || got[i].Lanes != w[1] {
			t.Fatalf("item %d: lane %d/%d, want %d/%d", i, got[i].Lane, got[i].Lanes, w[0], w[1])
		}
	}
}

func TestPackAllDay(t *testing.T) {
	from := time.Date(2026, 9, 28, 0, 0, 0, 0, time.Local)
	spans, rows := PackAllDay([]model.Occurrence{
		occ("2026-10-01 00:00", 3*24*60, true), // Thu..Sat
		occ("2026-10-02 00:00", 24*60, true),   // Fri, overlaps -> row 1
		occ("2026-09-28 00:00", 24*60, true),   // Mon, fits row 0
	}, from, 7)
	// Sorted by start: Mon (row 0), Thu..Sat (row 0), Fri (row 1).
	if rows != 2 || spans[0].Col0 != 0 || spans[1].Col0 != 3 || spans[1].Col1 != 5 || spans[1].Row != 0 || spans[2].Row != 1 {
		t.Fatalf("rows=%d spans=%+v", rows, spans)
	}
}

func TestViewTitle(t *testing.T) {
	a := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	if got := ViewTitle(ViewWeek, a); got != "28 Sep - 4 Oct 2026" {
		t.Fatalf("week title %q", got)
	}
	if got := ViewTitle(ViewMonth, a); got != "September 2026" {
		t.Fatalf("month title %q", got)
	}
}

func TestApplyEdit(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store, nil, nil)
	svc.EnsureDefaultCalendar()
	cals, _ := svc.EditableCalendars(0)
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)

	e := &model.Event{}
	bad := EditorInput{Title: "x", Calendar: cals[0], StartDate: day, EndDate: day, StartClock: "11:00", EndClock: "10:00"}
	if err := svc.ApplyEdit(e, bad); err == nil {
		t.Fatal("end before start accepted")
	}
	in := EditorInput{Title: " Review ", Calendar: cals[0], StartDate: day, EndDate: day, StartClock: "10:00", EndClock: "11:30",
		Reminders: "10, 1h", Rule: RuleForChoice(3, "")}
	if err := svc.ApplyEdit(e, in); err != nil {
		t.Fatal(err)
	}
	got, err := store.Event(e.ID)
	if err != nil || got.Title != "Review" || got.RRule != "FREQ=WEEKLY" || len(got.Reminders) != 2 ||
		got.StartTime.Hour() != 10 || got.EndTime.Sub(got.StartTime) != 90*time.Minute || got.UID == "" {
		t.Fatalf("saved: %+v %v", got, err)
	}
	// Deleting one occurrence adds an EXDATE; the series survives.
	if err := svc.DeleteOccurrence(got, got.StartTime.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Event(e.ID)
	if len(got.ExDates) != 1 {
		t.Fatalf("exdates: %v", got.ExDates)
	}
}
