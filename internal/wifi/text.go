package wifi

import (
	"fmt"
	"strings"
	"unicode"
)

// Sanitize makes untrusted text (an SSID, for example) safe to print on a
// terminal. Access points control these strings, so control characters
// (including ESC, which starts terminal escape sequences) are replaced by a
// middle dot, invalid UTF-8 by U+FFFD, and bidirectional override characters
// that could reorder the surrounding output are dropped.
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case isBidiControl(r):
			// dropped
		case unicode.IsControl(r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r):
			sb.WriteRune('·')
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func isBidiControl(r rune) bool {
	switch {
	case r == 0x061c, r == 0x200e, r == 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// cleanSSID decodes an SSID element. An SSID that is empty or made of NUL
// bytes only is hidden.
func cleanSSID(b []byte) (ssid string, hidden bool) {
	allZero := true
	for _, c := range b {
		if c != 0 {
			allZero = false
			break
		}
	}
	if len(b) == 0 || allZero {
		return "", true
	}
	ssid = Sanitize(string(b))
	return ssid, ssid == ""
}

// cleanCountry decodes the country string of the Country element.
func cleanCountry(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	c := b[:2]
	for _, ch := range c {
		if !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z') {
			return ""
		}
	}
	return strings.ToUpper(string(c))
}

// formatMAC renders a hardware address as upper-case, colon separated hex.
func formatMAC(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, ":")
}
