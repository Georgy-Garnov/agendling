package core

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/audio"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/recur"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// MaxNewEventPopups caps popups for a burst of new events (e.g. a shared calendar import).
const MaxNewEventPopups = 3

// Service wires storage, sync, reminders and audio together. UI callbacks are invoked
// on background goroutines; front ends must marshal them onto their UI thread.
type Service struct {
	Store  *storage.Store
	Engine *caldav.Engine
	Sched  *scheduler.Scheduler
	Audio  *audio.Manager
}

func NewService(store *storage.Store, onSync func(caldav.Status), onReminder func(scheduler.Alert)) *Service {
	return &Service{
		Store:  store,
		Engine: caldav.NewEngine(store, onSync),
		Sched:  scheduler.New(store, onReminder),
		Audio:  audio.NewManager(),
	}
}

// Start launches background sync and reminder scheduling.
func (s *Service) Start() {
	s.Engine.Restart()
	s.Sched.Start()
}

func (s *Service) Stop() {
	s.Audio.Stop()
	s.Engine.Stop()
	s.Sched.Stop()
}

// EnsureDefaultCalendar creates a local "Personal" calendar on first run.
func (s *Service) EnsureDefaultCalendar() {
	srcs, err := s.Store.Sources()
	if err != nil || len(srcs) > 0 {
		return
	}
	_ = s.Store.SaveSource(&model.CalendarSource{Name: "Personal", Type: model.SourceLocal, Color: "#4285f4", Enabled: true})
}

// PlayAlertSound plays the configured sound (falling back to the built-in chime) unless muted.
func (s *Service) PlayAlertSound() {
	st := s.Store.Settings()
	if st.Muted {
		return
	}
	go func() {
		if err := s.Audio.Play(st.CustomSoundPath, st.LoopSound); err != nil {
			log.Printf("play %q: %v; falling back to built-in chime", st.CustomSoundPath, err)
			_ = s.Audio.Play("", st.LoopSound)
		}
	}()
}

// ToggleMute flips the mute setting and returns the new state.
func (s *Service) ToggleMute() bool {
	st := s.Store.Settings()
	st.Muted = !st.Muted
	if err := s.Store.SaveSettings(st); err != nil {
		log.Printf("save settings: %v", err)
	}
	if st.Muted {
		s.Audio.Stop()
	}
	return st.Muted
}

// NewEventAlerts turns events reported as new by a sync into popup alerts
// (at most MaxNewEventPopups) and returns how many more were left out.
func (s *Service) NewEventAlerts(sourceID int64, events []*model.Event) (alerts []scheduler.Alert, rest int) {
	if len(events) == 0 || s.Store.Settings().NewEventAlertsOff {
		return nil, 0
	}
	cal, _ := s.Store.Source(sourceID)
	now := time.Now()
	for _, e := range events {
		if len(alerts) == MaxNewEventPopups {
			break
		}
		occ := recur.Expand([]*model.Event{e}, now, now.AddDate(2, 0, 0))
		if len(occ) == 0 {
			continue
		}
		alerts = append(alerts, scheduler.Alert{Event: e, Calendar: cal, Start: occ[0].Start, End: occ[0].End})
	}
	return alerts, len(events) - len(alerts)
}

// EditorInput is what an event editor form collects, independent of the toolkit.
type EditorInput struct {
	Title, Description, Location, URL string
	Calendar                          *model.CalendarSource
	AllDay                            bool
	StartDate, EndDate                time.Time // dates; EndDate is inclusive for all-day events
	StartClock, EndClock              string    // "HH:MM", ignored for all-day events
	Reminders                         string    // "5, 15, 1h"; empty = defaults
	Rule                              string    // RRULE ("" = no repeat)
}

// ApplyEdit validates the form, writes it into e and persists it.
func (s *Service) ApplyEdit(e *model.Event, in EditorInput) error {
	if in.Calendar == nil {
		return errors.New("choose a calendar")
	}
	var start, end time.Time
	if in.AllDay {
		start = DayStart(in.StartDate)
		end = DayStart(in.EndDate).AddDate(0, 0, 1)
	} else {
		var err error
		if start, err = Combine(in.StartDate, in.StartClock); err != nil {
			return err
		}
		if end, err = Combine(in.EndDate, in.EndClock); err != nil {
			return err
		}
	}
	if end.Before(start) {
		return errors.New("the event ends before it starts")
	}
	mins, err := ParseReminders(in.Reminders)
	if err != nil {
		return err
	}
	var reminders []model.ReminderOffset
	for _, m := range mins {
		reminders = append(reminders, model.MinutesOffset(m))
	}

	updated := *e
	updated.Title = strings.TrimSpace(in.Title)
	updated.Description = strings.ReplaceAll(in.Description, "\r\n", "\n")
	updated.Location = strings.TrimSpace(in.Location)
	updated.URL = strings.TrimSpace(in.URL)
	updated.StartTime, updated.EndTime, updated.AllDay = start, end, in.AllDay
	updated.Reminders = reminders
	if updated.RecurrenceID.IsZero() {
		if in.Rule != e.RRule {
			updated.ExDates = nil // exclusions of the old rule don't apply to a new one
		}
		updated.RRule = in.Rule
	}
	if updated.UID == "" {
		updated.UID = ics.NewUID()
	}
	if updated.TZID == "" && !in.AllDay {
		updated.TZID = LocalTZID
	}
	if err := s.SaveEvent(&updated, e.CalendarID, in.Calendar); err != nil {
		return err
	}
	*e = updated
	return nil
}

// SaveEvent persists an event, moving it between calendars if needed, and queues a push.
func (s *Service) SaveEvent(e *model.Event, oldCalID int64, cal *model.CalendarSource) error {
	if e.ID != 0 && oldCalID != cal.ID {
		series, err := s.Store.EventsByUID(oldCalID, e.UID)
		if err != nil {
			return err
		}
		if err := s.RemoveSeries(oldCalID, e.UID); err != nil {
			return err
		}
		// Recreate the whole series (with the edited row) in the target calendar.
		for _, ev := range series {
			if ev.Deleted {
				continue
			}
			cp := *ev
			if ev.ID == e.ID {
				cp = *e
			}
			cp.ID, cp.CalendarID, cp.Href, cp.ETag = 0, cal.ID, "", ""
			cp.Dirty = !cal.IsLocal()
			if err := s.Store.SaveEvent(&cp); err != nil {
				return err
			}
			if ev.ID == e.ID {
				*e = cp
			}
		}
	} else {
		e.CalendarID = cal.ID
		e.Dirty = !cal.IsLocal()
		if err := s.Store.SaveEvent(e); err != nil {
			return err
		}
	}
	if !cal.IsLocal() {
		s.Engine.SyncSource(cal.ID)
	}
	return nil
}

// RemoveSeries deletes all rows of a UID: immediately for local calendars, or marks them
// for remote deletion on the next sync.
func (s *Service) RemoveSeries(calID int64, uid string) error {
	cal, err := s.Store.Source(calID)
	if err != nil {
		return err
	}
	if cal.IsLocal() {
		return s.Store.PurgeUID(calID, uid)
	}
	series, err := s.Store.EventsByUID(calID, uid)
	if err != nil {
		return err
	}
	for _, ev := range series {
		ev.Deleted = true
		if err := s.Store.SaveEvent(ev); err != nil {
			return err
		}
	}
	s.Engine.SyncSource(calID)
	return nil
}

// DeleteOccurrence excludes one occurrence from its series via EXDATE.
func (s *Service) DeleteOccurrence(e *model.Event, occStart time.Time) error {
	series, err := s.Store.EventsByUID(e.CalendarID, e.UID)
	if err != nil {
		return err
	}
	cal, err := s.Store.Source(e.CalendarID)
	if err != nil {
		return err
	}
	var master *model.Event
	for _, ev := range series {
		if ev.RecurrenceID.IsZero() {
			master = ev
		}
	}
	if master == nil {
		return s.RemoveSeries(e.CalendarID, e.UID)
	}
	exdate := occStart
	if !e.RecurrenceID.IsZero() {
		exdate = e.RecurrenceID
		// Drop the override itself.
		if cal.IsLocal() {
			if err := s.Store.PurgeEvent(e.ID); err != nil {
				return err
			}
		} else {
			e.Deleted = true
			if err := s.Store.SaveEvent(e); err != nil {
				return err
			}
		}
	}
	if exdate.IsZero() {
		exdate = master.StartTime
	}
	master.ExDates = append(master.ExDates, exdate)
	master.Dirty = !cal.IsLocal()
	if err := s.Store.SaveEvent(master); err != nil {
		return err
	}
	if !cal.IsLocal() {
		s.Engine.SyncSource(cal.ID)
	}
	return nil
}

// EditableCalendars lists enabled calendars (plus the event's current one) for a picker,
// with the index of currentID.
func (s *Service) EditableCalendars(currentID int64) ([]*model.CalendarSource, int) {
	srcs, _ := s.Store.Sources()
	var cals []*model.CalendarSource
	idx := 0
	for _, src := range srcs {
		if !src.Enabled && src.ID != currentID {
			continue
		}
		if src.ID == currentID {
			idx = len(cals)
		}
		cals = append(cals, src)
	}
	return cals, idx
}
