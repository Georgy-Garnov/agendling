package core

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/caldav"
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
	cals, _ := svc.EditableCalendars(&model.Event{})
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)

	e := &model.Event{}
	bad := EditorInput{Title: "x", Calendar: &cals[0], StartDate: day, EndDate: day, StartClock: "11:00", EndClock: "10:00"}
	if err := svc.ApplyEdit(e, bad); err == nil {
		t.Fatal("end before start accepted")
	}
	in := EditorInput{Title: " Review ", Calendar: &cals[0], StartDate: day, EndDate: day, StartClock: "10:00", EndClock: "11:30",
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

func TestCalendarChoices(t *testing.T) {
	cals := []caldav.CalendarInfo{{Path: "/u/a/", Name: "A"}, {Path: "/u/b/", Name: "B"}, {Path: "/u/new/", Name: "New"}}
	ch := CalendarChoices(cals, []string{"/u/b/", "/u/gone/"})
	if !ch[0].Shown || ch[1].Shown || !ch[2].Shown {
		t.Fatalf("choices: %+v", ch)
	}
	hidden, any := HiddenCalendars(ch)
	if len(hidden) != 1 || hidden[0] != "/u/b/" || !any {
		t.Fatalf("hidden %v, any %v", hidden, any) // calendars gone from the server are dropped
	}
	for i := range ch {
		ch[i].Shown = false
	}
	if hidden, any := HiddenCalendars(ch); len(hidden) != 3 || any {
		t.Fatalf("all hidden: %v, %v", hidden, any)
	}
}

func TestCalendarsOfAccounts(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store, nil, nil)
	svc.EnsureDefaultCalendar()
	acct := &model.CalendarSource{Name: "Cloud", Type: model.SourceCalDAV, URL: "https://dav.example/", Username: "u",
		Enabled: true, CalendarPath: "/u/a/", HiddenCollections: []string{"/u/c/"}}
	if err := store.SaveSource(acct); err != nil {
		t.Fatal(err)
	}
	// What a sync of an account with three calendars, one unsubscribed in its settings, leaves behind.
	if err := store.SetSyncState(acct.ID, `{"v":3,"collections":["/u/a/","/u/b/","/u/c/"],"names":{"/u/a/":"Work","/u/b/":"Home","/u/c/":"Old"}}`); err != nil {
		t.Fatal(err)
	}
	labels := func(cals []Calendar) string {
		var s []string
		for _, c := range cals {
			mark := "+"
			if !c.Shown {
				mark = "-"
			}
			s = append(s, mark+c.Label())
		}
		return strings.Join(s, ",")
	}
	if got := labels(svc.Calendars()); got != "+Personal  (local),+Cloud / Work,+Cloud / Home" {
		t.Fatalf("calendars: %s", got)
	}

	day := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	personal := svc.Calendars()[0].Source.ID
	for _, e := range []*model.Event{
		{CalendarID: personal, UID: "p", Title: "Personal event"},
		{CalendarID: acct.ID, UID: "w", Title: "Work event", Href: "/u/a/w.ics"},
		{CalendarID: acct.ID, UID: "h", Title: "Home event", Href: "/u/b/h.ics"},
		{CalendarID: acct.ID, UID: "n", Title: "Unsent work event", Dirty: true}, // goes to the write calendar
	} {
		e.StartTime, e.EndTime = day, day.Add(time.Hour)
		if err := store.SaveEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	shown := func() string {
		evs, err := svc.ViewEvents(day.Add(-time.Hour), day.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, e := range evs {
			s = append(s, e.Title)
		}
		sort.Strings(s)
		return strings.Join(s, ",")
	}
	if got := shown(); got != "Home event,Personal event,Unsent work event,Work event" {
		t.Fatalf("all shown: %s", got)
	}
	cals := svc.Calendars()
	if err := svc.SetCalendarShown(cals[1].Key, false); err != nil { // Cloud / Work
		t.Fatal(err)
	}
	if err := svc.SetCalendarShown(cals[0].Key, false); err != nil { // Personal
		t.Fatal(err)
	}
	if got := shown(); got != "Home event" {
		t.Fatalf("work and personal hidden: %s", got)
	}
	if got := labels(svc.Calendars()); got != "-Personal  (local),-Cloud / Work,+Cloud / Home" {
		t.Fatalf("calendars after hiding: %s", got)
	}
	newWork := &model.Event{CalendarID: acct.ID, UID: "nw", Title: "New work", Href: "/u/a/nw.ics", StartTime: day, EndTime: day.Add(time.Hour)}
	newHome := &model.Event{CalendarID: acct.ID, UID: "nh", Title: "New home", Href: "/u/b/nh.ics", StartTime: day, EndTime: day.Add(time.Hour)}
	if got := svc.CalendarLabel(newHome); got != "Cloud / Home" {
		t.Fatalf("calendar label: %q", got)
	}
	if alerts, rest := svc.NewEventAlerts(acct.ID, []*model.Event{newWork, newHome}); len(alerts) != 1 || alerts[0].Event != newHome || rest != 0 {
		t.Fatalf("new-event alerts of a hidden calendar: %+v, %d", alerts, rest)
	}

	// A new event defaults to the first ticked calendar and is created in that collection.
	newEv := &model.Event{}
	ed, idx := svc.EditableCalendars(newEv)
	if ed[idx].Label() != "Cloud / Home" {
		t.Fatalf("default calendar: %s", ed[idx].Label())
	}
	in := EditorInput{Title: "Dentist", Calendar: &ed[idx], StartDate: day, EndDate: day, StartClock: "10:00", EndClock: "11:00"}
	if err := svc.ApplyEdit(newEv, in); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(newEv.Href, "/u/b/") || !newEv.Dirty || newEv.MovedFrom != "" {
		t.Fatalf("new event: href %q dirty %v moved %q", newEv.Href, newEv.Dirty, newEv.MovedFrom)
	}

	// Moving an event to another calendar of the account keeps one series and remembers the old copy.
	work, _ := store.EventsByUID(acct.ID, "w")
	ed, idx = svc.EditableCalendars(work[0])
	if ed[idx].Label() != "Cloud / Work" {
		t.Fatalf("current calendar: %s", ed[idx].Label())
	}
	in.Title, in.Calendar = "Work event", &ed[idx+1] // Cloud / Home
	if err := svc.ApplyEdit(work[0], in); err != nil {
		t.Fatal(err)
	}
	moved, _ := store.EventsByUID(acct.ID, "w")
	if len(moved) != 1 || moved[0].Href != "/u/b/w.ics" || moved[0].MovedFrom != "/u/a/w.ics" || !moved[0].Dirty {
		t.Fatalf("moved: %+v", moved[0])
	}
	if got := shown(); got != "Dentist,Home event,Work event" {
		t.Fatalf("after move: %s", got)
	}

	// Showing a calendar again.
	if err := svc.SetCalendarShown(cals[1].Key, true); err != nil {
		t.Fatal(err)
	}
	if got := labels(svc.Calendars()); got != "-Personal  (local),+Cloud / Work,+Cloud / Home" {
		t.Fatalf("calendars after showing: %s", got)
	}
}

func TestFilterSettingsPages(t *testing.T) {
	keys := func(q string) string {
		var s []string
		for _, p := range FilterSettingsPages(q) {
			s = append(s, p.Key)
		}
		return strings.Join(s, ",")
	}
	for q, want := range map[string]string{
		"":              "calendars,reminders,sound,alerts,startup,about",
		"snooze":        "reminders",
		"ICLOUD":        "calendars",
		"mp3":           "sound",
		"update":        "about",
		"start min":     "startup",
		"sound popup":   "alerts",
		"nothing-there": "",
	} {
		if got := keys(q); got != want {
			t.Errorf("FilterSettingsPages(%q) = %q, want %q", q, got, want)
		}
	}
}
