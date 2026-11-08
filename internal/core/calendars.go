package core

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// CalendarChoice is one calendar of a CalDAV account in the account editor.
type CalendarChoice struct {
	caldav.CalendarInfo
	Shown bool
}

// CalendarChoices pairs the calendars of an account with the user's selection: everything not
// listed in hidden is shown, so calendars that appeared on the server are shown by default.
func CalendarChoices(cals []caldav.CalendarInfo, hidden []string) []CalendarChoice {
	h := map[string]bool{}
	for _, p := range hidden {
		h[p] = true
	}
	res := make([]CalendarChoice, len(cals))
	for i, c := range cals {
		res[i] = CalendarChoice{CalendarInfo: c, Shown: !h[c.Path]}
	}
	return res
}

// HiddenCalendars returns the paths to store in CalendarSource.HiddenCollections, and whether at
// least one calendar stays shown.
func HiddenCalendars(choices []CalendarChoice) (hidden []string, anyShown bool) {
	for _, c := range choices {
		if c.Shown {
			anyShown = true
		} else {
			hidden = append(hidden, c.Path)
		}
	}
	return hidden, anyShown
}

// Calendar is one calendar as the user sees it: a local calendar, or one calendar (collection)
// of a CalDAV account.
type Calendar struct {
	Source     *model.CalendarSource
	Collection string // CalDAV collection path; "" for local calendars and accounts not synced yet
	Name       string // the calendar's own name, or the account name
	Account    string // the account name when the account has several calendars, else ""
	Key        string // identifies the calendar in AppSettings.HiddenCalendars
	Shown      bool   // ticked in the main window's calendar list
}

// Label is the calendar's name for pickers.
func (c Calendar) Label() string {
	switch {
	case c.Account != "":
		return c.Account + " / " + c.Name
	case c.Source.IsLocal():
		return c.Name + "  (local)"
	}
	return c.Name
}

func sourceKey(id int64) string { return "c" + strconv.FormatInt(id, 10) }

func collectionKey(id int64, coll string) string { return sourceKey(id) + ":" + coll }

// eventKey is the Key of the calendar an event belongs to; a CalDAV event not stored on the
// server yet belongs to its account's write calendar.
func eventKey(e *model.Event, src *model.CalendarSource) string {
	if src == nil || src.IsLocal() {
		return sourceKey(e.CalendarID)
	}
	coll := ""
	switch {
	case e.Href != "":
		coll = caldav.CollectionOf(e.Href)
	case src.CalendarPath != "":
		coll = caldav.NormalizeCollection(src.CalendarPath)
	default:
		return sourceKey(e.CalendarID)
	}
	return collectionKey(e.CalendarID, coll)
}

// calendarsOf lists the calendars of one source, skipping those unsubscribed in its settings.
func (s *Service) calendarsOf(src *model.CalendarSource, hidden map[string]bool) []Calendar {
	one := func(coll, key string) []Calendar {
		return []Calendar{{Source: src, Collection: coll, Name: src.Name, Key: key,
			Shown: !hidden[key] && !hidden[sourceKey(src.ID)]}}
	}
	if src.IsLocal() {
		return one("", sourceKey(src.ID))
	}
	unsubscribed := map[string]bool{}
	for _, p := range src.HiddenCollections {
		unsubscribed[caldav.NormalizeCollection(p)] = true
	}
	var infos []caldav.CalendarInfo
	for _, c := range caldav.KnownCalendars(s.Store, src.ID) {
		if !unsubscribed[c.Path] {
			infos = append(infos, c)
		}
	}
	switch len(infos) {
	case 0:
		return one("", sourceKey(src.ID))
	case 1:
		return one(infos[0].Path, collectionKey(src.ID, infos[0].Path))
	}
	res := make([]Calendar, len(infos))
	for i, c := range infos {
		key := collectionKey(src.ID, c.Path)
		res[i] = Calendar{Source: src, Collection: c.Path, Name: c.Name, Account: src.Name, Key: key,
			Shown: !hidden[key] && !hidden[sourceKey(src.ID)]}
	}
	return res
}

func (s *Service) hiddenCalendars() map[string]bool {
	h := map[string]bool{}
	for _, k := range s.Store.Settings().HiddenCalendars {
		h[k] = true
	}
	return h
}

// Calendars lists the calendars of all enabled sources, for the main window's calendar list.
func (s *Service) Calendars() []Calendar {
	srcs, _ := s.Store.Sources()
	hidden := s.hiddenCalendars()
	var res []Calendar
	for _, src := range srcs {
		if src.Enabled {
			res = append(res, s.calendarsOf(src, hidden)...)
		}
	}
	return res
}

// SetCalendarShown ticks or unticks a calendar in the main window's calendar list.
func (s *Service) SetCalendarShown(key string, shown bool) error {
	st := s.Store.Settings()
	hidden := map[string]bool{}
	for _, k := range st.HiddenCalendars {
		hidden[k] = true
	}
	if shown {
		delete(hidden, key)
		// An account hidden as a whole before its calendars were known: keep the others hidden.
		for _, c := range s.Calendars() {
			if hidden[sourceKey(c.Source.ID)] && strings.HasPrefix(key, sourceKey(c.Source.ID)+":") {
				delete(hidden, sourceKey(c.Source.ID))
				if c.Key != key {
					hidden[c.Key] = true
				}
			}
		}
	} else {
		hidden[key] = true
	}
	st.HiddenCalendars = st.HiddenCalendars[:0]
	for k := range hidden {
		st.HiddenCalendars = append(st.HiddenCalendars, k)
	}
	sort.Strings(st.HiddenCalendars)
	return s.Store.SaveSettings(st)
}

// ViewEvents returns the events for [from, to) of enabled sources whose calendars are ticked in
// the main window's calendar list.
func (s *Service) ViewEvents(from, to time.Time) ([]*model.Event, error) {
	events, err := s.Store.EventsInRange(from, to)
	if err != nil {
		return nil, err
	}
	hidden := s.hiddenCalendars()
	if len(hidden) == 0 {
		return events, nil
	}
	srcs, _ := s.Store.Sources()
	byID := map[int64]*model.CalendarSource{}
	for _, src := range srcs {
		byID[src.ID] = src
	}
	shown := events[:0]
	for _, e := range events {
		if !hidden[sourceKey(e.CalendarID)] && !hidden[eventKey(e, byID[e.CalendarID])] {
			shown = append(shown, e)
		}
	}
	return shown, nil
}

// EditableCalendars lists the calendars an event can be saved to (those of enabled sources,
// plus the event's current one) with the index of the event's calendar. A new event defaults to
// the first calendar ticked in the main window.
func (s *Service) EditableCalendars(e *model.Event) ([]Calendar, int) {
	srcs, _ := s.Store.Sources()
	hidden := s.hiddenCalendars()
	var cals []Calendar
	idx, current := -1, ""
	for _, src := range srcs {
		if src.ID == e.CalendarID {
			current = eventKey(e, src)
		}
		if src.Enabled || src.ID == e.CalendarID {
			cals = append(cals, s.calendarsOf(src, hidden)...)
		}
	}
	if e.ID != 0 {
		for i, c := range cals {
			if c.Key == current {
				idx = i
				break
			}
		}
		for i, c := range cals { // its calendar is not listed (yet): the account's first one
			if idx < 0 && c.Source.ID == e.CalendarID {
				idx = i
			}
		}
	}
	if idx < 0 {
		idx = 0
		for i, c := range cals {
			if c.Shown {
				idx = i
				break
			}
		}
	}
	return cals, idx
}

// CalendarListSignature changes whenever the main window's calendar list needs rebuilding
// (calendars added, removed, renamed or recoloured), but not when only ticks change.
func CalendarListSignature(cals []Calendar) string {
	var b strings.Builder
	for _, c := range cals {
		b.WriteString(c.Key + "\x00" + c.Account + "\x00" + c.Name + "\x00" + c.Source.Color + "\x01")
	}
	return b.String()
}

// CalendarLabel names the calendar an event belongs to, e.g. "iCloud / Family" for a calendar of
// an account with several, or the account / local calendar name.
func (s *Service) CalendarLabel(e *model.Event) string {
	src, err := s.Store.Source(e.CalendarID)
	if err != nil {
		return ""
	}
	key := eventKey(e, src)
	for _, c := range s.calendarsOf(src, nil) {
		if c.Key == key && c.Account != "" {
			return c.Account + " / " + c.Name
		}
	}
	return src.Name
}
