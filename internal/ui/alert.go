//go:build windows

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
)

type alertWindow struct {
	app    *App
	alert  scheduler.Alert
	isNew  bool // "a new event appeared" notice rather than a reminder
	mw     *walk.MainWindow
	snooze *walk.ComboBox
	closed bool
}

func (a *App) showAlert(al scheduler.Alert) {
	w := &alertWindow{app: a, alert: al}
	if err := w.create(); err != nil {
		// Fall back to a tray balloon so the reminder is never lost.
		if a.ni != nil {
			a.ni.ShowInfo("Reminder: "+al.Event.Title, al.Start.Format("Mon 2 Jan 15:04"))
		}
		return
	}
	a.alerts[w] = true
	a.svc.PlayAlertSound(al, false)
}

func (w *alertWindow) create() error {
	al := w.alert
	e := al.Event
	title := e.Title
	if title == "" {
		title = "(No title)"
	}
	calName, calColor := "", ParseColor("")
	if al.Calendar != nil {
		calName, calColor = w.app.svc.CalendarLabel(e), ParseColor(al.Calendar.Color)
		if calName == "" {
			calName = al.Calendar.Name
		}
	}
	windowTitle := "Reminder — " + title
	if w.isNew {
		windowTitle = "New event — " + title
	}
	desc := strings.TrimSpace(core.PlainText(e.Description))
	height := 250
	if desc != "" {
		height = 360
	}

	var content, bar *walk.Composite
	err := MainWindow{
		AssignTo:   &w.mw,
		Title:      windowTitle,
		Size:       Size{Width: 480, Height: height},
		MinSize:    Size{Width: 400, Height: 220},
		Font:       Font{Family: "Segoe UI", PointSize: 10},
		Background: SolidColorBrush{Color: flSurface},
		Layout:     VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{AssignTo: &content, Layout: VBox{Margins: Margins{Left: 20, Top: 16, Right: 20, Bottom: 12}, Spacing: 4}},
			Composite{Background: SolidColorBrush{Color: flLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},
			Composite{AssignTo: &bar, Background: SolidColorBrush{Color: flWindow},
				Layout: HBox{Margins: Margins{Left: 16, Top: 12, Right: 16, Bottom: 12}, Spacing: 8}},
		},
	}.Create()
	if err != nil {
		return err
	}

	if w.isNew {
		label(content, "New meeting", uiFont(9, true), flAccent)
	}
	t := caption(content, title)
	t.SetFont(uiFont(16, true))
	t.SetTextColor(flText)
	label(content, core.WhenText(al), uiFont(10, false), flText)
	row := box(content, false, walk.Margins{VNear: 2}, 6)
	newColorDot(row, calColor, flSurface)
	label(row, calName, uiFont(10, false), flText2)
	walk.NewHSpacer(row)

	openLink := func(link *walk.LinkLabelLink) { OpenURL(link.URL()) }
	addLink := func(glyph, text string) {
		r := box(content, false, walk.Margins{}, 6)
		label(r, glyph, iconFont(10), flText2)
		l, _ := walk.NewLinkLabel(r)
		l.SetBackground(walk.NullBrush())
		l.SetText(LinkMarkup(text, 60))
		l.LinkActivated().Attach(openLink)
		walk.NewHSpacer(r)
	}
	if e.Location != "" {
		addLink(glyphLocation, e.Location)
	}
	if e.URL != "" {
		addLink(glyphLink, e.URL)
	}
	if desc != "" {
		// The description with every URL clickable in place; scrolls when long.
		card := box(content, true, walk.Margins{HNear: 12, VNear: 10, HFar: 12, VFar: 10}, 0)
		makeCard(card, flSurface)
		content.Layout().(*walk.BoxLayout).SetStretchFactor(card, 1)
		sv, _ := walk.NewScrollView(card)
		sv.SetScrollbars(true, true) // with both allowed it fills the card's width
		sv.SetBackground(walk.NullBrush())
		sl := walk.NewVBoxLayout()
		sl.SetMargins(walk.Margins{})
		sl.SetAlignment(walk.AlignHNearVNear)
		sv.SetLayout(sl)
		card.Layout().(*walk.BoxLayout).SetStretchFactor(sv, 1)
		l, _ := walk.NewLinkLabel(sv)
		l.SetBackground(walk.NullBrush())
		l.SetText(LinkMarkup(desc, 60))
		l.LinkActivated().Attach(openLink)
		walk.NewVSpacer(sv)
	} else {
		walk.NewVSpacer(content)
	}

	pushButton(bar, "Stop sound", 110, w.stopSound)
	walk.NewHSpacer(bar)
	if w.isNew {
		pushButton(bar, "Open in calendar", 140, w.openInCalendar)
		pushButton(bar, "Dismiss", 96, w.dismiss)
	} else {
		items, cur := core.SnoozeLabels(w.app.store.Settings().SnoozeIntervalMinutes)
		w.snooze, _ = walk.NewComboBox(bar)
		w.snooze.SetModel(items)
		w.snooze.SetCurrentIndex(cur)
		w.snooze.SetMinMaxSize(walk.Size{Width: 90}, walk.Size{Width: 90})
		pushButton(bar, "Snooze", 96, w.doSnooze)
		pushButton(bar, "Dismiss", 96, w.dismiss)
	}

	if w.app.icon != nil {
		w.mw.SetIcon(w.app.icon)
	}
	// Closing via X behaves like Dismiss.
	w.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !w.closed {
			w.closed = true
			w.app.audio.Stop()
			delete(w.app.alerts, w)
		}
	})
	w.mw.Show()
	SetTopmost(w.mw)
	return nil
}

func (w *alertWindow) stopSound() { w.app.audio.Stop() }

func (w *alertWindow) openInCalendar() {
	w.app.audio.Stop()
	start := w.alert.Start
	w.close()
	w.app.view.GoTo(start, ViewWeek)
	w.app.showMain()
}

// showNewEvents notifies about events that appeared on the server since the last sync.
func (a *App) showNewEvents(sourceID int64, events []*model.Event) {
	alerts, rest := a.svc.NewEventAlerts(sourceID, events)
	shown := 0
	for _, al := range alerts {
		w := &alertWindow{app: a, isNew: true, alert: al}
		if err := w.create(); err != nil {
			continue
		}
		a.alerts[w] = true
		shown++
	}
	if rest += len(alerts) - shown; rest > 0 && a.ni != nil {
		a.ni.ShowInfo("New events", fmt.Sprintf("%d more new events", rest))
	}
	if len(alerts) > 0 || rest > 0 {
		var first scheduler.Alert // one sound for the batch: that of the first event shown
		if len(alerts) > 0 {
			first = alerts[0]
		}
		a.svc.PlayAlertSound(first, true)
	}
}

func (w *alertWindow) dismiss() {
	w.app.audio.Stop()
	w.close()
}

func (w *alertWindow) doSnooze() {
	w.app.audio.Stop()
	mins := core.ParseSnoozeLabel(w.snooze.Text(), w.app.store.Settings().SnoozeIntervalMinutes)
	w.app.sched.Snooze(w.alert, time.Duration(mins)*time.Minute)
	w.close()
}

func (w *alertWindow) close() {
	if !w.closed {
		w.closed = true
		delete(w.app.alerts, w)
	}
	w.mw.Close()
}
