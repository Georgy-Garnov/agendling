package core

// Zoom levels of the calendar view, in percent.
const (
	CalendarZoomMin     = 80
	CalendarZoomDefault = 100
	CalendarZoomMax     = 160
	CalendarZoomStep    = 10
)

// CalendarZoom treats the zero value from older settings as the default.
func CalendarZoom(percent int) int {
	if percent == 0 {
		return CalendarZoomDefault
	}
	return max(CalendarZoomMin, min(percent, CalendarZoomMax))
}

// NextZoom is the zoom level one step in (dir > 0) or out (dir < 0) from percent, on the
// CalendarZoomStep grid.
func NextZoom(percent, dir int) int {
	percent = CalendarZoom(percent)
	switch {
	case dir > 0:
		percent = (percent/CalendarZoomStep + 1) * CalendarZoomStep
	case dir < 0:
		percent = ((percent+CalendarZoomStep-1)/CalendarZoomStep - 1) * CalendarZoomStep
	}
	return CalendarZoom(percent)
}

// Scroll is the vertical scroll of a calendar view: Content units of content seen through a
// View units high viewport, from Pos.
type Scroll struct {
	Pos, Content, View float64
}

// Max is the largest position.
func (s Scroll) Max() float64 { return max(0, s.Content-s.View) }

// Scrollable reports whether the content is taller than the viewport.
func (s Scroll) Scrollable() bool { return s.Content-s.View >= 1 }

// Clamped keeps the position within [0, Max].
func (s Scroll) Clamped() Scroll {
	s.Pos = max(0, min(s.Pos, s.Max()))
	return s
}

// Thumb is the scrollbar thumb's offset and length in a track of the given length; the thumb is
// at least minLen long.
func (s Scroll) Thumb(track, minLen float64) (off, length float64) {
	if !s.Scrollable() || track <= 0 {
		return 0, track
	}
	length = min(track, max(minLen, track*s.View/s.Content))
	return (track - length) * s.Clamped().Pos / s.Max(), length
}

// PosForThumb is the position that puts the thumb at offset off of the track: dragging the thumb.
func (s Scroll) PosForThumb(off, track, minLen float64) float64 {
	_, length := s.Thumb(track, minLen)
	if track-length <= 0 {
		return 0
	}
	return max(0, min(off/(track-length), 1)) * s.Max()
}

// Zoomed is the position after the content is scaled by ratio, keeping the content under
// anchor (an offset in the viewport, e.g. the mouse) where it is.
func (s Scroll) Zoomed(ratio, anchor float64) float64 {
	return (s.Pos+anchor)*ratio - anchor
}
