package core

import (
	"strings"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/i18n"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
)

// A made-up language for these tests only: it checks that core's text goes through the catalog
// without depending on a real translation.
func init() {
	i18n.Register(&i18n.Catalog{
		Tag:  "qps",
		Name: "Pseudo",
		PluralForm: func(n int) int {
			if n <= 1 {
				return 0
			}
			return 1
		},
		Messages: map[string]string{
			"Monday, 2 January 2006": "Monday 2 January 2006",
			"%s (all day)":           "%s (journée)",
			"starts in %s":           "commence dans %s",
			"%d min":                 "%d mn",
			"(No title)":             "(Sans titre)",
			"Week":                   "Semaine",
			"Custom: %s":             "Personnalisé : %s",
			"Daily":                  "Chaque jour",
			"Sound":                  "Son",
			"default reminders minutes hours before snooze interval alert popup notification": "rappels par défaut",
		},
		Plurals: map[string][]string{"%d calendar": {"%d calendrier", "%d calendriers"}},
		Days:    [7]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
		Months: [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août",
			"septembre", "octobre", "novembre", "décembre"},
	})
}

func usePseudo(t *testing.T) {
	t.Helper()
	if i18n.Use("qps") != "qps" {
		t.Fatal("pseudo language not available")
	}
	t.Cleanup(func() { i18n.Use("en") })
}

func TestCoreTextIsTranslated(t *testing.T) {
	usePseudo(t)
	mon := time.Date(2026, 9, 28, 12, 0, 0, 0, time.Local)

	if got := ViewTitle(ViewDay, mon); got != "Lundi 28 septembre 2026" {
		t.Errorf("ViewTitle(day) = %q", got)
	}
	if got := ViewTitle(ViewMonth, mon); got != "Septembre 2026" {
		t.Errorf("ViewTitle(month) = %q", got)
	}
	if got := i18n.T(ViewNames[ViewWeek]); got != "Semaine" {
		t.Errorf("view name = %q", got)
	}
	if got := EventTitle(&model.Event{}); got != "(Sans titre)" {
		t.Errorf("EventTitle = %q", got)
	}
	if got := HumanDuration(15 * time.Minute); got != "15 mn" {
		t.Errorf("HumanDuration = %q", got)
	}

	labels, idx, _ := RepeatChoices("FREQ=DAILY")
	if labels[idx] != "Chaque jour" || labels[0] != "Does not repeat" {
		t.Errorf("RepeatChoices = %q (selected %d)", labels, idx)
	}
	if labels, idx, custom := RepeatChoices("FREQ=DAILY;COUNT=3"); labels[idx] != "Personnalisé : FREQ=DAILY;COUNT=3" ||
		RuleForChoice(idx, custom) != "FREQ=DAILY;COUNT=3" {
		t.Errorf("custom repeat = %q", labels[idx])
	}

	snooze, cur := SnoozeLabels(10)
	if snooze[cur] != "10 mn" || ParseSnoozeLabel(snooze[cur], 5) != 10 {
		t.Errorf("snooze label %q does not read back", snooze[cur])
	}

	allDay := scheduler.Alert{Event: &model.Event{AllDay: true}, Start: mon, End: mon.AddDate(0, 0, 1)}
	if got := WhenText(allDay); got != "Lundi 28 septembre 2026 (journée)" {
		t.Errorf("WhenText(all day) = %q", got)
	}
	start := time.Now().Add(20 * time.Minute)
	timed := scheduler.Alert{Event: &model.Event{}, Start: start, End: start.Add(time.Hour)}
	if got := WhenText(timed); !strings.HasSuffix(got, "  ·  commence dans 20 mn") {
		t.Errorf("WhenText(timed) = %q", got)
	}
}

func TestSettingsSearchMatchesBothLanguages(t *testing.T) {
	usePseudo(t)
	keys := func(q string) string {
		var res []string
		for _, p := range FilterSettingsPages(q) {
			res = append(res, p.Key)
		}
		return strings.Join(res, ",")
	}
	for q, want := range map[string]string{
		"son":                "sound",     // translated title
		"rappels par défaut": "reminders", // translated keywords
		"snooze":             "reminders", // English keywords still match
		"CHIME":              "sound",
	} {
		if got := keys(q); got != want {
			t.Errorf("FilterSettingsPages(%q) = %q, want %q", q, got, want)
		}
	}
}
