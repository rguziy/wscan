package wifi

import (
	"math"
	"strings"
	"testing"
)

// ---------------------------------------------------------------- builders

func el(id byte, body ...byte) []byte {
	return append([]byte{id, byte(len(body))}, body...)
}

func extEl(ext byte, body ...byte) []byte {
	return el(idExtension, append([]byte{ext}, body...)...)
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// streamMap builds a 2-bit-per-stream MCS map where the first n streams
// support the highest MCS range (code 2) and the rest are unsupported.
func streamMap(n int) []byte {
	var m uint16
	for i := 0; i < 8; i++ {
		code := uint16(3)
		if i < n {
			code = 2
		}
		m |= code << (2 * i)
	}
	return []byte{byte(m), byte(m >> 8)}
}

func htCapEl(streams int, sgi bool) []byte {
	body := make([]byte, 26)
	info := uint16(0x0002) // 40 MHz capable
	if sgi {
		info |= 0x0020 | 0x0040
	}
	body[0], body[1] = byte(info), byte(info>>8)
	for i := 0; i < streams; i++ {
		body[3+i] = 0xff
	}
	return el(idHTCapabilities, body...)
}

func htOpEl(primary byte, width40 bool) []byte {
	body := make([]byte, 22)
	body[0] = primary
	if width40 {
		body[1] = 0x05 // secondary channel above, any width
	}
	return el(idHTOperation, body...)
}

func vhtCapEl(streams int) []byte {
	body := cat([]byte{0x00, 0x00, 0x00, 0x00}, // SGI 80 bit is set below
		streamMap(streams), []byte{0, 0}, streamMap(streams), []byte{0, 0})
	body[0] = 0x20 // short GI for 80 MHz
	return el(idVHTCapabilities, body...)
}

func vhtOpEl(width byte) []byte { return el(idVHTOperation, width, 0, 0, 0, 0) }

func heCapEl(streams int, phy0 byte) []byte {
	phy := make([]byte, 11)
	phy[0] = phy0
	body := cat(make([]byte, 6), phy, streamMap(streams), streamMap(streams))
	return extEl(extHECapabilities, body...)
}

// heOpEl builds an HE Operation element. width6 < 0 omits the 6 GHz info.
func heOpEl(color byte, width6 int) []byte {
	if width6 < 0 {
		return extEl(extHEOperation, 0, 0, 0, color, 0xfc, 0xff)
	}
	return extEl(extHEOperation, 0, 0, 0x02, color, 0xfc, 0xff, // bit 17: 6 GHz info
		1, byte(width6), 0, 0, 0)
}

func ehtCapEl() []byte {
	body := cat(make([]byte, 11), []byte{0x22, 0x22, 0x22}) // 2 streams at every MCS group
	return extEl(extEHTCapabilities, body...)
}

func ehtOpEl(width byte) []byte {
	return extEl(extEHTOperation, 0x01, 0, 0, 0, 0, width, 0, 0)
}

func rsnEl(akms []byte, caps uint16) []byte {
	body := []byte{1, 0}                           // version
	body = append(body, 0x00, 0x0f, 0xac, 0x04)    // group: CCMP
	body = append(body, 1, 0, 0x00, 0x0f, 0xac, 4) // one pairwise: CCMP
	body = append(body, byte(len(akms)), 0)
	for _, a := range akms {
		body = append(body, 0x00, 0x0f, 0xac, a)
	}
	body = append(body, byte(caps), byte(caps>>8))
	return el(idRSN, body...)
}

func wpaVendorEl() []byte {
	return el(idVendor, 0x00, 0x50, 0xf2, 0x01, // WPA IE
		1, 0, // version
		0x00, 0x50, 0xf2, 0x02, // group: TKIP
		1, 0, 0x00, 0x50, 0xf2, 0x02, // pairwise: TKIP
		1, 0, 0x00, 0x50, 0xf2, 0x02) // AKM: PSK
}

func almost(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

func rawWith(freq int, ies ...[]byte) rawBSS {
	return rawBSS{
		BSSID:   []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01},
		FreqMHz: freq,
		IEs:     cat(ies...),
	}
}

// ---------------------------------------------------------------- PHY rates

// The expected numbers are the ones WinFi 2.0 shows for real access points.
func TestRatesMatchReference(t *testing.T) {
	tests := []struct {
		name     string
		raw      rawBSS
		gen      int
		std      string
		width    int
		streams  int
		min, max float64
	}{
		{"HE 2.4 GHz 40 MHz 2 streams", rawWith(2447,
			el(idSSID, 'a'), htCapEl(2, true), htOpEl(8, true), heCapEl(2, 0x02), heOpEl(5, -1)),
			6, "ax", 40, 2, 17.2, 573.5},
		{"HE 2.4 GHz 20 MHz 2 streams", rawWith(2432,
			el(idSSID, 'a'), htCapEl(2, true), htOpEl(4, false), heCapEl(2, 0x02), heOpEl(5, -1)),
			6, "ax", 20, 2, 8.6, 286.8},
		{"HT 2.4 GHz 40 MHz 3 streams", rawWith(2422,
			htCapEl(3, true), htOpEl(3, true)),
			4, "n", 40, 3, 15.0, 450.0},
		{"HT 2.4 GHz 40 MHz 2 streams", rawWith(2457,
			htCapEl(2, true), htOpEl(10, true)),
			4, "n", 40, 2, 15.0, 300.0},
		{"HT 20 MHz 2 streams, short GI", rawWith(2437,
			htCapEl(2, true), htOpEl(6, false)),
			4, "n", 20, 2, 7.2, 144.4},
		{"HT 20 MHz 2 streams, long GI", rawWith(2437,
			htCapEl(2, false), htOpEl(6, false)),
			4, "n", 20, 2, 6.5, 130.0},
		{"VHT 5 GHz 80 MHz 3 streams", rawWith(5180,
			htCapEl(3, true), htOpEl(36, true), vhtCapEl(3), vhtOpEl(1)),
			5, "ac", 80, 3, 32.5, 1300.0},
		{"HE 5 GHz 80 MHz 2 streams", rawWith(5180,
			htCapEl(2, true), vhtCapEl(2), vhtOpEl(1), heCapEl(2, 0x04), heOpEl(1, -1)),
			6, "ax", 80, 2, 36.0, 1201.0},
		{"HE 6 GHz 160 MHz 2 streams", rawWith(5975,
			heCapEl(2, 0x0c), heOpEl(7, 3)),
			6, "ax", 160, 2, 72.1, 2402.0},
		{"EHT 6 GHz 320 MHz 2 streams", rawWith(6135,
			heCapEl(2, 0x0c), heOpEl(7, 3), ehtCapEl(), ehtOpEl(4)),
			7, "be", 320, 2, 144.1, 5764.7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ap := decode(tc.raw)
			if ap.Generation != tc.gen || ap.Standard != tc.std {
				t.Errorf("generation/standard = %d/%s, want %d/%s", ap.Generation, ap.Standard, tc.gen, tc.std)
			}
			if ap.WidthMHz != tc.width {
				t.Errorf("width = %d, want %d", ap.WidthMHz, tc.width)
			}
			if ap.Streams != tc.streams {
				t.Errorf("streams = %d, want %d", ap.Streams, tc.streams)
			}
			if !almost(ap.MinRateMbps, tc.min, 0.05) || !almost(ap.MaxRateMbps, tc.max, 0.05) {
				t.Errorf("rates = %.1f..%.1f, want %.1f..%.1f", ap.MinRateMbps, ap.MaxRateMbps, tc.min, tc.max)
			}
		})
	}
}

func TestLegacyRates(t *testing.T) {
	rates := el(idSupportedRates, 0x82, 0x84, 0x8b, 0x96, 0x0c, 0x12, 0x18, 0x24)
	ext := el(idExtendedRates, 0x30, 0x48, 0x60, 0x6c, 0xff) // 0xff: HT-PHY selector, not a rate

	g := decode(rawWith(2412, rates, ext))
	if g.Standard != "g" || g.Generation != 0 || g.MinRateMbps != 1 || g.MaxRateMbps != 54 {
		t.Errorf("2.4 GHz legacy = %s gen %d %.1f..%.1f", g.Standard, g.Generation, g.MinRateMbps, g.MaxRateMbps)
	}
	b := decode(rawWith(2412, el(idSupportedRates, 0x82, 0x84, 0x8b, 0x96)))
	if b.Standard != "b" || b.MaxRateMbps != 11 {
		t.Errorf("11b-only = %s %.1f", b.Standard, b.MaxRateMbps)
	}
	a := decode(rawWith(5180, rates, ext))
	if a.Standard != "a" {
		t.Errorf("5 GHz legacy standard = %s, want a", a.Standard)
	}
}

func TestWiFiLabel(t *testing.T) {
	tests := []struct {
		gen  int
		band Band
		want string
	}{
		{4, Band2G, "4"}, {5, Band5G, "5"}, {6, Band5G, "6"}, {6, Band6G, "6E"}, {7, Band6G, "7"}, {0, Band2G, ""},
	}
	for _, tc := range tests {
		if got := (AP{Generation: tc.gen, Band: tc.band}).WiFiLabel(); got != tc.want {
			t.Errorf("WiFiLabel(gen %d, %v) = %q, want %q", tc.gen, tc.band, got, tc.want)
		}
	}
}

func TestVHTWidthSignalling(t *testing.T) {
	tests := []struct {
		b    []byte
		want int
	}{
		{[]byte{0, 0, 0}, 0},
		{[]byte{1, 42, 0}, 80},
		{[]byte{1, 50, 58}, 160},  // legacy 160 MHz: CCFS1 is 8 channels away
		{[]byte{1, 42, 106}, 160}, // legacy 80+80 MHz
		{[]byte{2, 50, 0}, 160},
		{[]byte{3, 42, 106}, 160},
		{[]byte{1}, 0},
	}
	for _, tc := range tests {
		if got := vhtOperationWidth(tc.b); got != tc.want {
			t.Errorf("vhtOperationWidth(%v) = %d, want %d", tc.b, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------- security

func TestSecurity(t *testing.T) {
	const (
		psk = 2
		sae = 8
		ent = 1
		owe = 18
	)
	tests := []struct {
		name  string
		ies   []byte
		capab uint16
		want  string
		pmf   string
	}{
		{"open", nil, 0x0001, "Open", ""},
		{"wep", nil, 0x0011, "WEP", ""},
		{"wpa1", wpaVendorEl(), 0x0011, "WPA", ""},
		{"wpa2", rsnEl([]byte{psk}, 0), 0x0011, "WPA2", ""},
		{"wpa+wpa2", cat(rsnEl([]byte{psk}, 0), wpaVendorEl()), 0x0011, "WPA/WPA2", ""},
		{"wpa3", rsnEl([]byte{sae}, 0x00c0), 0x0011, "WPA3", "required"},
		{"transition", rsnEl([]byte{psk, sae}, 0x0080), 0x0011, "WPA2/WPA3", "capable"},
		{"enterprise", rsnEl([]byte{ent}, 0x0080), 0x0011, "WPA2-Enterprise", "capable"},
		{"suite b 192", rsnEl([]byte{12}, 0x00c0), 0x0011, "WPA3-Enterprise", "required"},
		{"owe", rsnEl([]byte{owe}, 0x00c0), 0x0001, "OWE", "required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sec := decodeSecurity(newElementSet(tc.ies), tc.capab)
			if sec.Protocol != tc.want {
				t.Errorf("protocol = %q, want %q", sec.Protocol, tc.want)
			}
			if sec.PMF != tc.pmf {
				t.Errorf("PMF = %q, want %q", sec.PMF, tc.pmf)
			}
		})
	}

	sec := decodeSecurity(newElementSet(rsnEl([]byte{psk, sae}, 0)), 0)
	if strings.Join(sec.Ciphers, ",") != "CCMP" || strings.Join(sec.AKMs, ",") != "PSK,SAE" {
		t.Errorf("ciphers %v akms %v", sec.Ciphers, sec.AKMs)
	}
	wps := decodeSecurity(newElementSet(el(idVendor, 0x00, 0x50, 0xf2, 0x04, 0x10, 0x4a)), 0)
	if !wps.WPS {
		t.Error("WPS element not detected")
	}
	if got := decodeSecurity(newElementSet(el(idRSN, 1, 0, 0)), 0x0011).Protocol; got != "WEP" {
		t.Errorf("malformed RSN fell back to %q, want WEP", got)
	}
}

// ---------------------------------------------------------------- fields

func TestSSIDHandling(t *testing.T) {
	hidden := decode(rawWith(2412, el(idSSID)))
	if !hidden.Hidden || hidden.SSID != "" {
		t.Errorf("empty SSID: hidden=%v ssid=%q", hidden.Hidden, hidden.SSID)
	}
	zeros := decode(rawWith(2412, el(idSSID, 0, 0, 0)))
	if !zeros.Hidden {
		t.Error("NUL-only SSID should be hidden")
	}

	evil := decode(rawWith(2412, el(idSSID, []byte("\x1b[31mred\x1b]0;title\x07‮evil\n")...)))
	for _, bad := range []string{"\x1b", "\x07", "\n", "‮"} {
		if strings.Contains(evil.SSID, bad) {
			t.Errorf("SSID %q still contains %q", evil.SSID, bad)
		}
	}
	if !strings.Contains(evil.SSID, "red") || !strings.Contains(evil.SSID, "evil") {
		t.Errorf("sanitising removed visible text: %q", evil.SSID)
	}

	utf8ok := decode(rawWith(2412, el(idSSID, []byte("Café ☕ Wi-Fi")...)))
	if utf8ok.SSID != "Café ☕ Wi-Fi" {
		t.Errorf("valid UTF-8 changed: %q", utf8ok.SSID)
	}
	invalid := decode(rawWith(2412, el(idSSID, 'a', 0xff, 'b')))
	if invalid.SSID != "a�b" {
		t.Errorf("invalid UTF-8 = %q", invalid.SSID)
	}
}

func TestCommonFields(t *testing.T) {
	raw := rawWith(5180,
		el(idSSID, 'x'),
		el(idCountry, 'D', 'E', ' ', 36, 4, 20),
		el(idBSSLoad, 12, 0, 0x80, 0, 0),
		el(idRMEnabledCaps, 0, 0, 0, 0, 0),
		el(idExtendedCaps, 0, 0, 0x08, 0),
		el(idMobilityDomain, 1, 2, 0),
	)
	raw.SignalMBM, raw.HasMBM = -5850, true
	raw.BeaconTU = 100
	raw.TSF = uint64((13*86400 + 5*3600 + 35*60 + 54)) * 1_000_000
	raw.SeenMS = 1200
	raw.Status, raw.HasStatus = bssStatusAssociated, true

	ap := decode(raw)
	if ap.BSSID != "00:00:5E:00:53:01" {
		t.Errorf("BSSID = %s", ap.BSSID)
	}
	if ap.SignalDBM != -58 && ap.SignalDBM != -59 { // -58.5 rounds away from zero
		t.Errorf("signal = %d", ap.SignalDBM)
	}
	if ap.Country != "DE" {
		t.Errorf("country = %q", ap.Country)
	}
	if ap.Load == nil || ap.Load.Stations != 12 || ap.Load.Utilization != 50 {
		t.Errorf("load = %+v", ap.Load)
	}
	if strings.Join(ap.Roaming, "") != "kvr" {
		t.Errorf("roaming = %v", ap.Roaming)
	}
	if ap.BeaconIntervalMS != 102.4 {
		t.Errorf("beacon = %v", ap.BeaconIntervalMS)
	}
	if want := int64(13*86400 + 5*3600 + 35*60 + 54); ap.UptimeSeconds != want {
		t.Errorf("uptime = %d, want %d", ap.UptimeSeconds, want)
	}
	if !ap.Connected || ap.SeenAgoMS != 1200 || ap.Band != Band5G || ap.Channel != 36 {
		t.Errorf("connected=%v seen=%d band=%v ch=%d", ap.Connected, ap.SeenAgoMS, ap.Band, ap.Channel)
	}

	est := decode(rawBSS{BSSID: make([]byte, 6), FreqMHz: 2412, SignalUnspec: 42, HasUnspec: true})
	if est.SignalDBM != -58 {
		t.Errorf("estimated signal = %d, want -58", est.SignalDBM)
	}
	if got := cleanCountry([]byte{0, 0, ' '}); got != "" {
		t.Errorf("garbage country = %q", got)
	}
}

func TestElementSetPrecedenceAndTruncation(t *testing.T) {
	probe := el(idSSID, 'p')
	beacon := cat(el(idSSID, 'b'), el(idCountry, 'N', 'L', ' '))
	es := newElementSet(probe, beacon)
	if b, _ := es.get(idSSID); string(b) != "p" {
		t.Errorf("SSID = %q, probe response should win", b)
	}
	if b, ok := es.get(idCountry); !ok || string(b[:2]) != "NL" {
		t.Error("element missing from the probe response should come from the beacon")
	}

	truncated := cat(el(idSSID, 'a'), []byte{idCountry, 9, 'D'}) // claims 9 bytes, has 1
	es = newElementSet(truncated)
	if _, ok := es.get(idCountry); ok {
		t.Error("truncated element must be ignored")
	}
	if _, ok := es.get(idSSID); !ok {
		t.Error("elements before the truncation must survive")
	}
	_ = newElementSet([]byte{255, 0}, []byte{1}, nil) // must not panic
}

func TestDecodeNeverPanicsOnGarbage(t *testing.T) {
	// Every prefix of every element, as a fuzz-like smoke test.
	full := cat(htCapEl(2, true), htOpEl(1, true), vhtCapEl(2), vhtOpEl(1),
		heCapEl(2, 0x04), heOpEl(1, 3), ehtCapEl(), ehtOpEl(4), rsnEl([]byte{2, 8}, 0), wpaVendorEl())
	for n := 0; n <= len(full); n++ {
		decode(rawWith(5180, full[:n]))
		decode(rawWith(5975, full[:n]))
	}
	for id := 0; id < 256; id++ {
		for l := 0; l < 6; l++ {
			body := make([]byte, l)
			decode(rawWith(2412, el(byte(id), body...)))
			decode(rawWith(2412, extEl(byte(id), body...)))
		}
	}
}

// ---------------------------------------------------------------- bands

func TestBandAndChannel(t *testing.T) {
	tests := []struct {
		freq int
		band Band
		ch   int
	}{
		{2412, Band2G, 1}, {2437, Band2G, 6}, {2472, Band2G, 13}, {2484, Band2G, 14},
		{5180, Band5G, 36}, {5500, Band5G, 100}, {5825, Band5G, 165},
		{5935, Band6G, 2}, {5955, Band6G, 1}, {5975, Band6G, 5}, {6135, Band6G, 37}, {7115, Band6G, 233},
		{900, BandUnknown, 0},
	}
	for _, tc := range tests {
		if b := BandOf(tc.freq); b != tc.band {
			t.Errorf("BandOf(%d) = %v, want %v", tc.freq, b, tc.band)
		}
		if ch := ChannelOf(tc.freq); ch != tc.ch {
			t.Errorf("ChannelOf(%d) = %d, want %d", tc.freq, ch, tc.ch)
		}
	}
}

// ---------------------------------------------------------------- sorting

func TestSortSelectGroup(t *testing.T) {
	aps := []AP{
		{SSID: "b", BSSID: "02", SignalDBM: -60, Channel: 6, Band: Band2G},
		{SSID: "a", BSSID: "03", SignalDBM: -50, Channel: 11, Band: Band2G},
		{Hidden: true, BSSID: "01", SignalDBM: -40, Channel: 1, Band: Band2G},
		{SSID: "c", BSSID: "04", SignalDBM: -70, Channel: 36, Band: Band5G},
	}
	order := func(key SortKey) string {
		cp := append([]AP(nil), aps...)
		Sort(cp, key)
		var sb strings.Builder
		for _, a := range cp {
			sb.WriteString(a.BSSID)
		}
		return sb.String()
	}
	for key, want := range map[SortKey]string{
		SortSignal: "01030204", SortChannel: "01020304", SortSSID: "03020401", SortBSSID: "01020304",
	} {
		if got := order(key); got != want {
			t.Errorf("sort by %s = %s, want %s", key, got, want)
		}
	}

	only5 := Select(aps, map[Band]bool{Band5G: true}, SortSignal)
	if len(only5) != 1 || only5[0].SSID != "c" {
		t.Errorf("band filter = %+v", only5)
	}
	if len(Select(aps, nil, SortSignal)) != 4 {
		t.Error("empty band set must select everything")
	}
	if g := GroupByBand(aps); len(g[Band2G]) != 3 || len(g[Band5G]) != 1 {
		t.Errorf("groups = %v", g)
	}
	if _, err := ParseSortKey("nope"); err == nil {
		t.Error("bad sort key accepted")
	}
	if k, err := ParseSortKey("SSID"); err != nil || k != SortSSID {
		t.Errorf("ParseSortKey(SSID) = %q, %v", k, err)
	}
}

func TestDemoIsConsistent(t *testing.T) {
	res := Demo()
	seen := map[string]bool{}
	bands := map[Band]int{}
	for _, ap := range res.APs {
		if seen[ap.BSSID] {
			t.Errorf("duplicate demo BSSID %s", ap.BSSID)
		}
		seen[ap.BSSID] = true
		bands[ap.Band]++
		if ap.Channel == 0 || ap.MaxRateMbps <= 0 || ap.Security.Protocol == "" {
			t.Errorf("incomplete demo AP %+v", ap)
		}
		if !strings.HasPrefix(ap.BSSID, "00:00:5E:00:53:") {
			t.Errorf("demo BSSID %s is outside the documentation range", ap.BSSID)
		}
	}
	for _, b := range Bands() {
		if bands[b] == 0 {
			t.Errorf("demo has no %v network", b)
		}
	}
}
