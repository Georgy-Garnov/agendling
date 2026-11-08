//go:build windows

package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/update"
)

// Settings dialog geometry, in 1/96".
const (
	setNavWidth     = 260
	setColumnMax    = 760
	setPageMarginH  = 32
	setBottomHeight = 64
)

var navGlyphs = map[string]string{
	"calendars": glyphCalendar, "reminders": glyphAlarm, "sound": glyphVolume,
	"alerts": glyphMail, "startup": glyphPower, "about": glyphInfo,
}

// settingsHook, when set (by the screenshot tour), is called once the Settings dialog is shown.
var settingsHook func(s *settingsUI)

// dialogHook, when set (by the screenshot tour), is called once a dialog is shown.
var dialogHook func(name string, dlg *walk.Dialog)

// settingsUI is the Settings dialog: sections with a search box on the left, the selected
// section on the right, OK / Cancel / Apply at the bottom.
type settingsUI struct {
	a        *App
	dlg      *walk.Dialog
	search   *walk.LineEdit
	nav      *navList
	scroll   *walk.ScrollView
	column   *walk.Composite
	title    *walk.Label
	intro    *walk.TextLabel
	pages    map[string]*walk.Composite
	accounts *walk.Composite
	status   *walk.Label
	okBtn    *walk.PushButton
	applyBtn *walk.PushButton

	saved                        model.AppSettings
	rems, soundPath              *walk.LineEdit
	snooze                       *walk.NumberEdit
	loop, newAlerts, startup     *toggleSwitch
	minimized                    *toggleSwitch
	checkBtn                     *walk.PushButton
	updText                      *walk.TextLabel
	updLink                      *walk.LinkLabel
	sourcesChanged, workersStale bool
	closed                       bool
}

func (a *App) showSettings() {
	s := &settingsUI{a: a, saved: a.store.Settings(), pages: map[string]*walk.Composite{}}
	if err := s.build(); err != nil {
		log.Printf("settings: %v", err)
		return
	}
	a.applyIcon(s.dlg)
	s.dlg.Starting().Attach(func() {
		// Dialog.Show sizes to the minimum; open at the comfortable size, centred on the owner.
		w, h := scale(s.dlg, 980), scale(s.dlg, 680)
		ob := a.mw.BoundsPixels()
		s.dlg.SetBoundsPixels(walk.Rectangle{X: ob.X + (ob.Width-w)/2, Y: ob.Y + (ob.Height-h)/2, Width: w, Height: h})
	})
	if settingsHook != nil {
		s.dlg.Starting().Attach(func() { settingsHook(s) })
	}
	s.dlg.Run()
	s.closed = true
	a.audio.Stop()
	if s.workersStale {
		a.engine.Restart()
	}
	if s.sourcesChanged {
		a.view.Reload()
	}
}

// ---------- building blocks ----------

func box(parent walk.Container, vertical bool, margins walk.Margins, spacing int) *walk.Composite {
	c, _ := walk.NewComposite(parent)
	var l *walk.BoxLayout
	if vertical {
		l = walk.NewVBoxLayout()
	} else {
		l = walk.NewHBoxLayout()
	}
	l.SetMargins(margins)
	l.SetSpacing(spacing)
	if vertical {
		l.SetAlignment(walk.AlignHNearVNear)
	} else {
		l.SetAlignment(walk.AlignHNearVCenter)
	}
	c.SetLayout(l)
	c.SetBackground(walk.NullBrush())
	return c
}

func label(parent walk.Container, text string, font *walk.Font, color walk.Color) *walk.Label {
	l, _ := walk.NewLabel(parent)
	l.SetText(text)
	l.SetFont(font)
	l.SetTextColor(color)
	l.SetBackground(walk.NullBrush())
	return l
}

func caption(parent walk.Container, text string) *walk.TextLabel {
	l, _ := walk.NewTextLabel(parent)
	l.SetText(text)
	l.SetFont(uiFont(9, false))
	l.SetTextColor(flText2)
	l.SetBackground(walk.NullBrush())
	// TextLabel measures its minimum at this width; without it the text never wraps.
	l.SetMinMaxSize(walk.Size{Width: 160}, walk.Size{})
	return l
}

func pushButton(parent walk.Container, text string, minWidth int, onClick func()) *walk.PushButton {
	b, _ := walk.NewPushButton(parent)
	b.SetText(text)
	if minWidth > 0 {
		b.SetMinMaxSize(walk.Size{Width: minWidth, Height: 32}, walk.Size{Width: minWidth, Height: 32})
	} else {
		b.SetMinMaxSize(walk.Size{Height: 32}, walk.Size{Height: 32})
	}
	b.Clicked().Attach(onClick)
	return b
}

// card adds a rounded white card to the page: the setting's name and description on the left,
// and returns the container for the control on the right (or for wide controls below the text).
func (s *settingsUI) card(page *walk.Composite, name, desc string, below bool) *walk.Composite {
	c := box(page, true, walk.Margins{HNear: 16, VNear: 12, HFar: 16, VFar: 12}, 8)
	makeCard(c, flWindow)
	top := box(c, false, walk.Margins{}, 16)
	text := box(top, true, walk.Margins{}, 2)
	walk.NewHSpacer(text) // the text column takes the free width; the control sits at the right
	label(text, name, uiFont(10, false), flText)
	if desc != "" {
		caption(text, desc)
	}
	if below {
		return box(c, false, walk.Margins{}, 8)
	}
	return top
}

func (s *settingsUI) addPage(key string) *walk.Composite {
	p := box(s.column, true, walk.Margins{}, 6)
	p.SetVisible(false)
	s.pages[key] = p
	return p
}

// ---------- the dialog ----------

func (s *settingsUI) build() error {
	a := s.a
	dlg, err := walk.NewDialog(a.mw)
	if err != nil {
		return err
	}
	s.dlg = dlg
	dlg.SetTitle("Settings")
	dlg.SetFont(uiFont(10, false))
	dlg.SetBackground(solid(flWindow))
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{})
	root.SetSpacing(0)
	dlg.SetLayout(root)
	dlg.SetMinMaxSize(walk.Size{Width: 820, Height: 560}, walk.Size{})

	main := box(dlg, false, walk.Margins{}, 0)
	main.Layout().(*walk.BoxLayout).SetAlignment(walk.AlignHNearVNear)
	root.SetStretchFactor(main, 1)

	// Left: search and sections.
	navPanel := box(main, true, walk.Margins{HNear: 12, VNear: 14, HFar: 12, VFar: 12}, 12)
	navPanel.SetBackground(solid(flNav))
	navPanel.SetMinMaxSize(walk.Size{Width: setNavWidth}, walk.Size{Width: setNavWidth})
	s.buildSearch(navPanel)
	s.nav, err = newNavList(navPanel, s.showPage)
	if err != nil {
		return err
	}
	navPanel.Layout().(*walk.BoxLayout).SetStretchFactor(s.nav, 1)
	line, _ := walk.NewComposite(main)
	line.SetBackground(solid(flLine))
	line.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 1})

	// Right: the selected page in a scrollable column.
	s.scroll, _ = walk.NewScrollView(main)
	// With both scrollbars allowed the view fills the remaining width instead of sizing itself to
	// the widest page; the column below gets an explicit width so nothing scrolls sideways.
	s.scroll.SetScrollbars(true, true)
	s.scroll.SetBackground(solid(flWindow))
	sl := walk.NewVBoxLayout()
	sl.SetMargins(walk.Margins{HNear: setPageMarginH, VNear: 22, HFar: setPageMarginH, VFar: 24})
	sl.SetSpacing(0)
	sl.SetAlignment(walk.AlignHNearVNear)
	s.scroll.SetLayout(sl)
	s.column = box(s.scroll, true, walk.Margins{}, 6)
	s.title = label(s.column, "", uiFont(20, true), flText)
	s.intro = caption(s.column, "")
	s.intro.SetFont(uiFont(10, false))
	spacer, _ := walk.NewComposite(s.column)
	spacer.SetMinMaxSize(walk.Size{Height: 12}, walk.Size{Height: 12})
	spacer.SetBackground(walk.NullBrush())

	s.buildCalendars()
	s.buildReminders()
	s.buildSound()
	s.buildAlerts()
	s.buildStartup()
	s.buildAbout()
	walk.NewVSpacer(s.column) // pages keep their natural height

	// Bottom: status and OK / Cancel / Apply.
	sep, _ := walk.NewComposite(dlg)
	sep.SetBackground(solid(flLine))
	sep.SetMinMaxSize(walk.Size{Height: 1}, walk.Size{Height: 1})
	bottom := box(dlg, false, walk.Margins{HNear: 20, VNear: 14, HFar: 20, VFar: 14}, 8)
	bottom.SetBackground(solid(flSurface))
	bottom.SetMinMaxSize(walk.Size{Height: setBottomHeight}, walk.Size{Height: setBottomHeight})
	s.status = label(bottom, "", uiFont(9, false), flText2)
	walk.NewHSpacer(bottom)
	ok := pushButton(bottom, "OK", 96, func() {
		if s.apply() {
			acceptLater(dlg)
		}
	})
	s.okBtn = ok
	cancel := pushButton(bottom, "Cancel", 96, func() { dlg.Cancel() })
	s.applyBtn = pushButton(bottom, "Apply", 96, func() { s.apply() })
	dlg.SetDefaultButton(ok)
	dlg.SetCancelButton(cancel)

	// Explicit widths keep both columns still while switching sections (a ScrollView without a
	// horizontal scrollbar would otherwise size itself to the widest page).
	resize := func() {
		cw := dlg.ClientBoundsPixels().Width
		right := cw - scale(dlg, setNavWidth) - scale(dlg, 1)
		col := right - 2*scale(dlg, setPageMarginH) - int(win.GetSystemMetrics(win.SM_CXVSCROLL))
		col = min(col, scale(dlg, setColumnMax))
		s.column.SetMinMaxSizePixels(walk.Size{Width: col}, walk.Size{Width: col})
	}
	dlg.SizeChanged().Attach(resize)
	resize()

	s.filter()
	s.nav.choose(core.SettingsPages[0].Key)
	s.updateApply()
	return nil
}

func (s *settingsUI) buildSearch(parent walk.Container) {
	wrap := box(parent, false, walk.Margins{HNear: 10, VNear: 0, HFar: 6, VFar: 0}, 6)
	wrap.SetMinMaxSize(walk.Size{Height: 34}, walk.Size{Height: 34})
	var bmp *walk.Bitmap
	var brush *walk.BitmapBrush
	paint := func() {
		r := wrap.ClientBoundsPixels()
		if r.Width <= 0 {
			return
		}
		nb, err := roundedBitmap(r.Width, r.Height, wrap.DPI(), scale(wrap, 6), flNav, flSurface, walk.RGB(0xd0, 0xd5, 0xdd), true)
		if err != nil {
			return
		}
		nbr, _ := walk.NewBitmapBrush(nb)
		wrap.SetBackground(nbr)
		if brush != nil {
			brush.Dispose()
			bmp.Dispose()
		}
		bmp, brush = nb, nbr
	}
	wrap.SizeChanged().Attach(paint)
	label(wrap, glyphSearch, iconFont(10), flText2)
	s.search, _ = walk.NewLineEdit(wrap)
	s.search.SetCueBanner("Search settings")
	// Borderless, so it sits inside the rounded field.
	ex := win.GetWindowLong(s.search.Handle(), win.GWL_EXSTYLE)
	win.SetWindowLong(s.search.Handle(), win.GWL_EXSTYLE, ex&^win.WS_EX_CLIENTEDGE)
	win.SetWindowPos(s.search.Handle(), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED)
	wrap.Layout().(*walk.BoxLayout).SetStretchFactor(s.search, 1)
	s.search.TextChanged().Attach(s.filter)
	s.search.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyDown {
			s.nav.SetFocus()
		}
	})
}

// filter lists the sections matching the search; when the current one drops out, the first
// match is shown.
func (s *settingsUI) filter() {
	if s.nav == nil {
		return
	}
	matches := core.FilterSettingsPages(s.search.Text())
	items := make([]navItem, len(matches))
	keep := false
	for i, p := range matches {
		items[i] = navItem{Key: p.Key, Title: p.Title, Glyph: navGlyphs[p.Key]}
		keep = keep || p.Key == s.nav.selected
	}
	s.nav.setItems(items)
	if !keep && len(items) > 0 {
		s.nav.choose(items[0].Key)
	}
}

func (s *settingsUI) showPage(key string) {
	s.dlg.SetSuspended(true)
	defer s.dlg.SetSuspended(false)
	for k, p := range s.pages {
		p.SetVisible(k == key)
	}
	for _, p := range core.SettingsPages {
		if p.Key == key {
			s.title.SetText(p.Title)
			s.intro.SetText(p.Intro)
		}
	}
	s.scroll.SetFocus()
}

// selectPage shows a section, as if picked in the list (used by screenshot tests).
func (s *settingsUI) selectPage(key string) { s.nav.choose(key) }

// ---------- pages ----------

func (s *settingsUI) buildCalendars() {
	a := s.a
	p := s.addPage("calendars")
	actions := box(p, false, walk.Margins{VFar: 4}, 8)
	newFlatButton(actions, glyphAdd, "Add CalDAV account", btnGhost, flWindow, func() {
		s.editSource(&model.CalendarSource{Type: model.SourceCalDAV, Enabled: true, SyncInterval: 15, Color: "#0b8043"})
	})
	newFlatButton(actions, glyphAdd, "Add local calendar", btnGhost, flWindow, func() {
		s.editSource(&model.CalendarSource{Type: model.SourceLocal, Enabled: true, Color: "#8e24aa"})
	})
	gap, _ := walk.NewHSpacer(actions)
	actions.Layout().(*walk.BoxLayout).SetStretchFactor(gap, 1)
	newFlatButton(actions, glyphRefresh, "Sync now", btnGhost, flWindow, func() {
		if s.workersStale {
			a.engine.Restart() // starts workers for new sources and syncs all
			s.workersStale = false
		} else {
			a.engine.SyncNow()
		}
		s.status.SetText("Syncing…")
		// A first sync of a new account (discovery included) can take a while.
		for _, d := range []time.Duration{3 * time.Second, 10 * time.Second} {
			time.AfterFunc(d, func() {
				s.dlg.Synchronize(func() {
					if !s.closed {
						s.reloadAccounts()
						s.status.SetText("")
					}
				})
			})
		}
	})
	s.accounts = box(p, true, walk.Margins{}, 6)
	s.reloadAccounts()
	c := caption(p, "Which calendars are shown is chosen with the Calendars button in the main window; an account's "+
		"Edit dialog chooses which of its calendars are synced.")
	c.SetFont(uiFont(9, false))
}

func (s *settingsUI) editSource(src *model.CalendarSource) {
	if s.a.editSource(s.dlg, src) {
		s.sourcesChanged, s.workersStale = true, true
		s.reloadAccounts()
	}
}

// reloadAccounts rebuilds one card per calendar account.
func (s *settingsUI) reloadAccounts() {
	a := s.a
	s.accounts.SetSuspended(true)
	defer s.accounts.SetSuspended(false)
	for s.accounts.Children().Len() > 0 {
		s.accounts.Children().At(0).Dispose()
	}
	srcs, _ := a.store.Sources()
	for _, src := range srcs {
		src := src
		c := box(s.accounts, false, walk.Margins{HNear: 16, VNear: 12, HFar: 12, VFar: 12}, 12)
		makeCard(c, flWindow)
		color := ParseColor(src.Color)
		newColorDot(c, color, flSurface)
		text := box(c, true, walk.Margins{}, 2)
		walk.NewHSpacer(text) // the text column takes the free width
		label(text, src.Name, uiFont(10, true), flText)
		info := "Local calendar"
		if !src.IsLocal() {
			info = "CalDAV"
			if n := len(caldav.KnownCalendars(a.store, src.ID)); n > 1 {
				info += fmt.Sprintf(" · %d calendars", n)
			}
			info += " · " + core.SourceStatus(src)
		}
		if !src.Enabled {
			info += " · disabled"
		}
		l := caption(text, info)
		if src.LastError != "" {
			l.SetTextColor(walk.RGB(0xc4, 0x2b, 0x1c))
		}
		pushButton(c, "Edit…", 80, func() { s.editSource(src) })
		pushButton(c, "Remove", 80, func() { s.removeSource(src) })
	}
}

func (s *settingsUI) removeSource(src *model.CalendarSource) {
	msg := fmt.Sprintf("Remove calendar \"%s\"?", src.Name)
	if src.IsLocal() {
		msg += "\n\nAll its events will be permanently deleted."
	} else {
		msg += "\n\nLocal copies of its events are removed; the server is not changed."
	}
	if walk.MsgBox(s.dlg, "Remove calendar", msg, walk.MsgBoxOKCancel|walk.MsgBoxIconWarning) != walk.DlgCmdOK {
		return
	}
	if err := s.a.store.DeleteSource(src.ID); err != nil {
		walk.MsgBox(s.dlg, "Error", err.Error(), walk.MsgBoxIconError)
	}
	s.sourcesChanged, s.workersStale = true, true
	s.reloadAccounts()
}

func (s *settingsUI) buildReminders() {
	p := s.addPage("reminders")
	right := s.card(p, "Default reminders", "Used for events without reminders of their own. Several values separated by "+
		"commas; units m, h, d, w (e.g. 15, 1h, 1d).", false)
	s.rems, _ = walk.NewLineEdit(right)
	s.rems.SetText(core.FormatReminders(s.saved.DefaultReminderMinutes))
	s.rems.SetCueBanner("e.g. 15, 1h, 1d")
	s.rems.SetMinMaxSize(walk.Size{Width: 160, Height: 30}, walk.Size{Width: 160, Height: 30})
	s.rems.TextChanged().Attach(s.updateApply)

	right = s.card(p, "Snooze interval", "How many minutes Snooze postpones a reminder.", false)
	s.snooze, _ = walk.NewNumberEdit(right)
	s.snooze.SetRange(1, 1440)
	s.snooze.SetDecimals(0)
	s.snooze.SetValue(float64(s.saved.SnoozeIntervalMinutes))
	s.snooze.SetSpinButtonsVisible(true)
	s.snooze.SetMinMaxSize(walk.Size{Width: 90, Height: 30}, walk.Size{Width: 90, Height: 30})
	s.snooze.ValueChanged().Attach(s.updateApply)
}

func (s *settingsUI) buildSound() {
	a := s.a
	p := s.addPage("sound")
	row := s.card(p, "Alert sound", "An MP3 or WAV file; leave empty for the built-in chime.", true)
	s.soundPath, _ = walk.NewLineEdit(row)
	s.soundPath.SetText(s.saved.CustomSoundPath)
	s.soundPath.SetCueBanner("Built-in chime")
	s.soundPath.SetMinMaxSize(walk.Size{Height: 30}, walk.Size{Height: 30})
	row.Layout().(*walk.BoxLayout).SetStretchFactor(s.soundPath, 1)
	s.soundPath.TextChanged().Attach(s.updateApply)
	pushButton(row, "Browse…", 90, func() {
		fd := &walk.FileDialog{Title: "Choose alert sound", Filter: "Audio files (*.mp3;*.wav)|*.mp3;*.wav|All files (*.*)|*.*"}
		if ok, _ := fd.ShowOpen(s.dlg); ok {
			s.soundPath.SetText(fd.FilePath)
		}
	})
	pushButton(row, "Clear", 80, func() { s.soundPath.SetText("") })

	right := s.card(p, "Try it", "Plays the chosen sound once.", false)
	pushButton(right, "▶  Test", 90, func() {
		path := strings.TrimSpace(s.soundPath.Text())
		go func() {
			if err := a.audio.Play(path, false); err != nil {
				s.dlg.Synchronize(func() { walk.MsgBox(s.dlg, "Sound", err.Error(), walk.MsgBoxIconWarning) })
			}
		}()
	})
	pushButton(right, "■  Stop", 90, a.audio.Stop)

	right = s.card(p, "Repeat until stopped", "Keep playing until you press Stop Sound, at most 3 minutes.", false)
	s.loop, _ = newToggle(right, s.saved.LoopSound, flSurface, s.updateApply)
}

func (s *settingsUI) buildAlerts() {
	p := s.addPage("alerts")
	right := s.card(p, "Alert about new meetings", "Play the sound and show a popup when a sync brings in a new upcoming "+
		"event, e.g. an invitation. Calendars hidden in the main window stay quiet.", false)
	s.newAlerts, _ = newToggle(right, !s.saved.NewEventAlertsOff, flSurface, s.updateApply)
}

func (s *settingsUI) buildStartup() {
	p := s.addPage("startup")
	right := s.card(p, "Run at Windows startup", "Start Agendling when you sign in, so reminders are never missed.", false)
	s.startup, _ = newToggle(right, s.saved.RunAtStartup, flSurface, s.updateApply)
	right = s.card(p, "Start minimized", "Start hidden in the system tray.", false)
	s.minimized, _ = newToggle(right, s.saved.StartMinimized, flSurface, s.updateApply)
	caption(p, "Closing the main window keeps Agendling running in the tray; use Exit in the tray menu to quit.")
}

func (s *settingsUI) buildAbout() {
	p := s.addPage("about")

	head := box(p, false, walk.Margins{HNear: 16, VNear: 14, HFar: 16, VFar: 14}, 16)
	makeCard(head, flWindow)
	if logo, err := png.Decode(bytes.NewReader(appinfo.LogoPNG)); err == nil {
		// Flatten onto the card colour: the logo has transparent edges.
		flat := image.NewRGBA(logo.Bounds())
		draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), logo, logo.Bounds().Min, draw.Over)
		newPainted(head, 0, walk.Size{Width: 56, Height: 56}, sizeFixed, func(cv *walk.Canvas, b walk.Rectangle) {
			if bmp, err := walk.NewBitmapFromImageForDPI(flat, 96); err == nil {
				cv.DrawImageStretchedPixels(bmp, b)
				bmp.Dispose()
			}
		})
	}
	text := box(head, true, walk.Margins{}, 2)
	walk.NewHSpacer(text)
	label(text, appinfo.Name, uiFont(16, true), flText)
	label(text, "Version "+appinfo.Version, uiFont(10, false), flText)
	caption(text, "Desktop calendar with CalDAV sync and reminders.")

	below := s.card(p, "Updates", "Asks GitHub for the latest release, only when you press the button. Nothing is "+
		"downloaded: a newer version is offered as a link to its release page.", true)
	s.checkBtn = pushButton(below, "Check for updates", 150, s.checkUpdates)
	res := box(below, true, walk.Margins{}, 2)
	below.Layout().(*walk.BoxLayout).SetStretchFactor(res, 1)
	s.updText = caption(res, "")
	s.updText.SetFont(uiFont(10, false))
	s.updText.SetTextColor(flText)
	s.updLink, _ = walk.NewLinkLabel(res)
	s.updLink.SetBackground(walk.NullBrush())
	s.updLink.SetVisible(false)
	s.updLink.LinkActivated().Attach(func(l *walk.LinkLabelLink) { OpenURL(l.URL()) })

	below = s.card(p, "Project page", "Source code, releases and issue tracker.", true)
	link, _ := walk.NewLinkLabel(below)
	link.SetBackground(walk.NullBrush())
	link.SetText(`<a href="` + appinfo.Homepage + `">` + appinfo.Homepage + `</a>`)
	link.LinkActivated().Attach(func(l *walk.LinkLabelLink) { OpenURL(l.URL()) })

	if dir, err := appinfo.DataDir(); err == nil {
		below = s.card(p, "Data folder", "Calendars, settings and the log file.", true)
		path, _ := walk.NewLineEdit(below)
		path.SetText(dir)
		path.SetReadOnly(true)
		path.SetMinMaxSize(walk.Size{Height: 30}, walk.Size{Height: 30})
		below.Layout().(*walk.BoxLayout).SetStretchFactor(path, 1)
		pushButton(below, "Open folder", 110, func() { OpenFolder(dir) })
	}
}

func (s *settingsUI) checkUpdates() {
	s.checkBtn.SetEnabled(false)
	s.updText.SetText("Checking…")
	s.updLink.SetVisible(false)
	go func() {
		rel, err := update.Check(context.Background(), appinfo.Version)
		s.dlg.Synchronize(func() {
			if s.closed {
				return
			}
			s.checkBtn.SetEnabled(true)
			text, link := updateResult(rel, err)
			s.updText.SetText(text)
			if link != "" {
				s.updLink.SetText(link)
				s.updLink.SetVisible(true)
			}
		})
	}()
}

// updateResult describes the result of an update check and, when there is one, a link to the
// release page in LinkLabel markup.
func updateResult(rel update.Release, err error) (text, link string) {
	switch {
	case err != nil:
		return "Could not check for updates: " + err.Error() + ".", `<a href="` + update.ReleasesPage + `">Open the releases page</a>`
	case rel.Newer:
		return "Version " + rel.Tag + " is available.", `<a href="` + rel.URL + `">Open the release page</a>`
	case appinfo.Version == "dev" || !strings.HasPrefix(appinfo.Version, "v"):
		return "This is a development build. The latest release is " + rel.Tag + ".", `<a href="` + rel.URL + `">Open the release page</a>`
	}
	return "You have the latest version (" + rel.Tag + ").", ""
}

// ---------- apply ----------

func (s *settingsUI) collect() (model.AppSettings, error) {
	st := s.a.store.Settings() // keeps settings changed outside the dialog, e.g. calendar visibility
	mins, err := core.ParseReminders(s.rems.Text())
	if err != nil {
		return st, fmt.Errorf("invalid default reminders: %w", err)
	}
	st.DefaultReminderMinutes = mins
	st.SnoozeIntervalMinutes = int(s.snooze.Value())
	st.CustomSoundPath = strings.TrimSpace(s.soundPath.Text())
	st.LoopSound = s.loop.Checked()
	st.NewEventAlertsOff = !s.newAlerts.Checked()
	st.RunAtStartup = s.startup.Checked()
	st.StartMinimized = s.minimized.Checked()
	return st, nil
}

func (s *settingsUI) updateApply() {
	if s.applyBtn == nil || s.rems == nil || s.snooze == nil || s.soundPath == nil || s.loop == nil ||
		s.newAlerts == nil || s.startup == nil || s.minimized == nil {
		return // still being built
	}
	st, err := s.collect()
	s.applyBtn.SetEnabled(err != nil || !core.SameEditableSettings(st, s.saved))
}

func (s *settingsUI) apply() bool {
	st, err := s.collect()
	if err != nil {
		walk.MsgBox(s.dlg, appinfo.Name, err.Error(), walk.MsgBoxIconWarning)
		return false
	}
	if st.RunAtStartup != s.saved.RunAtStartup {
		if err := SetRunAtStartup(st.RunAtStartup); err != nil {
			walk.MsgBox(s.dlg, "Startup", "Could not update startup registration: "+err.Error(), walk.MsgBoxIconWarning)
		}
	}
	if err := s.a.store.SaveSettings(st); err != nil {
		walk.MsgBox(s.dlg, "Error", err.Error(), walk.MsgBoxIconError)
		return false
	}
	s.saved = st
	s.updateApply()
	s.status.SetText("Settings saved")
	return true
}
