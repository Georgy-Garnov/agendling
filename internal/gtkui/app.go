//go:build linux

// Package gtkui implements the Linux user interface with GTK 3 (gotk3): the main
// calendar window, tray icon, reminder popups, event editor and settings.
package gtkui

import (
	"fmt"
	"log"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

type App struct {
	svc   *core.Service
	store *storage.Store

	win      *gtk.Window
	view     *calView
	title    *gtk.Label
	status   *gtk.Label
	viewBtns [4]*gtk.ToggleButton
	syncing  bool // suppresses toggled handlers while updating view buttons

	trayEnd  func()
	hasTray  bool
	muteItem *systray.MenuItem
	iconDay  int

	quitting bool
	alerts   map[*alertWindow]bool

	syncMu     sync.Mutex
	syncErrors map[int64]string

	refreshMu      sync.Mutex
	refreshPending bool
}

func NewApp(store *storage.Store) *App {
	a := &App{alerts: map[*alertWindow]bool{}, syncErrors: map[int64]string{}}
	a.svc = core.NewService(store, a.onSyncStatus, a.onReminder)
	a.store = a.svc.Store
	a.view = newCalView(a)
	return a
}

// ui runs f on the GTK main thread.
func ui(f func()) {
	glib.IdleAdd(func() bool {
		f()
		return false
	})
}

// Run builds the UI and blocks in the GTK main loop until Exit.
func (a *App) Run(startMinimized bool, activations <-chan struct{}) error {
	gtk.Init(nil)
	a.svc.EnsureDefaultCalendar()

	if err := a.buildMainWindow(); err != nil {
		return err
	}
	a.startTray()
	a.store.OnChange(a.requestRefresh)
	go func() {
		for range activations {
			ui(a.showMain)
		}
	}()

	a.view.reload()
	a.svc.Start()
	// Repaint the "now" line once a minute while the window is visible.
	glib.TimeoutAdd(60_000, func() bool {
		if a.win.IsVisible() {
			a.view.area.QueueDraw()
		}
		a.refreshIcon()
		return true
	})

	if !startMinimized {
		a.showMain()
	}
	gtk.Main()
	return nil
}

func button(label string, onClick func()) *gtk.Button {
	b, _ := gtk.ButtonNewWithLabel(label)
	b.Connect("clicked", onClick)
	return b
}

func iconButton(icon, tooltip string, onClick func()) *gtk.Button {
	b, _ := gtk.ButtonNewFromIconName(icon, gtk.ICON_SIZE_BUTTON)
	b.SetTooltipText(tooltip)
	b.Connect("clicked", onClick)
	return b
}

func (a *App) buildMainWindow() error {
	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return fmt.Errorf("create window: %w", err)
	}
	a.win = win
	win.SetTitle(appinfo.Name)
	win.SetDefaultSize(1100, 760)
	win.SetSizeRequest(640, 420)

	hb, _ := gtk.HeaderBarNew()
	hb.SetShowCloseButton(true)
	hb.SetTitle(appinfo.Name)
	win.SetTitlebar(hb)

	nav, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	navCtx, _ := nav.GetStyleContext()
	navCtx.AddClass("linked")
	nav.Add(iconButton("go-previous-symbolic", "Previous", func() { a.view.navigate(-1) }))
	nav.Add(button("Today", func() { a.view.goTo(time.Now(), a.view.mode) }))
	nav.Add(iconButton("go-next-symbolic", "Next", func() { a.view.navigate(1) }))
	hb.PackStart(nav)

	a.title, _ = gtk.LabelNew("")
	titleCtx, _ := a.title.GetStyleContext()
	titleCtx.AddClass("title")
	hb.SetCustomTitle(a.title)

	modes, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	modesCtx, _ := modes.GetStyleContext()
	modesCtx.AddClass("linked")
	for m := core.ViewDay; m <= core.ViewMonth; m++ {
		mode := m
		tb, _ := gtk.ToggleButtonNewWithLabel(core.ViewNames[mode])
		tb.Connect("toggled", func() {
			if !a.syncing && tb.GetActive() {
				a.view.setMode(mode)
			} else if !a.syncing {
				a.updateViewButtons() // keep the current mode pressed
			}
		})
		a.viewBtns[mode] = tb
		modes.Add(tb)
	}
	hb.PackStart(modes)

	hb.PackEnd(iconButton("emblem-system-symbolic", "Settings", a.showSettings))
	hb.PackEnd(iconButton("view-refresh-symbolic", "Sync now", a.syncNow))
	newBtn := button("New event", func() { a.newEvent(time.Now().Truncate(time.Hour).Add(time.Hour), false) })
	newCtx, _ := newBtn.GetStyleContext()
	newCtx.AddClass("suggested-action")
	hb.PackEnd(newBtn)

	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	box.PackStart(a.view.build(), true, true, 0)
	a.status, _ = gtk.LabelNew("Ready")
	a.status.SetXAlign(0)
	a.status.SetMarginStart(8)
	a.status.SetMarginTop(3)
	a.status.SetMarginBottom(3)
	statusCtx, _ := a.status.GetStyleContext()
	statusCtx.AddClass("dim-label")
	box.PackEnd(a.status, false, false, 0)
	win.Add(box)

	// Closing the window hides it to the tray instead of exiting. Without a tray
	// (e.g. GNOME without the AppIndicator extension) it is minimized instead, so
	// the app stays reachable; launching it again also brings the window back.
	win.Connect("delete-event", func() bool {
		if a.quitting {
			return false
		}
		if a.hasTray {
			win.Hide()
		} else {
			win.Iconify()
		}
		return true
	})
	a.refreshIcon()
	return nil
}

func (a *App) updateViewButtons() {
	a.syncing = true
	for m, b := range a.viewBtns {
		if b != nil {
			b.SetActive(core.ViewMode(m) == a.view.mode)
		}
	}
	a.syncing = false
}

func (a *App) onViewChanged() {
	if a.title != nil {
		a.title.SetText(core.ViewTitle(a.view.mode, a.view.anchor))
	}
	a.updateViewButtons()
}

func (a *App) showMain() {
	a.view.reload()
	a.win.ShowAll()
	a.win.Present()
}

func (a *App) exit() {
	if a.quitting {
		return
	}
	a.quitting = true
	a.svc.Stop()
	for w := range a.alerts {
		w.close()
	}
	if a.trayEnd != nil {
		a.trayEnd()
	}
	gtk.MainQuit()
}

func (a *App) syncNow() {
	a.setStatus("Syncing…")
	a.svc.Engine.SyncNow()
}

func (a *App) toggleMute() {
	muted := a.svc.ToggleMute()
	if a.muteItem != nil {
		if muted {
			a.muteItem.Check()
		} else {
			a.muteItem.Uncheck()
		}
	}
	if muted {
		a.setStatus("Notification sounds muted")
	} else {
		a.setStatus("Notification sounds on")
	}
}

func (a *App) setStatus(s string) {
	if a.status != nil {
		a.status.SetText(s)
	}
}

// requestRefresh coalesces bursts of store changes into one UI refresh.
func (a *App) requestRefresh() {
	a.refreshMu.Lock()
	if a.refreshPending {
		a.refreshMu.Unlock()
		return
	}
	a.refreshPending = true
	a.refreshMu.Unlock()

	time.AfterFunc(150*time.Millisecond, func() {
		a.refreshMu.Lock()
		a.refreshPending = false
		a.refreshMu.Unlock()
		go a.svc.Sched.Reschedule()
		ui(func() {
			if a.win.IsVisible() {
				a.view.reload()
			}
		})
	})
}

func (a *App) onSyncStatus(st caldav.Status) {
	ui(func() {
		name := fmt.Sprint(st.SourceID)
		if src, err := a.store.Source(st.SourceID); err == nil {
			name = src.Name
		}
		switch {
		case st.Running:
			a.setStatus("Syncing " + name + "…")
		case st.Err != nil:
			a.setStatus(name + ": sync failed — " + st.Err.Error())
			a.syncMu.Lock()
			prev := a.syncErrors[st.SourceID]
			a.syncErrors[st.SourceID] = st.Err.Error()
			a.syncMu.Unlock()
			if prev != st.Err.Error() {
				notify("Calendar sync failed", name+": "+st.Err.Error())
			}
		default:
			a.syncMu.Lock()
			delete(a.syncErrors, st.SourceID)
			a.syncMu.Unlock()
			a.setStatus(fmt.Sprintf("%s synced at %s", name, time.Now().Format("15:04")))
		}
		if !st.Running {
			a.showNewEvents(st.SourceID, st.NewEvents)
		}
	})
}

func (a *App) onReminder(al scheduler.Alert) {
	ui(func() { a.showAlert(al) })
}

// ---------- tray ----------

// trayAvailable reports whether a StatusNotifierItem host (tray) is running.
func trayAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
	return err == nil && has
}

func (a *App) startTray() {
	if a.hasTray = trayAvailable(); !a.hasTray {
		log.Print("no system tray (StatusNotifierWatcher) found; closing the window will minimize it")
		return
	}
	start, end := systray.RunWithExternalLoop(func() {
		systray.SetTitle(appinfo.Name)
		systray.SetTooltip(appinfo.Name)
		systray.SetIcon(dateIconPNG(time.Now().Day()))
		open := systray.AddMenuItem("Open "+appinfo.Name, "")
		sync := systray.AddMenuItem("Sync Now", "")
		a.muteItem = systray.AddMenuItemCheckbox("Mute Notifications", "", a.store.Settings().Muted)
		settings := systray.AddMenuItem("Settings", "")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Exit", "")
		systray.SetOnTapped(func() { ui(a.showMain) })
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					ui(a.showMain)
				case <-sync.ClickedCh:
					ui(a.syncNow)
				case <-a.muteItem.ClickedCh:
					ui(a.toggleMute)
				case <-settings.ClickedCh:
					ui(func() { a.showMain(); a.showSettings() })
				case <-quit.ClickedCh:
					ui(a.exit)
					return
				}
			}
		}()
	}, nil)
	a.trayEnd = end
	start()
}

// refreshIcon updates the window and tray icons when the date changes.
func (a *App) refreshIcon() {
	day := time.Now().Day()
	if day == a.iconDay {
		return
	}
	a.iconDay = day
	png := dateIconPNG(day)
	if loader, err := gdk.PixbufLoaderNew(); err == nil {
		if pb, err := loader.WriteAndReturnPixbuf(png); err == nil {
			a.win.SetIcon(pb)
		}
	}
	if a.muteItem != nil { // tray is ready
		systray.SetIcon(png)
	}
}
