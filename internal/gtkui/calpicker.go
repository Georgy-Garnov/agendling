//go:build linux

package gtkui

import (
	"fmt"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/gotk3/gotk3/pango"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

// calPicker is the header bar's Calendars button with a dropdown: one row per calendar with its
// colour, the account it belongs to and an eye showing whether it is visible. Activating a row
// toggles it; the dropdown stays open for more changes.
type calPicker struct {
	app     *App
	button  *gtk.MenuButton
	label   *gtk.Label
	popover *gtk.Popover
	list    *gtk.ListBox
	showAll *gtk.Button
	cals    []core.Calendar
}

func newCalPicker(a *App) *calPicker {
	p := &calPicker{app: a}
	p.button, _ = gtk.MenuButtonNew()
	p.button.SetTooltipText("Choose which calendars to show")
	inner, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	p.label, _ = gtk.LabelNew("Calendars")
	inner.PackStart(p.label, false, false, 0)
	if arrow, err := gtk.ImageNewFromIconName("pan-down-symbolic", gtk.ICON_SIZE_BUTTON); err == nil {
		inner.PackStart(arrow, false, false, 0)
	}
	p.button.Add(inner)

	p.popover, _ = gtk.PopoverNew(p.button)
	body, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	addClass(body, "cal-picker")
	body.SetSizeRequest(320, -1)
	head, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	head.SetMarginStart(8)
	head.SetMarginEnd(2)
	title, _ := gtk.LabelNew("Show calendars")
	title.SetXAlign(0)
	addClass(title, "cal-picker-title")
	head.PackStart(title, true, true, 0)
	p.showAll, _ = gtk.ButtonNewWithLabel("Show all")
	p.showAll.SetRelief(gtk.RELIEF_NONE)
	p.showAll.Connect("clicked", func() {
		for _, c := range p.cals {
			if !c.Shown {
				_ = a.svc.SetCalendarShown(c.Key, true)
			}
		}
		p.rebuild()
	})
	head.PackEnd(p.showAll, false, false, 0)
	body.PackStart(head, false, false, 0)
	sep, _ := gtk.SeparatorNew(gtk.ORIENTATION_HORIZONTAL)
	body.PackStart(sep, false, false, 2)
	p.list, _ = gtk.ListBoxNew()
	p.list.SetSelectionMode(gtk.SELECTION_NONE)
	p.list.SetActivateOnSingleClick(true)
	p.list.Connect("row-activated", func(_ *gtk.ListBox, row *gtk.ListBoxRow) {
		// gotk3 hands out fresh wrappers for the same row, so rows are identified by index.
		if i := row.GetIndex(); i >= 0 && i < len(p.cals) {
			_ = a.svc.SetCalendarShown(p.cals[i].Key, !p.cals[i].Shown)
			p.rebuild()
		}
	})
	body.PackStart(p.list, false, false, 0)
	body.ShowAll()
	p.popover.Add(body)
	p.button.SetPopover(p.popover)
	p.popover.Connect("show", p.rebuild)
	return p
}

// refresh updates the button label (and the open dropdown) from the store.
func (p *calPicker) refresh() {
	p.cals = p.app.svc.Calendars()
	p.label.SetText(calendarSummary(p.cals))
	if p.popover.GetVisible() {
		p.rebuild()
	}
}

func (p *calPicker) rebuild() {
	p.cals = p.app.svc.Calendars()
	p.label.SetText(calendarSummary(p.cals))
	p.list.GetChildren().Foreach(func(w any) { w.(gtk.IWidget).ToWidget().Destroy() })
	anyHidden := false
	for _, c := range p.cals {
		anyHidden = anyHidden || !c.Shown
		row, _ := gtk.ListBoxRowNew()
		rb, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
		color := c.Source.Color
		if !c.Shown {
			color = core.ParseColor(color).Blend(0.65).Hex()
		}
		dot := markupLabel(`<span foreground="` + color + `" size="large">●</span>`)
		dot.SetLineWrap(false)
		rb.PackStart(dot, false, false, 0)
		text, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 1)
		name, _ := gtk.LabelNew(c.Name)
		name.SetXAlign(0)
		name.SetEllipsize(pango.ELLIPSIZE_END)
		owner, _ := gtk.LabelNew(calendarOwner(c))
		owner.SetXAlign(0)
		addClass(owner, "cal-account")
		if !c.Shown {
			addClass(name, "cal-hidden")
		}
		text.PackStart(name, false, false, 0)
		text.PackStart(owner, false, false, 0)
		rb.PackStart(text, true, true, 0)
		icon := "view-reveal-symbolic"
		if !c.Shown {
			icon = "view-conceal-symbolic"
		}
		if eye, err := gtk.ImageNewFromIconName(icon, gtk.ICON_SIZE_BUTTON); err == nil {
			if !c.Shown {
				addClass(eye, "cal-hidden")
			}
			rb.PackEnd(eye, false, false, 0)
		}
		row.Add(rb)
		row.SetTooltipText(glib.MarkupEscapeText(c.Label()))
		p.list.Add(row)
	}
	p.showAll.SetVisible(anyHidden)
	p.list.ShowAll()
}

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
	return fmt.Sprintf("Calendars %d/%d", shown, len(cals))
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
