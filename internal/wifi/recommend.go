package wifi

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// This file turns one or more scans into channel recommendations: for every
// band it ranks the channels a router could use by how little they overlap
// with the networks around, weighted by how strong those networks are.
//
// The result is a heuristic, not a guarantee. The regulatory tables are
// deliberately simplified (three regions); a router only offers the channels
// its country allows, so if a recommended channel is missing in its settings
// the next one in the list is the right pick.

// RecommendOptions controls Recommend.
type RecommendOptions struct {
	// Ignore lists SSIDs (exact match) or BSSIDs whose networks are left out
	// of the calculation, typically the user's own router.
	Ignore []string
	// IncludeConnected keeps the network the computer is connected to in the
	// calculation. By default its SSID is ignored like an Ignore entry.
	IncludeConnected bool
	// Country is an ISO 3166 alpha-2 code selecting the channels allowed.
	// Empty means: take it from the connected network, else from the
	// majority of networks seen.
	Country string
	// Width is the channel width to recommend on 5 and 6 GHz. Zero means 80.
	// 2.4 GHz is always evaluated at 20 MHz.
	Width int
	// Top is how many candidates to list per band. Zero means 3.
	Top int
	// Adapter says which bands the scanning adapter supports (Result.Bands).
	// Bands it cannot scan are reported as such instead of being ranked
	// from no data. Nil means unknown.
	Adapter map[Band]BandSupport
}

// Recommendation is the outcome of Recommend.
type Recommendation struct {
	Interface string    `json:"interface"`
	Time      time.Time `json:"time"`
	Scans     int       `json:"scans"`
	// Country is the regulatory country used; empty when unknown.
	Country string `json:"country,omitempty"`
	// CountrySource says where Country came from: "option",
	// "connected network", "majority of networks" or "default".
	CountrySource string `json:"country_source"`
	// Ignored describes what was left out, e.g. "RVL227-5GHz (connected)".
	Ignored []string `json:"ignored,omitempty"`
	// Hints are suggestions about the input, e.g. other SSIDs of the
	// connected router that should probably be ignored too.
	Hints []string             `json:"hints,omitempty"`
	Notes []string             `json:"notes,omitempty"`
	Bands []BandRecommendation `json:"bands"`
}

// BandRecommendation holds the ranking for one band.
type BandRecommendation struct {
	Band Band `json:"band"`
	// WidthMHz is the channel width the candidates were evaluated at.
	WidthMHz int `json:"width_mhz"`
	// Networks is how many networks (after ignoring) were considered.
	Networks int `json:"networks"`
	// Support is what is known about the adapter's ability to scan the band.
	Support BandSupport `json:"adapter_support,omitempty"`
	// Current is where the connected network sits now, when it is in this
	// band, scored the same way as the candidates.
	Current    *Candidate  `json:"current,omitempty"`
	Candidates []Candidate `json:"candidates"`
	Note       string      `json:"note,omitempty"`
}

// Candidate is one channel (or block of channels) and how loaded it is.
type Candidate struct {
	// Channel is the primary (control) channel number to enter in the router.
	Channel int `json:"channel"`
	// FrequencyMHz is the centre frequency of the primary channel.
	FrequencyMHz int `json:"frequency_mhz"`
	WidthMHz     int `json:"width_mhz"`
	// CenterMHz is the centre of the whole block (equals FrequencyMHz at
	// 20 MHz).
	CenterMHz int `json:"center_frequency_mhz"`
	// FromMHz and ToMHz are the edges of the occupied spectrum.
	FromMHz int `json:"from_mhz"`
	ToMHz   int `json:"to_mhz"`
	// Channels are all 20 MHz channels the block covers.
	Channels []int `json:"channels"`
	// Score is the interference score: 0 is empty, one strong network on the
	// same channel is about 0.7.
	Score float64 `json:"score"`
	// Level is clear, light, moderate or heavy.
	Level string `json:"level"`
	// Networks is how many networks overlap the candidate.
	Networks int `json:"networks"`
	// StrongestDBM is the strongest overlapping network, 0 when none.
	StrongestDBM int `json:"strongest_dbm,omitempty"`
	// DFS marks channels that must yield to radar; the router may move away
	// from them without notice.
	DFS bool `json:"dfs,omitempty"`
}

// Interference levels.
const (
	LevelClear    = "clear"
	LevelLight    = "light"
	LevelModerate = "moderate"
	LevelHeavy    = "heavy"
)

func levelOf(score float64) string {
	switch {
	case score < 0.15:
		return LevelClear
	case score < 0.6:
		return LevelLight
	case score < 1.5:
		return LevelModerate
	}
	return LevelHeavy
}

// ------------------------------------------------------------ channel plans

// 5 GHz bonded blocks, as lists of 20 MHz channels. The 5 GHz grid is not
// uniform (UNII-3 starts at 5735 MHz), so the blocks are spelled out.
var (
	blocks5G40 = [][]int{
		{36, 40}, {44, 48}, {52, 56}, {60, 64},
		{100, 104}, {108, 112}, {116, 120}, {124, 128}, {132, 136}, {140, 144},
		{149, 153}, {157, 161},
	}
	blocks5G80 = [][]int{
		{36, 40, 44, 48}, {52, 56, 60, 64},
		{100, 104, 108, 112}, {116, 120, 124, 128}, {132, 136, 140, 144},
		{149, 153, 157, 161},
	}
	blocks5G160 = [][]int{
		{36, 40, 44, 48, 52, 56, 60, 64},
		{100, 104, 108, 112, 116, 120, 124, 128},
	}
	channels5G = []int{
		36, 40, 44, 48, 52, 56, 60, 64,
		100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144,
		149, 153, 157, 161, 165,
	}
)

// channelFreq returns the centre frequency of a 20 MHz channel in MHz.
func channelFreq(b Band, ch int) int {
	switch b {
	case Band2G:
		if ch == 14 {
			return 2484
		}
		return 2407 + 5*ch
	case Band5G:
		return 5000 + 5*ch
	case Band6G:
		return 5950 + 5*ch
	}
	return 0
}

// blockChannels returns the 20 MHz channels of the block of the given width
// that contains primary, or nil when the plan has no such block.
func blockChannels(b Band, primary, width int) []int {
	if width <= 20 {
		return []int{primary}
	}
	switch b {
	case Band5G:
		var table [][]int
		switch width {
		case 40:
			table = blocks5G40
		case 80:
			table = blocks5G80
		case 160:
			table = blocks5G160
		}
		for _, blk := range table {
			for _, ch := range blk {
				if ch == primary {
					return blk
				}
			}
		}
	case Band6G:
		// 6 GHz channels are 1, 5, 9, ... and blocks are aligned to channel 1.
		n := width / 20
		if width%20 != 0 || (primary-1)%4 != 0 || primary < 1 {
			return nil
		}
		k := (primary - 1) / 4
		start := k - k%n
		blk := make([]int, n)
		for i := range blk {
			blk[i] = 1 + 4*(start+i)
		}
		return blk
	}
	return nil
}

// span returns the edges (MHz) of the spectrum a set of 20 MHz channels
// occupies.
func span(b Band, chans []int) (lo, hi float64) {
	lo = float64(channelFreq(b, chans[0]) - 10)
	hi = float64(channelFreq(b, chans[len(chans)-1]) + 10)
	if b == Band2G {
		lo, hi = lo-1, hi+1 // a 2.4 GHz channel is about 22 MHz wide
	}
	return lo, hi
}

// apSpan returns the spectrum an access point occupies.
func apSpan(ap AP) (lo, hi float64) {
	width := ap.WidthMHz
	if width < 20 {
		width = 20
	}
	if chans := blockChannels(ap.Band, ap.Channel, width); chans != nil && ap.Band != Band2G {
		return span(ap.Band, chans)
	}
	half := float64(width) / 2
	if ap.Band == Band2G {
		half++
	}
	f := float64(ap.FrequencyMHz)
	return f - half, f + half
}

// ------------------------------------------------------ regulatory domains

type region int

const (
	regionETSI region = iota // Europe and most of the world; also the fallback
	regionUS                 // FCC-like
	regionJP
)

var (
	usLike = setOf("US", "CA", "MX", "KR", "BR")
	no6GHz = setOf("CN", "RU", "BY")
)

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func regionOf(cc string) region {
	switch {
	case usLike[cc]:
		return regionUS
	case cc == "JP":
		return regionJP
	}
	return regionETSI
}

// allowed reports whether a 20 MHz channel may be used in the region.
func allowed(reg region, b Band, ch int) bool {
	switch b {
	case Band2G:
		if reg == regionUS {
			return ch >= 1 && ch <= 11
		}
		return ch >= 1 && ch <= 13
	case Band5G:
		switch {
		case ch >= 36 && ch <= 64:
			return true
		case ch >= 100 && ch <= 140:
			return true
		case ch == 144:
			return reg == regionUS
		case ch >= 149 && ch <= 165:
			return reg != regionJP
		}
	case Band6G:
		if ch < 1 || (ch-1)%4 != 0 {
			return false
		}
		if reg == regionUS {
			return ch <= 233
		}
		return ch <= 93 // lower 6 GHz only (5945-6425 MHz)
	}
	return false
}

func isDFS(chans []int) bool {
	for _, ch := range chans {
		if ch >= 52 && ch <= 144 {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------- scoring

// weight maps a signal strength to how much a network disturbs its
// neighbours: 0 at -95 dBm and below, 1 at -40 dBm and above, quadratic in
// between so that distant networks count little.
func weight(dbm float64, load *BSSLoad) float64 {
	w := math.Min(math.Max((dbm+95)/55, 0), 1)
	w *= w
	if load != nil {
		w *= 1 + float64(load.Utilization)/100 // a busy AP disturbs more
	}
	return w
}

// observed is one access point aggregated over several scans.
type observed struct {
	ap   AP
	dbm  float64 // mean over the scans it appeared in
	seen int
}

// aggregate merges the scans by BSSID.
func aggregate(scans [][]AP) []observed {
	byBSSID := map[string]*observed{}
	var order []string
	sums := map[string]float64{}
	for _, scan := range scans {
		for _, ap := range scan {
			key := strings.ToUpper(ap.BSSID)
			o, ok := byBSSID[key]
			if !ok {
				o = &observed{}
				byBSSID[key] = o
				order = append(order, key)
			}
			o.ap = ap
			o.seen++
			sums[key] += float64(ap.SignalDBM)
		}
	}
	out := make([]observed, 0, len(order))
	for _, key := range order {
		o := byBSSID[key]
		o.dbm = sums[key] / float64(o.seen)
		out = append(out, *o)
	}
	return out
}

// score rates the spectrum [lo, hi) in the given band against the observed
// networks.
func score(b Band, lo, hi float64, obs []observed, scans int) (s float64, n int, strongest int) {
	for _, o := range obs {
		if o.ap.Band != b {
			continue
		}
		alo, ahi := apSpan(o.ap)
		overlap := math.Min(hi, ahi) - math.Max(lo, alo)
		if overlap <= 0 {
			continue
		}
		frac := overlap / (ahi - alo) // share of the neighbour inside our block
		presence := float64(o.seen) / float64(scans)
		s += weight(o.dbm, o.ap.Load) * frac * presence
		n++
		if d := int(math.Round(o.dbm)); strongest == 0 || d > strongest {
			strongest = d
		}
	}
	return s, n, strongest
}

func newCandidate(b Band, chans []int, obs []observed, scans int) Candidate {
	lo, hi := span(b, chans)
	s, n, strongest := score(b, lo, hi, obs, scans)
	primary := chans[0]
	width := 20 * len(chans)
	if b == Band2G {
		width = 20
	}
	return Candidate{
		Channel:      primary,
		FrequencyMHz: channelFreq(b, primary),
		WidthMHz:     width,
		CenterMHz:    int(math.Round((lo + hi) / 2)),
		FromMHz:      int(math.Round(lo)),
		ToMHz:        int(math.Round(hi)),
		Channels:     append([]int(nil), chans...),
		Score:        math.Round(s*100) / 100,
		Level:        levelOf(s),
		Networks:     n,
		StrongestDBM: strongest,
		DFS:          b == Band5G && isDFS(chans),
	}
}

// candidateBlocks lists the blocks of the given width that are usable in the
// region, each starting at its lowest (primary) channel.
func candidateBlocks(reg region, b Band, width int) [][]int {
	var out [][]int
	switch b {
	case Band2G:
		for _, ch := range []int{1, 6, 11} {
			if allowed(reg, b, ch) {
				out = append(out, []int{ch})
			}
		}
		return out
	case Band5G:
		for _, ch := range channels5G {
			blk := blockChannels(b, ch, width)
			if blk == nil || blk[0] != ch {
				continue
			}
			if allAllowed(reg, b, blk) {
				out = append(out, blk)
			}
		}
	case Band6G:
		for ch := 1; ch <= 233; ch += 4 {
			blk := blockChannels(b, ch, width)
			if blk == nil || blk[0] != ch {
				continue
			}
			if allAllowed(reg, b, blk) {
				out = append(out, blk)
			}
		}
	}
	return out
}

func allAllowed(reg region, b Band, chans []int) bool {
	for _, ch := range chans {
		if !allowed(reg, b, ch) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- Recommend

// Recommend ranks channels for every band from one or more scans. Using
// several scans smooths out networks that come and go and signals that
// fluctuate.
func Recommend(scans [][]AP, opt RecommendOptions) *Recommendation {
	if opt.Top <= 0 {
		opt.Top = 3
	}
	if opt.Width == 0 {
		opt.Width = 80
	}
	rec := &Recommendation{Scans: len(scans)}
	nScans := max(len(scans), 1)

	all := aggregate(scans)

	// The connected network, if any (the most recent sighting wins).
	var connected *AP
	for i := range all {
		if all[i].ap.Connected {
			connected = &all[i].ap
		}
	}

	// Build the ignore sets.
	ignoreSSID := map[string]string{} // ssid -> reason
	ignoreBSSID := map[string]bool{}
	for _, item := range opt.Ignore {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if mac, ok := normalizeMAC(item); ok {
			ignoreBSSID[mac] = true
			continue
		}
		ignoreSSID[item] = "ignored"
	}
	if connected != nil && !opt.IncludeConnected && connected.SSID != "" {
		if _, ok := ignoreSSID[connected.SSID]; !ok {
			ignoreSSID[connected.SSID] = "connected"
		}
	}

	// Split the networks into kept and ignored, remembering which ignore
	// entries actually matched something.
	var kept []observed
	ignoredIn := map[Band]int{}
	matched := map[string]bool{}
	ignoredLabels := map[string]bool{}
	for _, o := range all {
		bssid := strings.ToUpper(o.ap.BSSID)
		reason, bySSID := ignoreSSID[o.ap.SSID]
		bySSID = bySSID && o.ap.SSID != ""
		if bySSID || ignoreBSSID[bssid] {
			ignoredIn[o.ap.Band]++
		}
		switch {
		case bySSID:
			matched[o.ap.SSID] = true
			label := o.ap.SSID
			if reason == "connected" {
				label += " (connected)"
			}
			ignoredLabels[label] = true
		case ignoreBSSID[bssid]:
			matched[bssid] = true
			ignoredLabels[bssid] = true
		default:
			kept = append(kept, o)
		}
	}
	for label := range ignoredLabels {
		rec.Ignored = append(rec.Ignored, label)
	}
	sort.Strings(rec.Ignored)
	for _, item := range opt.Ignore {
		item = strings.TrimSpace(item)
		key := item
		if mac, ok := normalizeMAC(item); ok {
			key = mac
		}
		if item != "" && !matched[key] {
			rec.Notes = append(rec.Notes, fmt.Sprintf("--ignore %q matched no network", item))
		}
	}

	rec.Hints = siblingHints(connected, kept)

	// Country.
	rec.Country, rec.CountrySource = pickCountry(opt.Country, connected, kept)
	reg := regionOf(rec.Country)
	if rec.CountrySource == "default" {
		rec.Notes = append(rec.Notes, "country unknown; using European channel rules (use --country to set it)")
	}

	for _, b := range Bands() {
		rec.Bands = append(rec.Bands, recommendBand(b, reg, rec.Country, opt, kept, connected, nScans, ignoredIn[b]))
	}
	return rec
}

func recommendBand(b Band, reg region, cc string, opt RecommendOptions, kept []observed, connected *AP, scans, ignored int) BandRecommendation {
	width := opt.Width
	if b == Band2G {
		width = 20
	}
	br := BandRecommendation{Band: b, WidthMHz: width}
	for _, o := range kept {
		if o.ap.Band == b {
			br.Networks++
		}
	}

	br.Support = opt.Adapter[b]
	switch br.Support {
	case SupportNone:
		br.Note = fmt.Sprintf("your adapter does not support %s, so it cannot scan this band", b)
		return br
	case SupportDisabled:
		br.Note = fmt.Sprintf("your adapter supports %s but it is switched off by the regulatory domain (set the country, for example: iw reg set %s)", b, countryOr(cc, "UA"))
		return br
	}

	if b == Band6G && no6GHz[cc] {
		br.Note = "6 GHz is not allowed in " + cc
		return br
	}
	blocks := candidateBlocks(reg, b, width)
	if len(blocks) == 0 {
		br.Note = fmt.Sprintf("no %d MHz channels exist in this band", width)
		return br
	}
	for _, blk := range blocks {
		br.Candidates = append(br.Candidates, newCandidate(b, blk, kept, scans))
	}
	sort.SliceStable(br.Candidates, func(i, j int) bool {
		a, c := br.Candidates[i], br.Candidates[j]
		if a.Score != c.Score {
			return a.Score < c.Score
		}
		if a.DFS != c.DFS {
			return !a.DFS // equally good: prefer a channel without radar rules
		}
		return a.Channel < c.Channel
	})
	if len(br.Candidates) > opt.Top {
		br.Candidates = br.Candidates[:opt.Top]
	}

	if br.Networks == 0 {
		switch {
		case ignored > 0:
			br.Note = "only your own (ignored) network was seen in this band; it is otherwise empty"
		case br.Support == SupportOK:
			br.Note = "no networks seen: the band is empty"
		default:
			br.Note = "no networks seen in this band: it is empty, or your adapter cannot scan it"
		}
	}

	if connected != nil && connected.Band == b {
		w := connected.WidthMHz
		if w < 20 || b == Band2G {
			w = 20
		}
		if blk := blockChannels(b, connected.Channel, w); blk != nil {
			cur := newCandidate(b, blk, kept, scans)
			// Report the network's real primary channel, not the first
			// channel of the block it sits in.
			cur.Channel = connected.Channel
			cur.FrequencyMHz = connected.FrequencyMHz
			br.Current = &cur
		}
	}
	return br
}

func countryOr(cc, fallback string) string {
	if cc == "" {
		return fallback
	}
	return cc
}

// pickCountry chooses the regulatory country: the option, else the connected
// network's country element, else the most common one among the networks.
func pickCountry(opt string, connected *AP, kept []observed) (cc, source string) {
	if opt = strings.ToUpper(strings.TrimSpace(opt)); opt != "" {
		return opt, "option"
	}
	if connected != nil && connected.Country != "" {
		return connected.Country, "connected network"
	}
	count := map[string]int{}
	for _, o := range kept {
		if o.ap.Country != "" {
			count[o.ap.Country]++
		}
	}
	best, bestN := "", 0
	for c, n := range count {
		if n > bestN || (n == bestN && c < best) {
			best, bestN = c, n
		}
	}
	if best != "" {
		return best, "majority of networks"
	}
	return "", "default"
}

// siblingHints finds networks that probably belong to the same device as the
// connected one: the virtual access points of one device share most of their
// BSSID and differ only in a small interface counter in the last octet.
func siblingHints(connected *AP, kept []observed) []string {
	if connected == nil {
		return nil
	}
	key := macMiddle(connected.BSSID)
	if key == "" {
		return nil
	}
	lastOctet := func(mac string) int {
		var v int
		if _, err := fmt.Sscanf(mac[len(mac)-2:], "%x", &v); err != nil {
			return -1000
		}
		return v
	}
	base := lastOctet(connected.BSSID)
	seen := map[string]bool{}
	var names []string
	for _, o := range kept {
		if o.ap.SSID == "" || o.ap.SSID == connected.SSID || seen[o.ap.SSID] {
			continue
		}
		if d := lastOctet(o.ap.BSSID) - base; macMiddle(o.ap.BSSID) == key && d >= -4 && d <= 4 {
			seen[o.ap.SSID] = true
			names = append(names, o.ap.SSID)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return []string{fmt.Sprintf("probably also your router (same hardware address): %s; add them with --ignore",
		strings.Join(names, ", "))}
}

// macMiddle returns octets 2-5 of a MAC address. The first octet differs
// between virtual interfaces (locally administered bit) and the last one is
// the interface counter, so neither is compared.
func macMiddle(mac string) string {
	parts := strings.Split(strings.ToUpper(mac), ":")
	if len(parts) != 6 {
		return ""
	}
	return strings.Join(parts[1:5], ":")
}

// normalizeMAC accepts aa:bb:cc:dd:ee:ff or aa-bb-cc-dd-ee-ff and returns the
// upper-case colon form.
func normalizeMAC(s string) (string, bool) {
	s = strings.ToUpper(strings.ReplaceAll(s, "-", ":"))
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return "", false
	}
	for _, p := range parts {
		if len(p) != 2 {
			return "", false
		}
		for _, c := range p {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'F') {
				return "", false
			}
		}
	}
	return s, true
}
