package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rguziy/wscan/internal/wifi"
)

func TestSignalLevel(t *testing.T) {
	cases := []struct{ dbm, want int }{
		{-30, 0}, {-50, 0}, {-51, 1}, {-60, 1}, {-61, 2},
		{-70, 2}, {-71, 3}, {-80, 3}, {-81, 4}, {-95, 4},
	}
	for _, c := range cases {
		if got := signalLevel(c.dbm); got != c.want {
			t.Errorf("signalLevel(%d) = %d, want %d", c.dbm, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"a very long network name", 8, "a very …"},
		{"anything", 0, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.width); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}

func renderDemo(t *testing.T, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Render(&buf, wifi.Demo(), o); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRenderPlainHasNoEscapes(t *testing.T) {
	out := renderDemo(t, Options{Profile: ProfileNone, Width: 120})
	if strings.Contains(out, "\x1b") {
		t.Error("plain output contains escape sequences")
	}
	for _, want := range []string{"2.4 GHz", "5 GHz", "6 GHz", "Lakeside", "<hidden>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q", want)
		}
	}
}

func TestRenderColourHasEscapes(t *testing.T) {
	out := renderDemo(t, Options{Profile: Profile256, Width: 120})
	if !strings.Contains(out, "\x1b[") {
		t.Error("colour output contains no escape sequences")
	}
}

func TestRenderBandFilter(t *testing.T) {
	out := renderDemo(t, Options{
		Profile: ProfileNone,
		Width:   120,
		Bands:   map[wifi.Band]bool{wifi.Band5G: true},
	})
	if !strings.Contains(out, "5 GHz") {
		t.Error("5 GHz section is missing")
	}
	for _, unwanted := range []string{"2.4 GHz ", "6 GHz "} {
		if strings.Contains(out, "── "+unwanted) {
			t.Errorf("filtered output still has the %q section", unwanted)
		}
	}
}

func TestRenderFitsWidth(t *testing.T) {
	for _, width := range []int{80, 100, 140} {
		out := renderDemo(t, Options{Profile: ProfileNone, Width: width, Details: true})
		for _, line := range strings.Split(out, "\n") {
			if w := textWidth(line); w > width {
				t.Errorf("width %d: line is %d cells wide: %q", width, w, line)
			}
		}
	}
}

func TestRenderUnknownSeenAge(t *testing.T) {
	res := wifi.Demo()
	for i := range res.APs {
		res.APs[i].SeenAgoMS = -1
	}
	res.Fresh = false
	var buf bytes.Buffer
	if err := Render(&buf, res, Options{Profile: ProfileNone, Width: 120, Details: true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "seen -") || strings.Contains(out, "ago") {
		t.Errorf("unknown age leaked into output:\n%s", out)
	}
	if !strings.Contains(out, "cached results") || strings.Contains(out, "newest") {
		t.Error("header should say cached results without an age")
	}
}
