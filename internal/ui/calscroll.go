//go:build windows

package ui

import (
	"math"
	"syscall"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

// ---------- fonts at the view's zoom ----------

// calFont is a font of the calendar view, created at the view's zoom and DPI when drawn: GDI
// fonts made from a pixel height scale smoothly, unlike walk fonts in whole points.
type calFont struct {
	pt   float64
	bold bool
}

type calFontKey struct {
	f         calFont
	dpi, zoom int
}

func (v *CalView) hfont(f calFont, dpi int) win.HFONT {
	key := calFontKey{f, dpi, v.zoom}
	if h, ok := v.fonts[key]; ok {
		return h
	}
	lf := win.LOGFONT{
		LfHeight:  -int32(math.Round(f.pt * float64(dpi) / 72 * float64(v.zoom) / 100)),
		LfWeight:  400,
		LfCharSet: win.DEFAULT_CHARSET,
		LfQuality: win.CLEARTYPE_QUALITY,
	}
	if f.bold {
		lf.LfWeight = 700
	}
	copy(lf.LfFaceName[:], syscall.StringToUTF16("Segoe UI"))
	h := win.CreateFontIndirect(&lf)
	v.fonts[key] = h
	return h
}

// dropFonts releases the fonts made for an earlier zoom or DPI.
func (v *CalView) dropFonts() {
	for k, h := range v.fonts {
		win.DeleteObject(win.HGDIOBJ(h))
		delete(v.fonts, k)
	}
}

func (v *CalView) text(c *walk.Canvas, s string, f calFont, col walk.Color, r walk.Rectangle, extra walk.DrawTextFormat) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	hdc := c.HDC()
	old := win.SelectObject(hdc, win.HGDIOBJ(v.hfont(f, c.DPI())))
	defer win.SelectObject(hdc, old)
	win.SetTextColor(hdc, win.COLORREF(col))
	win.SetBkMode(hdc, win.TRANSPARENT)
	rc := win.RECT{Left: int32(r.X), Top: int32(r.Y), Right: int32(r.X + r.Width), Bottom: int32(r.Y + r.Height)}
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	win.DrawTextEx(hdc, p, -1, &rc, uint32(walk.TextLeft|walk.TextNoPrefix|walk.TextEndEllipsis|extra), nil)
}

// ---------- scrolling ----------

// calPan is a drag of the view itself: a long press on an empty spot, then moving the mouse.
type calPan struct {
	pressed bool // left button held, waiting for the long press
	active  bool
	at      walk.Point
	pos     float64 // scroll position when the drag started
	seq     int
}

const longPress = 300 * time.Millisecond

// dp scales a size in 96-DPI pixels to the view's DPI; unlike px it ignores the zoom.
func (v *CalView) dp(n int) int { return scale(v.cw, n) }

// scroll is the scroll position of the current layout: hours in day and week, weeks in the
// month and two-week grids.
func (v *CalView) scroll() *int {
	if v.mode == ViewDay || v.mode == ViewWeek {
		return &v.scrollY
	}
	return &v.cellScroll
}

// zoomAnchor keeps the content under the mouse in place across a zoom: it is applied at the
// next paint, when the zoomed layout is known.
type zoomAnchor struct {
	frac float64 // where in the content the anchor was, 0..1
	y    int     // client pixels; -1 = the middle of the view
}

// setScrollArea records the scrollable part of the last painted layout and clamps the position.
func (v *CalView) setScrollArea(b walk.Rectangle, top, content int) {
	pos := v.scroll()
	s := core.Scroll{Pos: float64(*pos), Content: float64(content), View: float64(b.Height - top)}
	if z := v.zoomAnchor; z != nil {
		at := s.View / 2
		if z.y >= 0 {
			at = float64(z.y - top)
		}
		s.Pos = s.PosForAnchor(z.frac, at)
		v.zoomAnchor = nil
	}
	s = s.Clamped()
	*pos = int(math.Round(s.Pos))
	v.sb.set(s, b.Width, top, b.Height)
}

func (v *CalView) inZone(pt walk.Point) bool { return v.sb.contains(pt) }

// scrollBy moves the view by d pixels.
func (v *CalView) scrollBy(d int) {
	*v.scroll() += d
	v.cw.Invalidate()
}

// onScrollbarDown starts dragging the thumb, or pages towards the click on the track.
func (v *CalView) onScrollbarDown(pt walk.Point) {
	*v.scroll() = int(math.Round(v.sb.press(pt)))
	v.cw.Invalidate()
}

func (v *CalView) onMouseMove(x, y int, _ walk.MouseButton) {
	pt := walk.Point{X: x, Y: y}
	switch {
	case v.sb.drag:
		*v.scroll() = int(math.Round(v.sb.dragTo(y)))
		v.cw.Invalidate()
		return
	case v.pan.active:
		*v.scroll() = int(math.Round(v.pan.pos)) - (y - v.pan.at.Y)
		v.cw.Invalidate()
		return
	case v.pan.pressed && (abs(x-v.pan.at.X) > v.dp(4) || abs(y-v.pan.at.Y) > v.dp(4)):
		v.pan.pressed = false // moved before the long press: not a drag of the view
	}
	cursor := walk.CursorArrow()
	if !v.inZone(pt) {
		for _, h := range v.hits {
			if contains(h.r, pt) {
				cursor = walk.CursorHand()
				break
			}
		}
	}
	if cursor != v.cursor {
		v.cursor = cursor
		v.cw.SetCursor(cursor)
	}
	v.sb.move(pt)
}

func (v *CalView) onMouseUp(x, y int, button walk.MouseButton) {
	if v.sb.release() || v.pan.active {
		win.ReleaseCapture()
		v.pan.active = false
		v.cw.SetCursor(walk.CursorArrow())
		v.cursor = walk.CursorArrow()
		v.onMouseMove(x, y, 0) // refresh the hover look
		v.cw.Invalidate()
	}
	v.pan.pressed = false
}

// armPan waits for a long press at pt to start dragging the view (with the middle button the
// drag starts right away).
func (v *CalView) armPan(pt walk.Point, now bool) {
	if !v.sb.s.Scrollable() {
		return
	}
	v.pan = calPan{pressed: true, at: pt, seq: v.pan.seq + 1}
	win.SetCapture(v.cw.Handle())
	start := func() {
		v.pan.pressed, v.pan.active, v.pan.pos = false, true, float64(*v.scroll())
		v.cw.SetCursor(walk.CursorSizeNS())
		v.cw.Invalidate()
	}
	if now {
		start()
		return
	}
	seq := v.pan.seq
	time.AfterFunc(longPress, func() {
		v.cw.Synchronize(func() {
			if v.pan.pressed && v.pan.seq == seq {
				start()
			}
		})
	})
}

// zoomAt changes the zoom keeping the content at y (client pixels; -1 = the middle) in place.
func (v *CalView) zoomAt(percent, y int) {
	if percent == v.zoom {
		return
	}
	if s := v.sb.s; s.Content > 0 {
		at := s.View / 2
		if y >= 0 {
			at = float64(y - v.sb.zone.Y)
		}
		s.Pos = float64(*v.scroll())
		v.zoomAnchor = &zoomAnchor{frac: s.AnchorFrac(at), y: y}
	}
	v.zoom = percent
	v.dropFonts()
	v.grid.valid = false
	v.hits = v.hits[:0]
	v.cw.Invalidate()
}
