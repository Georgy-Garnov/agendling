package core

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

type ViewMode int

const (
	ViewDay ViewMode = iota
	ViewWeek
	View2Week
	ViewMonth
)

var ViewNames = [...]string{ViewDay: "Day", ViewWeek: "Week", View2Week: "2 Weeks", ViewMonth: "Month"}

// ViewRange returns the visible [from, to) interval of a view around anchor.
func ViewRange(mode ViewMode, anchor time.Time) (time.Time, time.Time) {
	switch mode {
	case ViewDay:
		d := DayStart(anchor)
		return d, d.AddDate(0, 0, 1)
	case ViewWeek:
		w := WeekStart(anchor)
		return w, w.AddDate(0, 0, 7)
	case View2Week:
		w := WeekStart(anchor)
		return w, w.AddDate(0, 0, 14)
	default:
		y, m, _ := anchor.Date()
		w := WeekStart(time.Date(y, m, 1, 0, 0, 0, 0, time.Local))
		return w, w.AddDate(0, 0, 42)
	}
}

// ViewTitle is the heading shown above a view, e.g. "28 Sep - 4 Oct 2026".
func ViewTitle(mode ViewMode, anchor time.Time) string {
	from, to := ViewRange(mode, anchor)
	last := to.AddDate(0, 0, -1)
	switch mode {
	case ViewDay:
		return from.Format("Monday, 2 January 2006")
	case ViewMonth:
		return anchor.Format("January 2006")
	default:
		if from.Year() != last.Year() {
			return from.Format("2 Jan 2006") + " - " + last.Format("2 Jan 2006")
		}
		if from.Month() != last.Month() {
			return from.Format("2 Jan") + " - " + last.Format("2 Jan 2006")
		}
		return fmt.Sprintf("%d - %s", from.Day(), last.Format("2 January 2006"))
	}
}

// Navigate moves anchor one view length forward (dir=1) or back (dir=-1).
func Navigate(mode ViewMode, anchor time.Time, dir int) time.Time {
	switch mode {
	case ViewDay:
		return anchor.AddDate(0, 0, dir)
	case ViewWeek:
		return anchor.AddDate(0, 0, 7*dir)
	case View2Week:
		return anchor.AddDate(0, 0, 14*dir)
	default:
		y, m, _ := anchor.Date()
		return time.Date(y, m+time.Month(dir), 1, 12, 0, 0, 0, time.Local)
	}
}

// IsAllDayLike reports whether an occurrence belongs in the all-day strip.
func IsAllDayLike(o model.Occurrence) bool {
	return o.Event.AllDay || o.End.Sub(o.Start) >= 24*time.Hour
}

type LaneItem struct {
	O           model.Occurrence
	Lane, Lanes int
}

// LayoutLanes assigns side-by-side columns to overlapping timed events within one day.
func LayoutLanes(items []model.Occurrence) []LaneItem {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].Start.Equal(items[j].Start) {
			return items[i].Start.Before(items[j].Start)
		}
		return items[i].End.After(items[j].End)
	})
	visEnd := func(o model.Occurrence) time.Time {
		if o.End.Sub(o.Start) < 20*time.Minute {
			return o.Start.Add(20 * time.Minute) // matches the minimum painted height
		}
		return o.End
	}
	var res []LaneItem
	var cluster []int
	var laneEnds []time.Time
	var clusterEnd time.Time
	flush := func() {
		for _, idx := range cluster {
			res[idx].Lanes = len(laneEnds)
		}
		cluster, laneEnds = nil, nil
	}
	for _, o := range items {
		if len(cluster) > 0 && !o.Start.Before(clusterEnd) {
			flush()
		}
		lane := -1
		for l, end := range laneEnds {
			if !o.Start.Before(end) {
				lane = l
				break
			}
		}
		if lane < 0 {
			lane = len(laneEnds)
			laneEnds = append(laneEnds, time.Time{})
		}
		laneEnds[lane] = visEnd(o)
		if len(cluster) == 0 || visEnd(o).After(clusterEnd) {
			clusterEnd = visEnd(o)
		}
		res = append(res, LaneItem{O: o, Lane: lane})
		cluster = append(cluster, len(res)-1)
	}
	flush()
	return res
}

// AllDaySpan is an all-day(-like) occurrence packed into a row of the all-day strip.
type AllDaySpan struct {
	O               model.Occurrence
	Col0, Col1, Row int
}

// PackAllDay lays out all-day occurrences over `days` columns starting at from.
// It returns the spans and the number of rows used.
func PackAllDay(occs []model.Occurrence, from time.Time, days int) ([]AllDaySpan, int) {
	var spans []AllDaySpan
	var rowsEnd []int
	dayIndex := func(t time.Time) int {
		y, m, d := t.Date()
		fy, fm, fd := from.Date()
		return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Sub(time.Date(fy, fm, fd, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	}
	var all []model.Occurrence
	for _, o := range occs {
		if IsAllDayLike(o) {
			all = append(all, o)
		}
	}
	// Greedy packing needs start order; longer spans first on ties.
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Start.Equal(all[j].Start) {
			return all[i].Start.Before(all[j].Start)
		}
		return all[i].End.After(all[j].End)
	})
	for _, o := range all {
		c0, c1 := dayIndex(o.Start), dayIndex(o.End.Add(-time.Second))
		c0, c1 = max(c0, 0), min(c1, days-1)
		if c1 < c0 {
			continue
		}
		row := -1
		for r, end := range rowsEnd {
			if end < c0 {
				row = r
				break
			}
		}
		if row < 0 {
			row = len(rowsEnd)
			rowsEnd = append(rowsEnd, 0)
		}
		rowsEnd[row] = c1
		spans = append(spans, AllDaySpan{o, c0, c1, row})
	}
	return spans, len(rowsEnd)
}

// DayItems returns the occurrences touching day d, all-day ones first.
func DayItems(occs []model.Occurrence, d time.Time) []model.Occurrence {
	next := d.AddDate(0, 0, 1)
	var items []model.Occurrence
	for _, o := range occs {
		end := o.End
		if !end.After(o.Start) {
			end = o.Start.Add(time.Minute)
		}
		if o.Start.Before(next) && end.After(d) {
			items = append(items, o)
		}
	}
	sort.SliceStable(items, func(a, b int) bool {
		ia, ib := IsAllDayLike(items[a]), IsAllDayLike(items[b])
		if ia != ib {
			return ia
		}
		return items[a].Start.Before(items[b].Start)
	})
	return items
}

// TimedItems returns non-all-day occurrences overlapping day d.
func TimedItems(occs []model.Occurrence, d time.Time) []model.Occurrence {
	next := d.AddDate(0, 0, 1)
	var items []model.Occurrence
	for _, o := range occs {
		if !IsAllDayLike(o) && o.Start.Before(next) && (o.End.After(d) || (o.End.Equal(o.Start) && !o.Start.Before(d))) {
			items = append(items, o)
		}
	}
	return items
}

// EventTitle returns a displayable title.
func EventTitle(e *model.Event) string {
	if strings.TrimSpace(e.Title) == "" {
		return "(No title)"
	}
	return e.Title
}

var RepeatPresets = []struct{ Label, Rule string }{
	{"Does not repeat", ""},
	{"Daily", "FREQ=DAILY"},
	{"Every weekday (Mon–Fri)", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
	{"Weekly", "FREQ=WEEKLY"},
	{"Every 2 weeks", "FREQ=WEEKLY;INTERVAL=2"},
	{"Monthly", "FREQ=MONTHLY"},
	{"Yearly", "FREQ=YEARLY"},
}

// RepeatChoices lists repeat labels for an event, adding a "Custom" entry for rules
// that match no preset. It returns the labels, the selected index and the custom rule.
func RepeatChoices(rule string) (labels []string, idx int, custom string) {
	idx = -1
	for i, p := range RepeatPresets {
		labels = append(labels, p.Label)
		if strings.EqualFold(p.Rule, rule) {
			idx = i
		}
	}
	if idx < 0 {
		custom = rule
		labels = append(labels, "Custom: "+rule)
		idx = len(labels) - 1
	}
	return labels, idx, custom
}

// RuleForChoice maps a repeat choice index back to an RRULE.
func RuleForChoice(idx int, custom string) string {
	if idx >= 0 && idx < len(RepeatPresets) {
		return RepeatPresets[idx].Rule
	}
	return custom
}

// TimeChoices are half-hour clock values for time pickers.
func TimeChoices() []string {
	var res []string
	for m := 0; m < 24*60; m += 30 {
		res = append(res, fmt.Sprintf("%02d:%02d", m/60, m%60))
	}
	return res
}

var SnoozeChoices = []int{1, 5, 10, 15, 30, 60}

// SnoozeLabels returns snooze menu labels and the index of the default value.
func SnoozeLabels(def int) ([]string, int) {
	choices := SnoozeChoices
	if !ContainsInt(choices, def) {
		choices = append([]int{def}, choices...)
	}
	var labels []string
	cur := 0
	for i, m := range choices {
		labels = append(labels, fmt.Sprintf("%d min", m))
		if m == def {
			cur = i
		}
	}
	return labels, cur
}

// ParseSnoozeLabel reads the minutes from a "10 min" label.
func ParseSnoozeLabel(s string, def int) int {
	if f := strings.Fields(s); len(f) > 0 {
		if v, err := strconv.Atoi(f[0]); err == nil && v > 0 {
			return v
		}
	}
	return def
}

var SyncIntervals = []struct {
	Label string
	Mins  int
}{{"Manual only", 0}, {"Every 5 minutes", 5}, {"Every 15 minutes", 15}, {"Every 30 minutes", 30}, {"Every hour", 60}}

// SourceStatus is the human-readable sync state of a calendar source.
func SourceStatus(s *model.CalendarSource) string {
	switch {
	case !s.Enabled:
		return "Disabled"
	case s.IsLocal():
		return "—"
	case s.LastError != "":
		return "Error: " + s.LastError
	case s.LastSync.IsZero():
		return "Not synced yet"
	default:
		return "Synced " + s.LastSync.Format("02.01 15:04")
	}
}
