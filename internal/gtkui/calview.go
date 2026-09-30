//go:build linux

package gtkui

import (
	"fmt"
	"math"
	"time"

	"github.com/gotk3/gotk3/cairo"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/gtk"
	"github.com/gotk3/gotk3/pango"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/recur"
)

type rect struct{ x, y, w, h float64 }

func (r rect) contains(x, y float64) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type hitKind int

const (
	hitEvent hitKind = iota
	hitDay
)

type hit struct {
	kind hitKind
	r    rect
	occ  model.Occurrence
	day  time.Time
}

type gridGeom struct {
	valid                         bool
	cells                         bool
	from                          time.Time
	days, weeks                   int
	left, top, allDayTop, headerH float64
	colW, hourH, cellW, cellH     float64
}

// calView draws day/week/2-week/month layouts on a GtkDrawingArea with Cairo.
type calView struct {
	app  *App
	area *gtk.DrawingArea

	mode     core.ViewMode
	anchor   time.Time
	scrollY  float64
	scrolled bool

	occs []model.Occurrence
	cals map[int64]*model.CalendarSource
	hits []hit
	grid gridGeom

	font, bold, small *pango.FontDescription
}

func newCalView(app *App) *calView {
	return &calView{app: app, mode: core.ViewWeek, anchor: time.Now(),
		font:  pango.FontDescriptionFromString("Sans 9"),
		bold:  pango.FontDescriptionFromString("Sans Bold 9"),
		small: pango.FontDescriptionFromString("Sans 8"),
	}
}

func (v *calView) build() *gtk.DrawingArea {
	da, _ := gtk.DrawingAreaNew()
	v.area = da
	da.SetCanFocus(true)
	da.AddEvents(int(gdk.BUTTON_PRESS_MASK | gdk.SCROLL_MASK | gdk.SMOOTH_SCROLL_MASK | gdk.KEY_PRESS_MASK))
	da.Connect("draw", func(_ *gtk.DrawingArea, cr *cairo.Context) bool {
		v.render(cr, float64(da.GetAllocatedWidth()), float64(da.GetAllocatedHeight()))
		return true
	})
	da.Connect("button-press-event", func(_ *gtk.DrawingArea, ev *gdk.Event) bool {
		v.onButton(gdk.EventButtonNewFromEvent(ev))
		return true
	})
	da.Connect("scroll-event", func(_ *gtk.DrawingArea, ev *gdk.Event) bool {
		v.onScroll(gdk.EventScrollNewFromEvent(ev))
		return true
	})
	da.Connect("key-press-event", func(_ *gtk.DrawingArea, ev *gdk.Event) bool {
		return v.onKey(gdk.EventKeyNewFromEvent(ev).KeyVal())
	})
	return da
}

func (v *calView) setMode(m core.ViewMode) { v.mode = m; v.reload() }
func (v *calView) navigate(dir int)        { v.anchor = core.Navigate(v.mode, v.anchor, dir); v.reload() }
func (v *calView) goTo(t time.Time, m core.ViewMode) {
	v.anchor, v.mode = t, m
	v.reload()
}

// reload re-reads occurrences for the visible range from the local store.
func (v *calView) reload() {
	from, to := core.ViewRange(v.mode, v.anchor)
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
	if v.area != nil {
		v.area.QueueDraw()
	}
}

func (v *calView) colorOf(o model.Occurrence) core.RGB {
	if c := v.cals[o.Event.CalendarID]; c != nil {
		return core.ParseColor(c.Color)
	}
	return core.DefaultColor
}

// ---------- drawing primitives ----------

func setColor(cr *cairo.Context, c core.RGB) { cr.SetSourceRGB(c.Floats()) }

func fill(cr *cairo.Context, r rect, c core.RGB) {
	setColor(cr, c)
	cr.Rectangle(r.x, r.y, r.w, r.h)
	cr.Fill()
}

func hline(cr *cairo.Context, c core.RGB, x1, x2, y float64) {
	setColor(cr, c)
	cr.SetLineWidth(1)
	cr.MoveTo(x1, math.Floor(y)+0.5)
	cr.LineTo(x2, math.Floor(y)+0.5)
	cr.Stroke()
}

func vline(cr *cairo.Context, c core.RGB, x, y1, y2 float64) {
	setColor(cr, c)
	cr.SetLineWidth(1)
	cr.MoveTo(math.Floor(x)+0.5, y1)
	cr.LineTo(math.Floor(x)+0.5, y2)
	cr.Stroke()
}

func circle(cr *cairo.Context, cx, cy, r float64, c core.RGB) {
	setColor(cr, c)
	cr.Arc(cx, cy, r, 0, 2*math.Pi)
	cr.Fill()
}

type textOpt int

const (
	single textOpt = 1 << iota
	wrap
	center
	vcenter
)

func (v *calView) text(cr *cairo.Context, s string, f *pango.FontDescription, c core.RGB, r rect, opt textOpt) {
	if r.w <= 1 || r.h <= 1 {
		return
	}
	l := pango.CairoCreateLayout(cr)
	l.SetFontDescription(f)
	l.SetText(s, -1)
	l.SetWidth(int(r.w * pango.PANGO_SCALE))
	ellipsizeEnd(l)
	if opt&wrap != 0 {
		l.SetWrap(pango.WRAP_WORD_CHAR)
		l.SetHeight(int(r.h * pango.PANGO_SCALE))
	} else {
		l.SetHeight(-1) // one line
	}
	tw, th := l.GetSize()
	x, y := r.x, r.y
	if opt&center != 0 {
		x += (r.w - float64(tw)/pango.PANGO_SCALE) / 2
	}
	if opt&vcenter != 0 {
		y += (r.h - float64(th)/pango.PANGO_SCALE) / 2
	}
	cr.Save()
	cr.Rectangle(r.x, r.y, r.w, r.h)
	cr.Clip()
	setColor(cr, c)
	cr.MoveTo(x, y)
	pango.CairoShowLayout(cr, l)
	cr.Restore()
}

// ---------- rendering ----------

func (v *calView) render(cr *cairo.Context, w, h float64) {
	fill(cr, rect{0, 0, w, h}, core.ColBackground)
	v.hits = v.hits[:0]
	from, _ := core.ViewRange(v.mode, v.anchor)
	switch v.mode {
	case core.ViewDay:
		v.paintTimeGrid(cr, w, h, from, 1)
	case core.ViewWeek:
		v.paintTimeGrid(cr, w, h, from, 7)
	case core.View2Week:
		v.paintCells(cr, w, h, from, 2)
	default:
		v.paintCells(cr, w, h, from, 6)
	}
}

func (v *calView) paintTimeGrid(cr *cairo.Context, w, h float64, from time.Time, days int) {
	const gutter, headerH, rowH, hourH = 56.0, 36.0, 20.0, 48.0
	colW := (w - gutter) / float64(days)
	colX := func(i int) float64 { return math.Round(gutter + float64(i)*colW) }
	now := time.Now()

	spans, rows := core.PackAllDay(v.occs, from, days)
	allDayH := float64(max(rows, 1))*rowH + 6
	top := headerH + allDayH

	maxScroll := math.Max(0, 24*hourH-(h-top))
	if !v.scrolled {
		v.scrollY, v.scrolled = 7.5*hourH, true
	}
	v.scrollY = math.Max(0, math.Min(v.scrollY, maxScroll))
	yAt := func(t, day time.Time) float64 { return top + t.Sub(day).Hours()*hourH - v.scrollY }

	for i := 0; i < days; i++ {
		if core.SameDay(from.AddDate(0, 0, i), now) {
			fill(cr, rect{colX(i), top, colX(i+1) - colX(i), h - top}, core.ColTodayCol)
		}
	}
	for hr := 0; hr <= 24; hr++ {
		y := top + float64(hr)*hourH - v.scrollY
		if y < top-20 || y > h {
			continue
		}
		hline(cr, core.ColGrid, gutter, w, y)
		hline(cr, core.ColGridLight, gutter, w, y+hourH/2)
		if hr < 24 {
			v.text(cr, fmt.Sprintf("%02d:00", hr), v.small, core.ColMuted, rect{4, y - 7, gutter - 10, 14}, single)
		}
	}
	for i := 0; i <= days; i++ {
		vline(cr, core.ColGrid, colX(i), top, h)
	}

	for i := 0; i < days; i++ {
		day := from.AddDate(0, 0, i)
		next := day.AddDate(0, 0, 1)
		for _, l := range core.LayoutLanes(core.TimedItems(v.occs, day)) {
			inner := colW - 4
			x0 := colX(i) + 2 + float64(l.Lane)*inner/float64(l.Lanes)
			bw := inner/float64(l.Lanes) - 2
			s, e := l.O.Start, l.O.End
			if s.Before(day) {
				s = day
			}
			if e.After(next) {
				e = next
			}
			y0, y1 := yAt(s, day), yAt(e, day)
			if y1-y0 < 18 {
				y1 = y0 + 18
			}
			if y1 < top || y0 > h {
				continue
			}
			v.paintEventBox(cr, rect{x0, y0 + 1, bw, y1 - y0 - 2}, l.O, top)
		}
	}

	if now.After(from) && now.Before(from.AddDate(0, 0, days)) {
		i := int(core.DayStart(now).Sub(from).Hours() / 24)
		y := yAt(now, core.DayStart(now))
		if y >= top {
			fill(cr, rect{colX(i), y - 1, colX(i+1) - colX(i), 2}, core.ColNow)
			circle(cr, colX(i), y, 4, core.ColNow)
		}
	}

	// Header and all-day strip on top.
	fill(cr, rect{0, 0, w, top}, core.ColBackground)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		r := rect{colX(i), 0, colX(i+1) - colX(i), headerH}
		col, f := core.ColText, v.font
		if core.SameDay(d, now) {
			col, f = core.ColAccent, v.bold
		}
		label := d.Format("Mon 2")
		if days == 1 {
			label = d.Format("Monday, 2 January")
		}
		v.text(cr, label, f, col, r, single|center|vcenter)
		v.hits = append(v.hits, hit{kind: hitDay, r: r, day: d})
		vline(cr, core.ColGrid, colX(i), 0, top)
	}
	vline(cr, core.ColGrid, colX(days), 0, top)
	v.text(cr, "all-day", v.small, core.ColMuted, rect{4, headerH + 2, gutter - 6, 16}, single)
	for _, s := range spans {
		r := rect{colX(s.Col0) + 2, headerH + 2 + float64(s.Row)*rowH, colX(s.Col1+1) - colX(s.Col0) - 4, rowH - 2}
		v.paintAllDayBar(cr, r, s.O)
	}
	hline(cr, core.ColGrid, 0, w, headerH)
	hline(cr, core.ColGrid, 0, w, top-1)

	v.grid = gridGeom{valid: true, from: from, days: days, left: gutter, top: top, allDayTop: headerH, colW: colW, hourH: hourH}
}

func (v *calView) paintEventBox(cr *cairo.Context, r rect, o model.Occurrence, clipTop float64) {
	col := v.colorOf(o)
	fill(cr, r, col.Blend(0.78))
	fill(cr, rect{r.x, r.y, 3, r.h}, col)
	inner := rect{r.x + 6, r.y + 1, r.w - 8, r.h - 2}
	title := core.EventTitle(o.Event)
	if inner.h >= 32 {
		v.text(cr, title, v.bold, core.ColText, rect{inner.x, inner.y, inner.w, 16}, single)
		rest := o.Start.Format("15:04") + "–" + o.End.Format("15:04")
		if o.Event.Location != "" {
			rest += "  " + o.Event.Location
		}
		v.text(cr, rest, v.small, core.ColText, rect{inner.x, inner.y + 16, inner.w, inner.h - 16}, wrap)
	} else {
		v.text(cr, o.Start.Format("15:04")+" "+title, v.small, core.ColText, inner, single|vcenter)
	}
	if r.y+r.h > clipTop {
		v.hits = append(v.hits, hit{kind: hitEvent, r: r, occ: o})
	}
}

func (v *calView) paintAllDayBar(cr *cairo.Context, r rect, o model.Occurrence) {
	fill(cr, r, v.colorOf(o))
	title := core.EventTitle(o.Event)
	if !o.Event.AllDay {
		title = o.Start.Format("15:04") + " " + title
	}
	v.text(cr, title, v.small, core.RGB{R: 255, G: 255, B: 255}, rect{r.x + 4, r.y, r.w - 6, r.h}, single|vcenter)
	v.hits = append(v.hits, hit{kind: hitEvent, r: r, occ: o})
}

func (v *calView) paintCells(cr *cairo.Context, w, h float64, from time.Time, weeks int) {
	const headerH, lineH = 24.0, 18.0
	cellW := w / 7
	cellH := (h - headerH) / float64(weeks)
	now := time.Now()
	anchorMonth := v.anchor.Month()

	for i := 0; i < 7; i++ {
		d := from.AddDate(0, 0, i)
		v.text(cr, d.Format("Monday"), v.font, core.ColMuted, rect{float64(i) * cellW, 0, cellW, headerH}, single|center|vcenter)
	}
	for wk := 0; wk < weeks; wk++ {
		for i := 0; i < 7; i++ {
			d := from.AddDate(0, 0, wk*7+i)
			x0, x1 := math.Round(float64(i)*cellW), math.Round(float64(i+1)*cellW)
			y0, y1 := headerH+math.Round(float64(wk)*cellH), headerH+math.Round(float64(wk+1)*cellH)
			cell := rect{x0, y0, x1 - x0, y1 - y0}
			switch {
			case core.SameDay(d, now):
				fill(cr, cell, core.ColToday)
			case v.mode == core.ViewMonth && d.Month() != anchorMonth:
				fill(cr, cell, core.ColOtherMonth)
			}
			hline(cr, core.ColGrid, x0, x1, y0)
			vline(cr, core.ColGrid, x0, y0, y1)

			numCol, numFont := core.ColText, v.font
			if v.mode == core.ViewMonth && d.Month() != anchorMonth {
				numCol = core.ColMuted
			}
			if core.SameDay(d, now) {
				numCol, numFont = core.ColAccent, v.bold
			}
			label := fmt.Sprint(d.Day())
			if d.Day() == 1 || (wk == 0 && i == 0) {
				label = d.Format("2 Jan")
			}
			numR := rect{x0 + 4, y0 + 2, cell.w - 8, 18}
			v.text(cr, label, numFont, numCol, numR, single)
			v.hits = append(v.hits, hit{kind: hitDay, r: numR, day: d})

			items := core.DayItems(v.occs, d)
			avail := int((cell.h - 24) / lineH)
			show := len(items)
			if show > avail {
				show = max(avail-1, 0)
			}
			y := y0 + 22
			for k := 0; k < show; k++ {
				o := items[k]
				r := rect{x0 + 3, y, cell.w - 6, lineH - 2}
				if core.IsAllDayLike(o) {
					v.paintAllDayBar(cr, r, o)
				} else {
					circle(cr, r.x+5.5, r.y+r.h/2, 3.5, v.colorOf(o))
					v.text(cr, o.Start.Format("15:04")+" "+core.EventTitle(o.Event), v.small, core.ColText,
						rect{r.x + 12, r.y, r.w - 12, r.h}, single|vcenter)
					v.hits = append(v.hits, hit{kind: hitEvent, r: r, occ: o})
				}
				y += lineH
			}
			if rest := len(items) - show; rest > 0 {
				r := rect{x0 + 6, y, cell.w - 8, lineH - 2}
				v.text(cr, fmt.Sprintf("+%d more", rest), v.small, core.ColAccent, r, single|vcenter)
				v.hits = append(v.hits, hit{kind: hitDay, r: r, day: d})
			}
		}
	}
	hline(cr, core.ColGrid, 0, w, h-1)
	v.grid = gridGeom{valid: true, cells: true, from: from, weeks: weeks, cellW: cellW, cellH: cellH, headerH: headerH}
}

// ---------- input ----------

func (v *calView) onButton(ev *gdk.EventButton) {
	v.area.GrabFocus()
	if ev.Button() != gdk.BUTTON_PRIMARY {
		return
	}
	x, y := ev.X(), ev.Y()
	double := ev.Type() == gdk.EVENT_2BUTTON_PRESS
	if ev.Type() != gdk.EVENT_BUTTON_PRESS && !double {
		return
	}
	for i := len(v.hits) - 1; i >= 0; i-- {
		h := v.hits[i]
		if !h.r.contains(x, y) {
			continue
		}
		if double {
			return // the single click already handled it
		}
		switch h.kind {
		case hitEvent:
			occ := h.occ
			ui(func() { v.app.editOccurrence(occ) })
		case hitDay:
			if v.mode != core.ViewDay {
				day := h.day
				ui(func() { v.goTo(day, core.ViewDay) })
			}
		}
		return
	}
	if !double || !v.grid.valid {
		return
	}
	g := v.grid
	if g.cells {
		col, row := int(x/g.cellW), int((y-g.headerH)/g.cellH)
		if y < g.headerH || col < 0 || col > 6 || row < 0 || row >= g.weeks {
			return
		}
		start := g.from.AddDate(0, 0, row*7+col).Add(9 * time.Hour)
		ui(func() { v.app.newEvent(start, false) })
		return
	}
	col := int((x - g.left) / g.colW)
	if x < g.left || col < 0 || col >= g.days {
		return
	}
	d := g.from.AddDate(0, 0, col)
	switch {
	case y >= g.allDayTop && y < g.top:
		ui(func() { v.app.newEvent(d, true) })
	case y >= g.top:
		mins := int((y-g.top+v.scrollY)/g.hourH*2) * 30 // snap to half hours
		start := d.Add(time.Duration(mins) * time.Minute)
		ui(func() { v.app.newEvent(start, false) })
	}
}

func (v *calView) onScroll(ev *gdk.EventScroll) {
	var dir float64
	switch ev.Direction() {
	case gdk.SCROLL_UP:
		dir = -1
	case gdk.SCROLL_DOWN:
		dir = 1
	case gdk.SCROLL_SMOOTH:
		dir = ev.DeltaY()
	}
	if dir == 0 {
		return
	}
	if v.mode == core.ViewDay || v.mode == core.ViewWeek {
		v.scrollY += dir * v.grid.hourH
		v.area.QueueDraw()
		return
	}
	if dir < 0 {
		v.navigate(-1)
	} else {
		v.navigate(1)
	}
}

func (v *calView) onKey(key uint) bool {
	switch key {
	case gdk.KEY_Left, gdk.KEY_Page_Up:
		v.navigate(-1)
	case gdk.KEY_Right, gdk.KEY_Page_Down:
		v.navigate(1)
	case gdk.KEY_Home, gdk.KEY_t:
		v.goTo(time.Now(), v.mode)
	case gdk.KEY_Up:
		v.scrollY -= v.grid.hourH
		v.area.QueueDraw()
	case gdk.KEY_Down:
		v.scrollY += v.grid.hourH
		v.area.QueueDraw()
	case gdk.KEY_d:
		v.setMode(core.ViewDay)
	case gdk.KEY_w:
		v.setMode(core.ViewWeek)
	case gdk.KEY_m:
		v.setMode(core.ViewMonth)
	case gdk.KEY_n:
		v.app.newEvent(time.Now().Truncate(time.Hour).Add(time.Hour), false)
	default:
		return false
	}
	return true
}
