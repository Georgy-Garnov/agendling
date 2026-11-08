//go:build windows

package ui

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

func (a *App) newEvent(start time.Time, allDay bool) {
	e := &model.Event{StartTime: start, EndTime: start.Add(time.Hour), AllDay: allDay}
	if allDay {
		e.StartTime = core.DayStart(start)
		e.EndTime = e.StartTime.AddDate(0, 0, 1)
	}
	a.editEvent(e, time.Time{})
}

func (a *App) editOccurrence(occ model.Occurrence) {
	// Re-read so we edit the current stored state, not a stale view copy.
	e, err := a.store.Event(occ.Event.ID)
	if err != nil {
		walk.MsgBox(a.mw, appinfo.Name, "This event no longer exists.", walk.MsgBoxIconWarning)
		a.view.Reload()
		return
	}
	a.editEvent(e, occ.Start)
}

type editorWidgets struct {
	dlg                        *walk.Dialog
	title, location, url, rems *walk.LineEdit
	calendar, repeat           *walk.ComboBox
	startDate, endDate         *walk.DateEdit
	startTime, endTime         *walk.ComboBox
	allDay                     *walk.CheckBox
	desc                       *walk.TextEdit
	descLinks                  *walk.LinkLabel
}

// updateDescLinks shows the URLs found in the description as clickable links under it.
// It always re-applies text and visibility: walk may apply declarative properties after
// the first text-change event, so cached state could leave the row hidden.
func updateDescLinks(w *editorWidgets) {
	if w.desc == nil || w.descLinks == nil {
		return
	}
	var parts []string
	for _, u := range core.ExtractURLs(core.PlainText(w.desc.Text())) {
		parts = append(parts, LinkMarkup(u, 70))
	}
	text := ""
	if len(parts) > 0 {
		text = "🔗 " + strings.Join(parts, "\r\n🔗 ")
	}
	w.descLinks.SetText(text)
	w.descLinks.SetVisible(len(parts) > 0)
}

// editEvent shows the modal editor. occStart identifies the clicked occurrence of a series.
func (a *App) editEvent(e *model.Event, occStart time.Time) {
	isNew := e.ID == 0
	cals, calIdx := a.svc.EditableCalendars(e)
	var calNames []string
	for _, c := range cals {
		calNames = append(calNames, c.Label())
	}
	if len(cals) == 0 {
		walk.MsgBox(a.mw, appinfo.Name, "Add a calendar in Settings first.", walk.MsgBoxIconInformation)
		return
	}

	repeatLabels, repeatIdx, customRule := core.RepeatChoices(e.RRule)

	displayEnd := e.EndTime
	if e.AllDay {
		displayEnd = e.EndTime.AddDate(0, 0, -1) // UI shows the inclusive last day
		if displayEnd.Before(e.StartTime) {
			displayEnd = e.StartTime
		}
	}
	rems := ""
	if len(e.Reminders) > 0 {
		mins := make([]int, len(e.Reminders))
		for i, r := range e.Reminders {
			mins[i] = r.Minutes()
		}
		rems = core.FormatReminders(mins)
	}

	var w editorWidgets
	var deleteBtn *walk.PushButton
	var acceptBtn, cancelBtn *walk.PushButton
	defaultRems := core.FormatReminders(a.store.Settings().DefaultReminderMinutes)

	seriesNote := ""
	if e.RRule != "" && e.RecurrenceID.IsZero() {
		seriesNote = "Recurring event: changes apply to the whole series."
	} else if !e.RecurrenceID.IsZero() {
		seriesNote = "This is a modified occurrence of a recurring event."
	}

	title := "Edit event"
	if isNew {
		title = "New event"
	}
	times := core.TimeChoices()
	field := func(text string) Label {
		return Label{Text: text, TextColor: flText2, Background: TransparentBrush{}}
	}
	var cards [3]*walk.Composite
	cardLayout := func() Layout {
		return Grid{Columns: 2, Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}, Spacing: 10}
	}
	err := Dialog{
		AssignTo:      &w.dlg,
		Title:         title,
		Font:          Font{Family: "Segoe UI", PointSize: 10},
		Background:    SolidColorBrush{Color: flWindow},
		MinSize:       Size{Width: 600, Height: 640},
		DefaultButton: &acceptBtn,
		CancelButton:  &cancelBtn,
		Layout:        VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{
				Layout: VBox{Margins: Margins{Left: 20, Top: 18, Right: 20, Bottom: 16}, Spacing: 10},
				Children: []Widget{
					LineEdit{AssignTo: &w.title, Text: e.Title, CueBanner: "Event title", Font: Font{Family: "Segoe UI", PointSize: 14},
						MinSize: Size{Height: 36}},
					Composite{
						AssignTo: &cards[0],
						Layout:   cardLayout(),
						Children: []Widget{
							field("Calendar"),
							ComboBox{AssignTo: &w.calendar, Model: calNames, CurrentIndex: calIdx},
							field("Starts"),
							Composite{Layout: HBox{MarginsZero: true}, Background: TransparentBrush{}, Children: []Widget{
								DateEdit{AssignTo: &w.startDate, Date: e.StartTime, Format: "ddd dd.MM.yyyy", MinSize: Size{Width: 150}},
								ComboBox{AssignTo: &w.startTime, Editable: true, Model: times, Value: e.StartTime.Format("15:04"),
									MinSize: Size{Width: 90}, MaxSize: Size{Width: 90}},
								HSpacer{},
							}},
							field("Ends"),
							Composite{Layout: HBox{MarginsZero: true}, Background: TransparentBrush{}, Children: []Widget{
								DateEdit{AssignTo: &w.endDate, Date: displayEnd, Format: "ddd dd.MM.yyyy", MinSize: Size{Width: 150}},
								ComboBox{AssignTo: &w.endTime, Editable: true, Model: times, Value: e.EndTime.Format("15:04"),
									MinSize: Size{Width: 90}, MaxSize: Size{Width: 90}},
								HSpacer{},
							}},
							Label{Background: TransparentBrush{}},
							CheckBox{AssignTo: &w.allDay, Text: "All-day event", Checked: e.AllDay, Background: TransparentBrush{},
								OnCheckedChanged: func() {
									w.startTime.SetEnabled(!w.allDay.Checked())
									w.endTime.SetEnabled(!w.allDay.Checked())
								}},
							field("Repeat"),
							ComboBox{AssignTo: &w.repeat, Model: repeatLabels, CurrentIndex: repeatIdx},
						},
					},
					Composite{
						AssignTo: &cards[1],
						Layout:   cardLayout(),
						Children: []Widget{
							field("Location"),
							LineEdit{AssignTo: &w.location, Text: e.Location},
							field("Web link"),
							Composite{Layout: HBox{MarginsZero: true}, Background: TransparentBrush{}, Children: []Widget{
								LineEdit{AssignTo: &w.url, Text: e.URL},
								PushButton{Text: "Open", MinSize: Size{Width: 70}, MaxSize: Size{Width: 70},
									OnClicked: func() { OpenURL(strings.TrimSpace(w.url.Text())) }},
							}},
							field("Reminders"),
							LineEdit{AssignTo: &w.rems, Text: rems, CueBanner: "default: " + defaultRems + "  (e.g. 5, 15, 1h, 1d)"},
						},
					},
					Composite{
						AssignTo: &cards[2],
						Layout:   VBox{Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}, Spacing: 6},
						Children: []Widget{
							field("Description"),
							TextEdit{AssignTo: &w.desc, Text: toCRLF(e.Description), VScroll: true, MinSize: Size{Height: 110},
								OnTextChanged: func() { updateDescLinks(&w) }},
							LinkLabel{AssignTo: &w.descLinks, Background: TransparentBrush{},
								OnLinkActivated: func(link *walk.LinkLabelLink) { OpenURL(link.URL()) }},
							Label{Text: seriesNote, TextColor: flText2, Background: TransparentBrush{}, Visible: seriesNote != ""},
						},
					},
				},
			},
			Composite{Background: SolidColorBrush{Color: flLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},
			Composite{
				Background: SolidColorBrush{Color: flSurface},
				Layout:     HBox{Margins: Margins{Left: 20, Top: 14, Right: 20, Bottom: 14}, Spacing: 8},
				Children: []Widget{
					PushButton{AssignTo: &deleteBtn, Text: "Delete", Visible: !isNew, MinSize: Size{Width: 96, Height: 32},
						OnClicked: func() {
							if a.deleteEvent(w.dlg, e, occStart) {
								w.dlg.Cancel()
							}
						}},
					HSpacer{},
					PushButton{AssignTo: &acceptBtn, Text: "Save", MinSize: Size{Width: 96, Height: 32}, OnClicked: func() {
						if err := a.applyEditor(&w, e, cals, customRule); err != nil {
							walk.MsgBox(w.dlg, "Cannot save", err.Error(), walk.MsgBoxIconWarning)
							return
						}
						acceptLater(w.dlg)
					}},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", MinSize: Size{Width: 96, Height: 32}, OnClicked: func() { w.dlg.Cancel() }},
				},
			},
		},
	}.Create(a.mw)
	if err != nil {
		log.Printf("editor: %v", err)
		return
	}
	for _, c := range cards {
		makeCard(c, flWindow)
	}
	a.applyIcon(w.dlg)
	w.startTime.SetEnabled(!e.AllDay)
	w.endTime.SetEnabled(!e.AllDay)
	// Fill the links row once the dialog is on screen: walk skips the layout pass for a
	// row made visible before the first show, so it would stay collapsed until an edit.
	w.dlg.Starting().Attach(func() { updateDescLinks(&w) })
	if dialogHook != nil {
		w.dlg.Starting().Attach(func() { dialogHook("event", w.dlg) })
	}
	w.dlg.Run()
}

func toCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// applyEditor collects the form and hands it to the core for validation and saving.
func (a *App) applyEditor(w *editorWidgets, e *model.Event, cals []core.Calendar, customRule string) error {
	in := core.EditorInput{
		Title:       w.title.Text(),
		Description: w.desc.Text(),
		Location:    w.location.Text(),
		URL:         w.url.Text(),
		AllDay:      w.allDay.Checked(),
		StartDate:   w.startDate.Date(),
		EndDate:     w.endDate.Date(),
		StartClock:  w.startTime.Text(),
		EndClock:    w.endTime.Text(),
		Reminders:   w.rems.Text(),
		Rule:        core.RuleForChoice(w.repeat.CurrentIndex(), customRule),
	}
	if i := w.calendar.CurrentIndex(); i >= 0 && i < len(cals) {
		in.Calendar = &cals[i]
	}
	return a.svc.ApplyEdit(e, in)
}

// deleteEvent asks how to delete and performs it. Returns true when something was deleted.
func (a *App) deleteEvent(owner walk.Form, e *model.Event, occStart time.Time) bool {
	recurring := e.RRule != "" || !e.RecurrenceID.IsZero()
	onlyThis := false
	if recurring {
		switch walk.MsgBox(owner, "Delete recurring event",
			"Delete only this occurrence?\n\nYes — only this occurrence\nNo — the entire series",
			walk.MsgBoxYesNoCancel|walk.MsgBoxIconQuestion) {
		case walk.DlgCmdYes:
			onlyThis = true
		case walk.DlgCmdNo:
		default:
			return false
		}
	} else if walk.MsgBox(owner, "Delete event", fmt.Sprintf("Delete \"%s\"?", e.Title),
		walk.MsgBoxOKCancel|walk.MsgBoxIconQuestion) != walk.DlgCmdOK {
		return false
	}

	var err error
	if onlyThis {
		err = a.svc.DeleteOccurrence(e, occStart)
	} else {
		err = a.svc.RemoveSeries(e.CalendarID, e.UID)
	}
	if err != nil {
		walk.MsgBox(owner, "Delete failed", err.Error(), walk.MsgBoxIconError)
		return false
	}
	return true
}
