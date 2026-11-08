// Package model holds the core data types shared by storage, sync, scheduler and UI.
package model

import (
	"sync"
	"time"
)

type SourceType string

const (
	SourceLocal  SourceType = "local"
	SourceCalDAV SourceType = "caldav"
)

// CalendarSource is either a local-only calendar or a remote CalDAV account/collection.
type CalendarSource struct {
	ID           int64
	Name         string
	Type         SourceType
	URL          string // server root or direct calendar collection URL
	Username     string
	Password     string
	Color        string // "#RRGGBB"
	SyncInterval int    // minutes; 0 = manual only
	Enabled      bool

	// CalendarPath is the discovered collection path used for writes (CalDAV only).
	CalendarPath string
	// HiddenCollections lists collection paths of the account the user chose not to show; they
	// are not synchronized either. Calendars added on the server later are shown by default.
	HiddenCollections []string
	LastSync          time.Time
	LastError         string
}

func (s *CalendarSource) IsLocal() bool { return s.Type == SourceLocal }

// ReminderOffset is how long before the event start a reminder fires.
type ReminderOffset time.Duration

func (r ReminderOffset) Minutes() int { return int(time.Duration(r) / time.Minute) }

func MinutesOffset(m int) ReminderOffset { return ReminderOffset(time.Duration(m) * time.Minute) }

type Event struct {
	ID          int64
	CalendarID  int64
	UID         string // iCalendar UID
	Title       string
	Description string
	Location    string
	URL         string
	StartTime   time.Time
	EndTime     time.Time
	AllDay      bool
	RRule       string      // RRULE value without the "RRULE:" prefix
	TZID        string      // IANA zone the event is defined in ("" = local, "UTC"); recurrences repeat in it
	ExDates     []time.Time // excluded occurrence starts
	Reminders   []ReminderOffset
	IsLocal     bool

	// RecurrenceID is set for overridden occurrences of a recurring series.
	RecurrenceID time.Time

	// CalDAV bookkeeping.
	Href    string
	ETag    string
	Dirty   bool // local change not yet pushed
	Deleted bool // deleted locally, pending remote DELETE
	// MovedFrom is the previous Href of an event moved to another calendar of the same account;
	// the old copy is deleted from the server once the event is stored at Href.
	MovedFrom string
}

func (e *Event) Duration() time.Duration { return e.EndTime.Sub(e.StartTime) }

var (
	zoneMu    sync.Mutex
	zoneCache = map[string]*time.Location{}
)

// Zone returns the location recurrences of this event are computed in.
// All-day and zone-less events use the local zone.
func (e *Event) Zone() *time.Location {
	if e.AllDay || e.TZID == "" {
		return time.Local
	}
	zoneMu.Lock()
	defer zoneMu.Unlock()
	if loc, ok := zoneCache[e.TZID]; ok {
		return loc
	}
	loc, err := time.LoadLocation(e.TZID)
	if err != nil {
		loc = time.Local
	}
	zoneCache[e.TZID] = loc
	return loc
}

// Occurrence is one concrete instance of an event within a queried range.
type Occurrence struct {
	Event *Event
	Start time.Time
	End   time.Time
}

type AppSettings struct {
	CalendarZoomPercent    int
	DefaultReminderMinutes []int
	CustomSoundPath        string
	SnoozeIntervalMinutes  int
	RunAtStartup           bool
	StartMinimized         bool
	LoopSound              bool
	// SoundLimitSeconds is how long an alert sound may play, repeated or not; 0 = until stopped.
	SoundLimitSeconds int
	// TypeSounds replace CustomSoundPath for reminders or for new meetings; CalendarSounds replace
	// them for an account or a local calendar ("c<id>") or one calendar of an account
	// ("c<id>:<collection path>"), see core.AlertSound.
	TypeSounds     AlertSounds
	CalendarSounds map[string]AlertSounds `json:",omitempty"`
	Muted          bool
	// NewEventAlertsOff disables the sound + popup for events that appear after a sync.
	NewEventAlertsOff bool
	// CloseWithoutTray is what closing the window does when there is no system tray (Linux):
	// "" asks, CloseMinimize keeps running minimized, CloseQuit exits.
	CloseWithoutTray string
	// HiddenCalendars holds the keys (core.Calendar.Key) of calendars unticked in the main
	// window's calendar list; their events are not drawn but still synced and reminded of.
	HiddenCalendars []string
}

// Sound is an optional replacement of the main alert sound, used while Custom is on. Path keeps
// the chosen file while it is off; an empty Path plays the built-in chime.
type Sound struct {
	Custom bool   `json:",omitempty"`
	Path   string `json:",omitempty"`
}

// AlertSounds are the sounds for the two kinds of alerts: a reminder before an event and a new
// meeting that appeared after a sync.
type AlertSounds struct {
	Reminder Sound
	NewEvent Sound
}

// Values of AppSettings.CloseWithoutTray.
const (
	CloseMinimize = "minimize"
	CloseQuit     = "quit"
)

func DefaultSettings() AppSettings {
	return AppSettings{
		DefaultReminderMinutes: []int{15},
		SnoozeIntervalMinutes:  5,
		LoopSound:              true,
		SoundLimitSeconds:      180,
	}
}
