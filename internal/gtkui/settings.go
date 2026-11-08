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

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/update"
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

func (a *App) showSettings() { a.showSettingsAt("") }

// showSettingsAt opens Settings on the given section ("" for the first).
func (a *App) showSettingsAt(page string) {
	saved := a.store.Settings()
	dlg, _ := gtk.DialogNew()
	dlg.SetTitle("Settings")
	addClass(dlg, "settings-dialog")
	dlg.SetTransientFor(a.win)
	dlg.SetModal(true)
	dlg.SetDefaultSize(940, 640)
	applyBtn, _ := dlg.AddButton("Apply", gtk.RESPONSE_APPLY)
	dlg.AddButton("Cancel", gtk.RESPONSE_CANCEL)
	ok, _ := dlg.AddButton("OK", gtk.RESPONSE_OK)
	okCtx, _ := ok.GetStyleContext()
	okCtx.AddClass("suggested-action")
	dlg.SetDefaultResponse(gtk.RESPONSE_OK)
	addClass := func(w interface {
		GetStyleContext() (*gtk.StyleContext, error)
	}, class string) {
		if ctx, err := w.GetStyleContext(); err == nil {
			ctx.AddClass(class)
		}
	}
	caption := func(text string) *gtk.Label {
		l, _ := gtk.LabelNew(text)
		l.SetXAlign(0)
		l.SetLineWrap(true)
		addClass(l, "settings-caption")
		return l
	}
	// card is one setting: its name and description on the left, the control on the right
	// (or below, for wide controls).
	card := func(name, desc string, control gtk.IWidget, below bool) *gtk.Box {
		c, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
		addClass(c, "settings-card")
		top, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
		text, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
		l, _ := gtk.LabelNew(name)
		l.SetXAlign(0)
		text.PackStart(l, false, false, 0)
		if desc != "" {
			text.PackStart(caption(desc), false, false, 0)
		}
		top.PackStart(text, true, true, 0)
		if control != nil && !below {
			w := control.ToWidget()
			w.SetVAlign(gtk.ALIGN_CENTER)
			top.PackEnd(control, false, false, 0)
		}
		c.PackStart(top, false, false, 0)
		if control != nil && below {
			c.PackStart(control, false, false, 0)
		}
		return c
	}

	sourcesChanged := false
	// Workers are only (re)started by Engine.Restart, so new or edited sources need one before
	// they sync; "Sync now" does it right away, otherwise it happens when the dialog closes.
	workersStale := false
	closed := false
	pages := map[string]*gtk.Box{}
	newPage := func(key string) *gtk.Box {
		b, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
		pages[key] = b
		return b
	}

	// ----- Calendars -----
	accounts, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	var reload func()
	edit := func(src *model.CalendarSource) {
		if a.editSource(dlg, src) {
			sourcesChanged, workersStale = true, true
			reload()
		}
	}
	remove := func(src *model.CalendarSource) {
		msg := fmt.Sprintf("Remove calendar \"%s\"?\n\n", src.Name)
		if src.IsLocal() {
			msg += "All its events will be permanently deleted."
		} else {
			msg += "Local copies of its events are removed; the server is not changed."
		}
		if !a.confirm(dlg, msg) {
			return
		}
		if err := a.store.DeleteSource(src.ID); err != nil {
			a.message(dlg, gtk.MESSAGE_ERROR, err.Error())
		}
		sourcesChanged, workersStale = true, true
		reload()
	}
	// One card per calendar account: colour, name, status, Edit and Remove.
	reload = func() {
		accounts.GetChildren().Foreach(func(w any) { w.(gtk.IWidget).ToWidget().Destroy() })
		srcs, _ := a.store.Sources()
		for _, src := range srcs {
			src := src
			c, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
			addClass(c, "settings-card")
			dot := markupLabel(`<span foreground="` + src.Color + `" size="large">●</span>`)
			dot.SetLineWrap(false)
			c.PackStart(dot, false, false, 0)
			text, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
			text.PackStart(markupLabel("<b>"+glib.MarkupEscapeText(src.Name)+"</b>"), false, false, 0)
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
			text.PackStart(caption(info), false, false, 0)
			c.PackStart(text, true, true, 0)
			rm := button("Remove", func() { remove(src) })
			rm.SetVAlign(gtk.ALIGN_CENTER)
			c.PackEnd(rm, false, false, 0)
			ed := button("Edit…", func() { edit(src) })
			ed.SetVAlign(gtk.ALIGN_CENTER)
			c.PackEnd(ed, false, false, 0)
			accounts.PackStart(c, false, false, 0)
		}
		accounts.ShowAll()
	}
	reload()

	flat := func(icon, text string, onClick func()) *gtk.Button {
		b, _ := gtk.ButtonNewWithLabel(text)
		if img, err := gtk.ImageNewFromIconName(icon, gtk.ICON_SIZE_BUTTON); err == nil {
			b.SetImage(img)
			b.SetAlwaysShowImage(true)
		}
		b.SetRelief(gtk.RELIEF_NONE)
		b.Connect("clicked", onClick)
		return b
	}
	btns, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	btns.PackStart(flat("list-add-symbolic", "Add CalDAV account", func() {
		edit(&model.CalendarSource{Type: model.SourceCalDAV, Enabled: true, SyncInterval: 15, Color: "#0b8043"})
	}), false, false, 0)
	btns.PackStart(flat("list-add-symbolic", "Add local calendar", func() {
		edit(&model.CalendarSource{Type: model.SourceLocal, Enabled: true, Color: "#8e24aa"})
	}), false, false, 0)
	btns.PackEnd(flat("view-refresh-symbolic", "Sync now", func() {
		if workersStale {
			a.svc.Engine.Restart() // starts workers for new sources and syncs all
			workersStale = false
		} else {
			a.svc.Engine.SyncNow()
		}
		// A first sync of a new account (discovery included) can take a while.
		for _, ms := range []uint{3000, 10000} {
			glib.TimeoutAdd(ms, func() bool {
				if !closed {
					reload()
				}
				return false
			})
		}
	}), false, false, 0)
	calPage := newPage("calendars")
	calPage.PackStart(btns, false, false, 0)
	calPage.PackStart(accounts, false, false, 0)
	calPage.PackStart(caption("Which calendars are shown is chosen with the Calendars button in the main window; an "+
		"account's Edit dialog chooses which of its calendars are synced."), false, false, 4)

	// ----- Reminders -----
	rems, _ := gtk.EntryNew()
	rems.SetText(core.FormatReminders(saved.DefaultReminderMinutes))
	rems.SetPlaceholderText("e.g. 15, 1h, 1d")
	rems.SetWidthChars(18)
	snooze, _ := gtk.SpinButtonNewWithRange(1, 1440, 1)
	snooze.SetValue(float64(saved.SnoozeIntervalMinutes))
	remPage := newPage("reminders")
	remPage.PackStart(card("Default reminders", "Used for events without reminders of their own. "+
		"Several values separated by commas; units m, h, d, w (e.g. 15, 1h, 1d).", rems, false), false, false, 0)
	remPage.PackStart(card("Snooze interval, minutes", "How long Snooze postpones a reminder.", snooze, false), false, false, 0)

	// ----- Sound -----
	soundBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	sound, _ := gtk.EntryNew()
	sound.SetText(saved.CustomSoundPath)
	sound.SetPlaceholderText("built-in chime")
	soundBox.PackStart(sound, true, true, 0)
	soundBox.PackStart(button("Browse…", func() {
		fc, err := gtk.FileChooserDialogNewWith2Buttons("Choose alert sound", dlg, gtk.FILE_CHOOSER_ACTION_OPEN,
			"Cancel", gtk.RESPONSE_CANCEL, "Open", gtk.RESPONSE_ACCEPT)
		if err != nil {
			return
		}
		filter, _ := gtk.FileFilterNew()
		filter.SetName("Audio files (*.mp3, *.wav)")
		for _, p := range []string{"*.mp3", "*.wav", "*.MP3", "*.WAV"} {
			filter.AddPattern(p)
		}
		fc.AddFilter(filter)
		if fc.Run() == gtk.RESPONSE_ACCEPT {
			sound.SetText(fc.GetFilename())
		}
		fc.Destroy()
	}), false, false, 0)
	soundBox.PackStart(button("Clear", func() { sound.SetText("") }), false, false, 0)
	testBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	testBox.PackStart(button("▶ Test", func() {
		path := strings.TrimSpace(entryText(sound))
		go func() {
			if err := a.svc.Audio.Play(path); err != nil {
				ui(func() { a.message(dlg, gtk.MESSAGE_WARNING, "Sound: "+err.Error()) })
			}
		}()
	}), false, false, 0)
	testBox.PackStart(button("■ Stop", a.svc.Audio.Stop), false, false, 0)
	loop, _ := gtk.SwitchNew()
	loop.SetActive(saved.LoopSound)
	soundPage := newPage("sound")
	soundPage.PackStart(card("Alert sound", "An MP3 or WAV file; leave empty for the built-in chime.", soundBox, true), false, false, 0)
	soundPage.PackStart(card("Try it", "Plays the chosen sound once.", testBox, false), false, false, 0)
	soundPage.PackStart(card("Repeat until stopped", "Keep playing until you press Stop Sound (at most 15 seconds).", loop, false), false, false, 0)

	// ----- New meetings -----
	newAlerts, _ := gtk.SwitchNew()
	newAlerts.SetActive(!saved.NewEventAlertsOff)
	alertsPage := newPage("alerts")
	alertsPage.PackStart(card("Alert about new meetings", "Play the sound and show a popup when a sync brings in a new "+
		"upcoming event, e.g. an invitation. Calendars unticked in the main window stay quiet.", newAlerts, false), false, false, 0)

	// ----- Startup -----
	startup, _ := gtk.SwitchNew()
	startup.SetActive(saved.RunAtStartup)
	minimized, _ := gtk.SwitchNew()
	minimized.SetActive(saved.StartMinimized)
	startPage := newPage("startup")
	startPage.PackStart(card("Run at login", "Start Agendling when you log in, so reminders are never missed.", startup, false), false, false, 0)
	startPage.PackStart(card("Start minimized", "Start hidden in the system tray.", minimized, false), false, false, 0)
	closeChoice, _ := gtk.ComboBoxTextNew()
	for _, l := range []string{"Ask", "Keep running minimized", "Quit"} {
		closeChoice.AppendText(l)
	}
	closeChoice.SetActive(map[string]int{"": 0, model.CloseMinimize: 1, model.CloseQuit: 2}[saved.CloseWithoutTray])
	startPage.PackStart(card("Closing the window without a tray", "Without a system tray to return from, keep Agendling "+
		"running minimized for reminders, or quit.", closeChoice, false), false, false, 0)
	startPage.PackStart(caption("With a tray, closing the main window keeps Agendling running there; use Exit in the tray menu "+
		"or Quit in the ☰ menu (Ctrl+Q) to quit. On GNOME the tray icon needs the AppIndicator extension (enabled by "+
		"default on Ubuntu)."), false, false, 4)

	// ----- About -----
	aboutPage := newPage("about")
	link := func(markup string) *gtk.Label {
		l, _ := gtk.LabelNew("")
		l.SetMarkup(markup)
		l.SetXAlign(0)
		l.SetLineWrap(true)
		l.Connect("activate-link", func(_ *gtk.Label, uri string) bool {
			openURL(uri)
			return true
		})
		return l
	}
	head, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 16)
	addClass(head, "settings-card")
	if loader, err := gdk.PixbufLoaderNew(); err == nil {
		if pb, err := loader.WriteAndReturnPixbuf(appinfo.LogoPNG); err == nil {
			if small, err := pb.ScaleSimple(56, 56, gdk.INTERP_BILINEAR); err == nil {
				if img, err := gtk.ImageNewFromPixbuf(small); err == nil {
					head.PackStart(img, false, false, 0)
				}
			}
		}
	}
	headText, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	headText.PackStart(markupLabel("<span size='x-large' weight='bold'>"+appinfo.Name+"</span>"), false, false, 0)
	headText.PackStart(markupLabel("Version "+glib.MarkupEscapeText(appinfo.Version)), false, false, 0)
	headText.PackStart(caption("Desktop calendar with CalDAV sync and reminders."), false, false, 0)
	head.PackStart(headText, true, true, 0)
	aboutPage.PackStart(head, false, false, 0)

	updBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	updResult := link("")
	var checkBtn *gtk.Button
	checkBtn = button("Check for updates", func() {
		checkBtn.SetSensitive(false)
		updResult.SetText("Checking…")
		go func() {
			rel, err := update.Check(context.Background(), appinfo.Version)
			ui(func() {
				if closed {
					return
				}
				checkBtn.SetSensitive(true)
				updResult.SetMarkup(updateMarkup(rel, err))
			})
		}()
	})
	checkBtn.SetHAlign(gtk.ALIGN_START)
	updBox.PackStart(checkBtn, false, false, 0)
	updBox.PackStart(updResult, false, false, 0)
	aboutPage.PackStart(card("Updates", "Asks GitHub for the latest release, only when you press the button. "+
		"Nothing is downloaded: a newer version is offered as a link to its release page.", updBox, true), false, false, 0)
	aboutPage.PackStart(card("Project page", "", link(`<a href="`+appinfo.Homepage+`">`+appinfo.Homepage+`</a>`), true), false, false, 0)
	if dir, err := appinfo.DataDir(); err == nil {
		dataBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
		dl, _ := gtk.LabelNew(dir)
		dl.SetSelectable(true)
		dl.SetXAlign(0)
		dataBox.PackStart(dl, true, true, 0)
		dataBox.PackEnd(button("Open folder", func() { openURL("file://" + dir) }), false, false, 0)
		aboutPage.PackStart(card("Data folder", "Calendars, settings and the log file.", dataBox, true), false, false, 0)
	}

	// ----- Layout: search and sections on the left, the selected page on the right -----
	stack, _ := gtk.StackNew()
	stack.SetTransitionType(gtk.STACK_TRANSITION_TYPE_NONE)
	for _, p := range core.SettingsPages {
		body, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
		body.SetMarginStart(28)
		body.SetMarginEnd(28)
		body.SetMarginTop(18)
		body.SetMarginBottom(18)
		t, _ := gtk.LabelNew(p.Title)
		t.SetXAlign(0)
		addClass(t, "settings-title")
		body.PackStart(t, false, false, 0)
		intro := caption(p.Intro)
		intro.SetMarginBottom(8)
		body.PackStart(intro, false, false, 0)
		body.PackStart(pages[p.Key], true, true, 0)
		scroll, _ := gtk.ScrolledWindowNew(nil, nil)
		scroll.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
		addClass(body, "settings-page")
		scroll.Add(body)
		stack.AddNamed(scroll, p.Key)
	}

	nav, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 10)
	addClass(nav, "settings-nav")
	nav.SetSizeRequest(240, -1)
	search, _ := gtk.SearchEntryNew()
	search.SetPlaceholderText("Search settings")
	search.SetMarginStart(10)
	search.SetMarginEnd(10)
	search.SetMarginTop(12)
	nav.PackStart(search, false, false, 0)
	list, _ := gtk.ListBoxNew()
	list.SetSelectionMode(gtk.SELECTION_BROWSE)
	// gotk3 hands out fresh wrappers for the same row, so rows are identified by their index.
	rows := map[string]*gtk.ListBoxRow{}
	keyOf := func(row *gtk.ListBoxRow) string {
		if i := row.GetIndex(); i >= 0 && i < len(core.SettingsPages) {
			return core.SettingsPages[i].Key
		}
		return ""
	}
	for _, p := range core.SettingsPages {
		row, _ := gtk.ListBoxRowNew()
		rb, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 10)
		rb.SetMarginStart(14)
		rb.SetMarginTop(7)
		rb.SetMarginBottom(7)
		if img, err := gtk.ImageNewFromIconName(p.Icon, gtk.ICON_SIZE_MENU); err == nil {
			rb.PackStart(img, false, false, 0)
		}
		l, _ := gtk.LabelNew(p.Title)
		l.SetXAlign(0)
		rb.PackStart(l, true, true, 0)
		row.Add(rb)
		list.Add(row)
		rows[p.Key] = row
	}
	list.Connect("row-selected", func(_ *gtk.ListBox, row *gtk.ListBoxRow) {
		if row != nil {
			stack.SetVisibleChildName(keyOf(row))
		}
	})
	nav.PackStart(list, true, true, 0)
	search.Connect("search-changed", func() {
		matches := core.FilterSettingsPages(entryText(&search.Entry))
		shown := map[string]bool{}
		for _, p := range matches {
			shown[p.Key] = true
		}
		for key, row := range rows {
			row.SetVisible(shown[key])
		}
		// Jump to the first match when the current page is filtered out.
		if cur := list.GetSelectedRow(); len(matches) > 0 && (cur == nil || !shown[keyOf(cur)]) {
			list.SelectRow(rows[matches[0].Key])
		}
	})
	search.Connect("stop-search", func() { search.SetText("") }) // Esc clears the search

	split, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	split.PackStart(nav, false, false, 0)
	split.PackStart(stack, true, true, 0)
	content, _ := dlg.GetContentArea()
	content.PackStart(split, true, true, 0)

	// ----- Apply / OK -----
	collect := func() (model.AppSettings, error) {
		st := a.store.Settings() // keeps settings changed outside the dialog, e.g. calendar ticks
		mins, err := core.ParseReminders(entryText(rems))
		if err != nil {
			return st, fmt.Errorf("invalid default reminders: %w", err)
		}
		st.DefaultReminderMinutes = mins
		st.SnoozeIntervalMinutes = snooze.GetValueAsInt()
		st.CustomSoundPath = strings.TrimSpace(entryText(sound))
		st.LoopSound = loop.GetActive()
		st.NewEventAlertsOff = !newAlerts.GetActive()
		st.RunAtStartup = startup.GetActive()
		st.StartMinimized = minimized.GetActive()
		st.CloseWithoutTray = []string{"", model.CloseMinimize, model.CloseQuit}[max(closeChoice.GetActive(), 0)]
		return st, nil
	}
	dirty := func() bool {
		st, err := collect()
		return err != nil || !core.SameEditableSettings(st, saved)
	}
	updateApply := func() { applyBtn.SetSensitive(dirty()) }
	for _, e := range []*gtk.Entry{rems, sound} {
		e.Connect("changed", updateApply)
	}
	snooze.Connect("value-changed", updateApply)
	for _, sw := range []*gtk.Switch{loop, newAlerts, startup, minimized} {
		sw.Connect("notify::active", updateApply)
	}
	closeChoice.Connect("changed", updateApply)
	apply := func() bool {
		st, err := collect()
		if err != nil {
			a.message(dlg, gtk.MESSAGE_WARNING, err.Error())
			return false
		}
		if st.RunAtStartup != saved.RunAtStartup {
			if err := setRunAtStartup(st.RunAtStartup); err != nil {
				a.message(dlg, gtk.MESSAGE_WARNING, "Could not update autostart: "+err.Error())
			}
		}
		if err := a.store.SaveSettings(st); err != nil {
			a.message(dlg, gtk.MESSAGE_ERROR, err.Error())
			return false
		}
		saved = st
		updateApply()
		return true
	}

	dlg.ShowAll()
	if rows[page] == nil {
		page = core.SettingsPages[0].Key
	}
	list.SelectRow(rows[page])
	updateApply()
	for {
		r := dlg.Run()
		if r == gtk.RESPONSE_APPLY {
			apply()
			continue
		}
		if r == gtk.RESPONSE_OK && !apply() {
			continue
		}
		break
	}
	closed = true
	dlg.Destroy()
	a.svc.Audio.Stop()
	if workersStale {
		a.svc.Engine.Restart()
	}
	if sourcesChanged {
		a.view.reload()
	}
}

// updateMarkup describes the result of an update check, linking to the release page.
func updateMarkup(rel update.Release, err error) string {
	esc := glib.MarkupEscapeText
	switch {
	case err != nil:
		return "Could not check for updates: " + esc(err.Error()) + `. <a href="` + update.ReleasesPage + `">Open the releases page</a>`
	case rel.Newer:
		return "Version <b>" + esc(rel.Tag) + "</b> is available: <a href=\"" + esc(rel.URL) + "\">open the release page</a>"
	case appinfo.Version == "dev" || !strings.HasPrefix(appinfo.Version, "v"):
		return "This is a development build. The latest release is <a href=\"" + esc(rel.URL) + "\">" + esc(rel.Tag) + "</a>."
	}
	return "You have the latest version (" + esc(rel.Tag) + ")."
}

// editSource shows the calendar account dialog; it returns true when saved.
func (a *App) editSource(parent gtk.IWindow, src *model.CalendarSource) bool {
	isCalDAV := src.Type == model.SourceCalDAV
	dlg, _ := gtk.DialogNew()
	dlg.SetTransientFor(parent)
	dlg.SetModal(true)
	dlg.SetDefaultSize(620, -1)
	addClass(dlg, "settings-dialog")
	if isCalDAV {
		dlg.SetTitle("CalDAV account")
	} else {
		dlg.SetTitle("Local calendar")
	}
	dlg.AddButton("Cancel", gtk.RESPONSE_CANCEL)
	ok, _ := dlg.AddButton("OK", gtk.RESPONSE_OK)
	addClass(ok, "suggested-action")
	dlg.SetDefaultResponse(gtk.RESPONSE_OK)

	body, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 10)
	addClass(body, "settings-page")
	body.SetMarginStart(18)
	body.SetMarginEnd(18)
	body.SetMarginTop(16)
	body.SetMarginBottom(12)
	// card is a group of labelled rows on a white card.
	card := func() (*gtk.Grid, func(string, gtk.IWidget)) {
		g, _ := gtk.GridNew()
		g.SetRowSpacing(10)
		g.SetColumnSpacing(14)
		addClass(g, "settings-card")
		body.PackStart(g, false, false, 0)
		row := 0
		return g, func(label string, w gtk.IWidget) {
			if label != "" {
				l, _ := gtk.LabelNew(label)
				l.SetXAlign(0)
				l.SetSizeRequest(110, -1)
				addClass(l, "settings-field")
				g.Attach(l, 0, row, 1, 1)
			}
			g.Attach(w, 1, row, 1, 1)
			row++
		}
	}
	captionLabel := func(text string) *gtk.Label {
		l, _ := gtk.LabelNew(text)
		l.SetXAlign(0)
		l.SetLineWrap(true)
		l.SetMaxWidthChars(60)
		addClass(l, "settings-caption")
		return l
	}

	// General: name, colour, enabled.
	_, add := card()
	name, _ := gtk.EntryNew()
	name.SetText(src.Name)
	name.SetHExpand(true)
	add("Name", name)
	c := core.ParseColor(src.Color)
	r, gg, b := c.Floats()
	colorBtn, _ := gtk.ColorButtonNewWithRGBA(gdk.NewRGBA(r, gg, b, 1))
	colorBtn.SetHAlign(gtk.ALIGN_START)
	add("Colour", colorBtn)
	enabledRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
	enabledText, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	el, _ := gtk.LabelNew("Enabled")
	el.SetXAlign(0)
	enabledText.PackStart(el, false, false, 0)
	enabledText.PackStart(captionLabel("Show this calendar and keep it synchronized."), false, false, 0)
	enabledRow.PackStart(enabledText, true, true, 0)
	enabled, _ := gtk.SwitchNew()
	enabled.SetActive(src.Enabled)
	enabled.SetVAlign(gtk.ALIGN_CENTER)
	enabledRow.PackEnd(enabled, false, false, 0)
	add("", enabledRow)

	var urlE, user, pass *gtk.Entry
	var interval *gtk.ComboBoxText
	// Calendar selection: filled from the last sync for existing accounts, refreshed by
	// "Test connection". fresh tells whether the list belongs to the URL now entered.
	var choices []core.CalendarChoice
	var switches []*gtk.Switch
	fresh := false
	calBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	shownChoices := func() []core.CalendarChoice {
		res := append([]core.CalendarChoice(nil), choices...)
		for i, sw := range switches {
			res[i].Shown = sw.GetActive()
		}
		return res
	}
	fillCalendars := func(list []core.CalendarChoice) {
		calBox.GetChildren().Foreach(func(w any) { w.(gtk.IWidget).ToWidget().Destroy() })
		choices, switches = list, nil
		if len(list) == 0 {
			calBox.PackStart(captionLabel("Test the connection to list the calendars of this account."), false, false, 0)
		}
		for _, ch := range list {
			row, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
			dot := markupLabel(`<span foreground="` + src.Color + `" size="large">●</span>`)
			dot.SetLineWrap(false)
			row.PackStart(dot, false, false, 0)
			l, _ := gtk.LabelNew(ch.Name)
			l.SetXAlign(0)
			l.SetTooltipText(ch.Path)
			row.PackStart(l, true, true, 0)
			sw, _ := gtk.SwitchNew()
			sw.SetActive(ch.Shown)
			sw.SetSensitive(len(list) > 1)
			row.PackEnd(sw, false, false, 0)
			calBox.PackStart(row, false, false, 0)
			switches = append(switches, sw)
		}
		calBox.ShowAll()
	}

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
		_, add := card()
		urlE, _ = gtk.EntryNew()
		urlE.SetText(src.URL)
		urlE.SetPlaceholderText("https://caldav.example.com")
		urlE.SetHExpand(true)
		urlE.Connect("changed", func() { fresh = false })
		add("Server URL", urlE)
		add("", captionLabel("For example https://caldav.icloud.com or https://host/remote.php/dav (Nextcloud). "+
			"A URL pointing at a single calendar syncs only that calendar."))
		user, _ = gtk.EntryNew()
		user.SetText(src.Username)
		user.Connect("changed", func() { fresh = false })
		add("Username", user)
		pass, _ = gtk.EntryNew()
		pass.SetText(src.Password)
		pass.SetVisibility(false)
		pass.SetInputPurpose(gtk.INPUT_PURPOSE_PASSWORD)
		add("App password", pass)
		testRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 10)
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
				cals, err := caldav.TestConnection(ctx, s)
				ui(func() {
					testBtn.SetSensitive(true)
					if err != nil {
						result.SetMarkup(`<span foreground="#c42b1c">` + glib.MarkupEscapeText(err.Error()) + `</span>`)
						return
					}
					result.SetMarkup(`<span foreground="#0f7b3f">` + fmt.Sprintf("Connected, %d calendar(s) found", len(cals)) + `</span>`)
					fillCalendars(core.CalendarChoices(cals, s.HiddenCollections)) // keeps choices made so far
					fresh = true
				})
			}()
		})
		testRow.PackStart(testBtn, false, false, 0)
		testRow.PackStart(result, true, true, 0)
		add("", testRow)

		cg, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
		addClass(cg, "settings-card")
		cg.PackStart(markupLabel("<b>Calendars to sync</b>"), false, false, 0)
		cg.PackStart(captionLabel("Turned-off calendars are not downloaded at all. Which synced calendars are shown is "+
			"chosen with the Calendars button in the main window."), false, false, 0)
		cg.PackStart(calBox, false, false, 4)
		body.PackStart(cg, false, false, 0)

		_, addSync := card()
		interval, _ = gtk.ComboBoxTextNew()
		idx := 2
		for i, it := range core.SyncIntervals {
			interval.AppendText(it.Label)
			if it.Mins == src.SyncInterval {
				idx = i
			}
		}
		interval.SetActive(idx)
		interval.SetHAlign(gtk.ALIGN_START)
		addSync("Sync", interval)

		if src.ID != 0 {
			fillCalendars(core.CalendarChoices(caldav.KnownCalendars(a.store, src.ID), src.HiddenCollections))
			fresh = true
		} else {
			fillCalendars(nil)
		}
	}

	content, _ := dlg.GetContentArea()
	content.PackStart(body, true, true, 0)
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
		if _, anyShown := core.HiddenCalendars(shownChoices()); isCalDAV && fresh && len(choices) > 0 && !anyShown {
			a.message(dlg, gtk.MESSAGE_WARNING, "Turn on at least one calendar, or turn off Enabled to hide the whole account.")
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
