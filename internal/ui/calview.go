//go:build windows

package ui

import (
	"fmt"
	"math"
	"time"

	"github.com/lxn/walk"
	"golang.org/x/sys/windows"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/recur"
)

type viewMode = core.ViewMode

const (
	ViewDay   = core.ViewDay
	ViewWeek  = core.ViewWeek
	View2Week = core.View2Week
	ViewMonth = core.ViewMonth
)

var viewNames = core.ViewNames

type hitKind int

const (
	hitEvent hitKind = iota
	hitDay           // day header / "+N more": drill down into the day
)

type hit struct {
	kind hitKind
	r    walk.Rectangle
	occ  model.Occurrence
	day  time.Time
}

var (
	colBackground = wc(core.ColBackground)
	colGrid       = wc(core.ColGrid)
	colGridLight  = wc(core.ColGridLight)
	colText       = wc(core.ColText)
	colMuted      = wc(core.ColMuted)
	colAccent     = wc(core.ColAccent)
	colNow        = wc(core.ColNow)
	colOtherMonth = wc(core.ColOtherMonth)
	colToday      = wc(core.ColToday)
)

// CalView is a custom-painted calendar supporting day, week, 2-week and month layouts.
type CalView struct {
	app *App
	cw  *walk.CustomWidget

	mode    viewMode
	anchor  time.Time
	scrollY int // time-grid scroll offset in pixels

	occs []model.Occurrence
	cals map[int64]*model.CalendarSource

	hits []hit
	grid gridGeom // last painted time-grid geometry, for click-to-create

	lastClickAt  time.Time
	lastClickPos walk.Point

	font, bold, small *walk.Font
	brushes           map[walk.Color]*walk.SolidColorBrush
	pens              map[walk.Color]*walk.CosmeticPen
	scrolledOnce      bool
}

type gridGeom struct {
	valid                      bool
	from                       time.Time
	days                       int
	left, top, bottom, allDayT int
	colW, hourH                float64
	// month/2-week cells
	cells    bool
	cellW    float64
	cellH    float64
	headerH  int
	numWeeks int
}

func newCalView(app *App) *CalView {
	v := &CalView{app: app, mode: ViewWeek, anchor: time.Now(),
		brushes: map[walk.Color]*walk.SolidColorBrush{}, pens: map[walk.Color]*walk.CosmeticPen{}}
	v.font, _ = walk.NewFont("Segoe UI", 9, 0)
	v.bold, _ = walk.NewFont("Segoe UI", 9, walk.FontBold)
	v.small, _ = walk.NewFont("Segoe UI", 8, 0)
	return v
}

func (v *CalView) attach(cw *walk.CustomWidget) {
	v.cw = cw
	cw.SetPaintMode(walk.PaintBuffered)
	cw.SetInvalidatesOnResize(true)
	cw.MouseDown().Attach(v.onMouseDown)
	cw.MouseWheel().Attach(v.onWheel)
	cw.KeyDown().Attach(v.onKey)
}

// Range returns the visible [from, to) interval.
func (v *CalView) Range() (time.Time, time.Time) { return core.ViewRange(v.mode, v.anchor) }

func (v *CalView) Title() string { return core.ViewTitle(v.mode, v.anchor) }

func (v *CalView) SetMode(m viewMode) {
	v.mode = m
	v.Reload()
}

func (v *CalView) Navigate(dir int) {
	v.anchor = core.Navigate(v.mode, v.anchor, dir)
	v.Reload()
}

func (v *CalView) GoTo(t time.Time, mode viewMode) {
	v.anchor = t
	v.mode = mode
	v.Reload()
}

// Reload re-reads occurrences for the visible range from the local store (fast; no network).
func (v *CalView) Reload() {
	from, to := v.Range()
	events, err := v.app.store.EventsInRange(from, to)
	if err != nil {
		v.app.setStatus("Failed to load events: " + err.Error())
	}
	v.occs = recur.Expand(events, from, to)
	v.cals = map[int64]*model.CalendarSource{}
	if srcs, err := v.app.store.Sources(); err == nil {
		for _, s := range srcs {
			v.cals[s.ID] = s
		}
	}
	v.app.onViewChanged()
	if v.cw != nil {
		v.cw.Invalidate()
	}
}

func (v *CalView) colorOf(o model.Occurrence) walk.Color {
	if c := v.cals[o.Event.CalendarID]; c != nil {
		return ParseColor(c.Color)
	}
	return ParseColor("")
}

func (v *CalView) brush(c walk.Color) walk.Brush {
	if b := v.brushes[c]; b != nil {
		return b
	}
	b, _ := walk.NewSolidColorBrush(c)
	v.brushes[c] = b
	return b
}

func (v *CalView) pen(c walk.Color) walk.Pen {
	if p := v.pens[c]; p != nil {
		return p
	}
	p, _ := walk.NewCosmeticPen(walk.PenSolid, c)
	v.pens[c] = p
	return p
}

func (v *CalView) text(c *walk.Canvas, s string, f *walk.Font, col walk.Color, r walk.Rectangle, extra walk.DrawTextFormat) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	c.DrawTextPixels(s, f, col, r, walk.TextLeft|walk.TextNoPrefix|walk.TextEndEllipsis|extra)
}

func (v *CalView) hline(c *walk.Canvas, col walk.Color, x1, x2, y int) {
	c.DrawLinePixels(v.pen(col), walk.Point{X: x1, Y: y}, walk.Point{X: x2, Y: y})
}

func (v *CalView) vline(c *walk.Canvas, col walk.Color, x, y1, y2 int) {
	c.DrawLinePixels(v.pen(col), walk.Point{X: x, Y: y1}, walk.Point{X: x, Y: y2})
}

// ---------- painting ----------

func (v *CalView) paint(c *walk.Canvas, _ walk.Rectangle) error {
	return v.render(c, v.cw.ClientBoundsPixels())
}

// render draws the current view into bounds b (native pixels).
func (v *CalView) render(c *walk.Canvas, b walk.Rectangle) error {
	c.FillRectanglePixels(v.brush(colBackground), b)
	v.hits = v.hits[:0]
	scale := float64(c.DPI()) / 96
	px := func(n int) int { return int(math.Round(float64(n) * scale)) }

	from, _ := v.Range()
	switch v.mode {
	case ViewDay:
		v.paintTimeGrid(c, b, px, from, 1)
	case ViewWeek:
		v.paintTimeGrid(c, b, px, from, 7)
	case View2Week:
		v.paintCells(c, b, px, from, 2)
	default:
		v.paintCells(c, b, px, from, 6)
	}
	return nil
}

func (v *CalView) paintTimeGrid(c *walk.Canvas, b walk.Rectangle, px func(int) int, from time.Time, days int) {
	gutter, headerH, rowH := px(56), px(36), px(20)
	colW := float64(b.Width-gutter) / float64(days)
	hourH := float64(px(48))
	colX := func(i int) int { return gutter + int(math.Round(float64(i)*colW)) }
	now := time.Now()

	// All-day strip: greedy row packing of spans across the visible days.
	spans, allDayRows := core.PackAllDay(v.occs, from, days)
	allDayH := max(allDayRows, 1)*rowH + px(6)
	top := headerH + allDayH

	// Time grid (painted first; header and all-day strip are painted over it).
	maxScroll := int(24*hourH) - (b.Height - top)
	if maxScroll < 0 {
		maxScroll = 0
	}
	if !v.scrolledOnce {
		v.scrollY = int(7.5 * hourH)
		v.scrolledOnce = true
	}
	v.scrollY = max(0, min(v.scrollY, maxScroll))
	yAt := func(t time.Time, day time.Time) int {
		h := t.Sub(day).Hours()
		return top + int(h*hourH) - v.scrollY
	}

	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		if core.SameDay(d, now) {
			c.FillRectanglePixels(v.brush(wc(core.ColTodayCol)), walk.Rectangle{X: colX(i), Y: top, Width: colX(i+1) - colX(i), Height: b.Height - top})
		}
	}
	for h := 0; h <= 24; h++ {
		y := top + int(float64(h)*hourH) - v.scrollY
		if y < top-px(20) || y > b.Height {
			continue
		}
		v.hline(c, colGrid, gutter, b.Width, y)
		half := y + int(hourH/2)
		v.hline(c, colGridLight, gutter, b.Width, half)
		if h < 24 {
			v.text(c, fmt.Sprintf("%02d:00", h), v.small, colMuted, walk.Rectangle{X: px(4), Y: y - px(7), Width: gutter - px(10), Height: px(14)}, walk.TextSingleLine)
		}
	}
	for i := 0; i <= days; i++ {
		v.vline(c, colGrid, colX(i), top, b.Height)
	}

	// Timed events with lane layout for overlaps.
	for i := 0; i < days; i++ {
		day := from.AddDate(0, 0, i)
		next := day.AddDate(0, 0, 1)
		items := core.TimedItems(v.occs, day)
		for _, l := range core.LayoutLanes(items) {
			inner := colW - float64(px(4))
			x0 := colX(i) + px(2) + int(float64(l.Lane)*inner/float64(l.Lanes))
			w := int(inner/float64(l.Lanes)) - px(2)
			s, e := l.O.Start, l.O.End
			if s.Before(day) {
				s = day
			}
			if e.After(next) {
				e = next
			}
			y0, y1 := yAt(s, day), yAt(e, day)
			if y1-y0 < px(18) {
				y1 = y0 + px(18)
			}
			if y1 < top || y0 > b.Height {
				continue
			}
			r := walk.Rectangle{X: x0, Y: y0 + 1, Width: w, Height: y1 - y0 - 2}
			v.paintEventBox(c, px, r, l.O)
		}
	}

	// Current time indicator.
	if now.After(from) && now.Before(from.AddDate(0, 0, days)) {
		i := int(core.DayStart(now).Sub(from).Hours() / 24)
		y := yAt(now, core.DayStart(now))
		if y >= top {
			r := walk.Rectangle{X: colX(i), Y: y - 1, Width: colX(i+1) - colX(i), Height: 2}
			c.FillRectanglePixels(v.brush(colNow), r)
			c.FillEllipsePixels(v.brush(colNow), walk.Rectangle{X: colX(i) - px(4), Y: y - px(4), Width: px(8), Height: px(8)})
		}
	}

	// Header and all-day strip on top.
	c.FillRectanglePixels(v.brush(colBackground), walk.Rectangle{X: 0, Y: 0, Width: b.Width, Height: top})
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		r := walk.Rectangle{X: colX(i), Y: 0, Width: colX(i+1) - colX(i), Height: headerH}
		col, f := colText, v.font
		if core.SameDay(d, now) {
			col, f = colAccent, v.bold
		}
		label := d.Format("Mon 2")
		if days == 1 {
			label = d.Format("Monday, 2 January")
		}
		c.DrawTextPixels(label, f, col, r, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
		v.hits = append(v.hits, hit{kind: hitDay, r: r, day: d})
		v.vline(c, colGrid, colX(i), 0, top)
	}
	v.vline(c, colGrid, colX(days), 0, top)
	v.text(c, "all-day", v.small, colMuted, walk.Rectangle{X: px(4), Y: headerH + px(2), Width: gutter - px(6), Height: px(16)}, walk.TextSingleLine)
	for _, s := range spans {
		r := walk.Rectangle{X: colX(s.Col0) + px(2), Y: headerH + px(2) + s.Row*rowH, Width: colX(s.Col1+1) - colX(s.Col0) - px(4), Height: rowH - px(2)}
		v.paintAllDayBar(c, px, r, s.O)
	}
	v.hline(c, colGrid, 0, b.Width, headerH)
	v.hline(c, colGrid, 0, b.Width, top-1)

	v.grid = gridGeom{valid: true, from: from, days: days, left: gutter, top: top, bottom: b.Height,
		allDayT: headerH, colW: colW, hourH: hourH}
}

func (v *CalView) paintEventBox(c *walk.Canvas, px func(int) int, r walk.Rectangle, o model.Occurrence) {
	col := v.colorOf(o)
	c.FillRectanglePixels(v.brush(wc(core.RGB{R: col.R(), G: col.G(), B: col.B()}.Blend(0.78))), r)
	c.FillRectanglePixels(v.brush(col), walk.Rectangle{X: r.X, Y: r.Y, Width: px(3), Height: r.Height})
	inner := walk.Rectangle{X: r.X + px(6), Y: r.Y + px(1), Width: r.Width - px(8), Height: r.Height - px(2)}
	title := o.Event.Title
	if title == "" {
		title = "(No title)"
	}
	timeStr := o.Start.Format("15:04") + "–" + o.End.Format("15:04")
	if inner.Height >= px(32) {
		v.text(c, title, v.bold, colText, walk.Rectangle{X: inner.X, Y: inner.Y, Width: inner.Width, Height: px(15)}, walk.TextSingleLine)
		rest := timeStr
		if o.Event.Location != "" {
			rest += "  " + o.Event.Location
		}
		v.text(c, rest, v.small, colText, walk.Rectangle{X: inner.X, Y: inner.Y + px(15), Width: inner.Width, Height: inner.Height - px(15)}, walk.TextWordbreak)
	} else {
		v.text(c, o.Start.Format("15:04")+" "+title, v.small, colText, inner, walk.TextSingleLine|walk.TextVCenter)
	}
	v.hits = append(v.hits, hit{kind: hitEvent, r: r, occ: o})
}

func (v *CalView) paintAllDayBar(c *walk.Canvas, px func(int) int, r walk.Rectangle, o model.Occurrence) {
	col := v.colorOf(o)
	c.FillRectanglePixels(v.brush(col), r)
	title := o.Event.Title
	if title == "" {
		title = "(No title)"
	}
	if !o.Event.AllDay {
		title = o.Start.Format("15:04") + " " + title
	}
	v.text(c, title, v.small, walk.RGB(255, 255, 255), walk.Rectangle{X: r.X + px(4), Y: r.Y, Width: r.Width - px(6), Height: r.Height}, walk.TextSingleLine|walk.TextVCenter)
	v.hits = append(v.hits, hit{kind: hitEvent, r: r, occ: o})
}

func (v *CalView) paintCells(c *walk.Canvas, b walk.Rectangle, px func(int) int, from time.Time, weeks int) {
	headerH := px(24)
	cellW := float64(b.Width) / 7
	cellH := float64(b.Height-headerH) / float64(weeks)
	now := time.Now()
	_, anchorMonth, _ := v.anchor.Date()
	lineH := px(18)

	for i := 0; i < 7; i++ {
		d := from.AddDate(0, 0, i)
		r := walk.Rectangle{X: int(float64(i) * cellW), Y: 0, Width: int(cellW), Height: headerH}
		c.DrawTextPixels(d.Format("Monday"), v.font, colMuted, r, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	}

	for w := 0; w < weeks; w++ {
		for i := 0; i < 7; i++ {
			d := from.AddDate(0, 0, w*7+i)
			x0 := int(math.Round(float64(i) * cellW))
			x1 := int(math.Round(float64(i+1) * cellW))
			y0 := headerH + int(math.Round(float64(w)*cellH))
			y1 := headerH + int(math.Round(float64(w+1)*cellH))
			cell := walk.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
			switch {
			case core.SameDay(d, now):
				c.FillRectanglePixels(v.brush(colToday), cell)
			case v.mode == ViewMonth && d.Month() != anchorMonth:
				c.FillRectanglePixels(v.brush(colOtherMonth), cell)
			}
			v.hline(c, colGrid, x0, x1, y0)
			v.vline(c, colGrid, x0, y0, y1)

			numCol, numFont := colText, v.font
			if v.mode == ViewMonth && d.Month() != anchorMonth {
				numCol = colMuted
			}
			if core.SameDay(d, now) {
				numCol, numFont = colAccent, v.bold
			}
			label := fmt.Sprint(d.Day())
			if d.Day() == 1 || (w == 0 && i == 0) {
				label = d.Format("2 Jan")
			}
			numR := walk.Rectangle{X: x0 + px(4), Y: y0 + px(2), Width: cell.Width - px(8), Height: px(18)}
			v.text(c, label, numFont, numCol, numR, walk.TextSingleLine)
			v.hits = append(v.hits, hit{kind: hitDay, r: numR, day: d})

			// Events of the day, all-day first.
			items := core.DayItems(v.occs, d)
			avail := (cell.Height - px(24)) / lineH
			if avail < 0 {
				avail = 0
			}
			show := len(items)
			if show > avail {
				show = max(avail-1, 0)
			}
			y := y0 + px(22)
			for k := 0; k < show; k++ {
				o := items[k]
				r := walk.Rectangle{X: x0 + px(3), Y: y, Width: cell.Width - px(6), Height: lineH - px(2)}
				if core.IsAllDayLike(o) {
					v.paintAllDayBar(c, px, r, o)
				} else {
					col := v.colorOf(o)
					c.FillEllipsePixels(v.brush(col), walk.Rectangle{X: r.X + px(2), Y: r.Y + r.Height/2 - px(3), Width: px(7), Height: px(7)})
					title := o.Event.Title
					if title == "" {
						title = "(No title)"
					}
					v.text(c, o.Start.Format("15:04")+" "+title, v.small, colText,
						walk.Rectangle{X: r.X + px(12), Y: r.Y, Width: r.Width - px(12), Height: r.Height}, walk.TextSingleLine|walk.TextVCenter)
					v.hits = append(v.hits, hit{kind: hitEvent, r: r, occ: o})
				}
				y += lineH
			}
			if rest := len(items) - show; rest > 0 {
				r := walk.Rectangle{X: x0 + px(6), Y: y, Width: cell.Width - px(8), Height: lineH - px(2)}
				v.text(c, fmt.Sprintf("+%d more", rest), v.small, colAccent, r, walk.TextSingleLine|walk.TextVCenter)
				v.hits = append(v.hits, hit{kind: hitDay, r: r, day: d})
			}
		}
	}
	v.hline(c, colGrid, 0, b.Width, b.Height-1)
	v.grid = gridGeom{valid: true, cells: true, from: from, cellW: cellW, cellH: cellH, headerH: headerH, numWeeks: weeks}
}

// ---------- input ----------

func (v *CalView) onMouseDown(x, y int, button walk.MouseButton) {
	v.cw.SetFocus()
	if button != walk.LeftButton {
		return
	}
	pt := walk.Point{X: x, Y: y}
	now := time.Now()
	dbl := now.Sub(v.lastClickAt) <= doubleClickTime() &&
		abs(pt.X-v.lastClickPos.X) < 6 && abs(pt.Y-v.lastClickPos.Y) < 6
	v.lastClickAt, v.lastClickPos = now, pt
	if dbl {
		v.lastClickAt = time.Time{} // don't turn a triple click into two doubles
	}

	for i := len(v.hits) - 1; i >= 0; i-- {
		h := v.hits[i]
		if !contains(h.r, pt) {
			continue
		}
		switch h.kind {
		case hitEvent:
			occ := h.occ
			v.lastClickAt = time.Time{}
			// Defer so the mouse message finishes before the modal dialog opens.
			v.cw.Synchronize(func() { v.app.editOccurrence(occ) })
		case hitDay:
			if v.mode != ViewDay {
				day := h.day
				v.cw.Synchronize(func() { v.GoTo(day, ViewDay) })
			}
		}
		return
	}
	if !dbl || !v.grid.valid {
		return
	}
	g := v.grid
	if g.cells {
		col := int(float64(x) / g.cellW)
		row := int(float64(y-g.headerH) / g.cellH)
		if y < g.headerH || col < 0 || col > 6 || row < 0 || row >= g.numWeeks {
			return
		}
		d := g.from.AddDate(0, 0, row*7+col)
		start := d.Add(9 * time.Hour)
		v.cw.Synchronize(func() { v.app.newEvent(start, false) })
		return
	}
	if x < g.left {
		return
	}
	col := int(float64(x-g.left) / g.colW)
	if col < 0 || col >= g.days {
		return
	}
	d := g.from.AddDate(0, 0, col)
	if y >= g.allDayT && y < g.top {
		v.cw.Synchronize(func() { v.app.newEvent(d, true) })
		return
	}
	if y < g.top {
		return
	}
	hours := float64(y-g.top+v.scrollY) / g.hourH
	mins := int(hours*2) * 30 // snap to half hours
	start := d.Add(time.Duration(mins) * time.Minute)
	v.cw.Synchronize(func() { v.app.newEvent(start, false) })
}

func (v *CalView) onWheel(x, y int, button walk.MouseButton) {
	delta := walk.MouseWheelEventDelta(button)
	if v.mode == ViewDay || v.mode == ViewWeek {
		v.scrollY -= int(float64(delta) / 120 * v.grid.hourH)
		v.cw.Invalidate()
		return
	}
	if delta > 0 {
		v.Navigate(-1)
	} else if delta < 0 {
		v.Navigate(1)
	}
}

func (v *CalView) onKey(key walk.Key) {
	switch key {
	case walk.KeyLeft, walk.KeyPrior:
		v.Navigate(-1)
	case walk.KeyRight, walk.KeyNext:
		v.Navigate(1)
	case walk.KeyHome, walk.KeyT:
		v.GoTo(time.Now(), v.mode)
	case walk.KeyUp:
		v.scrollY -= int(v.grid.hourH)
		v.cw.Invalidate()
	case walk.KeyDown:
		v.scrollY += int(v.grid.hourH)
		v.cw.Invalidate()
	case walk.KeyD:
		v.SetMode(ViewDay)
	case walk.KeyW:
		v.SetMode(ViewWeek)
	case walk.KeyM:
		v.SetMode(ViewMonth)
	case walk.KeyN:
		v.app.newEvent(time.Now().Truncate(time.Hour).Add(time.Hour), false)
	}
}

func contains(r walk.Rectangle, p walk.Point) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

var procGetDoubleClickTime = windows.NewLazySystemDLL("user32.dll").NewProc("GetDoubleClickTime")

func doubleClickTime() time.Duration {
	ms, _, _ := procGetDoubleClickTime.Call()
	if ms == 0 {
		ms = 500
	}
	return time.Duration(ms) * time.Millisecond
}
