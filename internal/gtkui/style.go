//go:build linux

package gtkui

import "github.com/gotk3/gotk3/gtk"

// appCSS is the app's look on top of the GTK theme: Fluent-like settings with a section list,
// white cards and accent switches, and the calendar dropdown.
const appCSS = `
.settings-dialog, .settings-dialog stack, .settings-dialog scrolledwindow, .settings-dialog viewport { background-color: #f3f4f6; }
.settings-dialog .dialog-action-box { background-color: #ffffff; border-top: 1px solid #e3e6ea; padding: 8px 12px; margin: 0; }
.settings-nav { background-color: #f7f8fa; border-right: 1px solid #e3e6ea; }
.settings-nav list { background-color: transparent; }
.settings-nav row { border-radius: 6px; margin: 1px 10px; }
.settings-nav row:hover { background-color: #f0f2f5; }
.settings-nav row:selected { background-color: #e3eefb; color: #1f2328; box-shadow: inset 3px 0 0 #0f6cbd; }
.settings-nav row:selected label { font-weight: 600; color: #1f2328; }
.settings-nav row:selected image { color: #1f2328; }
.settings-page { background-color: #f3f4f6; }
.settings-card { background-color: #ffffff; border: 1px solid #e3e6ea; border-radius: 8px; padding: 12px 16px; }
.settings-title { font-size: 20pt; font-weight: 600; color: #1f2328; }
.settings-caption { color: #5f6670; font-size: 9pt; }
.settings-field { color: #5f6670; }
.settings-card switch:checked { background-color: #0f6cbd; border-color: #0f6cbd; }
.event-title { font-size: 14pt; }

.cal-picker { padding: 6px; }
.cal-picker-title { font-weight: 600; }
.cal-picker list { background-color: transparent; }
.cal-picker row { border-radius: 6px; padding: 6px 8px; }
.cal-picker row:hover { background-color: #f0f2f5; }
.cal-picker .cal-account { color: #5f6670; font-size: 9pt; }
.cal-picker .cal-hidden { color: #8a9099; }
`

// installCSS applies appCSS to the whole screen; call once after gtk.Init.
func installCSS() {
	css, err := gtk.CssProviderNew()
	if err != nil || css.LoadFromData(appCSS) != nil {
		return
	}
	if screen, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL); err == nil {
		gtk.AddProviderForScreen(screen.GetScreen(), css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
		screen.Destroy()
	}
}

// addClass adds a CSS class to a widget.
func addClass(w interface {
	GetStyleContext() (*gtk.StyleContext, error)
}, class string) {
	if ctx, err := w.GetStyleContext(); err == nil {
		ctx.AddClass(class)
	}
}
