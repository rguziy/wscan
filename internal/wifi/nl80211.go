package wifi

import (
	"fmt"

	"github.com/rguziy/wscan/internal/genl"
)

// nl80211 constants (include/uapi/linux/nl80211.h). The decoding below is
// platform independent so it can be tested anywhere; the socket code that
// feeds it lives in scan_linux.go.
const (
	nl80211Family = "nl80211"

	cmdGetWiphy       = 1
	cmdGetInterface   = 5
	cmdGetScan        = 32
	cmdTriggerScan    = 33
	cmdNewScanResults = 34
	cmdScanAborted    = 35

	attrWiphy     = 1
	attrIfIndex   = 3
	attrIfName    = 4
	attrIfType    = 5
	attrMAC       = 6
	attrBSS       = 47
	attrScanSSIDs = 45

	attrWiphyBands     = 22
	attrSplitWiphyDump = 174

	// NL80211_BAND_ATTR_FREQS and NL80211_FREQUENCY_ATTR_*.
	bandAttrFreqs    = 1
	freqAttrFreq     = 1
	freqAttrDisabled = 2

	ifTypeStation = 2

	bssBSSID            = 1
	bssFrequency        = 2
	bssTSF              = 3
	bssBeaconInterval   = 4
	bssCapability       = 5
	bssInformationElems = 6
	bssSignalMBM        = 7
	bssSignalUnspec     = 8
	bssStatus           = 9
	bssSeenMSAgo        = 10
	bssBeaconIEs        = 11
	bssBeaconTSF        = 13
)

// parseBSS decodes the nested NL80211_ATTR_BSS attribute of a scan result.
func parseBSS(attr genl.Attr) (rawBSS, error) {
	fields, err := attr.Nested()
	if err != nil {
		return rawBSS{}, fmt.Errorf("decode BSS: %w", err)
	}
	var r rawBSS
	for _, f := range fields {
		switch f.Type {
		case bssBSSID:
			r.BSSID = append([]byte(nil), f.Data...)
		case bssFrequency:
			if v, ok := f.Uint32(); ok {
				r.FreqMHz = int(v)
			}
		case bssTSF:
			r.TSF, _ = f.Uint64()
		case bssBeaconTSF:
			r.BeaconTSF, _ = f.Uint64()
		case bssBeaconInterval:
			r.BeaconTU, _ = f.Uint16()
		case bssCapability:
			r.Capability, _ = f.Uint16()
		case bssInformationElems:
			r.IEs = append([]byte(nil), f.Data...)
		case bssBeaconIEs:
			r.BeaconIEs = append([]byte(nil), f.Data...)
		case bssSignalMBM:
			if v, ok := f.Uint32(); ok {
				r.SignalMBM, r.HasMBM = int32(v), true
			}
		case bssSignalUnspec:
			if v, ok := f.Uint8(); ok {
				r.SignalUnspec, r.HasUnspec = v, true
			}
		case bssStatus:
			if v, ok := f.Uint32(); ok {
				r.Status, r.HasStatus = v, true
			}
		case bssSeenMSAgo:
			r.SeenMS, _ = f.Uint32()
		}
	}
	if len(r.BSSID) != 6 {
		return rawBSS{}, fmt.Errorf("decode BSS: bad BSSID length %d", len(r.BSSID))
	}
	return r, nil
}

// parseScanMessages decodes the BSS entries of GET_SCAN replies.
func parseScanMessages(msgs []genl.Message) []AP {
	var aps []AP
	for _, m := range msgs {
		attrs, _ := genl.ParseAttrs(m.Data)
		for _, a := range attrs {
			if a.Type != attrBSS {
				continue
			}
			raw, err := parseBSS(a)
			if err != nil {
				continue // skip a malformed entry, keep the rest
			}
			aps = append(aps, decode(raw))
		}
	}
	return aps
}

// parseInterface decodes one GET_INTERFACE reply. ok is false for entries
// without an interface index (such as P2P devices).
func parseInterface(m genl.Message) (iface Interface, ok bool) {
	attrs, _ := genl.ParseAttrs(m.Data)
	var typ uint32
	for _, a := range attrs {
		switch a.Type {
		case attrIfIndex:
			if v, k := a.Uint32(); k {
				iface.Index = int(v)
			}
		case attrIfName:
			iface.Name = a.String()
		case attrIfType:
			typ, _ = a.Uint32()
		case attrMAC:
			if len(a.Data) == 6 {
				iface.MAC = formatMAC(a.Data)
			}
		case attrWiphy:
			if v, k := a.Uint32(); k {
				iface.Wiphy = int(v)
			}
		}
	}
	iface.Type = interfaceTypeName(typ)
	return iface, iface.Index != 0 && iface.Name != ""
}

func interfaceTypeName(t uint32) string {
	switch t {
	case 1:
		return "ad-hoc"
	case ifTypeStation:
		return "station"
	case 3:
		return "ap"
	case 4:
		return "ap-vlan"
	case 5:
		return "wds"
	case 6:
		return "monitor"
	case 7:
		return "mesh"
	case 8:
		return "p2p-client"
	case 9:
		return "p2p-go"
	}
	return "unknown"
}

// pickInterface chooses the interface to scan with: the named one, or the
// first station interface.
func pickInterface(ifaces []Interface, name string) (Interface, error) {
	if len(ifaces) == 0 {
		return Interface{}, fmt.Errorf("no Wi-Fi interfaces found")
	}
	if name != "" {
		for _, i := range ifaces {
			if i.Name == name {
				return i, nil
			}
		}
		names := make([]string, len(ifaces))
		for k, i := range ifaces {
			names[k] = i.Name
		}
		return Interface{}, fmt.Errorf("Wi-Fi interface %q not found (available: %v)", name, names)
	}
	for _, i := range ifaces {
		if i.Type == "station" {
			return i, nil
		}
	}
	return ifaces[0], nil
}

// parseWiphyBands works out which bands the adapter (wiphy) can use from the
// replies to a split GET_WIPHY dump. The bands are spread over several
// messages, so the replies are merged. It returns nil when the replies carry
// no band information at all, meaning "unknown".
func parseWiphyBands(msgs []genl.Message, wiphy int) map[Band]BandSupport {
	present := map[Band]bool{}
	usable := map[Band]bool{}
	for _, m := range msgs {
		attrs, _ := genl.ParseAttrs(m.Data)
		var id int = -1
		var bands []genl.Attr
		for _, a := range attrs {
			switch a.Type {
			case attrWiphy:
				if v, ok := a.Uint32(); ok {
					id = int(v)
				}
			case attrWiphyBands:
				bands, _ = a.Nested()
			}
		}
		if id != wiphy {
			continue
		}
		for _, band := range bands {
			fields, _ := band.Nested()
			for _, f := range fields {
				if f.Type != bandAttrFreqs {
					continue
				}
				freqs, _ := f.Nested()
				for _, fr := range freqs {
					entry, _ := fr.Nested()
					var mhz int
					disabled := false
					for _, e := range entry {
						switch e.Type {
						case freqAttrFreq:
							if v, ok := e.Uint32(); ok {
								mhz = int(v)
							}
						case freqAttrDisabled:
							disabled = true
						}
					}
					if b := BandOf(mhz); b != BandUnknown {
						present[b] = true
						if !disabled {
							usable[b] = true
						}
					}
				}
			}
		}
	}
	if len(present) == 0 {
		return nil
	}
	out := map[Band]BandSupport{}
	for _, b := range Bands() {
		switch {
		case usable[b]:
			out[b] = SupportOK
		case present[b]:
			out[b] = SupportDisabled
		default:
			out[b] = SupportNone
		}
	}
	return out
}
