//go:build windows

package ui

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/core"
)

const (
	appName      = appinfo.ID
	runValueName = appinfo.Name
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// OpenURL opens a link in the default browser.
func OpenURL(u string) {
	if u = core.NormalizeURL(u); u == "" {
		return
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	target, _ := syscall.UTF16PtrFromString(u)
	win.ShellExecute(0, verb, target, nil, nil, win.SW_SHOWNORMAL)
}

// SetTopmost pins a window above all others and tries to bring it to the foreground.
func SetTopmost(f walk.Form) {
	h := f.Handle()
	win.SetWindowPos(h, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_SHOWWINDOW)
	win.ShowWindow(h, win.SW_SHOWNORMAL)
	win.SetForegroundWindow(h)
	flashWindow(h)
}

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procFlashWindow = user32.NewProc("FlashWindowEx")
)

type flashWInfo struct {
	cbSize    uint32
	hwnd      win.HWND
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
}

func flashWindow(h win.HWND) {
	const flashwAll, flashwTimerNoFG = 3, 12
	fi := flashWInfo{hwnd: h, dwFlags: flashwAll | flashwTimerNoFG, uCount: 5}
	fi.cbSize = uint32(unsafe.Sizeof(fi))
	procFlashWindow.Call(uintptr(unsafe.Pointer(&fi)))
}

// BringToFront restores and activates a (possibly hidden) main window.
func BringToFront(f walk.Form) {
	h := f.Handle()
	f.Show()
	if win.IsIconic(h) {
		win.ShowWindow(h, win.SW_RESTORE)
	}
	win.SetForegroundWindow(h)
}

// SetRunAtStartup registers or removes the app in the per-user Run key.
func SetRunAtStartup(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	_ = k.DeleteValue(appinfo.LegacyDirName) // entry written by pre-release builds
	if !enabled {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValueName, `"`+exe+`" --minimized`)
}

// MigrateStartupEntry replaces a pre-release autostart entry with the current one.
func MigrateStartupEntry(enabled bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	_, _, legacyErr := k.GetStringValue(appinfo.LegacyDirName)
	k.Close()
	if legacyErr == nil {
		_ = SetRunAtStartup(enabled)
	}
}

var (
	instanceMutex  windows.Handle
	activateEvent  windows.Handle
	instanceName   = `Local\` + appName + `-single-instance`
	activationName = `Local\` + appName + `-activate`
)

// AcquireSingleInstance returns false if another instance using the same data
// directory is already running.
func AcquireSingleInstance(dataDir string) bool {
	hash := fnv.New32a()
	hash.Write([]byte(strings.ToLower(filepath.Clean(dataDir))))
	suffix := fmt.Sprintf("-%08x", hash.Sum32())
	instanceName += suffix
	activationName += suffix

	name, _ := windows.UTF16PtrFromString(instanceName)
	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return false
	}
	instanceMutex = h
	return true
}

// ReleaseSingleInstance lets a new instance start even while this one is still shutting down.
func ReleaseSingleInstance() {
	if instanceMutex != 0 {
		windows.CloseHandle(instanceMutex)
		instanceMutex = 0
	}
}

// ActivateRunningInstance asks the running instance to show its window.
func ActivateRunningInstance() bool {
	name, _ := windows.UTF16PtrFromString(activationName)
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	return windows.SetEvent(h) == nil
}

// listenForActivation calls fn whenever a second launch signals this instance.
// The goroutine sleeps in WaitForSingleObject, so it costs no CPU.
func listenForActivation(fn func()) {
	name, _ := windows.UTF16PtrFromString(activationName)
	h, err := windows.CreateEvent(nil, 0, 0, name) // auto-reset
	if err != nil {
		return
	}
	activateEvent = h
	go func() {
		for {
			ev, err := windows.WaitForSingleObject(h, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			fn()
		}
	}()
}

// forceExitAfter terminates the process if a graceful shutdown hangs.
func forceExitAfter(d time.Duration) {
	time.AfterFunc(d, func() {
		windows.TerminateProcess(windows.CurrentProcess(), 0)
	})
}

// ---------- colors ----------

func ParseColor(hex string) walk.Color { return wc(core.ParseColor(hex)) }

// wc converts a core color to a walk color.
func wc(c core.RGB) walk.Color { return walk.RGB(c.R, c.G, c.B) }

func FormatColor(c walk.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R(), c.G(), c.B())
}

// Blend mixes c with white; amount 0 = c, 1 = white.
func Blend(c walk.Color, amount float64) walk.Color {
	mix := func(v byte) byte { return byte(float64(v) + (255-float64(v))*amount) }
	return walk.RGB(mix(c.R()), mix(c.G()), mix(c.B()))
}

var customColors [16]win.COLORREF

// PickColor shows the standard Windows color dialog.
func PickColor(owner walk.Form, initial walk.Color) (walk.Color, bool) {
	cc := win.CHOOSECOLOR{
		HwndOwner:    owner.Handle(),
		RgbResult:    win.COLORREF(initial),
		LpCustColors: &customColors,
		Flags:        win.CC_RGBINIT | win.CC_FULLOPEN,
	}
	cc.LStructSize = uint32(unsafe.Sizeof(cc))
	if !win.ChooseColor(&cc) {
		return initial, false
	}
	return walk.Color(cc.RgbResult), true
}
