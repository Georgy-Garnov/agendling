package core

import (
	"github.com/Georgy-Garnov/agendling/internal/storage"
	"path/filepath"
	"testing"
)

func TestCalendarZoomSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Older settings have no zoom field and must keep the original scale.
	if got := CalendarZoom(store.Settings().CalendarZoomPercent); got != 100 {
		t.Fatalf("default zoom = %d", got)
	}
	service := &Service{Store: store}
	for _, tc := range []struct{ input, want int }{
		{120, 120}, {160, 160}, {1000, 160}, {1, 80}, {0, 100},
	} {
		if err := service.SaveCalendarZoom(tc.input); err != nil {
			t.Fatal(err)
		}
		reopened, err := storage.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got := reopened.Settings().CalendarZoomPercent
		reopened.Close()
		if got != tc.want {
			t.Fatalf("saved zoom for %d = %d, want %d", tc.input, got, tc.want)
		}
	}
}
