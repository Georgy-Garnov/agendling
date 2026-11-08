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

// calScrollbar is the view's overlay scrollbar, drawn like Windows 11 draws its own: a thin line
// at the right edge while the mouse is elsewhere, a wider thumb on a light track while the mouse
// is near it or the view is being dragged.
type calScrollbar struct {
	s          core.Scroll
	zone       walk.Rectangle // the strip along the right edge that belongs to the scrollbar
	hover      bool           // the mouse is in the zone
	thumbHover bool
	hovering   bool // a hover tracker runs
	drag       bool // the thumb is being dragged
	grab       int  // where in the thumb it was grabbed
}

// calPan is a drag of the view itself: a long press on an empty spot, then moving the mouse.
type calPan struct {
	pressed bool // left button held, waiting for the long press
	active  bool
	at      walk.Point
	pos     float64 // scroll position when the drag started
	seq     int
}

const (
	longPress     = 300 * time.Millisecond
	sbZoneWidth   = 16
	sbMinThumb    = 28
	sbThinWidth   = 3
	sbWideWidth   = 6
	sbTrackWidth  = 10
	sbEdgeMargin  = 3
	sbTrackMargin = 2
)

var (
	sbThumb      = walk.RGB(0x8a, 0x90, 0x99)
	sbThumbHover = walk.RGB(0x6f, 0x76, 0x80)
	sbThumbDrag  = walk.RGB(0x5a, 0x61, 0x6b)
	sbTrack      = walk.RGB(0xf1, 0xf3, 0xf5)
)

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

// setScrollArea records the scrollable part of the last painted layout and clamps the position.
func (v *CalView) setScrollArea(b walk.Rectangle, top, content int) {
	pos := v.scroll()
	s := core.Scroll{Pos: float64(*pos), Content: float64(content), View: float64(b.Height - top)}.Clamped()
	*pos = int(math.Round(s.Pos))
	v.sb.s = s
	w := v.dp(sbZoneWidth)
	v.sb.zone = walk.Rectangle{X: b.Width - w, Y: top, Width: w, Height: b.Height - top}
}

func (v *CalView) sbTrack() walk.Rectangle {
	z, m := v.sb.zone, v.dp(sbTrackMargin)
	return walk.Rectangle{X: z.X, Y: z.Y + m, Width: z.Width, Height: z.Height - 2*m}
}

func (v *CalView) sbThumb(wide bool) walk.Rectangle {
	t := v.sbTrack()
	off, length := v.sb.s.Thumb(float64(t.Height), float64(v.dp(sbMinThumb)))
	w, right := v.dp(sbThinWidth), t.X+t.Width-v.dp(sbEdgeMargin)
	if wide {
		w, right = v.dp(sbWideWidth), t.X+t.Width-v.dp(sbEdgeMargin+(sbTrackWidth-sbWideWidth)/2)
	}
	return walk.Rectangle{X: right - w, Y: t.Y + int(math.Round(off)), Width: w, Height: int(math.Round(length))}
}

func (v *CalView) paintScrollbar(c *walk.Canvas) {
	sb := &v.sb
	if !sb.s.Scrollable() {
		return
	}
	wide := sb.hover || sb.drag || v.pan.active
	col := sbThumb
	if wide {
		t := v.sbTrack()
		w := v.dp(sbTrackWidth)
		track := walk.Rectangle{X: t.X + t.Width - v.dp(sbEdgeMargin-1) - w, Y: t.Y, Width: w, Height: t.Height}
		fillRoundAlpha(c, track, float64(w)/2, sbTrack, 235)
		switch {
		case sb.drag || v.pan.active:
			col = sbThumbDrag
		case sb.thumbHover:
			col = sbThumbHover
		}
	}
	th := v.sbThumb(wide)
	fillRoundAlpha(c, th, float64(th.Width)/2, col, 255)
}

func (v *CalView) inZone(pt walk.Point) bool { return v.sb.s.Scrollable() && contains(v.sb.zone, pt) }

// scrollBy moves the view by d pixels.
func (v *CalView) scrollBy(d int) {
	*v.scroll() += d
	v.cw.Invalidate()
}

// onScrollbarDown starts dragging the thumb, or pages towards the click on the track.
func (v *CalView) onScrollbarDown(pt walk.Point) {
	th := v.sbThumb(true)
	if pt.Y >= th.Y && pt.Y < th.Y+th.Height {
		v.sb.drag, v.sb.grab = true, pt.Y-th.Y
		win.SetCapture(v.cw.Handle())
		v.cw.Invalidate()
		return
	}
	page := int(v.sb.s.View * 0.9)
	if pt.Y < th.Y {
		page = -page
	}
	v.scrollBy(page)
}

func (v *CalView) onMouseMove(x, y int, _ walk.MouseButton) {
	pt := walk.Point{X: x, Y: y}
	switch {
	case v.sb.drag:
		t := v.sbTrack()
		*v.scroll() = int(math.Round(v.sb.s.PosForThumb(float64(y-t.Y-v.sb.grab), float64(t.Height), float64(v.dp(sbMinThumb)))))
		v.cw.Invalidate()
		return
	case v.pan.active:
		*v.scroll() = int(math.Round(v.pan.pos)) - (y - v.pan.at.Y)
		v.cw.Invalidate()
		return
	case v.pan.pressed && (abs(x-v.pan.at.X) > v.dp(4) || abs(y-v.pan.at.Y) > v.dp(4)):
		v.pan.pressed = false // moved before the long press: not a drag of the view
	}
	hover := v.inZone(pt)
	th := v.sbThumb(true)
	thumbHover := hover && pt.Y >= th.Y && pt.Y < th.Y+th.Height
	if hover != v.sb.hover || thumbHover != v.sb.thumbHover {
		v.sb.hover, v.sb.thumbHover = hover, thumbHover
		v.cw.Invalidate()
	}
	if hover {
		trackHover(v.cw, &v.sb.hovering, func() {
			if v.sb.hover && !v.sb.drag {
				v.sb.hover, v.sb.thumbHover = false, false
				v.cw.Invalidate()
			}
		})
	}
}

func (v *CalView) onMouseUp(x, y int, button walk.MouseButton) {
	if v.sb.drag || v.pan.active {
		win.ReleaseCapture()
		v.sb.drag, v.pan.active = false, false
		v.cw.SetCursor(walk.CursorArrow())
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
	old := float64(max(v.zoom, 1))
	anchor := v.sb.s.View / 2
	if y >= 0 && v.sb.zone.Height > 0 {
		anchor = float64(y - v.sb.zone.Y)
	}
	ratio := float64(percent) / old
	if v.mode != ViewDay && v.mode != ViewWeek {
		// The month grid fills the window up to 100% and grows from there.
		ratio = max(1, float64(percent)/100) / max(1, old/100)
	}
	s := v.sb.s
	s.Pos = float64(*v.scroll())
	*v.scroll() = int(math.Round(s.Zoomed(ratio, anchor)))
	v.zoom = percent
	v.dropFonts()
	v.grid.valid = false
	v.hits = v.hits[:0]
	v.cw.Invalidate()
}
