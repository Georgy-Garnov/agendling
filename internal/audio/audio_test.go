package audio

import (
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

func TestAlertStreamDuration(t *testing.T) {
	const rate beep.SampleRate = 1000
	for _, tc := range []struct {
		name   string
		length int
		loop   bool
		limit  time.Duration
		want   int
	}{
		{"repeated short sound", 47, true, 15 * time.Second, 15000},
		{"long sound played once", 20000, false, 15 * time.Second, 15000},
		{"short sound played once", 47, false, 15 * time.Second, 47},
		{"repeated for three minutes", 47, true, 3 * time.Minute, 180000},
		{"long sound played once without a limit", 20000, false, 0, 20000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := alertStream(&chime{length: tc.length}, rate, tc.loop, tc.limit)
			var buf [256][2]float64
			played := 0
			for {
				n, ok := s.Stream(buf[:])
				played += n
				if played > tc.want {
					t.Fatalf("stream exceeded %d frames", tc.want)
				}
				if !ok {
					break
				}
				if n == 0 {
					t.Fatal("stream stopped making progress")
				}
			}
			if played != tc.want {
				t.Fatalf("played %d frames, want %d", played, tc.want)
			}
		})
	}
}

func TestAlertStreamRepeatsUntilStopped(t *testing.T) {
	s := alertStream(&chime{length: 47}, 1000, true, 0)
	var buf [256][2]float64
	for played := 0; played < 3600*1000; { // an hour of sound and still going
		n, ok := s.Stream(buf[:])
		if !ok || n == 0 {
			t.Fatalf("stopped after %d frames", played)
		}
		played += n
	}
}
