package audio

import (
	"testing"

	"github.com/gopxl/beep/v2"
)

func TestAlertStreamDuration(t *testing.T) {
	const rate beep.SampleRate = 1000
	for _, tc := range []struct {
		name   string
		length int
		loop   bool
		want   int
	}{
		{"repeated short sound", 47, true, 15000},
		{"long sound played once", 20000, false, 15000},
		{"short sound played once", 47, false, 47},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := alertStream(&chime{length: tc.length}, rate, tc.loop)
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
