//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"

	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

func hint(text string) *gtk.Label {
	l := markupLabel(`<span foreground="#6e6e6e">` + glib.MarkupEscapeText(text) + `</span>`)
	return l
}

func grid() *gtk.Grid {
	g, _ := gtk.GridNew()
	g.SetRowSpacing(8)
	g.SetColumnSpacing(10)
	g.SetMarginStart(12)
	g.SetMarginEnd(12)
	g.SetMarginTop(12)
	g.SetMarginBottom(12)
	return g
}

func rightLabel(s string) *gtk.Label {
	l, _ := gtk.LabelNew(s)
	l.SetXAlign(1)
	return l
}

func (a *App) showSettings() {
	st := a.store.Settings()
	dlg, _ := gtk.DialogNew()
	dlg.SetTitle("Settings")
	dlg.SetTransientFor(a.win)
	dlg.SetModal(true)
	dlg.SetDefaultSize(660, 480)
	dlg.AddButton("Cancel", gtk.RESPONSE_CANCEL)
	ok, _ := dlg.AddButton("OK", gtk.RESPONSE_OK)
	okCtx, _ := ok.GetStyleContext()
	okCtx.AddClass("suggested-action")
	dlg.SetDefaultResponse(gtk.RESPONSE_OK)

	nb, _ := gtk.NotebookNew()
	sourcesChanged := false

	// ----- Calendars tab -----
	const (
		colName = iota
		colType
		colStatus
		colColor
		colID
	)
	store, _ := gtk.ListStoreNew(glib.TYPE_STRING, glib.TYPE_STRING, glib.TYPE_STRING, glib.TYPE_STRING, glib.TYPE_INT64)
	var sources []*model.CalendarSource
	reload := func() {
		store.Clear()
		sources, _ = a.store.Sources()
		for _, s := range sources {
			typ := "CalDAV"
			if s.IsLocal() {
				typ = "Local"
			}
			_ = store.Set(store.Append(), []int{colName, colType, colStatus, colColor, colID},
				[]interface{}{s.Name, typ, core.SourceStatus(s), core.ParseColor(s.Color).Blend(0.55).Hex(), s.ID})
		}
	}
	reload()
	tv, _ := gtk.TreeViewNewWithModel(store)
	addColumn := func(title string, col int, width int, colored bool) {
		r, _ := gtk.CellRendererTextNew()
		c, _ := gtk.TreeViewColumnNewWithAttribute(title, r, "text", col)
		if colored {
			c.AddAttribute(r, "cell-background", colColor)
		}
		c.SetMinWidth(width)
		c.SetResizable(true)
		tv.AppendColumn(c)
	}
	addColumn("Name", colName, 170, true)
	addColumn("Type", colType, 70, false)
	addColumn("Status", colStatus, 300, false)
	selected := func() *model.CalendarSource {
		sel, _ := tv.GetSelection()
		m, iter, ok := sel.GetSelected()
		if !ok {
			return nil
		}
		val, err := m.(*gtk.ListStore).GetValue(iter, colID)
		if err != nil {
			return nil
		}
		id, _ := val.GoValue()
		for _, s := range sources {
			if s.ID == id.(int64) {
				return s
			}
		}
		return nil
	}
	edit := func(src *model.CalendarSource) {
		if a.editSource(dlg, src) {
			sourcesChanged = true
			reload()
		}
	}
	tv.Connect("row-activated", func() {
		if s := selected(); s != nil {
			edit(s)
		}
	})
	sw, _ := gtk.ScrolledWindowNew(nil, nil)
	sw.SetShadowType(gtk.SHADOW_IN)
	sw.SetVExpand(true)
	sw.Add(tv)

	btns, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	btns.PackStart(button("Add CalDAV account…", func() {
		edit(&model.CalendarSource{Type: model.SourceCalDAV, Enabled: true, SyncInterval: 15, Color: "#0b8043"})
	}), false, false, 0)
	btns.PackStart(button("Add local calendar…", func() {
		edit(&model.CalendarSource{Type: model.SourceLocal, Enabled: true, Color: "#8e24aa"})
	}), false, false, 0)
	btns.PackStart(button("Edit…", func() {
		if s := selected(); s != nil {
			edit(s)
		}
	}), false, false, 0)
	btns.PackStart(button("Remove", func() {
		s := selected()
		if s == nil {
			return
		}
		msg := fmt.Sprintf("Remove calendar \"%s\"?\n\n", s.Name)
		if s.IsLocal() {
			msg += "All its events will be permanently deleted."
		} else {
			msg += "Local copies of its events are removed; the server is not changed."
		}
		if !a.confirm(dlg, msg) {
			return
		}
		if err := a.store.DeleteSource(s.ID); err != nil {
			a.message(dlg, gtk.MESSAGE_ERROR, err.Error())
		}
		sourcesChanged = true
		reload()
	}), false, false, 0)
	btns.PackEnd(button("Sync now", func() {
		a.svc.Engine.SyncNow()
		glib.TimeoutAdd(3000, func() bool { reload(); return false })
	}), false, false, 0)

	calPage, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	calPage.SetMarginStart(12)
	calPage.SetMarginEnd(12)
	calPage.SetMarginTop(12)
	calPage.SetMarginBottom(12)
	calPage.PackStart(sw, true, true, 0)
	calPage.PackStart(btns, false, false, 0)
	calTab, _ := gtk.LabelNew("Calendars")
	nb.AppendPage(calPage, calTab)

	// ----- Notifications tab -----
	ng := grid()
	rems, _ := gtk.EntryNew()
	rems.SetText(core.FormatReminders(st.DefaultReminderMinutes))
	rems.SetPlaceholderText("e.g. 15, 1h, 1d")
	rems.SetHExpand(true)
	ng.Attach(rightLabel("Default reminders:"), 0, 0, 1, 1)
	ng.Attach(rems, 1, 0, 1, 1)
	ng.Attach(hint("Used for events without their own reminders. Units: m, h, d, w."), 1, 1, 1, 1)

	snooze, _ := gtk.SpinButtonNewWithRange(1, 1440, 1)
	snooze.SetValue(float64(st.SnoozeIntervalMinutes))
	snooze.SetHAlign(gtk.ALIGN_START)
	ng.Attach(rightLabel("Snooze interval (min):"), 0, 2, 1, 1)
	ng.Attach(snooze, 1, 2, 1, 1)

	soundBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	sound, _ := gtk.EntryNew()
	sound.SetText(st.CustomSoundPath)
	sound.SetPlaceholderText("built-in chime")
	sound.SetHExpand(true)
	soundBox.PackStart(sound, true, true, 0)
	soundBox.PackStart(button("Browse…", func() {
		fc, err := gtk.FileChooserDialogNewWith2Buttons("Choose alert sound", dlg, gtk.FILE_CHOOSER_ACTION_OPEN,
			"Cancel", gtk.RESPONSE_CANCEL, "Open", gtk.RESPONSE_ACCEPT)
		if err != nil {
			return
		}
		filter, _ := gtk.FileFilterNew()
		filter.SetName("Audio files (*.mp3, *.wav)")
		filter.AddPattern("*.mp3")
		filter.AddPattern("*.wav")
		filter.AddPattern("*.MP3")
		filter.AddPattern("*.WAV")
		fc.AddFilter(filter)
		if fc.Run() == gtk.RESPONSE_ACCEPT {
			sound.SetText(fc.GetFilename())
		}
		fc.Destroy()
	}), false, false, 0)
	soundBox.PackStart(button("Clear", func() { sound.SetText("") }), false, false, 0)
	ng.Attach(rightLabel("Alert sound:"), 0, 3, 1, 1)
	ng.Attach(soundBox, 1, 3, 1, 1)

	testBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	testBox.PackStart(button("▶ Test", func() {
		path := strings.TrimSpace(entryText(sound))
		go func() {
			if err := a.svc.Audio.Play(path, false); err != nil {
				ui(func() { a.message(dlg, gtk.MESSAGE_WARNING, "Sound: "+err.Error()) })
			}
		}()
	}), false, false, 0)
	testBox.PackStart(button("■ Stop", a.svc.Audio.Stop), false, false, 0)
	ng.Attach(testBox, 1, 4, 1, 1)

	loop, _ := gtk.CheckButtonNewWithLabel("Repeat the sound until stopped (max 3 minutes)")
	loop.SetActive(st.LoopSound)
	ng.Attach(loop, 1, 5, 1, 1)
	newAlerts, _ := gtk.CheckButtonNewWithLabel("Notify (sound + popup) when a new event appears after sync")
	newAlerts.SetActive(!st.NewEventAlertsOff)
	ng.Attach(newAlerts, 1, 6, 1, 1)
	notifTab, _ := gtk.LabelNew("Notifications")
	nb.AppendPage(ng, notifTab)

	// ----- Behavior tab -----
	bg := grid()
	startup, _ := gtk.CheckButtonNewWithLabel("Run at login")
	startup.SetActive(st.RunAtStartup)
	minimized, _ := gtk.CheckButtonNewWithLabel("Start minimized to the system tray")
	minimized.SetActive(st.StartMinimized)
	bg.Attach(startup, 0, 0, 1, 1)
	bg.Attach(minimized, 0, 1, 1, 1)
	bg.Attach(hint("Closing the main window keeps the app running in the tray. Use Exit in the tray menu to quit.\n"+
		"On GNOME the tray icon needs the AppIndicator extension (enabled by default on Ubuntu)."), 0, 2, 1, 1)
	behTab, _ := gtk.LabelNew("Behavior")
	nb.AppendPage(bg, behTab)

	content, _ := dlg.GetContentArea()
	content.PackStart(nb, true, true, 0)
	dlg.ShowAll()

	for dlg.Run() == gtk.RESPONSE_OK {
		mins, err := core.ParseReminders(entryText(rems))
		if err != nil {
			a.message(dlg, gtk.MESSAGE_WARNING, "Invalid reminders: "+err.Error())
			continue
		}
		st := a.store.Settings()
		st.DefaultReminderMinutes = mins
		st.SnoozeIntervalMinutes = snooze.GetValueAsInt()
		st.CustomSoundPath = strings.TrimSpace(entryText(sound))
		st.LoopSound = loop.GetActive()
		st.NewEventAlertsOff = !newAlerts.GetActive()
		st.RunAtStartup = startup.GetActive()
		st.StartMinimized = minimized.GetActive()
		if err := setRunAtStartup(st.RunAtStartup); err != nil {
			a.message(dlg, gtk.MESSAGE_WARNING, "Could not update autostart: "+err.Error())
		}
		if err := a.store.SaveSettings(st); err != nil {
			a.message(dlg, gtk.MESSAGE_ERROR, err.Error())
			continue
		}
		break
	}
	dlg.Destroy()
	a.svc.Audio.Stop()
	if sourcesChanged {
		a.svc.Engine.Restart()
		a.view.reload()
	}
}

// editSource shows the calendar account dialog; it returns true when saved.
func (a *App) editSource(parent gtk.IWindow, src *model.CalendarSource) bool {
	isCalDAV := src.Type == model.SourceCalDAV
	dlg, _ := gtk.DialogNew()
	dlg.SetTransientFor(parent)
	dlg.SetModal(true)
	dlg.SetDefaultSize(560, -1)
	if isCalDAV {
		dlg.SetTitle("CalDAV account")
	} else {
		dlg.SetTitle("Local calendar")
	}
	dlg.AddButton("Cancel", gtk.RESPONSE_CANCEL)
	ok, _ := dlg.AddButton("OK", gtk.RESPONSE_OK)
	okCtx, _ := ok.GetStyleContext()
	okCtx.AddClass("suggested-action")
	dlg.SetDefaultResponse(gtk.RESPONSE_OK)

	g := grid()
	row := 0
	add := func(label string, w gtk.IWidget) {
		if label != "" {
			g.Attach(rightLabel(label), 0, row, 1, 1)
		}
		g.Attach(w, 1, row, 1, 1)
		row++
	}
	name, _ := gtk.EntryNew()
	name.SetText(src.Name)
	name.SetHExpand(true)
	add("Name:", name)

	c := core.ParseColor(src.Color)
	r, gg, b := c.Floats()
	colorBtn, _ := gtk.ColorButtonNewWithRGBA(gdk.NewRGBA(r, gg, b, 1))
	colorBtn.SetHAlign(gtk.ALIGN_START)
	add("Color:", colorBtn)

	var urlE, user, pass *gtk.Entry
	var interval *gtk.ComboBoxText
	if isCalDAV {
		urlE, _ = gtk.EntryNew()
		urlE.SetText(src.URL)
		urlE.SetPlaceholderText("https://caldav.example.com")
		add("Server URL:", urlE)
		add("", hint("Examples: Nextcloud — https://host/remote.php/dav · Fastmail — https://caldav.fastmail.com\n"+
			"A URL pointing at a single calendar collection syncs only that calendar."))
		user, _ = gtk.EntryNew()
		user.SetText(src.Username)
		add("Username:", user)
		pass, _ = gtk.EntryNew()
		pass.SetText(src.Password)
		pass.SetVisibility(false)
		pass.SetInputPurpose(gtk.INPUT_PURPOSE_PASSWORD)
		add("App password:", pass)
		interval, _ = gtk.ComboBoxTextNew()
		idx := 2
		for i, it := range core.SyncIntervals {
			interval.AppendText(it.Label)
			if it.Mins == src.SyncInterval {
				idx = i
			}
		}
		interval.SetActive(idx)
		add("Sync:", interval)
	}
	enabled, _ := gtk.CheckButtonNewWithLabel("Enabled (shown and synchronized)")
	enabled.SetActive(src.Enabled)

	collect := func() *model.CalendarSource {
		s := *src
		s.Name = strings.TrimSpace(entryText(name))
		rgba := colorBtn.GetRGBA()
		s.Color = core.RGB{R: uint8(rgba.GetRed()*255 + 0.5), G: uint8(rgba.GetGreen()*255 + 0.5), B: uint8(rgba.GetBlue()*255 + 0.5)}.Hex()
		s.Enabled = enabled.GetActive()
		if isCalDAV {
			s.URL = strings.TrimSpace(entryText(urlE))
			s.Username = strings.TrimSpace(entryText(user))
			s.Password = entryText(pass)
			if i := interval.GetActive(); i >= 0 {
				s.SyncInterval = core.SyncIntervals[i].Mins
			}
			if s.URL != src.URL {
				s.CalendarPath = "" // re-discover for a different server
			}
		}
		return &s
	}

	if isCalDAV {
		testRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
		result, _ := gtk.LabelNew("")
		result.SetXAlign(0)
		result.SetLineWrap(true)
		var testBtn *gtk.Button
		testBtn = button("Test connection", func() {
			s := collect()
			testBtn.SetSensitive(false)
			result.SetText("Connecting…")
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				n, err := caldav.TestConnection(ctx, s)
				ui(func() {
					testBtn.SetSensitive(true)
					if err != nil {
						result.SetText("✖ " + err.Error())
					} else {
						result.SetText(fmt.Sprintf("✔ Connected, %d calendar(s) found", n))
					}
				})
			}()
		})
		testRow.PackStart(testBtn, false, false, 0)
		testRow.PackStart(result, true, true, 0)
		add("", testRow)
	}
	add("", enabled)

	content, _ := dlg.GetContentArea()
	content.PackStart(g, true, true, 0)
	dlg.ShowAll()
	defer dlg.Destroy()
	for dlg.Run() == gtk.RESPONSE_OK {
		s := collect()
		if s.Name == "" {
			a.message(dlg, gtk.MESSAGE_WARNING, "Please enter a name.")
			continue
		}
		if isCalDAV && (s.URL == "" || s.Username == "") {
			a.message(dlg, gtk.MESSAGE_WARNING, "Server URL and username are required.")
			continue
		}
		if err := a.store.SaveSource(s); err != nil {
			a.message(dlg, gtk.MESSAGE_ERROR, err.Error())
			continue
		}
		*src = *s
		return true
	}
	return false
}
