//go:build linux

package gtkui

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/gotk3/gotk3/cairo"
	"github.com/gotk3/gotk3/gtk"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
)

// linkMarkup renders plain text as Pango markup with every URL as a clickable link.
func linkMarkup(text string, maxLinkLen int) string {
	var b strings.Builder
	last := 0
	for _, loc := range core.URLRegexp.FindAllStringIndex(text, -1) {
		b.WriteString(html.EscapeString(text[last:loc[0]]))
		u := text[loc[0]:loc[1]]
		fmt.Fprintf(&b, `<a href="%s">%s</a>`, html.EscapeString(core.NormalizeURL(u)), html.EscapeString(core.Shorten(u, maxLinkLen)))
		last = loc[1]
	}
	b.WriteString(html.EscapeString(text[last:]))
	return b.String()
}

// linkLabel is a wrapping, left-aligned label whose links open in the browser.
func linkLabel(markup string) *gtk.Label {
	l, _ := gtk.LabelNew("")
	l.SetMarkup(markup)
	l.SetXAlign(0)
	l.SetYAlign(0)
	l.SetLineWrap(true)
	l.SetSelectable(true)
	l.Connect("activate-link", func(_ *gtk.Label, uri string) bool {
		openURL(uri)
		return true
	})
	return l
}

type alertWindow struct {
	app    *App
	alert  scheduler.Alert
	isNew  bool
	win    *gtk.Window
	snooze *gtk.ComboBoxText
	closed bool
}

func (a *App) showAlert(al scheduler.Alert) {
	w := &alertWindow{app: a, alert: al}
	w.create()
	a.alerts[w] = true
	a.svc.PlayAlertSound()
}

// showNewEvents notifies about events that appeared on the server since the last sync.
func (a *App) showNewEvents(sourceID int64, events []*model.Event) {
	alerts, rest := a.svc.NewEventAlerts(sourceID, events)
	for _, al := range alerts {
		w := &alertWindow{app: a, isNew: true, alert: al}
		w.create()
		a.alerts[w] = true
	}
	if rest > 0 {
		notify("New events", fmt.Sprintf("%d more new events", rest))
	}
	if len(alerts) > 0 || rest > 0 {
		a.svc.PlayAlertSound()
	}
}

func (w *alertWindow) create() {
	al := w.alert
	e := al.Event
	title := core.EventTitle(e)
	calName, calColor := "", core.DefaultColor
	if al.Calendar != nil {
		calName, calColor = al.Calendar.Name, core.ParseColor(al.Calendar.Color)
	}

	win, _ := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	w.win = win
	if w.isNew {
		win.SetTitle("New event — " + title)
	} else {
		win.SetTitle("Reminder — " + title)
	}
	win.SetDefaultSize(460, 320)
	win.SetKeepAbove(true)
	win.SetUrgencyHint(true)
	win.SetPosition(gtk.WIN_POS_CENTER)
	win.SetTypeHint(1) // GDK_WINDOW_TYPE_HINT_DIALOG

	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	box.SetMarginStart(14)
	box.SetMarginEnd(14)
	box.SetMarginTop(12)
	box.SetMarginBottom(12)

	if w.isNew {
		box.PackStart(markupLabel("<b>New event added to "+html.EscapeString(calName)+"</b>"), false, false, 0)
	}
	box.PackStart(markupLabel(`<span size="x-large" weight="bold">`+html.EscapeString(title)+`</span>`), false, false, 0)
	box.PackStart(markupLabel(html.EscapeString(core.WhenText(al))), false, false, 0)

	calRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	dot, _ := gtk.DrawingAreaNew()
	dot.SetSizeRequest(12, 12)
	dot.SetVAlign(gtk.ALIGN_CENTER)
	dot.Connect("draw", func(_ *gtk.DrawingArea, cr *cairo.Context) bool {
		circle(cr, 6, 6, 5, calColor)
		return true
	})
	calRow.PackStart(dot, false, false, 0)
	calRow.PackStart(markupLabel(`<span foreground="#5a5a5a">`+html.EscapeString(calName)+`</span>`), false, false, 0)
	box.PackStart(calRow, false, false, 0)

	if e.Location != "" {
		box.PackStart(iconRow("mark-location-symbolic", linkMarkup(e.Location, 60)), false, false, 0)
	}
	if e.URL != "" {
		box.PackStart(iconRow("insert-link-symbolic", linkMarkup(e.URL, 60)), false, false, 0)
	}
	if desc := strings.TrimSpace(core.PlainText(e.Description)); desc != "" {
		sw, _ := gtk.ScrolledWindowNew(nil, nil)
		sw.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
		sw.SetShadowType(gtk.SHADOW_IN)
		sw.SetMinContentHeight(110)
		lbl := linkLabel(linkMarkup(desc, 60))
		lbl.SetCanFocus(false) // otherwise GTK selects all its text when the window opens
		lbl.SetMarginStart(6)
		lbl.SetMarginEnd(6)
		lbl.SetMarginTop(4)
		lbl.SetMarginBottom(4)
		sw.Add(lbl)
		box.PackStart(sw, true, true, 0)
	} else {
		spacer, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
		box.PackStart(spacer, true, true, 0)
	}

	buttons, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	stop := button("Stop Sound", w.app.svc.Audio.Stop)
	if img, err := gtk.ImageNewFromIconName("audio-volume-muted-symbolic", gtk.ICON_SIZE_BUTTON); err == nil {
		stop.SetImage(img)
		stop.SetAlwaysShowImage(true)
	}
	buttons.PackStart(stop, false, false, 0)
	dismiss := button("Dismiss", w.dismiss)
	buttons.PackEnd(dismiss, false, false, 0)
	if w.isNew {
		buttons.PackEnd(button("Open in calendar", w.openInCalendar), false, false, 0)
	} else {
		buttons.PackEnd(button("Snooze", w.doSnooze), false, false, 0)
		w.snooze, _ = gtk.ComboBoxTextNew()
		labels, cur := core.SnoozeLabels(w.app.store.Settings().SnoozeIntervalMinutes)
		for _, l := range labels {
			w.snooze.AppendText(l)
		}
		w.snooze.SetActive(cur)
		buttons.PackEnd(w.snooze, false, false, 0)
	}
	box.PackEnd(buttons, false, false, 0)
	win.Add(box)

	// Closing via the title bar behaves like Dismiss.
	win.Connect("destroy", func() {
		if !w.closed {
			w.closed = true
			w.app.svc.Audio.Stop()
			delete(w.app.alerts, w)
		}
	})
	win.ShowAll()
	dismiss.GrabFocus() // not a text label, which would get selected
	win.Present()
}

// iconRow is a symbolic icon followed by a link-aware label.
func iconRow(icon, markup string) *gtk.Box {
	row, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	if img, err := gtk.ImageNewFromIconName(icon, gtk.ICON_SIZE_MENU); err == nil {
		img.SetVAlign(gtk.ALIGN_START)
		row.PackStart(img, false, false, 0)
	}
	l := linkLabel(markup)
	l.SetSelectable(false)
	row.PackStart(l, true, true, 0)
	return row
}

func markupLabel(markup string) *gtk.Label {
	l, _ := gtk.LabelNew("")
	l.SetMarkup(markup)
	l.SetXAlign(0)
	l.SetLineWrap(true)
	return l
}

func (w *alertWindow) dismiss() {
	w.app.svc.Audio.Stop()
	w.close()
}

func (w *alertWindow) doSnooze() {
	w.app.svc.Audio.Stop()
	mins := core.ParseSnoozeLabel(w.snooze.GetActiveText(), w.app.store.Settings().SnoozeIntervalMinutes)
	w.app.svc.Sched.Snooze(w.alert, time.Duration(mins)*time.Minute)
	w.close()
}

func (w *alertWindow) openInCalendar() {
	w.app.svc.Audio.Stop()
	start := w.alert.Start
	w.close()
	w.app.view.goTo(start, core.ViewWeek)
	w.app.showMain()
}

func (w *alertWindow) close() {
	if !w.closed {
		w.closed = true
		delete(w.app.alerts, w)
	}
	w.win.Destroy()
}
