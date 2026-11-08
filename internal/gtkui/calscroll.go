//go:build linux

package gtkui

import (
	"math"

	"github.com/gotk3/gotk3/cairo"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

// calScrollbar is the view's overlay scrollbar: a thin line at the right edge while the mouse is
// elsewhere, a wider thumb on a light track while the mouse is near it or the view is dragged.
// Positions are in widget pixels; s is in the view's (unzoomed) drawing units.
type calScrollbar struct {
	s                       core.Scroll
	top, bottom, right      float64
	hover, thumbHover, drag bool
	grab                    float64 // where in the thumb it was grabbed
}

// calPan is a drag of the view itself: a long press on an empty spot, then moving the mouse.
type calPan struct {
	pressed, active bool
	y, x            float64
	pos             float64 // scroll position when the drag started
	seq             int
}

// zoomAnchor keeps the content under the mouse in place across a zoom: it is applied at the
// next paint, when the zoomed layout is known.
type zoomAnchor struct {
	frac float64 // where in the content the anchor was, 0..1
	y    float64 // widget pixels; -1 = the middle of the view
}

const (
	longPressMS   = 300
	sbZoneWidth   = 16.0
	sbMinThumb    = 28.0
	sbThinWidth   = 3.0
	sbWideWidth   = 6.0
	sbTrackWidth  = 10.0
	sbEdgeMargin  = 3.0
	sbTrackMargin = 2.0
)

var (
	sbThumb      = core.RGB{R: 0x8a, G: 0x90, B: 0x99}
	sbThumbHover = core.RGB{R: 0x6f, G: 0x76, B: 0x80}
	sbThumbDrag  = core.RGB{R: 0x5a, G: 0x61, B: 0x6b}
	sbTrack      = core.RGB{R: 0xf1, G: 0xf3, B: 0xf5}
)

// scroll is the scroll position of the current layout: hours in day and week, weeks in the
// month and two-week grids.
func (v *calView) scroll() *float64 {
	if v.mode == core.ViewDay || v.mode == core.ViewWeek {
		return &v.scrollY
	}
	return &v.cellScroll
}

// setScrollArea records the scrollable part of the layout being painted (drawing units: content
// below top, a view h high) and clamps the position.
func (v *calView) setScrollArea(top, content, w, h float64) {
	pos := v.scroll()
	s := core.Scroll{Pos: *pos, Content: content, View: h - top}
	if z := v.anchor0; z != nil {
		at := s.View / 2
		if z.y >= 0 {
			at = z.y/v.scale - top
		}
		s.Pos = s.PosForAnchor(z.frac, at)
		v.anchor0 = nil
	}
	s = s.Clamped()
	*pos = s.Pos
	v.sb.s = s
	v.sb.top, v.sb.bottom, v.sb.right = top*v.scale, h*v.scale, w*v.scale
}

func (v *calView) inZone(x, y float64) bool {
	return v.sb.s.Scrollable() && x >= v.sb.right-sbZoneWidth && y >= v.sb.top && y < v.sb.bottom
}

func (v *calView) sbTrack() (y, h float64) {
	return v.sb.top + sbTrackMargin, v.sb.bottom - v.sb.top - 2*sbTrackMargin
}

func (v *calView) sbThumb(wide bool) rect {
	ty, th := v.sbTrack()
	off, length := v.sb.s.Thumb(th, sbMinThumb)
	w, right := sbThinWidth, v.sb.right-sbEdgeMargin
	if wide {
		w, right = sbWideWidth, v.sb.right-sbEdgeMargin-(sbTrackWidth-sbWideWidth)/2
	}
	return rect{right - w, ty + off, w, length}
}

func roundRect(cr *cairo.Context, r rect, radius float64) {
	radius = math.Min(radius, math.Min(r.w, r.h)/2)
	cr.NewPath()
	cr.Arc(r.x+r.w-radius, r.y+radius, radius, -math.Pi/2, 0)
	cr.Arc(r.x+r.w-radius, r.y+r.h-radius, radius, 0, math.Pi/2)
	cr.Arc(r.x+radius, r.y+r.h-radius, radius, math.Pi/2, math.Pi)
	cr.Arc(r.x+radius, r.y+radius, radius, math.Pi, 3*math.Pi/2)
	cr.ClosePath()
}

// paintScrollbar draws in widget pixels, after the zoomed drawing.
func (v *calView) paintScrollbar(cr *cairo.Context) {
	sb := &v.sb
	if !sb.s.Scrollable() {
		return
	}
	wide := sb.hover || sb.drag || v.pan.active
	col := sbThumb
	if wide {
		ty, th := v.sbTrack()
		r, g, b := sbTrack.Floats()
		cr.SetSourceRGBA(r, g, b, 0.92)
		roundRect(cr, rect{sb.right - sbEdgeMargin + 1 - sbTrackWidth, ty, sbTrackWidth, th}, sbTrackWidth/2)
		cr.Fill()
		switch {
		case sb.drag || v.pan.active:
			col = sbThumbDrag
		case sb.thumbHover:
			col = sbThumbHover
		}
	}
	t := v.sbThumb(wide)
	setColor(cr, col)
	roundRect(cr, t, t.w/2)
	cr.Fill()
}

func (v *calView) scrollBy(d float64) {
	*v.scroll() += d
	v.area.QueueDraw()
}

func (v *calView) setCursor(name string) {
	w, err := v.area.GetWindow()
	if err != nil {
		return
	}
	if name == "" {
		w.SetCursor(nil)
		return
	}
	disp, err := gdk.DisplayGetDefault()
	if err != nil {
		return
	}
	if c, err := gdk.CursorNewFromName(disp, name); err == nil {
		w.SetCursor(c)
	}
}

// onScrollbarPress starts dragging the thumb, or pages towards the click on the track.
func (v *calView) onScrollbarPress(y float64) {
	t := v.sbThumb(true)
	if y >= t.y && y < t.y+t.h {
		v.sb.drag, v.sb.grab = true, y-t.y
		v.area.QueueDraw()
		return
	}
	page := v.sb.s.View * 0.9
	if y < t.y {
		page = -page
	}
	v.scrollBy(page)
}

func (v *calView) onMotion(x, y float64) {
	switch {
	case v.sb.drag:
		ty, th := v.sbTrack()
		*v.scroll() = v.sb.s.PosForThumb(y-ty-v.sb.grab, th, sbMinThumb)
		v.area.QueueDraw()
		return
	case v.pan.active:
		*v.scroll() = v.pan.pos - (y-v.pan.y)/v.scale
		v.area.QueueDraw()
		return
	case v.pan.pressed && (math.Abs(x-v.pan.x) > 4 || math.Abs(y-v.pan.y) > 4):
		v.pan.pressed = false // moved before the long press: not a drag of the view
	}
	cursor := ""
	if !v.inZone(x, y) {
		for _, h := range v.hits {
			if h.r.contains(x/v.scale, y/v.scale) {
				cursor = "pointer"
				break
			}
		}
	}
	if cursor != v.cursor {
		v.cursor = cursor
		v.setCursor(cursor)
	}
	hover := v.inZone(x, y)
	t := v.sbThumb(true)
	thumbHover := hover && y >= t.y && y < t.y+t.h
	if hover != v.sb.hover || thumbHover != v.sb.thumbHover {
		v.sb.hover, v.sb.thumbHover = hover, thumbHover
		v.area.QueueDraw()
	}
}

func (v *calView) onRelease() {
	if v.sb.drag || v.pan.active {
		v.sb.drag, v.pan.active = false, false
		v.setCursor("")
		v.cursor = ""
		v.area.QueueDraw()
	}
	v.pan.pressed = false
}

func (v *calView) onLeave() {
	if v.sb.hover && !v.sb.drag {
		v.sb.hover, v.sb.thumbHover = false, false
		v.area.QueueDraw()
	}
}

// armPan waits for a long press at (x, y) to start dragging the view (with the middle button
// the drag starts right away).
func (v *calView) armPan(x, y float64, now bool) {
	if !v.sb.s.Scrollable() {
		return
	}
	v.pan = calPan{pressed: true, x: x, y: y, seq: v.pan.seq + 1}
	start := func() {
		v.pan.pressed, v.pan.active, v.pan.pos = false, true, *v.scroll()
		v.setCursor("ns-resize")
		v.area.QueueDraw()
	}
	if now {
		start()
		return
	}
	seq := v.pan.seq
	glib.TimeoutAdd(longPressMS, func() bool {
		if v.pan.pressed && v.pan.seq == seq {
			start()
		}
		return false
	})
}

// zoomAt changes the zoom keeping the content at y (widget pixels; -1 = the middle) in place.
func (v *calView) zoomAt(percent int, y float64) {
	if percent == v.zoom {
		return
	}
	s := v.sb.s
	if s.Content > 0 {
		at := s.View / 2
		if y >= 0 {
			at = (y - v.sb.top) / v.scale
		}
		v.anchor0 = &zoomAnchor{frac: s.AnchorFrac(at), y: y}
	}
	v.zoom = percent
	v.grid.valid = false
	v.hits = v.hits[:0]
	v.area.QueueDraw()
}
