package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rguziy/wscan/internal/wifi"
)

func demoRecommendation() *wifi.Recommendation {
	rec := wifi.Recommend([][]wifi.AP{wifi.Demo().APs}, wifi.RecommendOptions{})
	rec.Interface = "wlan0"
	return rec
}

func renderRec(t *testing.T, rec *wifi.Recommendation, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := RenderRecommendation(&buf, rec, o); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRenderRecommendationContent(t *testing.T) {
	out := renderRec(t, demoRecommendation(), Options{Profile: ProfileNone, Width: 120})
	if strings.Contains(out, "\x1b") {
		t.Error("plain output contains escape sequences")
	}
	for _, want := range []string{
		"channel recommendations", "2.4 GHz", "5 GHz", "6 GHz",
		"5745 MHz", "5735–5815 MHz", // channel 149 comes with its frequencies
		"DFS", "now", "Ignored", "(connected)", "Country",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q", want)
		}
	}
}

func TestRenderRecommendationFitsWidth(t *testing.T) {
	for _, width := range []int{50, 60, 80, 100, 140} {
		out := renderRec(t, demoRecommendation(), Options{Profile: ProfileNone, Width: width})
		for _, line := range strings.Split(out, "\n") {
			if w := textWidth(line); w > width {
				t.Errorf("width %d: line is %d cells wide: %q", width, w, line)
			}
			if line != strings.TrimRight(line, " ") {
				t.Errorf("width %d: trailing spaces in %q", width, line)
			}
		}
	}
}

func TestRenderRecommendationEmptyBand(t *testing.T) {
	rec := wifi.Recommend(nil, wifi.RecommendOptions{Country: "CN"})
	out := renderRec(t, rec, Options{Profile: ProfileNone, Width: 100})
	if !strings.Contains(out, "6 GHz is not allowed in CN") {
		t.Errorf("missing the 6 GHz explanation:\n%s", out)
	}
}

func TestLoadBar(t *testing.T) {
	cases := []struct {
		score float64
		want  string // filled part
	}{
		{0, ""},
		{0.01, "▌"}, // any load is visible
		{1.5, "█████"},
		{3, "██████████"},
		{9, "██████████"},
	}
	for _, c := range cases {
		filled, empty := loadBar(c.score, 10)
		if filled != c.want {
			t.Errorf("loadBar(%v) filled = %q, want %q", c.score, filled, c.want)
		}
		if textWidth(filled)+textWidth(empty) != 10 {
			t.Errorf("loadBar(%v) is %d cells wide", c.score, textWidth(filled)+textWidth(empty))
		}
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("one two three four five", 9)
	want := []string{"one two", "three", "four five"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if got := wrapWords("supercalifragilistic", 5); len(got) != 1 {
		t.Errorf("long word split: %q", got)
	}
}

func TestRenderUnscannableBand(t *testing.T) {
	adapter := map[wifi.Band]wifi.BandSupport{
		wifi.Band2G: wifi.SupportOK, wifi.Band5G: wifi.SupportOK, wifi.Band6G: wifi.SupportNone,
	}
	rec := wifi.Recommend(nil, wifi.RecommendOptions{Country: "UA", Adapter: adapter})
	out := renderRec(t, rec, Options{Profile: ProfileNone, Width: 100})
	if !strings.Contains(out, "not scannable") || !strings.Contains(out, "does not support 6 GHz") {
		t.Errorf("missing the unsupported-band explanation:\n%s", out)
	}
	// There must be no ranking table for that band.
	section := out[strings.Index(out, "6 GHz"):]
	if strings.Contains(section, "FREQUENCY") {
		t.Errorf("unsupported band still has a table:\n%s", section)
	}
}
