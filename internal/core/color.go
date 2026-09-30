package core

import (
	"fmt"
	"strconv"
	"strings"
)

type RGB struct{ R, G, B uint8 }

var DefaultColor = RGB{0x3a, 0x87, 0xad}

// ParseColor reads "#rrggbb", falling back to DefaultColor.
func ParseColor(hex string) RGB {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return DefaultColor
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return DefaultColor
	}
	return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// Blend mixes c with white; amount 0 = c, 1 = white.
func (c RGB) Blend(amount float64) RGB {
	mix := func(v uint8) uint8 { return uint8(float64(v) + (255-float64(v))*amount) }
	return RGB{mix(c.R), mix(c.G), mix(c.B)}
}

// Floats returns the components in 0..1 (for Cairo).
func (c RGB) Floats() (float64, float64, float64) {
	return float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255
}

// Palette shared by both front ends.
var (
	ColBackground = RGB{255, 255, 255}
	ColGrid       = RGB{222, 222, 222}
	ColGridLight  = RGB{240, 240, 240}
	ColText       = RGB{35, 35, 35}
	ColMuted      = RGB{125, 125, 125}
	ColAccent     = RGB{26, 115, 232}
	ColNow        = RGB{234, 67, 53}
	ColOtherMonth = RGB{248, 248, 248}
	ColToday      = RGB{232, 240, 254}
	ColTodayCol   = RGB{250, 252, 255}
)
