package core

import (
	"math"
	"testing"
)

func TestNextZoom(t *testing.T) {
	for _, tc := range []struct{ from, dir, want int }{
		{100, 1, 110}, {100, -1, 90}, {150, 1, 160}, {160, 1, 160}, {80, -1, 80},
		{125, 1, 130}, {125, -1, 120}, {0, 1, 110}, {1000, -1, 150},
	} {
		if got := NextZoom(tc.from, tc.dir); got != tc.want {
			t.Errorf("NextZoom(%d, %d) = %d, want %d", tc.from, tc.dir, got, tc.want)
		}
	}
}

func TestScrollThumb(t *testing.T) {
	s := Scroll{Pos: 0, Content: 1000, View: 250}
	if off, l := s.Thumb(200, 20); off != 0 || l != 50 {
		t.Fatalf("top: %v %v", off, l)
	}
	s.Pos = s.Max()
	if off, l := s.Thumb(200, 20); off != 150 || l != 50 {
		t.Fatalf("bottom: %v %v", off, l)
	}
	if p := s.PosForThumb(75, 200, 20); p != 375 {
		t.Fatalf("thumb in the middle: %v", p)
	}
	if p := s.PosForThumb(500, 200, 20); p != s.Max() {
		t.Fatalf("thumb dragged past the end: %v", p)
	}
	long := Scroll{Content: 100000, View: 100}
	if _, l := long.Thumb(200, 24); l != 24 {
		t.Fatalf("min thumb: %v", l)
	}
	fits := Scroll{Pos: 30, Content: 100, View: 100.5}
	if fits.Scrollable() || fits.Clamped().Pos != 0 {
		t.Fatal("content that fits must not scroll")
	}
}

func TestScrollZoomKeepsAnchor(t *testing.T) {
	// 10:00 is under the mouse, 100 px into the view; after zooming to 150% it still is.
	hourH := 48.0
	before := Scroll{Pos: 10*hourH - 100, Content: 24 * hourH, View: 400}
	frac := before.AnchorFrac(100)
	after := Scroll{Content: 24 * hourH * 1.5, View: 400}
	after.Pos = after.PosForAnchor(frac, 100)
	if got := (after.Pos + 100) / (hourH * 1.5); math.Abs(got-10) > 1e-9 {
		t.Fatalf("hour under the mouse after zoom: %v", got)
	}
}
