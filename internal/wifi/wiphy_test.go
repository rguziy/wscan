package wifi

import (
	"strings"
	"testing"

	"github.com/rguziy/wscan/internal/genl"
)

// wiphyMsg builds one reply of a split GET_WIPHY dump: the wiphy index and,
// for each band index, a list of frequencies (negative = disabled).
func wiphyMsg(wiphy uint32, bands map[uint16][]int) genl.Message {
	var b genl.Builder
	b.Uint32(attrWiphy, wiphy)
	if bands != nil {
		b.Nested(attrWiphyBands, func(nb *genl.Builder) {
			for idx, freqs := range bands {
				nb.Nested(idx, func(bb *genl.Builder) {
					bb.Nested(bandAttrFreqs, func(fb *genl.Builder) {
						for i, f := range freqs {
							disabled := f < 0
							if disabled {
								f = -f
							}
							fb.Nested(uint16(i), func(e *genl.Builder) {
								e.Uint32(freqAttrFreq, uint32(f))
								if disabled {
									e.Bytes(freqAttrDisabled, nil)
								}
							})
						}
					})
				})
			}
		})
	}
	return genl.Message{Command: cmdGetWiphy, Version: 1, Data: b.Data()}
}

func TestParseWiphyBands(t *testing.T) {
	msgs := []genl.Message{
		wiphyMsg(0, map[uint16][]int{
			0: {2412, 2417},
			1: {5180, -5200}, // one enabled channel is enough
		}),
		// The 6 GHz band arrives in a later message of the same wiphy; all of
		// its channels are off in the current regulatory domain.
		wiphyMsg(0, map[uint16][]int{3: {-5955, -5975}}),
		// Another adapter must not leak into the result.
		wiphyMsg(1, map[uint16][]int{3: {6115}}),
	}
	got := parseWiphyBands(msgs, 0)
	if got[Band2G] != SupportOK || got[Band5G] != SupportOK || got[Band6G] != SupportDisabled {
		t.Errorf("got %v", got)
	}

	// No 6 GHz hardware at all.
	got = parseWiphyBands([]genl.Message{wiphyMsg(0, map[uint16][]int{0: {2412}, 1: {5180}})}, 0)
	if got[Band6G] != SupportNone || got[Band5G] != SupportOK {
		t.Errorf("no-6GHz adapter: %v", got)
	}

	// Replies without band data mean "unknown", never "no support".
	if got := parseWiphyBands([]genl.Message{wiphyMsg(0, nil)}, 0); got != nil {
		t.Errorf("no band data: %v, want nil", got)
	}
	if got := parseWiphyBands(nil, 0); got != nil {
		t.Errorf("no replies: %v, want nil", got)
	}
}

func TestParseInterfaceWiphy(t *testing.T) {
	var b genl.Builder
	b.Uint32(attrWiphy, 2)
	b.Uint32(attrIfIndex, 3)
	b.String(attrIfName, "wlan0")
	b.Uint32(attrIfType, ifTypeStation)
	iface, ok := parseInterface(genl.Message{Data: b.Data()})
	if !ok || iface.Wiphy != 2 || iface.Name != "wlan0" {
		t.Errorf("got %+v, ok=%v", iface, ok)
	}
}

func TestRecommendUnsupportedBands(t *testing.T) {
	adapter := map[Band]BandSupport{Band2G: SupportOK, Band5G: SupportOK, Band6G: SupportNone}
	rec := Recommend([][]AP{{mkAP("n", "00:00:00:00:00:01", Band5G, 36, 20, -60)}},
		RecommendOptions{Country: "UA", Adapter: adapter})
	br := bandOf(t, rec, Band6G)
	if len(br.Candidates) != 0 || br.Support != SupportNone || !strings.Contains(br.Note, "does not support 6 GHz") {
		t.Errorf("6 GHz on an adapter without it: %+v", br)
	}
	if len(bandOf(t, rec, Band5G).Candidates) == 0 {
		t.Error("supported bands must still be ranked")
	}

	adapter[Band6G] = SupportDisabled
	br = bandOf(t, Recommend(nil, RecommendOptions{Country: "UA", Adapter: adapter}), Band6G)
	if len(br.Candidates) != 0 || !strings.Contains(br.Note, "regulatory domain") || !strings.Contains(br.Note, "iw reg set UA") {
		t.Errorf("disabled 6 GHz: %+v", br)
	}

	// Unknown support keeps the old behaviour: rank, with a caveat.
	br = bandOf(t, Recommend(nil, RecommendOptions{Country: "UA"}), Band6G)
	if len(br.Candidates) == 0 || !strings.Contains(br.Note, "cannot scan it") {
		t.Errorf("unknown support: %+v", br)
	}
}

func TestEmptyBandNotes(t *testing.T) {
	adapter := map[Band]BandSupport{Band2G: SupportOK, Band5G: SupportOK, Band6G: SupportOK}

	// Only our own network is in the band: it is ignored, so the band is
	// "empty" for the calculation, but the note must not blame the adapter.
	own := mkAP("RVL-6G", "38:32:7A:5A:2D:14", Band6G, 53, 160, -50)
	own.Connected = true
	br := bandOf(t, Recommend([][]AP{{own}}, RecommendOptions{Adapter: adapter}), Band6G)
	if !strings.Contains(br.Note, "only your own") || strings.Contains(br.Note, "adapter") {
		t.Errorf("own-network-only note = %q", br.Note)
	}

	// A supported adapter that sees nothing: the band is simply empty.
	br = bandOf(t, Recommend(nil, RecommendOptions{Country: "UA", Adapter: adapter}), Band6G)
	if br.Note != "no networks seen: the band is empty" {
		t.Errorf("empty band note = %q", br.Note)
	}
}

func TestCurrentRowShowsRealPrimaryChannel(t *testing.T) {
	// 160 MHz on primary channel 53 occupies the block 33-61. The "now" row
	// must say 53 (and its frequency), not the first channel of the block.
	own := mkAP("RVL-6G", "38:32:7A:5A:2D:14", Band6G, 53, 160, -50)
	own.Connected = true
	rec := Recommend([][]AP{{own}}, RecommendOptions{Country: "UA", Width: 80})
	cur := bandOf(t, rec, Band6G).Current
	if cur == nil {
		t.Fatal("no current row")
	}
	if cur.Channel != 53 || cur.FrequencyMHz != 6215 || cur.WidthMHz != 160 || cur.FromMHz != 6105 || cur.ToMHz != 6265 {
		t.Errorf("current = ch %d, %d MHz, width %d, span %d-%d", cur.Channel, cur.FrequencyMHz, cur.WidthMHz, cur.FromMHz, cur.ToMHz)
	}
}
