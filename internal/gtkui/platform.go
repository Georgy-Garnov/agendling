//go:build linux

package gtkui

// #cgo pkg-config: pango
// #include <stdint.h>
// #include <pango/pango.h>
// static void set_ellipsize_end(uintptr_t layout) {
//     pango_layout_set_ellipsize((PangoLayout *)layout, PANGO_ELLIPSIZE_END);
// }
import "C"

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gotk3/gotk3/pango"

	"github.com/Georgy-Garnov/agendling/internal/appinfo"
	"github.com/Georgy-Garnov/agendling/internal/core"
)

// ellipsizeEnd makes a layout end with "…" when it overflows (not wrapped by gotk3).
func ellipsizeEnd(l *pango.Layout) {
	C.set_ellipsize_end(C.uintptr_t(l.Native()))
}

// openURL opens a link in the default browser.
func openURL(u string) {
	if u = core.NormalizeURL(u); u == "" {
		return
	}
	if err := exec.Command("xdg-open", u).Start(); err != nil {
		log.Printf("xdg-open %q: %v", u, err)
	}
}

// notify shows a desktop notification (best effort).
func notify(title, body string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(path, "--app-name="+appinfo.Name, "--icon=x-office-calendar", title, body).Start()
	}
}

const autostartName = appinfo.ID + ".desktop"

// setRunAtStartup installs or removes an XDG autostart entry.
func setRunAtStartup(enabled bool) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "autostart", autostartName)
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=Desktop calendar with CalDAV sync and reminders
Exec="%s" --minimized
Icon=x-office-calendar
Terminal=false
X-GNOME-Autostart-enabled=true
`, appinfo.Name, strings.ReplaceAll(exe, `"`, `\"`))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(entry), 0o644)
}

// digit glyphs, 5×7.
var digits = [10][7]string{
	{"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	{"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	{"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	{"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	{"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	{"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	{"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	{"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	{"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	{"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
}

// dateIconPNG draws a 32×32 calendar-page icon showing the day of month.
func dateIconPNG(day int) []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	white := color.RGBA{255, 255, 255, 255}
	red := color.RGBA{219, 68, 55, 255}
	dark := color.RGBA{40, 40, 40, 255}
	border := color.RGBA{90, 90, 90, 255}
	for y := 1; y < size-1; y++ {
		for x := 1; x < size-1; x++ {
			c := white
			if y < 10 {
				c = red
			}
			if x == 1 || x == size-2 || y == 1 || y == size-2 {
				c = border
			}
			img.Set(x, y, c)
		}
	}
	text := fmt.Sprint(day)
	const scale = 2
	width := len(text)*5*scale + (len(text)-1)*scale
	x0 := (size - width) / 2
	y0 := 12 + (size-12-7*scale)/2 - 1
	for i, ch := range text {
		glyph := digits[ch-'0']
		for gy, row := range glyph {
			for gx, bit := range row {
				if bit != '1' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.Set(x0+i*6*scale+gx*scale+dx, y0+gy*scale+dy, dark)
					}
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
