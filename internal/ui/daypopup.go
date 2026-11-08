//go:build windows

package ui

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// dayPopup shows all events of a day over its cell in the month and 2-week views, opened from
// "+N more": a click on an event opens it, "Open day" switches to the day view. It closes when it
// loses focus or on Esc.
type dayPopup struct {
	v        *CalView
	dlg      *walk.Dialog
	list     *painted
	day      time.Time
	items    []model.Occurrence
	more     int // events beyond dpMaxRows, offered in the day view
	hover    int // row under the mouse; dpOpenDay = the "Open day" link; -1 none
	hovering bool
}

const (
	dpWidth   = 300
	dpHeader  = 46
	dpRow     = 32
	dpPadding = 6
	dpMaxRows = 12
	dpOpenDay = -2
)

func (v *CalView) showDayPopup(day time.Time, cell walk.Rectangle) {
	if v.dayPop != nil {
		v.dayPop.close()
	}
	p := &dayPopup{v: v, day: day, items: core.DayItems(v.occs, day), hover: -1}
	if len(p.items) > dpMaxRows {
		p.more = len(p.items) - (dpMaxRows - 1)
		p.items = p.items[:dpMaxRows-1]
	}
	if err := p.open(cell); err == nil {
		v.dayPop = p
	}
}

func (p *dayPopup) rows() int {
	if p.more > 0 {
		return len(p.items) + 1
	}
	return len(p.items)
}

func (p *dayPopup) size() walk.Size {
	return walk.Size{Width: dpWidth, Height: dpHeader + p.rows()*dpRow + dpPadding}
}

func (p *dayPopup) open(cell walk.Rectangle) error {
	dlg, err := walk.NewDialog(p.v.app.mw)
	if err != nil {
		return err
	}
	p.dlg = dlg
	l := walk.NewVBoxLayout()
	l.SetMargins(walk.Margins{})
	dlg.SetLayout(l)
	dlg.SetBackground(solid(flSurface))
	p.list, err = newPainted(dlg, win.WS_TABSTOP, p.size(), sizeFixed, p.paint)
	if err != nil {
		dlg.Dispose()
		return err
	}
	p.list.MouseMove().Attach(func(x, y int, _ walk.MouseButton) {
		h := p.hitTest(x, y)
		if h != p.hover {
			p.hover = h
			p.list.Invalidate()
		}
		cursor := walk.CursorArrow()
		if h != -1 {
			cursor = walk.CursorHand()
		}
		p.list.SetCursor(cursor)
		trackHover(p.list, &p.hovering, func() { p.hover = -1; p.list.Invalidate() })
	})
	p.list.MouseUp().Attach(func(x, y int, b walk.MouseButton) {
		if b != walk.LeftButton {
			return
		}
		switch h := p.hitTest(x, y); {
		case h == dpOpenDay || (p.more > 0 && h == len(p.items)):
			day := p.day
			p.close()
			p.v.GoTo(day, ViewDay)
		case h >= 0 && h < len(p.items):
			occ := p.items[h]
			p.close()
			p.v.app.editOccurrence(occ)
		}
	})
	p.list.KeyDown().Attach(func(k walk.Key) {
		if k == walk.KeyEscape {
			p.close()
		}
	})

	// A borderless popup: Windows 11 gives it rounded corners, a hairline border and a shadow.
	hwnd := dlg.Handle()
	style := uint32(win.WS_POPUP | win.WS_CLIPCHILDREN)
	win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style))
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, win.WS_EX_TOOLWINDOW)
	round := int32(2) // DWMWA_WINDOW_CORNER_PREFERENCE = DWMWCP_ROUND
	dwmSetWindowAttribute.Call(uintptr(hwnd), 33, uintptr(unsafe.Pointer(&round)), 4)
	win.SetWindowPos(hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED)

	dlg.Deactivating().Attach(p.close)
	dlg.Show()
	p.place(cell)
	p.list.SetFocus()
	return nil
}

// place puts the popup over the day's cell, kept within the screen.
func (p *dayPopup) place(cell walk.Rectangle) {
	sz := walk.SizeFrom96DPI(p.size(), p.dlg.DPI())
	pt := win.POINT{X: int32(cell.X), Y: int32(cell.Y)}
	win.ClientToScreen(p.v.cw.Handle(), &pt)
	x, y := int(pt.X)-scale(p.dlg, 4), int(pt.Y)-scale(p.dlg, 4)
	mi := win.MONITORINFO{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if win.GetMonitorInfo(win.MonitorFromWindow(p.v.app.mw.Handle(), win.MONITOR_DEFAULTTONEAREST), &mi) {
		wa := mi.RcWork
		x = max(int(wa.Left), min(x, int(wa.Right)-sz.Width))
		y = max(int(wa.Top), min(y, int(wa.Bottom)-sz.Height))
	}
	p.dlg.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: sz.Width, Height: sz.Height})
}

func (p *dayPopup) close() {
	if p.dlg == nil {
		return
	}
	if p.v.dayPop == p {
		p.v.dayPop = nil
	}
	dlg := p.dlg
	p.dlg = nil
	dlg.Synchronize(func() { dlg.Dispose() })
}

func (p *dayPopup) openDayRect(b walk.Rectangle) walk.Rectangle {
	w := scale(p.list, textWidth("Open day", uiFont(10, false))+8)
	return walk.Rectangle{X: b.Width - w - scale(p.list, 16), Y: 0, Width: w + scale(p.list, 8), Height: scale(p.list, dpHeader)}
}

func (p *dayPopup) hitTest(x, y int) int {
	b := p.list.ClientBoundsPixels()
	if r := p.openDayRect(b); x >= r.X && x < r.X+r.Width && y < r.Height {
		return dpOpenDay
	}
	top := scale(p.list, dpHeader)
	if y < top {
		return -1
	}
	if i := (y - top) / scale(p.list, dpRow); i < p.rows() {
		return i
	}
	return -1
}

func (p *dayPopup) paint(cv *walk.Canvas, b walk.Rectangle) {
	px := func(v int) int { return scale(p.list, v) }
	bg := solid(flSurface)
	cv.FillRectanglePixels(bg, b)
	bg.Dispose()

	header := walk.Rectangle{X: px(16), Y: 0, Width: b.Width - px(32), Height: px(dpHeader)}
	drawText(cv, p.day.Format("Monday, 2 January"), uiFont(11, true), flText, header, textLeftMiddle)
	link := flAccent
	if p.hover == dpOpenDay {
		link = flAccentHover
	}
	drawText(cv, "Open day", uiFont(10, false), link, p.openDayRect(b), walk.TextRight|walk.TextVCenter|walk.TextSingleLine)
	line := solid(flLine)
	cv.FillRectanglePixels(line, walk.Rectangle{X: px(12), Y: px(dpHeader) - px(1), Width: b.Width - px(24), Height: px(1)})
	line.Dispose()

	for i := 0; i < p.rows(); i++ {
		r := walk.Rectangle{X: px(6), Y: px(dpHeader) + i*px(dpRow) + px(2), Width: b.Width - px(12), Height: px(dpRow) - px(4)}
		rowBg := flSurface
		if i == p.hover {
			fillRound(cv, flHover, r, px(6))
			rowBg = flHover
		}
		if i == len(p.items) { // the events that did not fit
			drawText(cv, fmt.Sprintf("%d more in the day view", p.more), uiFont(10, false), flAccent,
				walk.Rectangle{X: r.X + px(12), Y: r.Y, Width: r.Width - px(24), Height: r.Height}, textLeftMiddle)
			continue
		}
		o := p.items[i]
		col := p.v.colorOf(o)
		d := px(10)
		radius := d / 2
		when := o.Start.Format("15:04")
		if core.IsAllDayLike(o) {
			radius, when = px(3), "all day"
		}
		if dot, err := roundedBitmap(d, d, p.list.DPI(), radius, rowBg, col, col, false); err == nil {
			cv.DrawImagePixels(dot, walk.Point{X: r.X + px(12), Y: r.Y + (r.Height-d)/2})
			dot.Dispose()
		}
		drawText(cv, when, uiFont(9, false), flText2,
			walk.Rectangle{X: r.X + px(32), Y: r.Y, Width: px(52), Height: r.Height}, textLeftMiddle)
		drawText(cv, core.CalendarEventTitle(o.Event), uiFont(10, false), flText,
			walk.Rectangle{X: r.X + px(88), Y: r.Y, Width: r.Width - px(96), Height: r.Height}, textLeftMiddle)
	}
}
