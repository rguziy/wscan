package wifi

import (
	"reflect"
	"strings"
	"testing"
)

func mkAP(ssid, bssid string, b Band, ch, width, dbm int) AP {
	return AP{
		SSID: ssid, BSSID: bssid, Band: b, Channel: ch, WidthMHz: width,
		FrequencyMHz: channelFreq(b, ch), SignalDBM: dbm, Country: "DE",
	}
}

func bandOf(t *testing.T, rec *Recommendation, b Band) BandRecommendation {
	t.Helper()
	for _, br := range rec.Bands {
		if br.Band == b {
			return br
		}
	}
	t.Fatalf("no recommendation for %v", b)
	return BandRecommendation{}
}

func TestBlockChannels(t *testing.T) {
	cases := []struct {
		b       Band
		primary int
		width   int
		want    []int
	}{
		{Band5G, 36, 20, []int{36}},
		{Band5G, 44, 40, []int{44, 48}},
		{Band5G, 52, 80, []int{52, 56, 60, 64}},
		{Band5G, 157, 80, []int{149, 153, 157, 161}},
		{Band5G, 100, 160, []int{100, 104, 108, 112, 116, 120, 124, 128}},
		{Band5G, 165, 40, nil},
		{Band6G, 5, 80, []int{1, 5, 9, 13}},
		{Band6G, 37, 80, []int{33, 37, 41, 45}},
		{Band6G, 9, 40, []int{9, 13}},
		{Band6G, 3, 40, nil},
	}
	for _, c := range cases {
		if got := blockChannels(c.b, c.primary, c.width); !reflect.DeepEqual(got, c.want) {
			t.Errorf("blockChannels(%v, %d, %d) = %v, want %v", c.b, c.primary, c.width, got, c.want)
		}
	}
}

func TestSpanAndFrequencies(t *testing.T) {
	// Channel 149 at 80 MHz covers 5735-5815 MHz; the user needs to see the
	// frequency, not just the channel number.
	scene := [][]AP{{mkAP("far", "00:00:00:00:00:01", Band5G, 36, 20, -60)}}
	rec := Recommend(scene, RecommendOptions{Country: "DE", Width: 80, Top: 20})
	var c *Candidate
	for _, cand := range bandOf(t, rec, Band5G).Candidates {
		if cand.Channel == 149 {
			c = &cand
		}
	}
	if c == nil {
		t.Fatal("channel 149 is not a candidate")
	}
	if c.FrequencyMHz != 5745 || c.CenterMHz != 5775 || c.FromMHz != 5735 || c.ToMHz != 5815 || c.WidthMHz != 80 {
		t.Errorf("149/80: freq=%d center=%d span=%d-%d width=%d", c.FrequencyMHz, c.CenterMHz, c.FromMHz, c.ToMHz, c.WidthMHz)
	}
	if !reflect.DeepEqual(c.Channels, []int{149, 153, 157, 161}) {
		t.Errorf("channels = %v", c.Channels)
	}
}

func Test24GHzPrefersEmptyChannel(t *testing.T) {
	scene := [][]AP{{
		mkAP("a", "00:00:00:00:00:01", Band2G, 1, 20, -45),
		mkAP("b", "00:00:00:00:00:02", Band2G, 1, 20, -50),
		mkAP("c", "00:00:00:00:00:03", Band2G, 6, 20, -60),
		mkAP("d", "00:00:00:00:00:04", Band2G, 4, 20, -70), // spills onto 1 and 6
	}}
	rec := Recommend(scene, RecommendOptions{Country: "DE"})
	br := bandOf(t, rec, Band2G)
	if br.WidthMHz != 20 {
		t.Errorf("2.4 GHz width = %d, want 20", br.WidthMHz)
	}
	got := []int{}
	for _, c := range br.Candidates {
		got = append(got, c.Channel)
	}
	if !reflect.DeepEqual(got, []int{11, 6, 1}) {
		t.Fatalf("order = %v, want [11 6 1]", got)
	}
	if br.Candidates[0].Level != LevelClear || br.Candidates[2].Level != LevelHeavy {
		t.Errorf("levels: best %q worst %q", br.Candidates[0].Level, br.Candidates[2].Level)
	}
	if br.Candidates[2].Networks != 3 { // a, b and the overlapping d
		t.Errorf("channel 1 overlaps %d networks, want 3", br.Candidates[2].Networks)
	}
	if br.Candidates[2].StrongestDBM != -45 {
		t.Errorf("strongest on channel 1 = %d", br.Candidates[2].StrongestDBM)
	}
}

func Test24GHzUSHasNoChannel13(t *testing.T) {
	if allowed(regionUS, Band2G, 13) || !allowed(regionETSI, Band2G, 13) {
		t.Error("channel 13 must be allowed in Europe but not in the US")
	}
}

func Test5GHzPrefersEmptyBlockAndNonDFS(t *testing.T) {
	scene := [][]AP{{
		mkAP("lo", "00:00:00:00:00:01", Band5G, 36, 80, -45),
		mkAP("mid", "00:00:00:00:00:02", Band5G, 100, 80, -50),
	}}
	rec := Recommend(scene, RecommendOptions{Country: "DE", Width: 80})
	br := bandOf(t, rec, Band5G)
	// Free blocks: 52-64 (DFS), 116-128 (DFS), 149-161. The non-DFS one wins
	// the tie, DFS blocks follow in channel order.
	got := []int{}
	for _, c := range br.Candidates {
		got = append(got, c.Channel)
	}
	if !reflect.DeepEqual(got, []int{149, 52, 116}) {
		t.Fatalf("order = %v, want [149 52 116]", got)
	}
	if br.Candidates[0].DFS || !br.Candidates[1].DFS {
		t.Errorf("DFS flags: %v %v", br.Candidates[0].DFS, br.Candidates[1].DFS)
	}
}

func Test5GHzRegions(t *testing.T) {
	has := func(cc string, width, primary int) bool {
		for _, blk := range candidateBlocks(regionOf(cc), Band5G, width) {
			if blk[0] == primary {
				return true
			}
		}
		return false
	}
	if has("DE", 80, 132) || !has("US", 80, 132) {
		t.Error("block 132-144 needs channel 144: US only")
	}
	if has("JP", 80, 149) || !has("DE", 80, 149) {
		t.Error("149-161 is not allowed in Japan")
	}
	if !has("UA", 160, 100) {
		t.Error("160 MHz at 100-128 should be available")
	}
}

func Test6GHz(t *testing.T) {
	rec := Recommend(nil, RecommendOptions{Country: "DE", Width: 80})
	br := bandOf(t, rec, Band6G)
	if len(br.Candidates) != 3 || br.Candidates[0].Channel != 1 || br.Candidates[0].Level != LevelClear {
		t.Fatalf("candidates = %+v", br.Candidates)
	}
	if br.Candidates[0].FrequencyMHz != 5955 {
		t.Errorf("channel 1 frequency = %d, want 5955", br.Candidates[0].FrequencyMHz)
	}
	if br.Note == "" {
		t.Error("an empty band should explain that the adapter may not scan it")
	}

	// Europe has only the lower 6 GHz; the US has all of it.
	if len(candidateBlocks(regionETSI, Band6G, 160)) != 3 { // 480 MHz = three 160 MHz blocks
		t.Errorf("ETSI 160 MHz blocks = %v", candidateBlocks(regionETSI, Band6G, 160))
	}
	if len(candidateBlocks(regionUS, Band6G, 160)) <= 3 {
		t.Error("US should have more 160 MHz blocks")
	}

	cn := bandOf(t, Recommend(nil, RecommendOptions{Country: "CN"}), Band6G)
	if len(cn.Candidates) != 0 || !strings.Contains(cn.Note, "not allowed") {
		t.Errorf("CN 6 GHz: %+v", cn)
	}
}

func TestWidthNotAvailable(t *testing.T) {
	br := bandOf(t, Recommend(nil, RecommendOptions{Country: "DE", Width: 320}), Band5G)
	if len(br.Candidates) != 0 || br.Note == "" {
		t.Errorf("320 MHz on 5 GHz: %+v", br)
	}
}

func TestConnectedNetworkIsIgnoredByDefault(t *testing.T) {
	own5 := mkAP("Home", "AA:BB:CC:00:00:02", Band5G, 36, 80, -35)
	own5.Connected = true
	own5.Country = "UA"
	scene := [][]AP{{own5, mkAP("Home", "AA:BB:CC:00:00:01", Band2G, 6, 20, -30)}}

	rec := Recommend(scene, RecommendOptions{})
	if !reflect.DeepEqual(rec.Ignored, []string{"Home (connected)"}) {
		t.Errorf("ignored = %v", rec.Ignored)
	}
	if rec.Country != "UA" || rec.CountrySource != "connected network" {
		t.Errorf("country = %q from %q", rec.Country, rec.CountrySource)
	}
	// Without our own network, 36 is the best block, not worst.
	br := bandOf(t, rec, Band5G)
	if br.Candidates[0].Channel != 36 || br.Candidates[0].Level != LevelClear {
		t.Errorf("best 5 GHz = %+v", br.Candidates[0])
	}
	if br.Current == nil || br.Current.Channel != 36 || br.Current.WidthMHz != 80 {
		t.Errorf("current = %+v", br.Current)
	}
	if bandOf(t, rec, Band2G).Networks != 0 {
		t.Error("both SSIDs of the connected network must be ignored")
	}

	rec = Recommend(scene, RecommendOptions{IncludeConnected: true})
	if len(rec.Ignored) != 0 {
		t.Errorf("ignored = %v with IncludeConnected", rec.Ignored)
	}
	if got := bandOf(t, rec, Band5G).Candidates[0].Channel; got == 36 {
		t.Error("own network on 36 should push the recommendation away when included")
	}
}

func TestIgnoreListAndNotes(t *testing.T) {
	scene := [][]AP{{
		mkAP("Mine", "00:00:00:00:00:01", Band2G, 1, 20, -40),
		mkAP("Mine-IoT", "00:00:00:00:00:02", Band2G, 1, 20, -40),
		mkAP("Neighbour", "00:00:00:00:00:03", Band2G, 11, 20, -60),
	}}
	rec := Recommend(scene, RecommendOptions{
		Country: "DE",
		Ignore:  []string{"Mine", "00-00-00-00-00-02", "Ghost", "  "},
	})
	if !reflect.DeepEqual(rec.Ignored, []string{"00:00:00:00:00:02", "Mine"}) {
		t.Errorf("ignored = %v", rec.Ignored)
	}
	if len(rec.Notes) != 1 || !strings.Contains(rec.Notes[0], "Ghost") {
		t.Errorf("notes = %v", rec.Notes)
	}
	br := bandOf(t, rec, Band2G)
	if br.Networks != 1 || br.Candidates[0].Channel == 11 {
		t.Errorf("networks=%d best=%d", br.Networks, br.Candidates[0].Channel)
	}
}

func TestCountrySelection(t *testing.T) {
	de := mkAP("a", "00:00:00:00:00:01", Band2G, 1, 20, -50)
	nl := mkAP("b", "00:00:00:00:00:02", Band2G, 6, 20, -50)
	nl.Country = "NL"
	nl2 := nl
	nl2.BSSID = "00:00:00:00:00:03"

	rec := Recommend([][]AP{{de, nl, nl2}}, RecommendOptions{})
	if rec.Country != "NL" || rec.CountrySource != "majority of networks" {
		t.Errorf("majority: %q from %q", rec.Country, rec.CountrySource)
	}
	rec = Recommend([][]AP{{de, nl, nl2}}, RecommendOptions{Country: "us"})
	if rec.Country != "US" || rec.CountrySource != "option" {
		t.Errorf("option: %q from %q", rec.Country, rec.CountrySource)
	}
	noCC := de
	noCC.Country = ""
	rec = Recommend([][]AP{{noCC}}, RecommendOptions{})
	if rec.Country != "" || rec.CountrySource != "default" || len(rec.Notes) == 0 {
		t.Errorf("default: %q from %q notes %v", rec.Country, rec.CountrySource, rec.Notes)
	}
}

func TestSeveralScansSmoothOutFlickering(t *testing.T) {
	steady := mkAP("steady", "00:00:00:00:00:01", Band2G, 1, 20, -50)
	flicker := mkAP("flicker", "00:00:00:00:00:02", Band2G, 6, 20, -50)

	one := Recommend([][]AP{{steady, flicker}}, RecommendOptions{Country: "DE"})
	three := Recommend([][]AP{{steady, flicker}, {steady}, {steady}}, RecommendOptions{Country: "DE"})

	score := func(rec *Recommendation, ch int) float64 {
		for _, c := range bandOf(t, rec, Band2G).Candidates {
			if c.Channel == ch {
				return c.Score
			}
		}
		return -1
	}
	if a, b := score(one, 6), score(three, 6); b >= a || b <= 0 {
		t.Errorf("channel 6 score %v with one scan, %v with three: should shrink to a third", a, b)
	}
	if a, b := score(one, 1), score(three, 1); a != b {
		t.Errorf("a steady network must not change: %v vs %v", a, b)
	}
	if three.Scans != 3 {
		t.Errorf("scans = %d", three.Scans)
	}
}

func TestSignalAveragedAcrossScans(t *testing.T) {
	a := mkAP("x", "00:00:00:00:00:01", Band2G, 1, 20, -40)
	b := a
	b.SignalDBM = -60
	rec := Recommend([][]AP{{a}, {b}}, RecommendOptions{Country: "DE"})
	for _, c := range bandOf(t, rec, Band2G).Candidates {
		if c.Channel == 1 && c.StrongestDBM != -50 {
			t.Errorf("strongest = %d, want the mean -50", c.StrongestDBM)
		}
	}
}

func TestWeakNetworksCountLess(t *testing.T) {
	strong := weight(-40, nil)
	mid := weight(-70, nil)
	weak := weight(-90, nil)
	gone := weight(-100, nil)
	if !(strong == 1 && strong > mid && mid > weak && weak > gone && gone == 0) {
		t.Errorf("weights: %v %v %v %v", strong, mid, weak, gone)
	}
	if weight(-60, &BSSLoad{Utilization: 80}) <= weight(-60, nil) {
		t.Error("a busy access point must weigh more")
	}
}

func TestSiblingHints(t *testing.T) {
	own := mkAP("RVL-6G", "38:32:7A:5A:2D:14", Band6G, 5, 160, -50)
	own.Connected = true
	scene := [][]AP{{
		own,
		mkAP("RVL-5G", "38:32:7A:5A:2D:13", Band5G, 48, 80, -40),
		mkAP("RVL-guest", "3A:32:7A:5A:2D:15", Band5G, 48, 80, -40), // first octet differs
		mkAP("Other", "50:FF:20:44:C4:ED", Band5G, 100, 80, -60),
	}}
	rec := Recommend(scene, RecommendOptions{})
	if len(rec.Hints) != 1 || !strings.Contains(rec.Hints[0], "RVL-5G, RVL-guest") || strings.Contains(rec.Hints[0], "Other") {
		t.Errorf("hints = %v", rec.Hints)
	}
}

func TestNormalizeMAC(t *testing.T) {
	for in, want := range map[string]string{
		"aa:bb:cc:dd:ee:ff": "AA:BB:CC:DD:EE:FF",
		"aa-bb-cc-dd-ee-ff": "AA:BB:CC:DD:EE:FF",
	} {
		if got, ok := normalizeMAC(in); !ok || got != want {
			t.Errorf("normalizeMAC(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "MyWiFi", "aa:bb:cc:dd:ee", "aa:bb:cc:dd:ee:gg", "a:b:c:d:e:f"} {
		if _, ok := normalizeMAC(in); ok {
			t.Errorf("normalizeMAC(%q) accepted", in)
		}
	}
}

func TestRecommendDemo(t *testing.T) {
	// The demo scene must produce a complete, sane recommendation.
	res := Demo()
	rec := Recommend([][]AP{res.APs}, RecommendOptions{})
	if len(rec.Bands) != 3 {
		t.Fatalf("bands = %d", len(rec.Bands))
	}
	if rec.Country != "DE" || len(rec.Ignored) == 0 {
		t.Errorf("country %q ignored %v", rec.Country, rec.Ignored)
	}
	for _, br := range rec.Bands {
		if len(br.Candidates) == 0 {
			t.Errorf("%v has no candidates", br.Band)
		}
		for i := 1; i < len(br.Candidates); i++ {
			if br.Candidates[i].Score < br.Candidates[i-1].Score {
				t.Errorf("%v candidates are not sorted by score", br.Band)
			}
		}
	}
}
