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
	LastSync     time.Time
	LastError    string
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
	DefaultReminderMinutes []int
	CustomSoundPath        string
	SnoozeIntervalMinutes  int
	RunAtStartup           bool
	StartMinimized         bool
	LoopSound              bool
	Muted                  bool
	// NewEventAlertsOff disables the sound + popup for events that appear after a sync.
	NewEventAlertsOff bool
}

func DefaultSettings() AppSettings {
	return AppSettings{
		DefaultReminderMinutes: []int{15},
		SnoozeIntervalMinutes:  5,
		LoopSound:              true,
	}
}
