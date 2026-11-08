//go:build linux

package gtkui

import (
	"fmt"
	"time"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/gotk3/gotk3/pango"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

const dayPopoverMaxRows = 12

// showDayPopover lists all events of a day under its "+N more" (r, in drawing units): a click on
// an event opens it, "Open day" switches to the day view.
func (v *calView) showDayPopover(day time.Time, r rect) {
	pop, err := gtk.PopoverNew(v.area)
	if err != nil {
		return
	}
	addClass(pop, "day-popover")
	pop.SetPointingTo(*gdk.RectangleNew(int(r.x*v.scale), int(r.y*v.scale), int(r.w*v.scale), int(r.h*v.scale)))
	pop.SetPosition(gtk.POS_BOTTOM)
	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	box.SetMarginStart(6)
	box.SetMarginEnd(6)
	box.SetMarginTop(6)
	box.SetMarginBottom(6)
	box.SetSizeRequest(280, -1)

	openDay := func() {
		pop.Popdown()
		v.goTo(day, core.ViewDay)
	}
	header, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	header.SetMarginStart(6)
	title := markupLabel("<b>" + glib.MarkupEscapeText(day.Format("Monday, 2 January")) + "</b>")
	title.SetXAlign(0)
	header.PackStart(title, true, true, 0)
	link := button("Open day", openDay)
	link.SetRelief(gtk.RELIEF_NONE)
	addClass(link, "day-popover-link")
	header.PackEnd(link, false, false, 0)
	box.PackStart(header, false, false, 0)
	sep, _ := gtk.SeparatorNew(gtk.ORIENTATION_HORIZONTAL)
	box.PackStart(sep, false, false, 2)

	items := core.DayItems(v.occs, day)
	more := 0
	if len(items) > dayPopoverMaxRows {
		more = len(items) - (dayPopoverMaxRows - 1)
		items = items[:dayPopoverMaxRows-1]
	}
	for _, o := range items {
		o := o
		row, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 10)
		mark, when := "●", o.Start.Format("15:04")
		if core.IsAllDayLike(o) {
			mark, when = "■", "all day"
		}
		dot := markupLabel(`<span foreground="` + v.colorOf(o).Hex() + `">` + mark + `</span>`)
		row.PackStart(dot, false, false, 0)
		t, _ := gtk.LabelNew(when)
		t.SetWidthChars(7)
		t.SetXAlign(0)
		addClass(t, "dim-label")
		row.PackStart(t, false, false, 0)
		name, _ := gtk.LabelNew(core.CalendarEventTitle(o.Event))
		name.SetXAlign(0)
		name.SetEllipsize(pango.ELLIPSIZE_END)
		name.SetMaxWidthChars(30)
		row.PackStart(name, true, true, 0)
		b, _ := gtk.ButtonNew()
		b.SetRelief(gtk.RELIEF_NONE)
		b.Add(row)
		b.Connect("clicked", func() {
			pop.Popdown()
			v.app.editOccurrence(o)
		})
		box.PackStart(b, false, false, 0)
	}
	if more > 0 {
		b := button(fmt.Sprintf("%d more in the day view", more), openDay)
		b.SetRelief(gtk.RELIEF_NONE)
		addClass(b, "day-popover-link")
		box.PackStart(b, false, false, 0)
	}
	pop.Add(box)
	pop.Connect("closed", func() { pop.Destroy() })
	box.ShowAll()
	pop.Popup()
}
