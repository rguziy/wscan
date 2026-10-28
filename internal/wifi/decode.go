package wifi

import (
	"math"
)

// bssStatusAssociated is NL80211_BSS_STATUS_ASSOCIATED.
const bssStatusAssociated = 1

// rawBSS is the platform independent form of a scan result entry: the fields
// the kernel reports about one BSS, before the information elements are
// interpreted.
type rawBSS struct {
	BSSID        []byte
	FreqMHz      int
	TSF          uint64 // microseconds, from a probe response
	BeaconTSF    uint64 // microseconds, from a beacon
	BeaconTU     uint16 // beacon interval in time units of 1.024 ms
	Capability   uint16
	SignalMBM    int32 // signal in 100*dBm
	HasMBM       bool
	SignalUnspec uint8 // signal quality 0..100 when no dBm value is known
	HasUnspec    bool
	Status       uint32
	HasStatus    bool
	SeenMS       uint32 // milliseconds since the entry was last seen
	SeenUnknown  bool   // the platform does not report the age of the entry
	IEs          []byte
	BeaconIEs    []byte
}

// decode turns a raw scan entry into an AP.
func decode(r rawBSS) AP {
	es := newElementSet(r.IEs, r.BeaconIEs)

	ap := AP{
		BSSID:            formatMAC(r.BSSID),
		Band:             BandOf(r.FreqMHz),
		FrequencyMHz:     r.FreqMHz,
		Channel:          ChannelOf(r.FreqMHz),
		BeaconIntervalMS: math.Round(float64(r.BeaconTU)*1.024*10) / 10,
		SeenAgoMS:        int64(r.SeenMS),
		Connected:        r.HasStatus && r.Status == bssStatusAssociated,
	}

	if r.SeenUnknown {
		ap.SeenAgoMS = -1
	}

	ssid, _ := es.get(idSSID)
	ap.SSID, ap.Hidden = cleanSSID(ssid)

	switch {
	case r.HasMBM:
		ap.SignalDBM = int(math.Round(float64(r.SignalMBM) / 100))
	case r.HasUnspec:
		ap.SignalDBM = int(r.SignalUnspec) - 100 // rough estimate
	}

	phy := analyzePHY(es, ap.Band)
	ap.WidthMHz = phy.width
	ap.Generation = phy.gen
	ap.Standard = phy.standard
	ap.Streams = phy.streams
	ap.MinRateMbps = round1(phy.minRate)
	ap.MaxRateMbps = round1(phy.maxRate)
	ap.BSSColor = phy.bssColor

	if tsf := firstNonZero(r.BeaconTSF, r.TSF); tsf > 0 {
		ap.UptimeSeconds = int64(tsf / 1_000_000)
	}

	if b, ok := es.get(idCountry); ok {
		ap.Country = cleanCountry(b)
	}
	if b, ok := es.get(idBSSLoad); ok && len(b) >= 3 {
		ap.Load = &BSSLoad{
			Stations:    int(b[0]) | int(b[1])<<8,
			Utilization: int(b[2]) * 100 / 255,
		}
	}

	if _, ok := es.get(idRMEnabledCaps); ok {
		ap.Roaming = append(ap.Roaming, "k")
	}
	if b, ok := es.get(idExtendedCaps); ok && len(b) >= 3 && b[2]&(1<<3) != 0 {
		ap.Roaming = append(ap.Roaming, "v") // BSS Transition Management
	}
	if _, ok := es.get(idMobilityDomain); ok {
		ap.Roaming = append(ap.Roaming, "r")
	}

	ap.Security = decodeSecurity(es, r.Capability)
	return ap
}

func firstNonZero(vals ...uint64) uint64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// phyInfo is the PHY related summary of an AP.
type phyInfo struct {
	gen      int
	standard string
	width    int
	streams  int
	minRate  float64
	maxRate  float64
	bssColor int
}

// analyzePHY works out the Wi-Fi generation, the operating channel width and
// the data rate range of a BSS from its HT, VHT, HE and EHT elements. Rates
// are theoretical PHY rates: the best MCS and stream count the AP advertises
// at its current channel width with the shortest guard interval.
func analyzePHY(es *elementSet, band Band) phyInfo {
	info := phyInfo{width: 20}

	htCap, hasHT := es.get(idHTCapabilities)
	htOp, hasHTOp := es.get(idHTOperation)
	vhtCap, hasVHT := es.get(idVHTCapabilities)
	vhtOp, hasVHTOp := es.get(idVHTOperation)
	heCap, hasHE := es.getExt(extHECapabilities)
	heOp, hasHEOp := es.getExt(extHEOperation)
	ehtCap, hasEHT := es.getExt(extEHTCapabilities)
	ehtOp, hasEHTOp := es.getExt(extEHTOperation)

	// Operating channel width: the widest one any operation element reports.
	setWidth := func(w int) {
		if w > info.width {
			info.width = w
		}
	}
	if hasHTOp && len(htOp) >= 2 && htOp[1]&0x04 != 0 && htOp[1]&0x03 != 0 {
		setWidth(40) // any-width STA channel width with a secondary channel
	}
	if hasVHTOp {
		setWidth(vhtOperationWidth(vhtOp))
	}
	if hasHEOp {
		color, width6, vhtWidth := parseHEOperation(heOp)
		info.bssColor = color
		setWidth(width6)
		if !hasVHTOp {
			setWidth(vhtWidth)
		}
	}
	if hasEHTOp {
		setWidth(ehtOperationWidth(ehtOp))
	}

	// Generation: the highest PHY the AP advertises. Rates come from the
	// highest PHY whose capabilities decode.
	var caps phyCaps
	have := false
	try := func(gen int, c phyCaps, ok bool) {
		if info.gen < gen {
			info.gen = gen
		}
		if ok && !have {
			caps, have = c, true
		}
	}
	if hasEHT {
		c, ok := ehtCaps(ehtCap, heCap, info.width)
		try(7, c, ok)
	}
	if hasHE {
		c, ok := heCaps(heCap, info.width)
		try(6, c, ok)
	}
	if hasVHT {
		sgi := hasHT && len(htCap) >= 2 && htShortGI(uint16(htCap[0])|uint16(htCap[1])<<8, info.width)
		c, ok := vhtCaps(vhtCap, info.width, sgi)
		try(5, c, ok)
	}
	if hasHT {
		c, ok := htCaps(htCap, info.width)
		try(4, c, ok)
	}

	min, max := legacyRates(es)
	if have {
		info.streams, info.minRate, info.maxRate = caps.streams, caps.minRate, caps.maxRate
	} else {
		info.minRate, info.maxRate = min, max
	}

	switch info.gen {
	case 7:
		info.standard = "be"
	case 6:
		info.standard = "ax"
	case 5:
		info.standard = "ac"
	case 4:
		info.standard = "n"
	default:
		switch {
		case band != Band2G:
			info.standard = "a"
		case max > 11:
			info.standard = "g"
		default:
			info.standard = "b"
		}
	}
	return info
}

// legacyRates returns the lowest and highest rate (Mbit/s) in the Supported
// Rates and Extended Supported Rates elements.
func legacyRates(es *elementSet) (min, max float64) {
	var rates []byte
	if b, ok := es.get(idSupportedRates); ok {
		rates = append(rates, b...)
	}
	if b, ok := es.get(idExtendedRates); ok {
		rates = append(rates, b...)
	}
	first := true
	for _, r := range rates {
		v := r & 0x7f
		if v == 0x7f {
			continue // BSS membership selector (HT/VHT/HE/SAE required), not a rate
		}
		mbps := float64(v) / 2
		if first || mbps < min {
			min = mbps
		}
		if first || mbps > max {
			max = mbps
		}
		first = false
	}
	return min, max
}
