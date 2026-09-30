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
	a.svc.PlayAlertSound()
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
		calName, calColor = al.Calendar.Name, ParseColor(al.Calendar.Color)
	}

	var children []Widget
	if w.isNew {
		children = append(children, Label{Text: "New event added to " + calName,
			Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, TextColor: colAccent})
	}
	children = append(children,
		Label{Text: title, Font: Font{Family: "Segoe UI", PointSize: 14, Bold: true}, TextColor: walk.RGB(30, 30, 30)},
		Label{Text: core.WhenText(al), Font: Font{Family: "Segoe UI", PointSize: 10}},
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 4},
			Children: []Widget{
				CustomWidget{
					MinSize: Size{Width: 12, Height: 12}, MaxSize: Size{Width: 12, Height: 12},
					PaintPixels: func(c *walk.Canvas, r walk.Rectangle) error {
						b, err := walk.NewSolidColorBrush(calColor)
						if err != nil {
							return err
						}
						defer b.Dispose()
						size := c.DPI() * 10 / 96
						return c.FillEllipsePixels(b, walk.Rectangle{X: 1, Y: 1, Width: size, Height: size})
					},
				},
				Label{Text: calName, TextColor: walk.RGB(90, 90, 90)},
				HSpacer{},
			},
		},
	)
	openLink := func(link *walk.LinkLabelLink) { OpenURL(link.URL()) }
	if e.Location != "" {
		children = append(children, LinkLabel{
			Alignment: AlignHNearVCenter, MaxSize: Size{Width: 420},
			Text: "📍 " + LinkMarkup(e.Location, 60), OnLinkActivated: openLink,
		})
	}
	if e.URL != "" {
		children = append(children, LinkLabel{
			Alignment: AlignHNearVCenter, MaxSize: Size{Width: 420},
			Text: "🔗 " + LinkMarkup(e.URL, 60), OnLinkActivated: openLink,
		})
	}
	if desc := strings.TrimSpace(core.PlainText(e.Description)); desc != "" {
		// The description with every URL clickable in place; scrolls when long.
		children = append(children, ScrollView{
			HorizontalFixed: true,
			MinSize:         Size{Height: 110},
			StretchFactor:   1,
			Layout:          VBox{Margins: Margins{Left: 4, Top: 4, Right: 4, Bottom: 4}},
			Background:      SolidColorBrush{Color: walk.RGB(255, 255, 255)},
			Children: []Widget{
				LinkLabel{Text: LinkMarkup(desc, 60), MinSize: Size{Width: 400}, MaxSize: Size{Width: 400}, OnLinkActivated: openLink,
					Alignment: AlignHNearVNear, StretchFactor: 1,
					Background: SolidColorBrush{Color: walk.RGB(255, 255, 255)}},
				VSpacer{},
			},
		})
	} else {
		children = append(children, VSpacer{})
	}

	items, cur := core.SnoozeLabels(w.app.store.Settings().SnoozeIntervalMinutes)

	buttons := []Widget{PushButton{Text: "🔇 Stop Sound", OnClicked: w.stopSound}, HSpacer{}}
	windowTitle := "Reminder — " + title
	if w.isNew {
		windowTitle = "New event — " + title
		buttons = append(buttons,
			PushButton{Text: "Open in calendar", OnClicked: w.openInCalendar},
			PushButton{Text: "Dismiss", OnClicked: w.dismiss})
	} else {
		buttons = append(buttons,
			ComboBox{AssignTo: &w.snooze, Model: items, CurrentIndex: cur, MaxSize: Size{Width: 80}},
			PushButton{Text: "Snooze", OnClicked: w.doSnooze},
			PushButton{Text: "Dismiss", OnClicked: w.dismiss})
	}
	children = append(children, Composite{Layout: HBox{MarginsZero: true}, Children: buttons})

	err := MainWindow{
		AssignTo: &w.mw,
		Title:    windowTitle,
		Size:     Size{Width: 460, Height: 300},
		MinSize:  Size{Width: 380, Height: 200},
		Layout:   VBox{Margins: Margins{Left: 14, Top: 12, Right: 14, Bottom: 12}},
		Children: children,
	}.Create()
	if err != nil {
		return err
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
		a.svc.PlayAlertSound()
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
