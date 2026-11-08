//go:build windows && uitour

package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/Georgy-Garnov/agendling/internal/core"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/scheduler"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// RunTour opens the app with the given store, saves PNGs of its windows into dir and exits: the
// documentation screenshots (week, month, calendars, editor, reminder, new-event, settings,
// account) and one shot per Settings section. It is built only with -tags uitour (see
// tools/uishots) to review the UI on a real Windows desktop.
func RunTour(store *storage.Store, dir string) error {
	a := NewApp(store)
	var failed error
	fail := func(err error) {
		if err != nil && failed == nil {
			failed = err
		}
	}
	grab := func(hwnd win.HWND) (*image.RGBA, win.RECT) {
		// Some controls finish painting a little after the window; give them time.
		win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN|win.RDW_FRAME)
		time.Sleep(600 * time.Millisecond)
		win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN|win.RDW_FRAME)
		time.Sleep(300 * time.Millisecond)
		img, rc, err := captureImage(hwnd)
		fail(err)
		return img, rc
	}
	shot := func(hwnd win.HWND, name string) {
		img, _ := grab(hwnd)
		if img != nil {
			fail(savePNG(img, filepath.Join(dir, name+".png")))
		}
	}
	ui := func(f func()) {
		done := make(chan struct{})
		a.mw.Synchronize(func() { f(); close(done) })
		<-done
		time.Sleep(700 * time.Millisecond)
	}
	find := func(title string) *model.Event {
		evs, _ := store.EventsInRange(time.Now().AddDate(0, 0, -14), time.Now().AddDate(0, 0, 14))
		for _, e := range evs {
			if e.Title == title {
				return e
			}
		}
		fail(fmt.Errorf("no demo event %q", title))
		return nil
	}
	calendar := func(name string) core.Calendar {
		for _, c := range a.svc.Calendars() {
			if c.Name == name {
				return c
			}
		}
		return core.Calendar{}
	}
	alertWindows := func() []*alertWindow {
		var res []*alertWindow
		for w := range a.alerts {
			res = append(res, w)
		}
		return res
	}
	settings := make(chan *settingsUI, 1)
	settingsHook = func(s *settingsUI) { settings <- s }
	dialogs := make(chan *walk.Dialog, 1)
	dialogHook = func(_ string, d *walk.Dialog) { dialogs <- d }

	go func() {
		for a.mw == nil {
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(4 * time.Second) // first sync of both accounts
		ui(a.showMain)
		ui(func() {
			if a.ni == nil || a.ni.Icon() == nil {
				fail(fmt.Errorf("the tray icon has no image"))
			}
		})
		ui(func() { a.view.SetMode(ViewWeek) })
		ui(func() { shot(a.mw.Handle(), "week") })
		ui(func() { a.view.SetMode(ViewMonth) })
		time.Sleep(time.Second) // the month grid repaints after the reload
		ui(func() { shot(a.mw.Handle(), "month") })
		ui(func() { a.view.SetMode(ViewWeek) })

		// The calendar dropdown over the window, with one calendar hidden.
		ui(func() { a.svc.SetCalendarShown(calendar("Sport").Key, false) })
		ui(a.toggleCalPicker)
		ui(func() {
			main, mrc := grab(a.mw.Handle())
			if a.picker == nil || main == nil {
				fail(fmt.Errorf("calendar dropdown did not open"))
				return
			}
			pop, prc := grab(a.picker.dlg.Handle())
			shot(a.picker.dlg.Handle(), "calendars-popup")
			if pop != nil {
				overlay(main, pop, int(prc.Left-mrc.Left), int(prc.Top-mrc.Top))
				fail(savePNG(main, filepath.Join(dir, "calendars.png")))
			}
		})
		ui(func() { shot(a.mw.Handle(), "main-filtered") })
		ui(func() {
			if a.picker != nil {
				a.picker.close()
			}
			a.svc.SetCalendarShown(calendar("Sport").Key, true)
		})

		// Reminder and new-meeting popups.
		srcs, _ := store.Sources()
		var work, cloud *model.CalendarSource // the demo accounts
		for _, s := range srcs {
			switch s.Name {
			case "Work":
				work = s
			case "iCloud":
				cloud = s
			}
		}
		soon := time.Now().Add(time.Minute).Truncate(time.Minute)
		sync := &model.Event{Title: "Product sync", Location: "Room 2 / online", StartTime: soon, EndTime: soon.Add(45 * time.Minute),
			Description: "Agenda: roadmap review and release scope.\nJoin: https://meet.example.com/product-sync\nNotes: https://docs.example.com/product-sync"}
		ui(func() {
			a.showAlert(scheduler.Alert{Event: sync, Calendar: work, Start: sync.StartTime, End: sync.EndTime, OffsetMin: 1})
		})
		ui(func() {
			for _, w := range alertWindows() {
				shot(w.mw.Handle(), "reminder")
				w.close()
			}
		})
		if planning := find("Quarterly planning"); planning != nil && work != nil {
			ui(func() { a.showNewEvents(work.ID, []*model.Event{planning}) })
			ui(func() {
				for _, w := range alertWindows() {
					shot(w.mw.Handle(), "new-event")
					w.close()
				}
			})
		}

		// Event editor on an existing event.
		if review := find("Design review"); review != nil {
			a.mw.Synchronize(func() { a.editEvent(review, review.StartTime) })
			d := <-dialogs
			time.Sleep(900 * time.Millisecond)
			ui(func() { shot(d.Handle(), "editor") })
			ui(func() { d.Cancel() })
		}

		// Settings: every section, the search, and the account editor.
		a.mw.Synchronize(a.showSettings) // modal: the steps below run in its message loop
		s := <-settings
		time.Sleep(700 * time.Millisecond)
		for _, key := range []string{"calendars", "reminders", "sound", "alerts", "startup", "about"} {
			ui(func() { s.selectPage(key) })
			ui(func() { shot(s.dlg.Handle(), "settings-"+key) })
		}
		ui(func() { s.search.SetText("snooze") })
		ui(func() { shot(s.dlg.Handle(), "settings-search") })
		ui(func() { s.search.SetText("") })
		ui(func() { s.selectPage("calendars") })
		if cloud != nil {
			s.dlg.Synchronize(func() { s.editSource(cloud) })
			d := <-dialogs
			time.Sleep(900 * time.Millisecond)
			ui(func() { shot(d.Handle(), "account") })
			ui(func() { d.Cancel() })
		}
		ui(func() { s.dlg.Cancel() })

		// Settings again, browsing sections, then OK: the app must keep running.
		a.mw.Synchronize(a.showSettings)
		s = <-settings
		time.Sleep(700 * time.Millisecond)
		for _, key := range []string{"sound", "about", "startup", "calendars"} {
			ui(func() { s.selectPage(key) })
		}
		ui(func() { win.SendMessage(s.okBtn.Handle(), win.BM_CLICK, 0, 0) })
		time.Sleep(time.Second)
		ui(func() {
			if a.mw.IsDisposed() || !a.mw.Visible() {
				fail(fmt.Errorf("the main window is gone after OK in Settings"))
			}
			shot(a.mw.Handle(), "after-settings-ok")
		})
		ui(a.exit)
	}()
	if err := a.Run(false); err != nil {
		return err
	}
	return failed
}

// overlay draws src onto dst at (x, y) with a hairline frame, like the dropdown on screen.
func overlay(dst, src *image.RGBA, x, y int) {
	r := src.Bounds().Add(image.Pt(x, y))
	draw.Draw(dst, r, src, image.Point{}, draw.Src)
	frame := color.RGBA{0xc8, 0xcc, 0xd2, 0xff}
	for i := r.Min.X - 1; i <= r.Max.X; i++ {
		dst.Set(i, r.Min.Y-1, frame)
		dst.Set(i, r.Max.Y, frame)
	}
	for j := r.Min.Y - 1; j <= r.Max.Y; j++ {
		dst.Set(r.Min.X-1, j, frame)
		dst.Set(r.Max.X, j, frame)
	}
}

func savePNG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

var tourUser32 = syscall.NewLazyDLL("user32.dll")

// captureImage grabs a window, including parts covered by others.
func captureImage(hwnd win.HWND) (*image.RGBA, win.RECT, error) {
	var rc win.RECT
	win.GetWindowRect(hwnd, &rc)
	w, h := int32(rc.Right-rc.Left), int32(rc.Bottom-rc.Top)
	screen := win.GetDC(0)
	defer win.ReleaseDC(0, screen)
	dc := win.CreateCompatibleDC(screen)
	defer win.DeleteDC(dc)
	bmp := win.CreateCompatibleBitmap(screen, w, h)
	defer win.DeleteObject(win.HGDIOBJ(bmp))
	old := win.SelectObject(dc, win.HGDIOBJ(bmp))
	const pwRenderFullContent = 2
	tourUser32.NewProc("PrintWindow").Call(uintptr(hwnd), uintptr(dc), pwRenderFullContent)
	win.SelectObject(dc, old)

	var bi win.BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bi.BmiHeader))
	bi.BmiHeader.BiWidth, bi.BmiHeader.BiHeight = w, -h
	bi.BmiHeader.BiPlanes, bi.BmiHeader.BiBitCount = 1, 32
	bi.BmiHeader.BiCompression = win.BI_RGB
	buf := make([]byte, int(w)*int(h)*4)
	if w <= 0 || h <= 0 || win.GetDIBits(dc, bmp, 0, uint32(h), &buf[0], &bi, win.DIB_RGB_COLORS) == 0 {
		return nil, rc, fmt.Errorf("GetDIBits failed")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	return img, rc, nil
}
