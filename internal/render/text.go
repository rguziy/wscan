package render

import (
	"strings"
	"unicode"
)

// runeWidth returns the number of terminal cells a rune occupies: 0 for
// combining and format characters, 2 for East Asian wide characters and
// emoji, 1 otherwise. It is an approximation of wcwidth that covers what
// shows up in SSIDs.
func runeWidth(r rune) int {
	switch {
	case r == 0, r < 0x20, r >= 0x7f && r < 0xa0:
		return 0
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case isWide(r):
		return 2
	}
	return 1
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f680 && r <= 0x1f6ff,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x1fa70 && r <= 0x1faff,
		r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}

// textWidth returns the display width of s in terminal cells.
func textWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// truncate shortens s to at most width cells, ending it with an ellipsis when
// something was cut.
func truncate(s string, width int) string {
	if textWidth(s) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	var sb strings.Builder
	used := 0
	for _, r := range s {
		w := runeWidth(r)
		if used+w > width-1 {
			break
		}
		sb.WriteRune(r)
		used += w
	}
	sb.WriteRune('…')
	return sb.String()
}

// seg is a piece of text with one style. A cell is a list of segments.
type seg struct {
	text  string
	style Style
}

func segsWidth(segs []seg) int {
	w := 0
	for _, s := range segs {
		w += textWidth(s.text)
	}
	return w
}

// fitSegs truncates a cell to at most width cells, ending it with an
// ellipsis when something was cut.
func fitSegs(segs []seg, width int) []seg {
	if segsWidth(segs) <= width {
		return segs
	}
	if width <= 0 {
		return nil
	}
	budget := width - 1 // leave room for the ellipsis
	var (
		out  []seg
		last Style
	)
	for _, s := range segs {
		if budget <= 0 {
			break
		}
		if w := textWidth(s.text); w <= budget {
			out = append(out, s)
			budget -= w
			last = s.style
			continue
		}
		var sb strings.Builder
		for _, r := range s.text {
			rw := runeWidth(r)
			if rw > budget {
				break
			}
			sb.WriteRune(r)
			budget -= rw
		}
		out = append(out, seg{sb.String(), s.style})
		last = s.style
		break
	}
	return append(out, seg{"…", last})
}

// wrapWords breaks text into lines of at most width cells at spaces. A single
// word longer than width stays whole on its own line.
func wrapWords(text string, width int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case textWidth(cur)+1+textWidth(word) <= width:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
