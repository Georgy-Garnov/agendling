//go:build windows

// Package ui implements the native Win32 user interface (lxn/walk): main calendar
// window, system tray, alert popups, event editor and settings.
package ui

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/audio"
	"github.com/Georgy-Garnov/agendling/internal/caldav"
	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

type App struct {
	svc    *core.Service
	store  *storage.Store
	engine *caldav.Engine
	sched  *scheduler.Scheduler
	audio  *audio.Manager

	mw         *walk.MainWindow
	ni         *walk.NotifyIcon
	view       *CalView
	toolbar    *walk.Composite
	seg        *segmented
	calBtn     *flatButton
	titleLabel *walk.Label
	statusBar  *walk.Composite
	status     *walk.Label
	zoomCtl    *zoomControl
	// The Calendars dropdown, while open, and when it last closed.
	picker       *calPicker
	pickerClosed time.Time
	muteAction   *walk.Action

	icon    *walk.Icon
	iconDay int

	quitting bool
	alerts   map[*alertWindow]bool

	syncMu     sync.Mutex
	syncErrors map[int64]string

	refreshMu      sync.Mutex
	refreshPending bool
	nowTicker      *time.Ticker
}

func NewApp(store *storage.Store) *App {
	a := &App{
		alerts:     map[*alertWindow]bool{},
		syncErrors: map[int64]string{},
	}
	a.svc = core.NewService(store, a.onSyncStatus, a.onReminder)
	a.store, a.engine, a.sched, a.audio = a.svc.Store, a.svc.Engine, a.svc.Sched, a.svc.Audio
	a.view = newCalView(a)
	return a
}

// Run builds the UI and blocks in the message loop until Exit.
func (a *App) Run(startMinimized bool) error {
	a.svc.EnsureDefaultCalendar()

	var cw *walk.CustomWidget
	err := MainWindow{
		AssignTo: &a.mw,
		Title:    appinfo.Name,
		MinSize:  Size{Width: 760, Height: 460},
		Size:     Size{Width: 1180, Height: 780},
		Visible:  false,
		Font:     Font{Family: "Segoe UI", PointSize: 10},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{
				AssignTo:   &a.toolbar,
				Background: SolidColorBrush{Color: flSurface},
				Layout:     HBox{Margins: Margins{Left: 14, Top: 10, Right: 14, Bottom: 10}, Spacing: 6},
			},
			Composite{Background: SolidColorBrush{Color: flLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},
			CustomWidget{
				AssignTo:            &cw,
				PaintPixels:         func(c *walk.Canvas, r walk.Rectangle) error { return a.view.paint(c, r) },
				ClearsBackground:    false,
				InvalidatesOnResize: true,
				StretchFactor:       1,
			},
			// Our own status bar: the native one leaves trails of its size grip when resized.
			Composite{Background: SolidColorBrush{Color: flLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},
			Composite{
				AssignTo:   &a.statusBar,
				Background: SolidColorBrush{Color: flSurface},
				Layout:     HBox{Margins: Margins{Left: 12, Top: 3, Right: 10, Bottom: 3}, Spacing: 8},
			},
		},
	}.Create()
	if err != nil {
		return fmt.Errorf("create main window: %w", err)
	}
	a.view.attach(cw)
	a.buildToolbar()
	a.buildStatusBar()

	// Close (X) hides to tray instead of exiting.
	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !a.quitting {
			*canceled = true
			a.hideMain()
		}
	})
	a.mw.VisibleChanged().Attach(a.updateNowTicker)

	if err := a.createTray(); err != nil {
		return err
	}
	a.refreshIcon()

	// Store changes (UI edits or background sync) refresh the view and reschedule reminders.
	a.store.OnChange(a.requestRefresh)

	listenForActivation(func() { a.mw.Synchronize(a.showMain) })

	a.view.Reload()
	a.svc.Start()

	if !startMinimized {
		a.showMain()
	}
	a.mw.Run()
	return nil
}

func (a *App) createTray() error {
	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return fmt.Errorf("create tray icon: %w", err)
	}
	a.ni = ni
	ni.SetToolTip(appinfo.Name)
	if a.icon != nil {
		ni.SetIcon(a.icon) // drawn already while the window was built
	}

	add := func(text string, fn func()) *walk.Action {
		act := walk.NewAction()
		act.SetText(text)
		act.Triggered().Attach(fn)
		ni.ContextMenu().Actions().Add(act)
		return act
	}
	open := add("Open "+appinfo.Name, a.showMain)
	open.SetDefault(true)
	add("Sync Now", a.syncNow)
	a.muteAction = add("Mute Notifications", a.toggleMute)
	a.muteAction.SetCheckable(true)
	a.muteAction.SetChecked(a.store.Settings().Muted)
	add("Settings", func() { a.showMain(); a.showSettings() })
	ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())
	add("Exit", a.exit)

	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.showMain()
		}
	})
	return ni.SetVisible(true)
}

func (a *App) showMain() {
	a.updateCalButton()
	a.view.Reload()
	BringToFront(a.mw)
}

// applyIcon gives a dialog the app icon instead of the default window icon.
func (a *App) applyIcon(f walk.Form) {
	if a.icon != nil {
		_ = f.SetIcon(a.icon)
	}
}

// buildToolbar fills the header: navigation and the period on the left; view switcher,
// calendars, new event, sync and settings on the right.
func (a *App) buildToolbar() {
	tb := a.toolbar
	newFlatButton(tb, "", "Today", btnOutline, flSurface, func() { a.view.GoTo(time.Now(), a.view.mode) })
	prev, _ := newFlatButton(tb, glyphPrev, "", btnGhost, flSurface, func() { a.view.Navigate(-1) })
	prev.SetToolTipText("Previous")
	next, _ := newFlatButton(tb, glyphNext, "", btnGhost, flSurface, func() { a.view.Navigate(1) })
	next.SetToolTipText("Next")
	a.titleLabel = label(tb, "", uiFont(15, true), flText)
	a.titleLabel.SetMinMaxSize(walk.Size{Width: 300}, walk.Size{})
	walk.NewHSpacer(tb)
	a.seg, _ = newSegmented(tb, viewNames[:], flSurface, func(i int) { a.view.SetMode(viewMode(i)) })
	walk.NewHSpacerFixed(tb, 6)
	a.calBtn, _ = newFlatButton(tb, "", "Calendars", btnOutline, flSurface, a.toggleCalPicker)
	a.calBtn.SetTrailing(glyphChevron)
	a.calBtn.SetToolTipText("Choose which calendars to show")
	newFlatButton(tb, glyphAdd, "New event", btnPrimary, flSurface, func() { a.newEvent(time.Now().Truncate(time.Hour).Add(time.Hour), false) })
	walk.NewHSpacerFixed(tb, 4)
	sync, _ := newFlatButton(tb, glyphRefresh, "", btnGhost, flSurface, a.syncNow)
	sync.SetToolTipText("Sync now")
	settings, _ := newFlatButton(tb, glyphSettings, "", btnGhost, flSurface, a.showSettings)
	settings.SetToolTipText("Settings")
	a.updateCalButton()
	a.onViewChanged()
}

// buildStatusBar fills the bottom bar: the sync status, and the zoom of the calendar.
func (a *App) buildStatusBar() {
	sb := a.statusBar
	a.status = label(sb, "Ready", uiFont(9, false), flText2)
	walk.NewHSpacer(sb)
	a.zoomCtl, _ = newZoomControl(sb, a.view.zoom, flSurface, func(percent int) { a.setZoom(percent, -1) })
	a.zoomCtl.SetToolTipText("Calendar zoom: Ctrl+mouse wheel, Ctrl+Plus, Ctrl+Minus; Ctrl+0 or a click on the percentage resets it")
}

// setZoom zooms the calendar, keeping what is at y (-1: the middle) in place, and saves the level.
func (a *App) setZoom(percent, y int) {
	percent = core.CalendarZoom(percent)
	if percent == a.view.zoom {
		return
	}
	if err := a.svc.SaveCalendarZoom(percent); err != nil {
		a.setStatus("Failed to save calendar zoom: " + err.Error())
	}
	a.view.zoomAt(percent, y)
	if a.zoomCtl != nil {
		a.zoomCtl.SetPercent(percent)
	}
}

// updateCalButton shows on the Calendars button how many calendars are visible.
func (a *App) updateCalButton() {
	if a.calBtn != nil {
		a.calBtn.SetText(calendarSummary(a.svc.Calendars()))
	}
}

// calPopupForTest opens the calendar dropdown and returns its window handle (screenshot tour).
func (a *App) calPopupForTest() func() win.HWND {
	a.mw.Synchronize(a.toggleCalPicker)
	return func() win.HWND {
		if a.picker == nil {
			return 0
		}
		return a.picker.dlg.Handle()
	}
}

func (a *App) hideMain() { a.mw.Hide() }

func (a *App) exit() {
	if a.quitting {
		return
	}
	a.quitting = true
	// A relaunch must work right away, and shutdown must never hang the process.
	ReleaseSingleInstance()
	forceExitAfter(3 * time.Second)
	a.svc.Stop()
	for w := range a.alerts {
		w.close()
	}
	if a.ni != nil {
		a.ni.Dispose()
	}
	a.mw.Close()
	// The message loop only notices the closed window on its next message; end it explicitly.
	walk.App().Exit(0)
}

func (a *App) syncNow() {
	a.setStatus("Syncing…")
	a.engine.SyncNow()
}

func (a *App) toggleMute() {
	muted := a.svc.ToggleMute()
	a.muteAction.SetChecked(muted)
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
		go a.sched.Reschedule()
		a.mw.Synchronize(func() {
			if a.mw.Visible() {
				a.updateCalButton()
				if a.picker != nil {
					a.picker.reload()
				}
				a.view.Reload()
			}
		})
	})
}

func (a *App) onViewChanged() {
	if a.titleLabel != nil {
		a.titleLabel.SetText(a.view.Title())
	}
	if a.seg != nil {
		a.seg.SetSelected(int(a.view.mode))
	}
	a.refreshIcon()
}

// updateNowTicker repaints the "now" line once a minute, but only while the window is visible.
func (a *App) updateNowTicker() {
	if a.mw.Visible() {
		if a.nowTicker == nil {
			t := time.NewTicker(time.Minute)
			a.nowTicker = t
			go func() {
				for range t.C {
					a.mw.Synchronize(func() {
						if a.view.cw != nil {
							a.view.cw.Invalidate()
						}
					})
				}
			}()
		}
		return
	}
	if a.nowTicker != nil {
		a.nowTicker.Stop()
		a.nowTicker = nil
	}
}

func (a *App) onSyncStatus(st caldav.Status) {
	a.mw.Synchronize(func() {
		src, err := a.store.Source(st.SourceID)
		name := fmt.Sprint(st.SourceID)
		if err == nil {
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
			if prev != st.Err.Error() && a.ni != nil {
				a.ni.ShowWarning("Calendar sync failed", name+": "+st.Err.Error())
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
	a.mw.Synchronize(func() { a.showAlert(al) })
}

// refreshIcon draws a calendar glyph with today's date for the tray and window icons.
func (a *App) refreshIcon() {
	day := time.Now().Day()
	if a.mw == nil || (a.icon != nil && a.iconDay == day) {
		return
	}
	ic, err := makeDateIcon(day)
	if err != nil {
		log.Printf("icon: %v", err)
		return
	}
	old := a.icon
	a.icon, a.iconDay = ic, day
	if a.mw != nil {
		a.mw.SetIcon(ic)
	}
	if a.ni != nil {
		a.ni.SetIcon(ic)
	}
	if old != nil {
		old.Dispose()
	}
}

func makeDateIcon(day int) (*walk.Icon, error) {
	const size = 32
	bmp, err := walk.NewBitmapForDPI(walk.Size{Width: size, Height: size}, 96)
	if err != nil {
		return nil, err
	}
	defer bmp.Dispose()
	c, err := walk.NewCanvasFromImage(bmp)
	if err != nil {
		return nil, err
	}
	white, _ := walk.NewSolidColorBrush(walk.RGB(255, 255, 255))
	defer white.Dispose()
	red, _ := walk.NewSolidColorBrush(walk.RGB(219, 68, 55))
	defer red.Dispose()
	border, _ := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(90, 90, 90))
	defer border.Dispose()
	font, _ := walk.NewFont("Segoe UI", 13, walk.FontBold)
	defer font.Dispose()

	c.FillRectanglePixels(white, walk.Rectangle{Width: size, Height: size})
	c.FillRectanglePixels(red, walk.Rectangle{Width: size, Height: 9})
	c.DrawRectanglePixels(border, walk.Rectangle{Width: size, Height: size})
	c.DrawTextPixels(fmt.Sprint(day), font, walk.RGB(40, 40, 40), walk.Rectangle{Y: 8, Width: size, Height: size - 8},
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	c.Dispose()
	return walk.NewIconFromBitmap(bmp)
}

// acceptLater closes a dialog with OK once the current event has been handled. walk performs
// layouts requested during an event (e.g. by a label's SetText) when the event ends; closing the
// dialog inside the event shuts its layout machinery first, and that deferred layout then panics
// with "send on closed channel", taking the whole app down.
func acceptLater(dlg *walk.Dialog) {
	dlg.Synchronize(func() {
		if !dlg.IsDisposed() {
			dlg.Accept()
		}
	})
}
