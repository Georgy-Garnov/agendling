//go:build windows

package ui

import (
	"math"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

// overlayScrollbar is a vertical scrollbar drawn over the right edge of a painted widget, like
// Windows 11 draws its own: a thin line while the mouse is elsewhere, a wider thumb on a light
// track while the mouse is near it or something is being dragged. The widget paints it last and
// passes it its mouse events.
type overlayScrollbar struct {
	w          walk.Window // the widget it is drawn on
	s          core.Scroll
	zone       walk.Rectangle // the strip along the right edge that belongs to the scrollbar
	hover      bool           // the mouse is in the zone
	thumbHover bool
	hovering   bool // a hover tracker runs
	drag       bool // the thumb is being dragged
	grab       int  // where in the thumb it was grabbed
}

const (
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

func (sb *overlayScrollbar) dp(n int) int { return scale(sb.w, n) }

// set records the scroll state and the scrolled area, from top to bottom along the right edge.
func (sb *overlayScrollbar) set(s core.Scroll, right, top, bottom int) {
	sb.s = s
	w := sb.dp(sbZoneWidth)
	sb.zone = walk.Rectangle{X: right - w, Y: top, Width: w, Height: bottom - top}
}

func (sb *overlayScrollbar) contains(pt walk.Point) bool {
	return sb.s.Scrollable() && contains(sb.zone, pt)
}

func (sb *overlayScrollbar) track() walk.Rectangle {
	z, m := sb.zone, sb.dp(sbTrackMargin)
	return walk.Rectangle{X: z.X, Y: z.Y + m, Width: z.Width, Height: z.Height - 2*m}
}

func (sb *overlayScrollbar) thumb(wide bool) walk.Rectangle {
	t := sb.track()
	off, length := sb.s.Thumb(float64(t.Height), float64(sb.dp(sbMinThumb)))
	w, right := sb.dp(sbThinWidth), t.X+t.Width-sb.dp(sbEdgeMargin)
	if wide {
		w, right = sb.dp(sbWideWidth), t.X+t.Width-sb.dp(sbEdgeMargin+(sbTrackWidth-sbWideWidth)/2)
	}
	return walk.Rectangle{X: right - w, Y: t.Y + int(math.Round(off)), Width: w, Height: int(math.Round(length))}
}

// paint draws the scrollbar; busy shows it as dragged, e.g. while the content itself is dragged.
func (sb *overlayScrollbar) paint(c *walk.Canvas, busy bool) {
	if !sb.s.Scrollable() {
		return
	}
	wide := sb.hover || sb.drag || busy
	col := sbThumb
	if wide {
		t := sb.track()
		w := sb.dp(sbTrackWidth)
		track := walk.Rectangle{X: t.X + t.Width - sb.dp(sbEdgeMargin-1) - w, Y: t.Y, Width: w, Height: t.Height}
		fillRoundAlpha(c, track, float64(w)/2, sbTrack, 235)
		switch {
		case sb.drag || busy:
			col = sbThumbDrag
		case sb.thumbHover:
			col = sbThumbHover
		}
	}
	th := sb.thumb(wide)
	fillRoundAlpha(c, th, float64(th.Width)/2, col, 255)
}

// press handles a left click in the zone: on the thumb it starts a drag, on the track it returns
// the position one page towards the click.
func (sb *overlayScrollbar) press(pt walk.Point) float64 {
	th := sb.thumb(true)
	if pt.Y >= th.Y && pt.Y < th.Y+th.Height {
		sb.drag, sb.grab = true, pt.Y-th.Y
		win.SetCapture(sb.w.Handle())
		sb.w.Invalidate()
		return sb.s.Pos
	}
	page := sb.s.View * 0.9
	if pt.Y < th.Y {
		page = -page
	}
	return sb.s.Pos + page
}

// dragTo is the position for the thumb dragged to y.
func (sb *overlayScrollbar) dragTo(y int) float64 {
	t := sb.track()
	return sb.s.PosForThumb(float64(y-t.Y-sb.grab), float64(t.Height), float64(sb.dp(sbMinThumb)))
}

// move updates the hover look for the mouse at pt.
func (sb *overlayScrollbar) move(pt walk.Point) {
	hover := sb.contains(pt)
	th := sb.thumb(true)
	thumbHover := hover && pt.Y >= th.Y && pt.Y < th.Y+th.Height
	if hover != sb.hover || thumbHover != sb.thumbHover {
		sb.hover, sb.thumbHover = hover, thumbHover
		sb.w.Invalidate()
	}
	if hover {
		trackHover(sb.w, &sb.hovering, func() {
			if sb.hover && !sb.drag {
				sb.hover, sb.thumbHover = false, false
				sb.w.Invalidate()
			}
		})
	}
}

// release ends a thumb drag; it reports whether there was one.
func (sb *overlayScrollbar) release() bool {
	if !sb.drag {
		return false
	}
	sb.drag = false
	win.ReleaseCapture()
	sb.w.Invalidate()
	return true
}
