//go:build windows

package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/i18n"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// formCard is a rounded white card for a group of fields.
func formCard(parent walk.Container) *walk.Composite {
	c := box(parent, true, walk.Margins{HNear: 16, VNear: 14, HFar: 16, VFar: 14}, 10)
	makeCard(c, flWindow)
	return c
}

// formLabels are the English labels passed to formRow; formLabelWidth fits the longest one.
var formLabels = []string{
	i18n.Mark("Name"), i18n.Mark("Colour"), i18n.Mark("Server URL"), i18n.Mark("Username"),
	i18n.Mark("App password"), i18n.Mark("Certificate"), i18n.Mark("Sync"),
	i18n.Mark("Issued to"), i18n.Mark("Issued by"), i18n.Mark("Valid until"), "SHA-256",
}

// formLabelWidth is the width of the label column of form cards: 120, or wider when a translated
// label needs it. The language is chosen at startup, so it is measured once.
var formLabelWidth = sync.OnceValue(func() int {
	w := 120
	for _, s := range formLabels {
		w = max(w, textWidth(i18n.T(s), uiFont(10, false))+4)
	}
	return w
})

// formRow is a labelled row of a form card; the control goes into the returned container.
func formRow(card walk.Container, text string) *walk.Composite {
	row := box(card, false, walk.Margins{}, 12)
	l := label(row, text, uiFont(10, false), flText2)
	l.SetMinMaxSize(walk.Size{Width: formLabelWidth()}, walk.Size{Width: formLabelWidth()})
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
		dlg.SetTitle(i18n.T("CalDAV account"))
	} else {
		dlg.SetTitle(i18n.T("Local calendar"))
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
	name, _ := walk.NewLineEdit(formRow(general, i18n.T("Name")))
	name.SetText(src.Name)
	colorRow := formRow(general, i18n.T("Colour"))
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
	pushButton(colorRow, i18n.T("Pick…"), 80, func() {
		if c, ok := PickColor(dlg, ParseColor(colorEd.Text())); ok {
			colorEd.SetText(FormatColor(c))
		}
	})
	walk.NewHSpacer(colorRow)
	enabledRow := box(general, false, walk.Margins{}, 12)
	enabledText := box(enabledRow, true, walk.Margins{}, 2)
	walk.NewHSpacer(enabledText)
	label(enabledText, i18n.T("Enabled"), uiFont(10, false), flText)
	caption(enabledText, i18n.T("Show this calendar and keep it synchronized."))
	enabled, _ := newToggle(enabledRow, src.Enabled, flSurface, nil)

	// Server and calendar selection, for CalDAV accounts.
	var (
		urlEdit, user, pass *walk.LineEdit
		interval            *walk.ComboBox
		calList             *walk.Composite
		choices             []core.CalendarChoice
		toggles             []*toggleSwitch
		fresh               bool // the calendar list belongs to the URL now entered
		trusted             = src.TrustedCert
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
			caption(calList, i18n.T("Test the connection to list the calendars of this account."))
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
			s.TrustedCert = trusted
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
		urlEdit, _ = walk.NewLineEdit(formRow(server, i18n.T("Server URL")))
		urlEdit.SetText(src.URL)
		urlEdit.SetCueBanner("https://caldav.example.com")
		urlEdit.TextChanged().Attach(func() { fresh = false })
		hint := formRow(server, "")
		caption(hint, i18n.T("For example https://caldav.icloud.com or https://host/remote.php/dav (Nextcloud). "+
			"A URL pointing at a single calendar syncs only that calendar."))
		user, _ = walk.NewLineEdit(formRow(server, i18n.T("Username")))
		user.SetText(src.Username)
		user.TextChanged().Attach(func() { fresh = false })
		pass, _ = walk.NewLineEdit(formRow(server, i18n.T("App password")))
		pass.SetText(src.Password)
		pass.SetPasswordMode(true)
		// A certificate trusted for this account (e.g. a NAS with a self-signed one).
		certRow := formRow(server, i18n.T("Certificate"))
		certText := label(certRow, "", uiFont(10, false), flText)
		walk.NewHSpacer(certRow)
		var showCert func()
		pushButton(certRow, i18n.T("Forget"), 80, func() {
			trusted = ""
			showCert()
		})
		showCert = func() {
			certRow.Parent().(walk.Widget).SetVisible(trusted != "")
			certText.SetText(i18n.Tf("Trusted, SHA-256 %s", caldav.ShortFingerprint(trusted)))
			certText.SetToolTipText(caldav.FormatFingerprint(trusted))
		}
		showCert()

		testRow := formRow(server, "")
		var testBtn *walk.PushButton
		var testResult *walk.TextLabel
		var runTest func()
		runTest = func() {
			s := collect()
			testBtn.SetEnabled(false)
			testResult.SetText(i18n.T("Connecting…"))
			testResult.SetTextColor(flText2)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				cals, err := caldav.TestConnection(ctx, s)
				dlg.Synchronize(func() {
					testBtn.SetEnabled(true)
					if cert := caldav.UntrustedCertificate(err); cert != nil {
						testResult.SetText(i18n.T("The server's certificate is not trusted."))
						testResult.SetTextColor(walk.RGB(0xc4, 0x2b, 0x1c))
						if a.confirmCertificate(dlg, caldav.DescribeCertificate(cert)) {
							trusted = caldav.CertFingerprint(cert)
							showCert()
							runTest()
						}
						return
					}
					if err != nil {
						testResult.SetText(err.Error())
						testResult.SetTextColor(walk.RGB(0xc4, 0x2b, 0x1c))
						return
					}
					testResult.SetText(fmt.Sprintf(i18n.TN(len(cals), "Connected, %d calendar found", "Connected, %d calendars found"), len(cals)))
					testResult.SetTextColor(walk.RGB(0x0f, 0x7b, 0x3f))
					fillCalendars(core.CalendarChoices(cals, s.HiddenCollections)) // keeps choices made so far
					fresh = true
				})
			}()
		}
		testBtn = pushButton(testRow, i18n.T("Test connection"), 140, runTest)
		testResult = caption(testRow, "")
		testRow.Layout().(*walk.BoxLayout).SetStretchFactor(testResult, 1)

		cals := formCard(body)
		label(cals, i18n.T("Calendars to sync"), uiFont(10, true), flText)
		caption(cals, i18n.T("Turned-off calendars are not downloaded at all. Which synced calendars are shown is chosen with the "+
			"Calendars button in the main window."))
		calList = box(cals, true, walk.Margins{VNear: 4}, 8)

		syncCard := formCard(body)
		interval, _ = walk.NewComboBox(formRow(syncCard, i18n.T("Sync")))
		labels := make([]string, len(core.SyncIntervals))
		idx := 2
		for i, it := range core.SyncIntervals {
			labels[i] = i18n.T(it.Label)
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
	ok := pushButton(bottom, i18n.T("OK"), 96, func() {
		s := collect()
		if s.Name == "" {
			walk.MsgBox(dlg, appinfo.Name, i18n.T("Please enter a name."), walk.MsgBoxIconWarning)
			return
		}
		if isCalDAV && (s.URL == "" || s.Username == "") {
			walk.MsgBox(dlg, appinfo.Name, i18n.T("Server URL and username are required."), walk.MsgBoxIconWarning)
			return
		}
		if _, anyShown := core.HiddenCalendars(shownChoices()); isCalDAV && fresh && len(choices) > 0 && !anyShown {
			walk.MsgBox(dlg, appinfo.Name, i18n.T("Turn on at least one calendar, or turn off Enabled to hide the whole account."), walk.MsgBoxIconWarning)
			return
		}
		if err := a.store.SaveSource(s); err != nil {
			walk.MsgBox(dlg, i18n.T("Error"), err.Error(), walk.MsgBoxIconError)
			return
		}
		*src = *s
		acceptLater(dlg)
	})
	cancel := pushButton(bottom, i18n.T("Cancel"), 96, func() { dlg.Cancel() })
	dlg.SetDefaultButton(ok)
	dlg.SetCancelButton(cancel)

	a.applyIcon(dlg)
	if dialogHook != nil {
		dlg.Starting().Attach(func() { dialogHook("account", dlg) })
	}
	name.SetFocus()
	return dlg.Run() == walk.DlgCmdOK
}
