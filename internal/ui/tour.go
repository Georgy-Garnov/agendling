//go:build windows && uitour

package ui

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
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
	ui := func(f func()) {
		done := make(chan struct{})
		a.mw.Synchronize(func() { f(); close(done) })
		<-done
		time.Sleep(700 * time.Millisecond)
	}
	// grab and shot run on the tour goroutine, not in ui: the waits must leave the UI thread
	// free to finish layout and painting before the capture.
	grab := func(hwnd win.HWND) (*image.RGBA, win.RECT) {
		redraw := func() {
			win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN|win.RDW_FRAME)
		}
		ui(redraw)
		ui(redraw)
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
	popups := func() []*alertWindow {
		var ws []*alertWindow
		ui(func() {
			for w := range a.alerts {
				ws = append(ws, w)
			}
		})
		return ws
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
		shot(a.mw.Handle(), "week")
		ui(func() { a.view.SetMode(ViewMonth) })
		time.Sleep(time.Second) // the month grid repaints after the reload
		shot(a.mw.Handle(), "month")
		ui(func() { a.view.SetMode(ViewWeek) })

		// The calendar dropdown over the window, with one calendar hidden.
		ui(func() { a.svc.SetCalendarShown(calendar("Sport").Key, false) })
		ui(a.toggleCalPicker)
		var pick win.HWND
		ui(func() {
			if a.picker != nil {
				pick = a.picker.dlg.Handle()
			}
		})
		if pick == 0 {
			fail(fmt.Errorf("calendar dropdown did not open"))
		} else if main, mrc := grab(a.mw.Handle()); main != nil {
			if pop, prc := grab(pick); pop != nil {
				fail(savePNG(pop, filepath.Join(dir, "calendars-popup.png")))
				overlay(main, pop, int(prc.Left-mrc.Left), int(prc.Top-mrc.Top))
				fail(savePNG(main, filepath.Join(dir, "calendars.png")))
			}
		}
		shot(a.mw.Handle(), "main-filtered")
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
		for _, w := range popups() {
			shot(w.mw.Handle(), "reminder")
			ui(w.close)
		}
		if planning := find("Quarterly planning"); planning != nil && work != nil {
			ui(func() { a.showNewEvents(work.ID, []*model.Event{planning}) })
			for _, w := range popups() {
				shot(w.mw.Handle(), "new-event")
				ui(w.close)
			}
		}

		// Event editor on an existing event.
		if review := find("Design review"); review != nil {
			a.mw.Synchronize(func() { a.editEvent(review, review.StartTime) })
			d := <-dialogs
			time.Sleep(900 * time.Millisecond)
			shot(d.Handle(), "editor")
			ui(func() { d.Cancel() })
		}

		// Settings: every section, the search, and the account editor.
		a.mw.Synchronize(a.showSettings) // modal: the steps below run in its message loop
		s := <-settings
		time.Sleep(700 * time.Millisecond)
		for _, key := range []string{"calendars", "reminders", "sound", "alerts", "startup", "about"} {
			ui(func() { s.selectPage(key) })
			shot(s.dlg.Handle(), "settings-"+key)
		}
		ui(func() { s.search.SetText("snooze") })
		shot(s.dlg.Handle(), "settings-search")
		ui(func() { s.search.SetText("") })
		ui(func() { s.selectPage("calendars") })
		if cloud != nil {
			s.dlg.Synchronize(func() { s.editSource(cloud) })
			d := <-dialogs
			time.Sleep(900 * time.Millisecond)
			shot(d.Handle(), "account")
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
		})
		shot(a.mw.Handle(), "after-settings-ok")
		ui(a.exit)
	}()
	if err := a.Run(false); err != nil {
		return err
	}
	return failed
}

// overlay draws src onto dst at (x, y), keeping src's transparent corners.
func overlay(dst, src *image.RGBA, x, y int) {
	draw.Draw(dst, src.Bounds().Add(image.Pt(x, y)), src, image.Point{}, draw.Over)
}

func savePNG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

var (
	tourUser32 = syscall.NewLazyDLL("user32.dll")
	tourDwmapi = syscall.NewLazyDLL("dwmapi.dll")
)

// captureImage grabs a window, including parts covered by others, as it looks on screen: cropped
// to the visible frame (without the invisible resize borders) with transparent rounded corners
// and a hairline border, like Windows 11 draws it. The rectangle is the visible frame on screen.
func captureImage(hwnd win.HWND) (*image.RGBA, win.RECT, error) {
	img, rc, err := captureWindow(hwnd)
	if err != nil {
		return nil, rc, err
	}
	frame := rc
	const dwmwaExtendedFrameBounds = 9
	if hr, _, _ := tourDwmapi.NewProc("DwmGetWindowAttribute").Call(uintptr(hwnd), dwmwaExtendedFrameBounds,
		uintptr(unsafe.Pointer(&frame)), unsafe.Sizeof(frame)); hr != 0 {
		frame = rc
	}
	crop := image.Rect(int(frame.Left-rc.Left), int(frame.Top-rc.Top), int(frame.Right-rc.Left), int(frame.Bottom-rc.Top)).Intersect(img.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(out, out.Bounds(), img, crop.Min, draw.Src)
	dpi := 96
	if d, _, _ := tourUser32.NewProc("GetDpiForWindow").Call(uintptr(hwnd)); d != 0 {
		dpi = int(d)
	}
	roundCorners(out, 8*float64(dpi)/96)
	return out, frame, nil
}

// roundCorners clips img to a rounded rectangle with radius r (antialiased) and draws a hairline
// border along the edge. Pixels on the outermost row and column that the window did not paint
// (black: the system draws its border there) take the colour of their inner neighbour first.
func roundCorners(img *image.RGBA, r float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	at := func(x, y int) []uint8 { i := img.PixOffset(x, y); return img.Pix[i : i+4 : i+4] }
	black := func(p []uint8) bool { return p[0] == 0 && p[1] == 0 && p[2] == 0 }
	for x := 0; x < w && h > 1; x++ {
		for _, y := range [][2]int{{0, 1}, {h - 1, h - 2}} {
			if p := at(x, y[0]); black(p) {
				copy(p, at(x, y[1]))
			}
		}
	}
	for y := 0; y < h && w > 1; y++ {
		for _, x := range [][2]int{{0, 1}, {w - 1, w - 2}} {
			if p := at(x[0], y); black(p) {
				copy(p, at(x[1], y))
			}
		}
	}
	border := [3]float64{0x75, 0x7a, 0x82}
	const borderAlpha = 0.45
	hw, hh := float64(w)/2, float64(h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Signed distance from the pixel centre to the rounded rectangle, negative inside.
			qx := math.Abs(float64(x)+0.5-hw) - (hw - r)
			qy := math.Abs(float64(y)+0.5-hh) - (hh - r)
			d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
			if d < -1.5 {
				continue
			}
			p := at(x, y)
			if edge := math.Min(math.Max(d+1.5, 0), 1) * borderAlpha; edge > 0 {
				for c := 0; c < 3; c++ {
					p[c] = uint8(float64(p[c])*(1-edge) + border[c]*edge + 0.5)
				}
			}
			cover := math.Min(math.Max(0.5-d, 0), 1)
			for c := 0; c < 4; c++ { // premultiplied alpha
				p[c] = uint8(float64(p[c])*cover + 0.5)
			}
		}
	}
}

// captureWindow renders the whole window rectangle with PrintWindow.
func captureWindow(hwnd win.HWND) (*image.RGBA, win.RECT, error) {
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
