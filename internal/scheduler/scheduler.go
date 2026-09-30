// Package scheduler fires reminder alerts. It keeps a single timer armed for the next
// due reminder instead of polling, so it costs no CPU between reminders.
package scheduler

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/recur"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

const (
	lookahead = 8 * 24 * time.Hour // covers "1 week before" reminders
	// Upper bound on a single sleep; guards against clock changes and system sleep,
	// during which Go's monotonic timers may not advance.
	maxSleep = 10 * time.Minute
)

type Alert struct {
	Event     *model.Event
	Calendar  *model.CalendarSource
	Start     time.Time
	End       time.Time
	OffsetMin int
	Snoozed   bool
}

type pending struct {
	at    time.Time
	alert Alert
	key   firedKey
}

type firedKey struct {
	uid    string
	start  int64
	offset int
}

type Scheduler struct {
	store  *storage.Store
	onFire func(Alert)

	mu      sync.Mutex
	timer   *time.Timer
	snoozed []pending
	stopped bool
}

func New(store *storage.Store, onFire func(Alert)) *Scheduler {
	return &Scheduler{store: store, onFire: onFire}
}

func (s *Scheduler) Start() {
	_ = s.store.PruneFired(time.Now().Add(-30 * 24 * time.Hour))
	s.Reschedule()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
	}
}

// Snooze re-shows the alert after d.
func (s *Scheduler) Snooze(a Alert, d time.Duration) {
	a.Snoozed = true
	s.mu.Lock()
	s.snoozed = append(s.snoozed, pending{at: time.Now().Add(d), alert: a})
	s.mu.Unlock()
	s.Reschedule()
}

// Reschedule fires everything that is due and re-arms the timer for the next reminder.
// Safe to call from any goroutine, e.g. whenever events or settings change.
func (s *Scheduler) Reschedule() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	now := time.Now()
	due, next := s.collect(now)

	var dueSnoozes []Alert
	kept := s.snoozed[:0]
	for _, p := range s.snoozed {
		if !p.at.After(now) {
			dueSnoozes = append(dueSnoozes, p.alert)
		} else {
			kept = append(kept, p)
			if next.IsZero() || p.at.Before(next) {
				next = p.at
			}
		}
	}
	s.snoozed = kept

	wait := maxSleep
	if !next.IsZero() && next.Sub(now) < wait {
		wait = next.Sub(now)
	}
	if wait < time.Second {
		wait = time.Second
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(wait, s.Reschedule)
	} else {
		s.timer.Reset(wait)
	}
	s.mu.Unlock()

	for _, p := range due {
		if err := s.store.MarkFired(p.key.uid, time.Unix(p.key.start, 0), p.key.offset); err != nil {
			log.Printf("scheduler: mark fired: %v", err)
		}
		s.onFire(p.alert)
	}
	for _, a := range dueSnoozes {
		s.onFire(a)
	}
}

// collect returns reminders due now and the time of the next future reminder.
func (s *Scheduler) collect(now time.Time) (due []pending, next time.Time) {
	events, err := s.store.EventsInRange(now.Add(-24*time.Hour), now.Add(lookahead))
	if err != nil {
		log.Printf("scheduler: load events: %v", err)
		return nil, time.Time{}
	}
	sources, _ := s.store.Sources()
	cals := map[int64]*model.CalendarSource{}
	for _, src := range sources {
		cals[src.ID] = src
	}
	defaults := s.store.Settings().DefaultReminderMinutes

	for _, occ := range recur.Expand(events, now.Add(-24*time.Hour), now.Add(lookahead)) {
		offsets := reminderMinutes(occ.Event, defaults)
		for _, off := range offsets {
			at := occ.Start.Add(-time.Duration(off) * time.Minute)
			key := firedKey{uid: occ.Event.UID, start: occ.Start.Unix(), offset: off}
			if at.After(now) {
				if next.IsZero() || at.Before(next) {
					next = at
				}
				continue
			}
			// Due or missed (e.g. app was closed): fire only while the event is still relevant.
			end := occ.End
			if !end.After(occ.Start) {
				end = occ.Start.Add(time.Minute)
			}
			if !end.After(now) || s.store.WasFired(key.uid, occ.Start, off) {
				continue
			}
			due = append(due, pending{at: at, key: key, alert: Alert{
				Event: occ.Event, Calendar: cals[occ.Event.CalendarID],
				Start: occ.Start, End: occ.End, OffsetMin: off,
			}})
		}
	}
	// Several offsets of one occurrence may be overdue at once: show only the latest.
	sort.Slice(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	seen := map[[2]any]int{}
	var dedup []pending
	for _, p := range due {
		k := [2]any{p.key.uid, p.key.start}
		if i, ok := seen[k]; ok {
			_ = s.store.MarkFired(dedup[i].key.uid, time.Unix(dedup[i].key.start, 0), dedup[i].key.offset)
			dedup[i] = p
			continue
		}
		seen[k] = len(dedup)
		dedup = append(dedup, p)
	}
	return dedup, next
}

func reminderMinutes(e *model.Event, defaults []int) []int {
	if len(e.Reminders) == 0 {
		return defaults
	}
	res := make([]int, 0, len(e.Reminders))
	for _, r := range e.Reminders {
		res = append(res, r.Minutes())
	}
	return res
}
