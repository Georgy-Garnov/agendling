package core

import (
	"fmt"
	"maps"
	"strings"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

// AlertSound is the sound file for an alert about event e of source src ("" = built-in chime):
// the sound of the event's calendar, then of its account, then of the kind of alert (reminder or
// new meeting), then the main alert sound. Whatever is not switched on falls through, so alerts
// keep the main sound until the user sets something else.
func AlertSound(st model.AppSettings, e *model.Event, src *model.CalendarSource, isNew bool) string {
	pick := func(s model.AlertSounds) model.Sound {
		if isNew {
			return s.NewEvent
		}
		return s.Reminder
	}
	if e != nil && src != nil {
		for _, key := range []string{eventKey(e, src), sourceKey(src.ID)} {
			if snd := pick(st.CalendarSounds[key]); snd.Custom {
				return snd.Path
			}
		}
	}
	if snd := pick(st.TypeSounds); snd.Custom {
		return snd.Path
	}
	return st.CustomSoundPath
}

// SoundTarget is a card under "By calendar" in Settings → Sound: an account or local calendar,
// followed by the calendars of an account that has several.
type SoundTarget struct {
	Key    string // the key in AppSettings.CalendarSounds
	Source *model.CalendarSource
	Name   string
	Detail string // what it is: "Local calendar", "CalDAV account · 3 calendars", or the account
	Child  bool   // a calendar of the account listed above it
}

// SoundTargets lists every source and, for accounts with several calendars, each calendar.
// An account with one calendar is a single card: its sounds then also apply to calendars
// added on the server later.
func (s *Service) SoundTargets() []SoundTarget {
	srcs, _ := s.Store.Sources()
	var res []SoundTarget
	for _, src := range srcs {
		cals := s.calendarsOf(src, nil)
		t := SoundTarget{Key: sourceKey(src.ID), Source: src, Name: src.Name, Detail: "CalDAV account"}
		switch {
		case src.IsLocal():
			t.Detail = "Local calendar"
		case len(cals) > 1:
			t.Detail = fmt.Sprintf("CalDAV account · %d calendars", len(cals))
		}
		if !src.Enabled {
			t.Detail += " · disabled"
		}
		res = append(res, t)
		if len(cals) > 1 {
			for _, c := range cals {
				res = append(res, SoundTarget{Key: c.Key, Source: src, Name: c.Name, Detail: src.Name, Child: true})
			}
		}
	}
	return res
}

// CleanSound trims the path of a sound chosen in Settings.
func CleanSound(snd model.Sound) model.Sound {
	snd.Path = strings.TrimSpace(snd.Path)
	return snd
}

// CleanCalendarSounds drops entries that neither use a sound nor remember a file; nil if none
// is left.
func CleanCalendarSounds(m map[string]model.AlertSounds) map[string]model.AlertSounds {
	var res map[string]model.AlertSounds
	for k, v := range m {
		v.Reminder, v.NewEvent = CleanSound(v.Reminder), CleanSound(v.NewEvent)
		if v == (model.AlertSounds{}) {
			continue
		}
		if res == nil {
			res = map[string]model.AlertSounds{}
		}
		res[k] = v
	}
	return res
}

func sameCalendarSounds(a, b map[string]model.AlertSounds) bool {
	return maps.Equal(CleanCalendarSounds(a), CleanCalendarSounds(b))
}
