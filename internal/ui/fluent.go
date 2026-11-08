//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// A small Fluent-style kit drawn with GDI: palette, fonts, Segoe icon glyphs and a few owner-drawn
// widgets (navigation list, toggle switch, segmented control, rounded cards). Native Win32 controls
// keep their look; these fill the gaps where they look dated.

// Palette, after Windows 11 / Fluent.
var (
	flAccent      = walk.RGB(0x0f, 0x6c, 0xbd)
	flAccentHover = walk.RGB(0x11, 0x5e, 0xa3)
	flWindow      = walk.RGB(0xf3, 0xf4, 0xf6)
	flNav         = walk.RGB(0xf7, 0xf8, 0xfa)
	flSurface     = walk.RGB(0xff, 0xff, 0xff)
	flLine        = walk.RGB(0xe3, 0xe6, 0xea)
	flText        = walk.RGB(0x1f, 0x23, 0x28)
	flText2       = walk.RGB(0x5f, 0x66, 0x70)
	flSelected    = walk.RGB(0xe3, 0xee, 0xfb)
	flHover       = walk.RGB(0xf0, 0xf2, 0xf5)
	flSwitchOff   = walk.RGB(0xc3, 0xc8, 0xd0)
)

// Segoe Fluent Icons / Segoe MDL2 Assets code points.
const (
	glyphCalendar = "\uE787"
	glyphAlarm    = "\uEA8F"
	glyphVolume   = "\uE767"
	glyphMail     = "\uE715"
	glyphPower    = "\uE7E8"
	glyphInfo     = "\uE946"
	glyphSearch   = "\uE721"
	glyphView     = "\uE890"
	glyphHide     = "\uED1A"
	glyphChevron  = "\uE70D"
	glyphRefresh  = "\uE72C"
	glyphSettings = "\uE713"
	glyphAdd      = "\uE710"
	glyphPrev     = "\uE76B"
	glyphLocation = "\uE707"
	glyphLink     = "\uE71B"
	glyphNext     = "\uE76C"
)

var (
	fontMu    sync.Mutex
	fontCache = map[string]*walk.Font{}
)

func cachedFont(family string, size int, style walk.FontStyle) *walk.Font {
	key := family + "|" + string(rune(size)) + "|" + string(rune(style))
	fontMu.Lock()
	defer fontMu.Unlock()
	if f := fontCache[key]; f != nil {
		return f
	}
	f, err := walk.NewFont(family, size, style)
	if err != nil {
		f, _ = walk.NewFont("Segoe UI", size, style)
	}
	fontCache[key] = f
	return f
}

// uiFont is the text face; semibold uses the separate Segoe UI Semibold family, as GDI has no
// 600 weight in the regular family.
func uiFont(size int, semibold bool) *walk.Font {
	if semibold {
		return cachedFont("Segoe UI Semibold", size, 0)
	}
	return cachedFont("Segoe UI", size, 0)
}

var iconFamily = func() string {
	if _, err := os.Stat(filepath.Join(os.Getenv("WINDIR"), "Fonts", "SegoeIcons.ttf")); err == nil {
		return "Segoe Fluent Icons" // Windows 11
	}
	return "Segoe MDL2 Assets" // Windows 10
}()

func iconFont(size int) *walk.Font { return cachedFont(iconFamily, size, 0) }

// scale converts 96-DPI units to the widget's pixels.
func scale(w walk.Window, v int) int { return v * w.DPI() / 96 }

func solid(c walk.Color) *walk.SolidColorBrush {
	b, _ := walk.NewSolidColorBrush(c)
	return b
}

func fillRound(cv *walk.Canvas, c walk.Color, r walk.Rectangle, radius int) {
	b := solid(c)
	defer b.Dispose()
	cv.FillRoundedRectanglePixels(b, r, walk.Size{Width: radius * 2, Height: radius * 2})
}

func drawText(cv *walk.Canvas, s string, f *walk.Font, c walk.Color, r walk.Rectangle, format walk.DrawTextFormat) {
	cv.DrawTextPixels(s, f, c, r, format|walk.TextNoPrefix)
}

const textLeftMiddle = walk.TextLeft | walk.TextVCenter | walk.TextSingleLine | walk.TextEndEllipsis

var measureCanvas = sync.OnceValue(func() *walk.Canvas {
	bmp, err := walk.NewBitmapForDPI(walk.Size{Width: 1, Height: 1}, 96)
	if err != nil {
		return nil
	}
	cv, _ := walk.NewCanvasFromImage(bmp)
	return cv
})

// textWidth is the width of a single line of text, in 1/96".
func textWidth(s string, f *walk.Font) int {
	cv := measureCanvas()
	if cv == nil {
		return 7 * len([]rune(s))
	}
	// walk does not pass DT_CALCRECT itself, so without it the result is just the given bounds.
	r, _, err := cv.MeasureTextPixels(s, f, walk.Rectangle{Width: 10000, Height: 1000},
		walk.TextSingleLine|walk.TextNoPrefix|walk.DrawTextFormat(win.DT_CALCRECT))
	if err != nil {
		return 7 * len([]rune(s))
	}
	return r.Width
}

// roundedBitmap draws a rounded rectangle (fill, optional border) on a background colour into a
// bitmap of size w×h, supersampled 3× so GDI's corners come out smooth.
func roundedBitmap(w, h, dpi, radius int, bg, fill, border walk.Color, withBorder bool) (*walk.Bitmap, error) {
	const ss = 3
	big, err := walk.NewBitmapForDPI(walk.Size{Width: w * ss, Height: h * ss}, dpi)
	if err != nil {
		return nil, err
	}
	defer big.Dispose()
	cv, err := walk.NewCanvasFromImage(big)
	if err != nil {
		return nil, err
	}
	bgb := solid(bg)
	cv.FillRectanglePixels(bgb, walk.Rectangle{Width: w * ss, Height: h * ss})
	bgb.Dispose()
	r := walk.Rectangle{Width: w * ss, Height: h * ss}
	if withBorder {
		fillRound(cv, border, r, radius*ss)
		r = walk.Rectangle{X: ss, Y: ss, Width: w*ss - 2*ss, Height: h*ss - 2*ss}
		fillRound(cv, fill, r, radius*ss-ss)
	} else {
		fillRound(cv, fill, r, radius*ss)
	}
	cv.Dispose()

	out, err := walk.NewBitmapForDPI(walk.Size{Width: w, Height: h}, dpi)
	if err != nil {
		return nil, err
	}
	ocv, err := walk.NewCanvasFromImage(out)
	if err != nil {
		out.Dispose()
		return nil, err
	}
	ocv.DrawImageStretchedPixels(big, walk.Rectangle{Width: w, Height: h})
	ocv.Dispose()
	return out, nil
}

// makeCard gives a composite a rounded white card background with a hairline border on the
// page colour. Children should use a transparent background to sit on it.
func makeCard(c *walk.Composite, page walk.Color) {
	var bmp *walk.Bitmap
	var brush *walk.BitmapBrush
	update := func() {
		r := c.ClientBoundsPixels()
		if r.Width <= 0 || r.Height <= 0 {
			return
		}
		nb, err := roundedBitmap(r.Width, r.Height, c.DPI(), scale(c, 8), page, flSurface, flLine, true)
		if err != nil {
			return
		}
		nbr, err := walk.NewBitmapBrush(nb)
		if err != nil {
			nb.Dispose()
			return
		}
		c.SetBackground(nbr)
		if brush != nil {
			brush.Dispose()
			bmp.Dispose()
		}
		bmp, brush = nb, nbr
		c.Invalidate()
	}
	c.SizeChanged().Attach(update)
	c.Disposing().Attach(func() {
		if brush != nil {
			brush.Dispose()
			bmp.Dispose()
		}
	})
	update()
}

// trackHover calls onLeave once the cursor has left w (Win32 offers no mouse-leave event to
// walk's custom widgets without TrackMouseEvent plumbing, so it polls while hovering).
func trackHover(w walk.Window, active *bool, onLeave func()) {
	if *active {
		return
	}
	*active = true
	var check func()
	check = func() {
		time.AfterFunc(120*time.Millisecond, func() {
			w.Synchronize(func() {
				if w.IsDisposed() {
					return
				}
				var pt win.POINT
				win.GetCursorPos(&pt)
				var rc win.RECT
				win.GetWindowRect(w.Handle(), &rc)
				if pt.X < rc.Left || pt.X >= rc.Right || pt.Y < rc.Top || pt.Y >= rc.Bottom || !w.Visible() {
					*active = false
					onLeave()
					return
				}
				check()
			})
		})
	}
	check()
}

// ---------- painted widget base ----------

const paintedClass = "Agendling_Painted"

func init() {
	walk.AppendToWalkInit(func() { walk.MustRegisterWindowClass(paintedClass) })
}

// Sizing of a painted widget in its parent's layout.
type sizing int

const (
	sizeFixed sizing = iota // exactly fixed (1/96")
	sizeFillH               // fixed height, takes the available width
	sizeFill                // takes all available space
)

// painted is an owner-drawn widget, double buffered, with an honest layout size (walk's
// CustomWidget is greedy in both directions and stretches whatever contains it).
type painted struct {
	walk.WidgetBase
	paint  func(cv *walk.Canvas, b walk.Rectangle)
	fixed  walk.Size // 1/96"
	sizing sizing
}

func newPainted(parent walk.Container, style uint32, fixed walk.Size, sz sizing, paint func(cv *walk.Canvas, b walk.Rectangle)) (*painted, error) {
	p := &painted{paint: paint, fixed: fixed, sizing: sz}
	if err := walk.InitWidget(p, parent, paintedClass, win.WS_VISIBLE|style, 0); err != nil {
		return nil, err
	}
	return p, nil
}

// setFixedSize changes the size requested from the layout.
func (p *painted) setFixedSize(s walk.Size) {
	p.fixed = s
	p.RequestLayout()
	p.Invalidate()
}

func (p *painted) WndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_PAINT:
		var ps win.PAINTSTRUCT
		hdc := win.BeginPaint(hwnd, &ps)
		p.draw(hdc)
		win.EndPaint(hwnd, &ps)
		return 0
	case win.WM_PRINTCLIENT:
		p.draw(win.HDC(wParam))
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_SIZE:
		p.Invalidate()
	}
	return p.WidgetBase.WndProc(hwnd, msg, wParam, lParam)
}

func (p *painted) draw(hdc win.HDC) {
	b := p.ClientBoundsPixels()
	if b.Width <= 0 || b.Height <= 0 || p.paint == nil {
		return
	}
	bmp, err := walk.NewBitmapForDPI(walk.Size{Width: b.Width, Height: b.Height}, p.DPI())
	if err != nil {
		return
	}
	defer bmp.Dispose()
	cv, err := walk.NewCanvasFromImage(bmp)
	if err != nil {
		return
	}
	p.paint(cv, b)
	win.BitBlt(hdc, 0, 0, int32(b.Width), int32(b.Height), cv.HDC(), 0, 0, win.SRCCOPY)
	cv.Dispose()
}

func (p *painted) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &paintedItem{size: walk.SizeFrom96DPI(p.fixed, ctx.DPI()), sizing: p.sizing}
}

type paintedItem struct {
	walk.LayoutItemBase
	size   walk.Size
	sizing sizing
}

func (li *paintedItem) LayoutFlags() walk.LayoutFlags {
	switch li.sizing {
	case sizeFillH:
		return walk.ShrinkableHorz | walk.GrowableHorz | walk.GreedyHorz
	case sizeFill:
		return walk.ShrinkableHorz | walk.GrowableHorz | walk.GreedyHorz | walk.ShrinkableVert | walk.GrowableVert | walk.GreedyVert
	}
	return 0
}

func (li *paintedItem) IdealSize() walk.Size { return li.size }
func (li *paintedItem) MinSize() walk.Size   { return li.size }

// ---------- navigation list ----------

type navItem struct {
	Key, Title, Glyph string
}

// navList is the Settings section list: icon + title rows, a rounded highlight and an accent bar
// on the selected row.
type navList struct {
	*painted
	items    []navItem
	selected string
	hover    int
	hovering bool
	onSelect func(key string)
}

func newNavList(parent walk.Container, onSelect func(string)) (*navList, error) {
	n := &navList{hover: -1, onSelect: onSelect}
	cw, err := newPainted(parent, win.WS_TABSTOP, walk.Size{Width: 100, Height: 100}, sizeFill, n.paint)
	if err != nil {
		return nil, err
	}
	n.painted = cw
	cw.MouseMove().Attach(func(x, y int, _ walk.MouseButton) {
		if i := n.rowAt(y); i != n.hover {
			n.hover = i
			cw.Invalidate()
		}
		trackHover(cw, &n.hovering, func() { n.hover = -1; cw.Invalidate() })
	})
	cw.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		if b == walk.LeftButton {
			if i := n.rowAt(y); i >= 0 {
				cw.SetFocus()
				n.choose(n.items[i].Key)
			}
		}
	})
	cw.KeyDown().Attach(func(key walk.Key) {
		cur := n.index(n.selected)
		switch key {
		case walk.KeyDown:
			if cur+1 < len(n.items) {
				n.choose(n.items[cur+1].Key)
			}
		case walk.KeyUp:
			if cur > 0 {
				n.choose(n.items[cur-1].Key)
			}
		}
	})
	cw.SetCursor(walk.CursorHand())
	return n, nil
}

func (n *navList) rowHeight() int { return scale(n, 38) }

func (n *navList) rowAt(y int) int {
	if i := y / n.rowHeight(); y >= 0 && i < len(n.items) {
		return i
	}
	return -1
}

func (n *navList) index(key string) int {
	for i, it := range n.items {
		if it.Key == key {
			return i
		}
	}
	return -1
}

func (n *navList) choose(key string) {
	if key == n.selected {
		return
	}
	n.selected = key
	n.Invalidate()
	if n.onSelect != nil {
		n.onSelect(key)
	}
}

// setItems replaces the rows (e.g. after a search); the selection is kept when still listed.
func (n *navList) setItems(items []navItem) {
	n.items = items
	n.hover = -1
	n.Invalidate()
}

func (n *navList) paint(cv *walk.Canvas, b walk.Rectangle) {
	bg := solid(flNav)
	cv.FillRectanglePixels(bg, b)
	bg.Dispose()
	rh := n.rowHeight()
	pad := scale(n, 2)
	for i, it := range n.items {
		r := walk.Rectangle{X: 0, Y: i*rh + pad, Width: b.Width, Height: rh - 2*pad}
		sel := it.Key == n.selected
		switch {
		case sel:
			fillRound(cv, flSelected, r, scale(n, 6))
			bar := walk.Rectangle{X: scale(n, 1), Y: r.Y + (r.Height-scale(n, 16))/2, Width: scale(n, 3), Height: scale(n, 16)}
			fillRound(cv, flAccent, bar, scale(n, 2))
		case i == n.hover:
			fillRound(cv, flHover, r, scale(n, 6))
		}
		drawText(cv, it.Glyph, iconFont(11), flText2,
			walk.Rectangle{X: r.X + scale(n, 12), Y: r.Y, Width: scale(n, 20), Height: r.Height}, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
		drawText(cv, it.Title, uiFont(10, sel), flText,
			walk.Rectangle{X: r.X + scale(n, 42), Y: r.Y, Width: r.Width - scale(n, 48), Height: r.Height}, textLeftMiddle)
	}
}

// ---------- toggle switch ----------

// toggleSwitch is an on/off switch drawn on the given background colour.
type toggleSwitch struct {
	*painted
	checked   bool
	bg        walk.Color
	onChanged func()
}

func newToggle(parent walk.Container, checked bool, bg walk.Color, onChanged func()) (*toggleSwitch, error) {
	t := &toggleSwitch{checked: checked, bg: bg, onChanged: onChanged}
	cw, err := newPainted(parent, win.WS_TABSTOP, walk.Size{Width: 44, Height: 24}, sizeFixed, t.paint)
	if err != nil {
		return nil, err
	}
	t.painted = cw
	cw.SetCursor(walk.CursorHand())
	cw.MouseDown().Attach(func(_, _ int, b walk.MouseButton) {
		if b == walk.LeftButton && cw.Enabled() {
			t.SetChecked(!t.checked)
			if t.onChanged != nil {
				t.onChanged()
			}
		}
	})
	cw.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeySpace {
			t.SetChecked(!t.checked)
			if t.onChanged != nil {
				t.onChanged()
			}
		}
	})
	return t, nil
}

func (t *toggleSwitch) Checked() bool { return t.checked }

func (t *toggleSwitch) SetChecked(v bool) {
	t.checked = v
	t.Invalidate()
}

func (t *toggleSwitch) paint(cv *walk.Canvas, b walk.Rectangle) {
	track := flSwitchOff
	if t.checked {
		track = flAccent
	}
	bmp, err := roundedBitmap(b.Width, b.Height, t.DPI(), b.Height/2, t.bg, track, track, false)
	if err != nil {
		return
	}
	defer bmp.Dispose()
	cv.DrawImagePixels(bmp, walk.Point{})
	d := b.Height - scale(t, 8)
	x := scale(t, 4)
	if t.checked {
		x = b.Width - d - scale(t, 4)
	}
	knob := solid(flSurface)
	defer knob.Dispose()
	cv.FillEllipsePixels(knob, walk.Rectangle{X: x, Y: (b.Height - d) / 2, Width: d, Height: d})
}

// ---------- segmented control ----------

// segmented is a row of mutually exclusive options in a rounded track, e.g. the view modes.
type segmented struct {
	*painted
	labels   []string
	selected int
	hover    int
	hovering bool
	bg       walk.Color
	onSelect func(int)
}

func newSegmented(parent walk.Container, labels []string, bg walk.Color, onSelect func(int)) (*segmented, error) {
	s := &segmented{labels: labels, hover: -1, bg: bg, onSelect: onSelect}
	w := 0
	for _, l := range labels {
		w += segmentWidth(l)
	}
	cw, err := newPainted(parent, 0, walk.Size{Width: w + 8, Height: 32}, sizeFixed, s.paint)
	if err != nil {
		return nil, err
	}
	s.painted = cw
	cw.SetCursor(walk.CursorHand())
	cw.MouseMove().Attach(func(x, _ int, _ walk.MouseButton) {
		if i := s.segmentAt(x); i != s.hover {
			s.hover = i
			cw.Invalidate()
		}
		trackHover(cw, &s.hovering, func() { s.hover = -1; cw.Invalidate() })
	})
	cw.MouseDown().Attach(func(x, _ int, b walk.MouseButton) {
		if i := s.segmentAt(x); b == walk.LeftButton && i >= 0 && i != s.selected {
			s.SetSelected(i)
			if s.onSelect != nil {
				s.onSelect(i)
			}
		}
	})
	return s, nil
}

func (s *segmented) SetSelected(i int) {
	s.selected = i
	s.Invalidate()
}

func segmentWidth(label string) int { return textWidth(label, uiFont(10, true)) + 28 }

// segments splits the inner width proportionally to the label widths.
func (s *segmented) segments() []walk.Rectangle {
	b := s.ClientBoundsPixels()
	inset := scale(s, 3)
	total := 0
	for _, l := range s.labels {
		total += segmentWidth(l)
	}
	res := make([]walk.Rectangle, len(s.labels))
	x := inset
	inner := b.Width - 2*inset
	for i, l := range s.labels {
		w := inner * segmentWidth(l) / total
		if i == len(s.labels)-1 {
			w = b.Width - inset - x
		}
		res[i] = walk.Rectangle{X: x, Y: inset, Width: w, Height: b.Height - 2*inset}
		x += w
	}
	return res
}

func (s *segmented) segmentAt(x int) int {
	for i, r := range s.segments() {
		if x >= r.X && x < r.X+r.Width {
			return i
		}
	}
	return -1
}

func (s *segmented) paint(cv *walk.Canvas, b walk.Rectangle) {
	track, err := roundedBitmap(b.Width, b.Height, s.DPI(), scale(s, 7), s.bg, walk.RGB(0xe8, 0xeb, 0xef), flLine, false)
	if err != nil {
		return
	}
	cv.DrawImagePixels(track, walk.Point{})
	track.Dispose()
	for i, r := range s.segments() {
		color := flText2
		switch {
		case i == s.selected:
			pill, err := roundedBitmap(r.Width, r.Height, s.DPI(), scale(s, 5), walk.RGB(0xe8, 0xeb, 0xef), flSurface, flLine, true)
			if err == nil {
				cv.DrawImagePixels(pill, walk.Point{X: r.X, Y: r.Y})
				pill.Dispose()
			}
			color = flText
		case i == s.hover:
			color = flText
		}
		drawText(cv, s.labels[i], uiFont(10, i == s.selected), color, r, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	}
}

// ---------- flat buttons ----------

type btnKind int

const (
	btnGhost   btnKind = iota // no frame until hovered
	btnOutline                // white with a hairline frame
	btnPrimary                // accent filled
)

// flatButton is a toolbar button: an optional glyph, text and trailing glyph.
type flatButton struct {
	*painted
	glyph, text string
	kind        btnKind
	bg          walk.Color
	hover       bool
	hovering    bool
	onClick     func()
	trailing    string // glyph after the text, e.g. a dropdown chevron
}

func newFlatButton(parent walk.Container, glyph, text string, kind btnKind, bg walk.Color, onClick func()) (*flatButton, error) {
	f := &flatButton{glyph: glyph, text: text, kind: kind, bg: bg, onClick: onClick}
	cw, err := newPainted(parent, 0, walk.Size{Width: 32, Height: 32}, sizeFixed, f.paint)
	if err != nil {
		return nil, err
	}
	f.painted = cw
	f.resize()
	cw.SetCursor(walk.CursorHand())
	cw.MouseMove().Attach(func(_, _ int, _ walk.MouseButton) {
		if !f.hover {
			f.hover = true
			cw.Invalidate()
		}
		trackHover(cw, &f.hovering, func() { f.hover = false; cw.Invalidate() })
	})
	cw.MouseUp().Attach(func(_, _ int, b walk.MouseButton) {
		if b == walk.LeftButton && f.onClick != nil {
			f.onClick()
		}
	})
	return f, nil
}

func (f *flatButton) resize() {
	w := 24
	if f.glyph != "" {
		w += 22
	}
	if f.text != "" {
		w += textWidth(f.text, uiFont(10, f.kind == btnPrimary))
	}
	if f.trailing != "" {
		w += 18
	}
	if f.text == "" && f.glyph != "" {
		w = 34
	}
	f.setFixedSize(walk.Size{Width: w, Height: 32})
}

// SetTrailing sets a glyph after the text, e.g. a dropdown chevron.
func (f *flatButton) SetTrailing(g string) {
	f.trailing = g
	f.resize()
}

func (f *flatButton) SetText(s string) {
	f.text = s
	f.resize()
	f.Invalidate()
}

func (f *flatButton) paint(cv *walk.Canvas, b walk.Rectangle) {
	fill, fg, border := f.bg, flText, f.bg
	switch {
	case f.kind == btnPrimary && f.hover:
		fill, fg, border = flAccentHover, flSurface, flAccentHover
	case f.kind == btnPrimary:
		fill, fg, border = flAccent, flSurface, flAccent
	case f.kind == btnOutline && f.hover:
		fill, border = flHover, walk.RGB(0xd0, 0xd5, 0xdd)
	case f.kind == btnOutline:
		fill, border = flSurface, walk.RGB(0xd0, 0xd5, 0xdd)
	case f.hover:
		fill, border = flHover, flHover
	}
	bmp, err := roundedBitmap(b.Width, b.Height, f.DPI(), scale(f, 6), f.bg, fill, border, border != fill)
	if err != nil {
		return
	}
	cv.DrawImagePixels(bmp, walk.Point{})
	bmp.Dispose()
	x := scale(f, 12)
	if f.text == "" {
		drawText(cv, f.glyph, iconFont(11), fg, b, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
		return
	}
	if f.glyph != "" {
		drawText(cv, f.glyph, iconFont(10), fg, walk.Rectangle{X: x, Y: 0, Width: scale(f, 16), Height: b.Height}, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
		x += scale(f, 22)
	}
	tw := b.Width - x - scale(f, 12)
	if f.trailing != "" {
		tw -= scale(f, 16)
		drawText(cv, f.trailing, iconFont(8), flText2, walk.Rectangle{X: b.Width - scale(f, 24), Y: 0, Width: scale(f, 14), Height: b.Height},
			walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	}
	drawText(cv, f.text, uiFont(10, f.kind == btnPrimary), fg, walk.Rectangle{X: x, Y: 0, Width: tw, Height: b.Height}, textLeftMiddle)
}

// newColorDot is a small filled circle, e.g. a calendar's colour.
func newColorDot(parent walk.Container, c, bg walk.Color) *painted {
	var p *painted
	p, _ = newPainted(parent, 0, walk.Size{Width: 16, Height: 16}, sizeFixed, func(cv *walk.Canvas, b walk.Rectangle) {
		bgb := solid(bg)
		cv.FillRectanglePixels(bgb, b)
		bgb.Dispose()
		d := scale(p, 12)
		dot, err := roundedBitmap(d, d, p.DPI(), d/2, bg, c, c, false)
		if err != nil {
			return
		}
		cv.DrawImagePixels(dot, walk.Point{X: (b.Width - d) / 2, Y: (b.Height - d) / 2})
		dot.Dispose()
	})
	return p
}
