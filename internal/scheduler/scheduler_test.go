package scheduler

import (
	"sync"
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

func TestScheduler(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/cal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	src := &model.CalendarSource{Name: "Local", Type: model.SourceLocal, Enabled: true}
	store.SaveSource(src)

	now := time.Now()
	// Due now: 10-minute reminder for an event starting in 5 minutes (15m + 10m offsets overdue → fire once).
	store.SaveEvent(&model.Event{CalendarID: src.ID, UID: "due", Title: "Due", StartTime: now.Add(5 * time.Minute), EndTime: now.Add(time.Hour),
		Reminders: []model.ReminderOffset{model.MinutesOffset(10), model.MinutesOffset(15)}})
	// Future: not yet.
	store.SaveEvent(&model.Event{CalendarID: src.ID, UID: "later", Title: "Later", StartTime: now.Add(3 * time.Hour), EndTime: now.Add(4 * time.Hour)})
	// Already over: never fires.
	store.SaveEvent(&model.Event{CalendarID: src.ID, UID: "past", Title: "Past", StartTime: now.Add(-2 * time.Hour), EndTime: now.Add(-time.Hour)})

	var mu sync.Mutex
	var fired []Alert
	s := New(store, func(a Alert) { mu.Lock(); fired = append(fired, a); mu.Unlock() })
	s.Start()
	defer s.Stop()

	mu.Lock()
	if len(fired) != 1 || fired[0].Event.UID != "due" || fired[0].OffsetMin != 10 {
		t.Fatalf("fired: %+v", fired)
	}
	mu.Unlock()

	// A restart must not re-fire the same reminder.
	s2 := New(store, func(a Alert) { t.Errorf("re-fired %s", a.Event.UID) })
	s2.Start()
	s2.Stop()

	// Snooze re-delivers after the delay.
	s.Snooze(fired[0], 1100*time.Millisecond)
	time.Sleep(2500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 2 || !fired[1].Snoozed {
		t.Fatalf("after snooze: %d alerts", len(fired))
	}
}
