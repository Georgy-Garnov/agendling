//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

// calPicker is the dropdown under the toolbar's Calendars button: one row per calendar with
// its colour, the account it belongs to and an eye that shows whether it is visible. Clicking a
// row toggles it; the dropdown stays open for more changes and closes when it loses focus.
type calPicker struct {
	a        *App
	dlg      *walk.Dialog
	list     *painted
	cals     []core.Calendar
	hover    int // row under the cursor, -1 none; -2 the "Show all" link
	hovering bool
}

const (
	pickWidth   = 340
	pickHeader  = 48
	pickRow     = 52
	pickPadding = 8
)

var dwmSetWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

// calendarSummary is the Calendars button label: how many calendars are shown when not all.
func calendarSummary(cals []core.Calendar) string {
	shown := 0
	for _, c := range cals {
		if c.Shown {
			shown++
		}
	}
	if shown == len(cals) {
		return "Calendars"
	}
	return fmt.Sprintf("Calendars  %d/%d", shown, len(cals))
}

func (a *App) toggleCalPicker() {
	if a.picker != nil {
		a.picker.close()
		return
	}
	if time.Since(a.pickerClosed) < 300*time.Millisecond {
		return // the click that took focus away from the dropdown also lands on the button
	}
	p := &calPicker{a: a, hover: -1}
	if err := p.open(); err != nil {
		return
	}
	a.picker = p
}

func (p *calPicker) open() error {
	a := p.a
	p.cals = a.svc.Calendars()
	dlg, err := walk.NewDialog(a.mw)
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
	p.list.SetCursor(walk.CursorHand())
	p.list.MouseMove().Attach(func(x, y int, _ walk.MouseButton) {
		if h := p.hitTest(x, y); h != p.hover {
			p.hover = h
			p.list.Invalidate()
		}
		trackHover(p.list, &p.hovering, func() { p.hover = -1; p.list.Invalidate() })
	})
	p.list.MouseUp().Attach(func(x, y int, b walk.MouseButton) {
		if b != walk.LeftButton {
			return
		}
		switch h := p.hitTest(x, y); {
		case h == -2:
			for _, c := range p.cals {
				if !c.Shown {
					_ = a.svc.SetCalendarShown(c.Key, true)
				}
			}
			p.reload()
		case h >= 0:
			_ = a.svc.SetCalendarShown(p.cals[h].Key, !p.cals[h].Shown)
			p.reload()
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
	p.place()
	p.list.SetFocus()
	return nil
}

func (p *calPicker) size() walk.Size {
	return walk.Size{Width: pickWidth, Height: pickHeader + len(p.cals)*pickRow + pickPadding}
}

// place puts the dropdown under the Calendars button, right edges aligned.
func (p *calPicker) place() {
	var rc win.RECT
	win.GetWindowRect(p.a.calBtn.Handle(), &rc)
	sz := walk.SizeFrom96DPI(p.size(), p.dlg.DPI())
	gap := scale(p.dlg, 6)
	p.dlg.SetBoundsPixels(walk.Rectangle{X: int(rc.Right) - sz.Width, Y: int(rc.Bottom) + gap, Width: sz.Width, Height: sz.Height})
}

func (p *calPicker) close() {
	if p.dlg == nil || p.a.picker != p {
		return
	}
	p.a.picker = nil
	p.a.pickerClosed = time.Now()
	dlg := p.dlg
	p.dlg = nil
	dlg.Synchronize(func() { dlg.Dispose() })
}

func (p *calPicker) reload() {
	p.cals = p.a.svc.Calendars()
	p.list.setFixedSize(p.size())
	p.place()
	p.list.Invalidate()
	p.a.updateCalButton()
}

func (p *calPicker) anyHidden() bool {
	for _, c := range p.cals {
		if !c.Shown {
			return true
		}
	}
	return false
}

// showAllRect is the "Show all" link in the header.
func (p *calPicker) showAllRect(b walk.Rectangle) walk.Rectangle {
	w := scale(p.list, textWidth("Show all", uiFont(10, false))+8)
	return walk.Rectangle{X: b.Width - w - scale(p.list, 16), Y: 0, Width: w + scale(p.list, 8), Height: scale(p.list, pickHeader)}
}

func (p *calPicker) hitTest(x, y int) int {
	b := p.list.ClientBoundsPixels()
	if r := p.showAllRect(b); p.anyHidden() && x >= r.X && x < r.X+r.Width && y < r.Height {
		return -2
	}
	top := scale(p.list, pickHeader)
	if y < top {
		return -1
	}
	if i := (y - top) / scale(p.list, pickRow); i < len(p.cals) {
		return i
	}
	return -1
}

func (p *calPicker) paint(cv *walk.Canvas, b walk.Rectangle) {
	px := func(v int) int { return scale(p.list, v) }
	bg := solid(flSurface)
	cv.FillRectanglePixels(bg, b)
	bg.Dispose()

	header := walk.Rectangle{X: px(16), Y: 0, Width: b.Width - px(32), Height: px(pickHeader)}
	drawText(cv, "Show calendars", uiFont(11, true), flText, header, textLeftMiddle)
	if p.anyHidden() {
		color := flAccent
		if p.hover == -2 {
			color = flAccentHover
		}
		drawText(cv, "Show all", uiFont(10, false), color, p.showAllRect(b), walk.TextRight|walk.TextVCenter|walk.TextSingleLine)
	}
	line := solid(flLine)
	cv.FillRectanglePixels(line, walk.Rectangle{X: px(12), Y: px(pickHeader) - px(1), Width: b.Width - px(24), Height: px(1)})
	line.Dispose()

	for i, c := range p.cals {
		r := walk.Rectangle{X: px(6), Y: px(pickHeader) + i*px(pickRow) + px(3), Width: b.Width - px(12), Height: px(pickRow) - px(6)}
		if i == p.hover {
			fillRound(cv, flHover, r, px(6))
		}
		rowBg := flSurface
		if i == p.hover {
			rowBg = flHover
		}
		// Colour dot, dimmed when hidden.
		dotColor := ParseColor(c.Source.Color)
		if !c.Shown {
			dotColor = Blend(dotColor, 0.65)
		}
		d := px(12)
		if dot, err := roundedBitmap(d, d, p.list.DPI(), d/2, rowBg, dotColor, dotColor, false); err == nil {
			cv.DrawImagePixels(dot, walk.Point{X: r.X + px(12), Y: r.Y + (r.Height-d)/2})
			dot.Dispose()
		}
		nameColor, eye, eyeColor := flText, glyphView, flText
		if !c.Shown {
			nameColor, eye, eyeColor = flText2, glyphHide, walk.RGB(0x9a, 0xa0, 0xa8)
		}
		textX := r.X + px(36)
		textW := r.Width - px(36) - px(44)
		drawText(cv, c.Name, uiFont(10, false), nameColor,
			walk.Rectangle{X: textX, Y: r.Y + px(5), Width: textW, Height: px(20)}, textLeftMiddle)
		drawText(cv, calendarOwner(c), uiFont(9, false), flText2,
			walk.Rectangle{X: textX, Y: r.Y + px(24), Width: textW, Height: px(17)}, textLeftMiddle)
		drawText(cv, eye, iconFont(12), eyeColor,
			walk.Rectangle{X: r.X + r.Width - px(40), Y: r.Y, Width: px(32), Height: r.Height}, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	}
}

// calendarOwner is the second line of a calendar row: its account, or what kind of calendar it is.
func calendarOwner(c core.Calendar) string {
	switch {
	case c.Account != "":
		return c.Account
	case c.Source.IsLocal():
		return "Local calendar"
	}
	return "CalDAV account"
}
