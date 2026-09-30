//go:build linux

package gtkui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gotk3/gotk3/gtk"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

const dateLayout = "Mon 02.01.2006"

func (a *App) newEvent(start time.Time, allDay bool) {
	e := &model.Event{StartTime: start, EndTime: start.Add(time.Hour), AllDay: allDay}
	if allDay {
		e.StartTime = core.DayStart(start)
		e.EndTime = e.StartTime.AddDate(0, 0, 1)
	}
	a.editEvent(e, time.Time{})
}

func (a *App) editOccurrence(occ model.Occurrence) {
	e, err := a.store.Event(occ.Event.ID)
	if err != nil {
		a.message(a.win, gtk.MESSAGE_WARNING, "This event no longer exists.")
		a.view.reload()
		return
	}
	a.editEvent(e, occ.Start)
}

func (a *App) message(parent gtk.IWindow, kind gtk.MessageType, text string) {
	d := gtk.MessageDialogNew(parent, gtk.DIALOG_MODAL, kind, gtk.BUTTONS_OK, "%s", text)
	d.Run()
	d.Destroy()
}

func (a *App) confirm(parent gtk.IWindow, text string) bool {
	d := gtk.MessageDialogNew(parent, gtk.DIALOG_MODAL, gtk.MESSAGE_QUESTION, gtk.BUTTONS_OK_CANCEL, "%s", text)
	defer d.Destroy()
	return d.Run() == gtk.RESPONSE_OK
}

// dateField is an entry with a calendar popover for picking a date.
type dateField struct {
	box   *gtk.Box
	entry *gtk.Entry
}

func newDateField(t time.Time) *dateField {
	f := &dateField{}
	f.box, _ = gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	ctx, _ := f.box.GetStyleContext()
	ctx.AddClass("linked")
	f.entry, _ = gtk.EntryNew()
	f.entry.SetWidthChars(15)
	f.entry.SetText(t.Format(dateLayout))
	mb, _ := gtk.MenuButtonNew()
	pop, _ := gtk.PopoverNew(mb)
	cal, _ := gtk.CalendarNew()
	cal.SelectMonth(uint(t.Month()-1), uint(t.Year()))
	cal.SelectDay(uint(t.Day()))
	cal.SetMarginStart(6)
	cal.SetMarginEnd(6)
	cal.SetMarginTop(6)
	cal.SetMarginBottom(6)
	cal.Connect("day-selected-double-click", func() { pop.Popdown() })
	cal.Connect("day-selected", func() {
		y, m, d := cal.GetDate()
		f.entry.SetText(time.Date(int(y), time.Month(m+1), int(d), 0, 0, 0, 0, time.Local).Format(dateLayout))
	})
	cal.Show()
	pop.Add(cal)
	mb.SetPopover(pop)
	f.box.PackStart(f.entry, false, false, 0)
	f.box.PackStart(mb, false, false, 0)
	return f
}

func (f *dateField) date() (time.Time, error) {
	s, _ := f.entry.GetText()
	s = strings.TrimSpace(s)
	for _, layout := range []string{dateLayout, "02.01.2006", "2.1.2006", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q (use DD.MM.YYYY)", s)
}

func timeCombo(t time.Time) *gtk.ComboBoxText {
	c, _ := gtk.ComboBoxTextNewWithEntry()
	for _, s := range core.TimeChoices() {
		c.AppendText(s)
	}
	if entry, err := c.GetEntry(); err == nil {
		entry.SetText(t.Format("15:04"))
		entry.SetWidthChars(6)
	}
	return c
}

func entryText(e *gtk.Entry) string {
	s, _ := e.GetText()
	return s
}

func bufferText(tv *gtk.TextView) string {
	buf, _ := tv.GetBuffer()
	start, end := buf.GetBounds()
	s, _ := buf.GetText(start, end, false)
	return s
}

// editEvent shows the modal editor. occStart identifies the clicked occurrence of a series.
func (a *App) editEvent(e *model.Event, occStart time.Time) {
	isNew := e.ID == 0
	cals, calIdx := a.svc.EditableCalendars(e.CalendarID)
	if len(cals) == 0 {
		a.message(a.win, gtk.MESSAGE_INFO, "Add a calendar in Settings first.")
		return
	}

	dlg, _ := gtk.DialogNew()
	dlg.SetTransientFor(a.win)
	dlg.SetModal(true)
	dlg.SetDefaultSize(540, 600)
	if isNew {
		dlg.SetTitle("New event")
	} else {
		dlg.SetTitle("Edit event")
	}
	const respDelete = gtk.ResponseType(1)
	if !isNew {
		del, _ := dlg.AddButton("Delete", respDelete)
		delCtx, _ := del.GetStyleContext()
		delCtx.AddClass("destructive-action")
	}
	dlg.AddButton("Cancel", gtk.RESPONSE_CANCEL)
	save, _ := dlg.AddButton("Save", gtk.RESPONSE_OK)
	saveCtx, _ := save.GetStyleContext()
	saveCtx.AddClass("suggested-action")
	dlg.SetDefaultResponse(gtk.RESPONSE_OK)

	grid, _ := gtk.GridNew()
	grid.SetRowSpacing(8)
	grid.SetColumnSpacing(10)
	grid.SetMarginStart(14)
	grid.SetMarginEnd(14)
	grid.SetMarginTop(12)
	grid.SetMarginBottom(12)
	row := 0
	addRow := func(label string, w gtk.IWidget) {
		l, _ := gtk.LabelNew(label)
		l.SetXAlign(1)
		l.SetYAlign(0.5)
		grid.Attach(l, 0, row, 1, 1)
		grid.Attach(w, 1, row, 1, 1)
		row++
	}

	title, _ := gtk.EntryNew()
	title.SetText(e.Title)
	title.SetHExpand(true)
	title.SetActivatesDefault(true)
	addRow("Title:", title)

	calCombo, _ := gtk.ComboBoxTextNew()
	for _, c := range cals {
		label := c.Name
		if c.IsLocal() {
			label += "  (local)"
		}
		calCombo.AppendText(label)
	}
	calCombo.SetActive(calIdx)
	addRow("Calendar:", calCombo)

	displayEnd := e.EndTime
	if e.AllDay {
		displayEnd = e.EndTime.AddDate(0, 0, -1) // the UI shows the inclusive last day
		if displayEnd.Before(e.StartTime) {
			displayEnd = e.StartTime
		}
	}
	startDate, endDate := newDateField(e.StartTime), newDateField(displayEnd)
	startTime, endTime := timeCombo(e.StartTime), timeCombo(e.EndTime)
	pair := func(d *dateField, t *gtk.ComboBoxText) *gtk.Box {
		b, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
		b.PackStart(d.box, false, false, 0)
		b.PackStart(t, false, false, 0)
		return b
	}
	addRow("Start:", pair(startDate, startTime))
	addRow("End:", pair(endDate, endTime))

	allDay, _ := gtk.CheckButtonNewWithLabel("All-day event")
	allDay.SetActive(e.AllDay)
	syncAllDay := func() {
		startTime.SetSensitive(!allDay.GetActive())
		endTime.SetSensitive(!allDay.GetActive())
	}
	allDay.Connect("toggled", syncAllDay)
	syncAllDay()
	addRow("", allDay)

	repeatLabels, repeatIdx, customRule := core.RepeatChoices(e.RRule)
	repeat, _ := gtk.ComboBoxTextNew()
	for _, l := range repeatLabels {
		repeat.AppendText(l)
	}
	repeat.SetActive(repeatIdx)
	addRow("Repeat:", repeat)

	location, _ := gtk.EntryNew()
	location.SetText(e.Location)
	addRow("Location:", location)

	urlBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	urlEntry, _ := gtk.EntryNew()
	urlEntry.SetText(e.URL)
	urlEntry.SetHExpand(true)
	urlBox.PackStart(urlEntry, true, true, 0)
	urlBox.PackStart(button("Open", func() { openURL(entryText(urlEntry)) }), false, false, 0)
	addRow("Web link:", urlBox)

	rems, _ := gtk.EntryNew()
	if len(e.Reminders) > 0 {
		mins := make([]int, len(e.Reminders))
		for i, r := range e.Reminders {
			mins[i] = r.Minutes()
		}
		rems.SetText(core.FormatReminders(mins))
	}
	rems.SetPlaceholderText("default: " + core.FormatReminders(a.store.Settings().DefaultReminderMinutes) + "  (e.g. 5, 15, 1h, 1d)")
	addRow("Reminders:", rems)

	desc, _ := gtk.TextViewNew()
	desc.SetWrapMode(gtk.WRAP_WORD_CHAR)
	desc.SetLeftMargin(4)
	desc.SetRightMargin(4)
	buf, _ := desc.GetBuffer()
	buf.SetText(e.Description)
	sw, _ := gtk.ScrolledWindowNew(nil, nil)
	sw.SetShadowType(gtk.SHADOW_IN)
	sw.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	sw.SetVExpand(true)
	sw.SetMinContentHeight(120)
	sw.Add(desc)
	descLabel, _ := gtk.LabelNew("Description:")
	descLabel.SetXAlign(1)
	descLabel.SetYAlign(0)
	grid.Attach(descLabel, 0, row, 1, 1)
	grid.Attach(sw, 1, row, 1, 1)
	row++

	// Links found in the description, clickable while editing.
	links := linkLabel("")
	updateLinks := func() {
		var parts []string
		for _, u := range core.ExtractURLs(core.PlainText(bufferText(desc))) {
			parts = append(parts, "→ "+linkMarkup(u, 70))
		}
		links.SetMarkup(strings.Join(parts, "\n"))
		links.SetVisible(len(parts) > 0)
	}
	buf.Connect("changed", updateLinks)
	grid.Attach(links, 1, row, 1, 1)
	row++

	if e.RRule != "" && e.RecurrenceID.IsZero() {
		addRow("", markupLabel(`<span foreground="#6e6e6e">Recurring event: changes apply to the whole series.</span>`))
	} else if !e.RecurrenceID.IsZero() {
		addRow("", markupLabel(`<span foreground="#6e6e6e">This is a modified occurrence of a recurring event.</span>`))
	}

	content, _ := dlg.GetContentArea()
	content.PackStart(grid, true, true, 0)
	dlg.ShowAll()
	updateLinks()
	title.GrabFocus()

	for {
		switch dlg.Run() {
		case gtk.RESPONSE_OK:
			sd, err1 := startDate.date()
			ed, err2 := endDate.date()
			if err1 != nil || err2 != nil {
				a.message(dlg, gtk.MESSAGE_WARNING, fmt.Sprint(firstErr(err1, err2)))
				continue
			}
			in := core.EditorInput{
				Title:       entryText(title),
				Description: bufferText(desc),
				Location:    entryText(location),
				URL:         entryText(urlEntry),
				AllDay:      allDay.GetActive(),
				StartDate:   sd,
				EndDate:     ed,
				StartClock:  startTime.GetActiveText(),
				EndClock:    endTime.GetActiveText(),
				Reminders:   entryText(rems),
				Rule:        core.RuleForChoice(repeat.GetActive(), customRule),
			}
			if i := calCombo.GetActive(); i >= 0 && i < len(cals) {
				in.Calendar = cals[i]
			}
			if err := a.svc.ApplyEdit(e, in); err != nil {
				a.message(dlg, gtk.MESSAGE_WARNING, "Cannot save: "+err.Error())
				continue
			}
		case respDelete:
			if !a.deleteEvent(dlg, e, occStart) {
				continue
			}
		}
		break
	}
	dlg.Destroy()
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// deleteEvent asks how to delete and performs it. Returns true when something was deleted.
func (a *App) deleteEvent(parent gtk.IWindow, e *model.Event, occStart time.Time) bool {
	var err error
	if e.RRule != "" || !e.RecurrenceID.IsZero() {
		d := gtk.MessageDialogNew(parent, gtk.DIALOG_MODAL, gtk.MESSAGE_QUESTION, gtk.BUTTONS_NONE,
			"%s", "Delete this recurring event?")
		d.AddButton("Cancel", gtk.RESPONSE_CANCEL)
		d.AddButton("Entire series", gtk.RESPONSE_NO)
		d.AddButton("Only this occurrence", gtk.RESPONSE_YES)
		resp := d.Run()
		d.Destroy()
		switch resp {
		case gtk.RESPONSE_YES:
			err = a.svc.DeleteOccurrence(e, occStart)
		case gtk.RESPONSE_NO:
			err = a.svc.RemoveSeries(e.CalendarID, e.UID)
		default:
			return false
		}
	} else {
		if !a.confirm(parent, fmt.Sprintf("Delete \"%s\"?", core.EventTitle(e))) {
			return false
		}
		err = a.svc.RemoveSeries(e.CalendarID, e.UID)
	}
	if err != nil {
		a.message(parent, gtk.MESSAGE_ERROR, "Delete failed: "+err.Error())
		return false
	}
	return true
}
