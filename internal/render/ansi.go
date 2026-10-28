// Package render draws scan results as colourful, band-grouped terminal
// tables.
package render

import (
	"fmt"
	"os"
	"strings"
)

// Profile is the colour capability of the output.
type Profile int

// Colour profiles.
const (
	ProfileNone      Profile = iota // plain text
	Profile256                      // 256-colour palette
	ProfileTrueColor                // 24-bit colour
)

// ParseColorMode resolves --color auto|always|never for the given output
// file, honouring the NO_COLOR convention (https://no-color.org).
func ParseColorMode(mode string, out *os.File) (Profile, error) {
	switch strings.ToLower(mode) {
	case "never", "no", "false", "0":
		return ProfileNone, nil
	case "always", "yes", "true", "1":
		return detectDepth(), nil
	case "auto", "":
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || !IsTerminal(out) {
			return ProfileNone, nil
		}
		return detectDepth(), nil
	}
	return ProfileNone, fmt.Errorf("unknown colour mode %q (use auto, always or never)", mode)
}

func detectDepth() Profile {
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return ProfileTrueColor
	}
	return Profile256
}

// Color is an RGB colour.
type Color struct{ R, G, B uint8 }

// Style is a foreground colour plus text attributes. The zero value means
// "terminal default, no attributes".
type Style struct {
	FG     Color
	HasFG  bool
	Bold   bool
	Italic bool
}

func fg(r, g, b uint8) Style { return Style{FG: Color{r, g, b}, HasFG: true} }

func (s Style) bold() Style   { s.Bold = true; return s }
func (s Style) italic() Style { s.Italic = true; return s }

// Paint wraps text in the escape sequences for the style. Plain profiles and
// empty styles return the text unchanged.
func (p Profile) Paint(text string, s Style) string {
	if p == ProfileNone || text == "" || s == (Style{}) {
		return text
	}
	var codes []string
	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Italic {
		codes = append(codes, "3")
	}
	if s.HasFG {
		if p == ProfileTrueColor {
			codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", s.FG.R, s.FG.G, s.FG.B))
		} else {
			codes = append(codes, fmt.Sprintf("38;5;%d", to256(s.FG)))
		}
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

// to256 maps an RGB colour to the nearest xterm-256 palette entry (greys use
// the 24-step ramp, everything else the 6x6x6 colour cube).
func to256(c Color) int {
	r, g, b := int(c.R), int(c.G), int(c.B)
	if abs(r-g) < 10 && abs(g-b) < 10 {
		switch {
		case r < 8:
			return 16
		case r > 248:
			return 231
		}
		return 232 + (r-8)*24/247
	}
	cube := func(v int) int { return (v*5 + 127) / 255 }
	return 16 + 36*cube(r) + 6*cube(g) + cube(b)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
