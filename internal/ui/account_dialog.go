//go:build windows

package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// formCard is a rounded white card for a group of fields.
func formCard(parent walk.Container) *walk.Composite {
	c := box(parent, true, walk.Margins{HNear: 16, VNear: 14, HFar: 16, VFar: 14}, 10)
	makeCard(c, flWindow)
	return c
}

// formRow is a labelled row of a form card; the control goes into the returned container.
func formRow(card walk.Container, text string) *walk.Composite {
	row := box(card, false, walk.Margins{}, 12)
	l := label(row, text, uiFont(10, false), flText2)
	l.SetMinMaxSize(walk.Size{Width: 120}, walk.Size{Width: 120})
	ctl := box(row, false, walk.Margins{}, 8)
	row.Layout().(*walk.BoxLayout).SetStretchFactor(ctl, 1)
	return ctl
}

// editSource edits a calendar account or local calendar; it returns true when saved.
func (a *App) editSource(owner walk.Form, src *model.CalendarSource) bool {
	isCalDAV := src.Type == model.SourceCalDAV
	dlg, err := walk.NewDialog(owner)
	if err != nil {
		log.Printf("source dialog: %v", err)
		return false
	}
	if isCalDAV {
		dlg.SetTitle("CalDAV account")
	} else {
		dlg.SetTitle("Local calendar")
	}
	dlg.SetFont(uiFont(10, false))
	dlg.SetBackground(solid(flWindow))
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{})
	root.SetSpacing(0)
	dlg.SetLayout(root)
	dlg.SetMinMaxSize(walk.Size{Width: 640}, walk.Size{})
	body := box(dlg, true, walk.Margins{HNear: 20, VNear: 18, HFar: 20, VFar: 16}, 10)

	// General: name, colour, enabled.
	general := formCard(body)
	name, _ := walk.NewLineEdit(formRow(general, "Name"))
	name.SetText(src.Name)
	colorRow := formRow(general, "Colour")
	var colorEd *walk.LineEdit
	var swatch *painted
	swatch, _ = newPainted(colorRow, 0, walk.Size{Width: 28, Height: 28}, sizeFixed, func(cv *walk.Canvas, b walk.Rectangle) {
		c := ParseColor(colorEd.Text())
		if bmp, err := roundedBitmap(b.Width, b.Height, swatch.DPI(), scale(swatch, 6), flSurface, c, c, false); err == nil {
			cv.DrawImagePixels(bmp, walk.Point{})
			bmp.Dispose()
		}
	})
	colorEd, _ = walk.NewLineEdit(colorRow)
	colorEd.SetText(src.Color)
	colorEd.SetMinMaxSize(walk.Size{Width: 100}, walk.Size{Width: 100})
	colorEd.TextChanged().Attach(func() { swatch.Invalidate() })
	pushButton(colorRow, "Pick…", 80, func() {
		if c, ok := PickColor(dlg, ParseColor(colorEd.Text())); ok {
			colorEd.SetText(FormatColor(c))
		}
	})
	walk.NewHSpacer(colorRow)
	enabledRow := box(general, false, walk.Margins{}, 12)
	enabledText := box(enabledRow, true, walk.Margins{}, 2)
	walk.NewHSpacer(enabledText)
	label(enabledText, "Enabled", uiFont(10, false), flText)
	caption(enabledText, "Show this calendar and keep it synchronized.")
	enabled, _ := newToggle(enabledRow, src.Enabled, flSurface, nil)

	// Server and calendar selection, for CalDAV accounts.
	var (
		urlEdit, user, pass *walk.LineEdit
		interval            *walk.ComboBox
		calList             *walk.Composite
		choices             []core.CalendarChoice
		toggles             []*toggleSwitch
		fresh               bool // the calendar list belongs to the URL now entered
	)
	shownChoices := func() []core.CalendarChoice {
		res := append([]core.CalendarChoice(nil), choices...)
		for i, t := range toggles {
			res[i].Shown = t.Checked()
		}
		return res
	}
	fillCalendars := func(list []core.CalendarChoice) {
		calList.SetSuspended(true)
		defer calList.SetSuspended(false)
		for calList.Children().Len() > 0 {
			calList.Children().At(0).Dispose()
		}
		choices, toggles = list, nil
		if len(list) == 0 {
			caption(calList, "Test the connection to list the calendars of this account.")
		}
		for _, c := range list {
			row := box(calList, false, walk.Margins{}, 12)
			newColorDot(row, ParseColor(colorEd.Text()), flSurface)
			l := label(row, c.Name, uiFont(10, false), flText)
			l.SetToolTipText(c.Path)
			walk.NewHSpacer(row)
			t, _ := newToggle(row, c.Shown, flSurface, nil)
			t.SetEnabled(len(list) > 1)
			toggles = append(toggles, t)
		}
	}
	collect := func() *model.CalendarSource {
		s := *src
		s.Name = strings.TrimSpace(name.Text())
		s.Color = FormatColor(ParseColor(colorEd.Text()))
		s.Enabled = enabled.Checked()
		if isCalDAV {
			s.URL = strings.TrimSpace(urlEdit.Text())
			s.Username = strings.TrimSpace(user.Text())
			s.Password = pass.Text()
			if i := interval.CurrentIndex(); i >= 0 {
				s.SyncInterval = core.SyncIntervals[i].Mins
			}
			if s.URL != src.URL {
				s.CalendarPath = "" // re-discover for a different server
			}
			switch {
			case fresh:
				s.HiddenCollections, _ = core.HiddenCalendars(shownChoices())
			case s.URL != src.URL || s.Username != src.Username:
				s.HiddenCollections = nil // the listed calendars belong to the previous account
			}
		}
		return &s
	}

	if isCalDAV {
		server := formCard(body)
		urlEdit, _ = walk.NewLineEdit(formRow(server, "Server URL"))
		urlEdit.SetText(src.URL)
		urlEdit.SetCueBanner("https://caldav.example.com")
		urlEdit.TextChanged().Attach(func() { fresh = false })
		hint := formRow(server, "")
		caption(hint, "For example https://caldav.icloud.com or https://host/remote.php/dav (Nextcloud). "+
			"A URL pointing at a single calendar syncs only that calendar.")
		user, _ = walk.NewLineEdit(formRow(server, "Username"))
		user.SetText(src.Username)
		user.TextChanged().Attach(func() { fresh = false })
		pass, _ = walk.NewLineEdit(formRow(server, "App password"))
		pass.SetText(src.Password)
		pass.SetPasswordMode(true)
		testRow := formRow(server, "")
		var testBtn *walk.PushButton
		var testResult *walk.TextLabel
		testBtn = pushButton(testRow, "Test connection", 140, func() {
			s := collect()
			testBtn.SetEnabled(false)
			testResult.SetText("Connecting…")
			testResult.SetTextColor(flText2)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				cals, err := caldav.TestConnection(ctx, s)
				dlg.Synchronize(func() {
					testBtn.SetEnabled(true)
					if err != nil {
						testResult.SetText(err.Error())
						testResult.SetTextColor(walk.RGB(0xc4, 0x2b, 0x1c))
						return
					}
					testResult.SetText(fmt.Sprintf("Connected, %d calendar(s) found", len(cals)))
					testResult.SetTextColor(walk.RGB(0x0f, 0x7b, 0x3f))
					fillCalendars(core.CalendarChoices(cals, s.HiddenCollections)) // keeps choices made so far
					fresh = true
				})
			}()
		})
		testResult = caption(testRow, "")
		testRow.Layout().(*walk.BoxLayout).SetStretchFactor(testResult, 1)

		cals := formCard(body)
		label(cals, "Calendars to sync", uiFont(10, true), flText)
		caption(cals, "Turned-off calendars are not downloaded at all. Which synced calendars are shown is chosen with the "+
			"Calendars button in the main window.")
		calList = box(cals, true, walk.Margins{VNear: 4}, 8)

		syncCard := formCard(body)
		interval, _ = walk.NewComboBox(formRow(syncCard, "Sync"))
		labels := make([]string, len(core.SyncIntervals))
		idx := 2
		for i, it := range core.SyncIntervals {
			labels[i] = it.Label
			if it.Mins == src.SyncInterval {
				idx = i
			}
		}
		interval.SetModel(labels)
		interval.SetCurrentIndex(idx)
		interval.SetMinMaxSize(walk.Size{Width: 220}, walk.Size{Width: 220})

		if src.ID != 0 {
			fillCalendars(core.CalendarChoices(caldav.KnownCalendars(a.store, src.ID), src.HiddenCollections))
			fresh = true
		} else {
			fillCalendars(nil)
		}
	}

	// Bottom bar.
	sep, _ := walk.NewComposite(dlg)
	sep.SetBackground(solid(flLine))
	sep.SetMinMaxSize(walk.Size{Height: 1}, walk.Size{Height: 1})
	bottom := box(dlg, false, walk.Margins{HNear: 20, VNear: 14, HFar: 20, VFar: 14}, 8)
	bottom.SetBackground(solid(flSurface))
	walk.NewHSpacer(bottom)
	ok := pushButton(bottom, "OK", 96, func() {
		s := collect()
		if s.Name == "" {
			walk.MsgBox(dlg, appinfo.Name, "Please enter a name.", walk.MsgBoxIconWarning)
			return
		}
		if isCalDAV && (s.URL == "" || s.Username == "") {
			walk.MsgBox(dlg, appinfo.Name, "Server URL and username are required.", walk.MsgBoxIconWarning)
			return
		}
		if _, anyShown := core.HiddenCalendars(shownChoices()); isCalDAV && fresh && len(choices) > 0 && !anyShown {
			walk.MsgBox(dlg, appinfo.Name, "Turn on at least one calendar, or turn off Enabled to hide the whole account.", walk.MsgBoxIconWarning)
			return
		}
		if err := a.store.SaveSource(s); err != nil {
			walk.MsgBox(dlg, "Error", err.Error(), walk.MsgBoxIconError)
			return
		}
		*src = *s
		acceptLater(dlg)
	})
	cancel := pushButton(bottom, "Cancel", 96, func() { dlg.Cancel() })
	dlg.SetDefaultButton(ok)
	dlg.SetCancelButton(cancel)

	a.applyIcon(dlg)
	if dialogHook != nil {
		dlg.Starting().Attach(func() { dialogHook("account", dlg) })
	}
	name.SetFocus()
	return dlg.Run() == walk.DlgCmdOK
}
