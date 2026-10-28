package wifi

import "encoding/binary"

// mcsEntry is one modulation and coding scheme: coded bits per subcarrier and
// the coding rate num/den.
type mcsEntry struct{ bits, num, den int }

// mcsTable is indexed by MCS 0..13 (HT uses mcs%8, VHT 0..9, HE 0..11,
// EHT 0..13).
var mcsTable = [...]mcsEntry{
	{1, 1, 2}, {2, 1, 2}, {2, 3, 4}, {4, 1, 2}, {4, 3, 4}, {6, 2, 3}, {6, 3, 4}, {6, 5, 6}, // BPSK .. 64-QAM
	{8, 3, 4}, {8, 5, 6}, // 256-QAM
	{10, 3, 4}, {10, 5, 6}, // 1024-QAM
	{12, 3, 4}, {12, 5, 6}, // 4096-QAM
}

// dataSubcarriers returns the number of data subcarriers per OFDM symbol for a
// PHY generation (4 = HT, 5 = VHT, 6 = HE, 7 = EHT) and channel width in MHz.
func dataSubcarriers(gen, width int) int {
	switch gen {
	case 4:
		if width >= 40 {
			return 108
		}
		return 52
	case 5:
		switch {
		case width >= 160:
			return 468
		case width >= 80:
			return 234
		case width >= 40:
			return 108
		}
		return 52
	default: // HE and EHT
		switch {
		case width >= 320:
			return 3920
		case width >= 160:
			return 1960
		case width >= 80:
			return 980
		case width >= 40:
			return 468
		}
		return 234
	}
}

// symbolTime returns the OFDM symbol duration in microseconds, using the
// shortest guard interval the generation supports: 0.4 us (SGI) for HT/VHT
// and 0.8 us for HE/EHT.
func symbolTime(gen int, shortGI bool) float64 {
	if gen >= 6 {
		return 13.6
	}
	if shortGI {
		return 3.6
	}
	return 4.0
}

// phyRate returns the PHY data rate in Mbit/s.
func phyRate(gen, width, nss, mcs int, shortGI bool) float64 {
	if nss < 1 || mcs < 0 || mcs >= len(mcsTable) {
		return 0
	}
	m := mcsTable[mcs]
	bitsPerSymbol := float64(dataSubcarriers(gen, width)*m.bits*nss) * float64(m.num) / float64(m.den)
	return bitsPerSymbol / symbolTime(gen, shortGI)
}

// phyCaps is what an access point can do at its operating channel width.
type phyCaps struct {
	streams int
	minRate float64 // MCS 0, one stream
	maxRate float64 // best MCS, all streams
}

// htCaps decodes the HT Capabilities element.
func htCaps(b []byte, width int) (phyCaps, bool) {
	// HT cap info (2), A-MPDU parameters (1), supported MCS set (16), ...
	if len(b) < 19 {
		return phyCaps{}, false
	}
	info := binary.LittleEndian.Uint16(b[0:2])
	maxMCS := -1
	for i := 0; i < 32; i++ { // the Rx bitmask covers MCS 0..31
		if b[3+i/8]&(1<<(i%8)) != 0 {
			maxMCS = i
		}
	}
	if maxMCS < 0 {
		return phyCaps{}, false
	}
	sgi := htShortGI(info, width)
	nss := maxMCS/8 + 1
	return phyCaps{
		streams: nss,
		minRate: phyRate(4, width, 1, 0, sgi),
		maxRate: phyRate(4, width, nss, maxMCS%8, sgi),
	}, true
}

// htShortGI reports short guard interval support for the given width.
func htShortGI(info uint16, width int) bool {
	if width >= 40 {
		return info&(1<<6) != 0
	}
	return info&(1<<5) != 0
}

// vhtCaps decodes the VHT Capabilities element. htSGI is the short GI support
// taken from the HT capabilities and applies to 20 and 40 MHz channels.
func vhtCaps(b []byte, width int, htSGI bool) (phyCaps, bool) {
	// VHT cap info (4), Rx MCS map (2), Rx highest rate (2), Tx MCS map (2),
	// Tx highest rate (2).
	if len(b) < 12 {
		return phyCaps{}, false
	}
	info := binary.LittleEndian.Uint32(b[0:4])
	txMap := binary.LittleEndian.Uint16(b[8:10])
	sgi := htSGI
	switch {
	case width >= 160:
		sgi = info&(1<<6) != 0
	case width >= 80:
		sgi = info&(1<<5) != 0
	}
	var best phyCaps
	for nss := 1; nss <= 8; nss++ {
		code := int(txMap>>(2*(nss-1))) & 3
		if code == 3 {
			continue // this many streams is not supported
		}
		mcs := 7 + code
		if width == 20 && mcs == 9 && nss != 3 && nss != 6 {
			mcs = 8 // MCS 9 is not defined for most stream counts at 20 MHz
		}
		if r := phyRate(5, width, nss, mcs, sgi); r > best.maxRate {
			best.maxRate, best.streams = r, nss
		}
	}
	if best.streams == 0 {
		return phyCaps{}, false
	}
	best.minRate = phyRate(5, width, 1, 0, sgi)
	return best, true
}

// heCaps decodes the HE Capabilities element body (without the extension ID).
func heCaps(b []byte, width int) (phyCaps, bool) {
	// HE MAC cap (6), HE PHY cap (11), then the Rx and Tx HE-MCS maps for
	// <= 80 MHz (2 + 2), optionally followed by the same pair for 160 MHz and
	// 80+80 MHz.
	const (
		macLen  = 6
		phyLen  = 11
		mapBase = macLen + phyLen
	)
	if len(b) < mapBase+4 {
		return phyCaps{}, false
	}
	txMap := binary.LittleEndian.Uint16(b[mapBase+2 : mapBase+4])
	if width >= 160 && b[macLen]&(1<<3) != 0 && len(b) >= mapBase+8 {
		txMap = binary.LittleEndian.Uint16(b[mapBase+6 : mapBase+8])
	}
	var best phyCaps
	for nss := 1; nss <= 8; nss++ {
		code := int(txMap>>(2*(nss-1))) & 3
		if code == 3 {
			continue
		}
		mcs := [...]int{7, 9, 11}[code]
		if r := phyRate(6, width, nss, mcs, false); r > best.maxRate {
			best.maxRate, best.streams = r, nss
		}
	}
	if best.streams == 0 {
		return phyCaps{}, false
	}
	best.minRate = phyRate(6, width, 1, 0, false)
	return best, true
}

// ehtCaps decodes the EHT Capabilities element body. he is the HE
// Capabilities body (may be nil); it tells whether the access point is a
// 20 MHz-only device, which changes the layout of the EHT-MCS map.
func ehtCaps(eht, he []byte, width int) (phyCaps, bool) {
	// EHT MAC cap (2), EHT PHY cap (9), then the supported EHT-MCS and NSS set.
	const mapBase = 2 + 9
	if len(eht) < mapBase+3 {
		return phyCaps{}, false
	}
	set := eht[mapBase:]
	type group struct {
		mcs int
		b   byte
	}
	var groups []group
	only20 := len(he) > 6 && he[6]&0xfe == 0 // HE PHY channel width set empty
	if only20 {
		if len(set) < 4 {
			return phyCaps{}, false
		}
		groups = []group{{7, set[0]}, {9, set[1]}, {11, set[2]}, {13, set[3]}}
	} else {
		groups = []group{{9, set[0]}, {11, set[1]}, {13, set[2]}}
	}
	var best phyCaps
	for _, g := range groups {
		nss := int(g.b >> 4) // Tx maximum
		if nss == 0 {
			nss = int(g.b & 0x0f) // fall back to Rx
		}
		if nss == 0 {
			continue
		}
		if r := phyRate(7, width, nss, g.mcs, false); r > best.maxRate {
			best.maxRate, best.streams = r, nss
		}
	}
	if best.streams == 0 {
		return phyCaps{}, false
	}
	best.minRate = phyRate(7, width, 1, 0, false)
	return best, true
}

// vhtOperationWidth decodes the VHT Operation Information (channel width,
// CCFS0, CCFS1). It returns 0 when the width is 20/40 MHz, which the HT
// Operation element describes.
func vhtOperationWidth(b []byte) int {
	if len(b) < 3 {
		return 0
	}
	switch b[0] {
	case 1:
		if b[2] != 0 { // legacy signalling of 160 and 80+80 MHz
			d := int(b[1]) - int(b[2])
			if d < 0 {
				d = -d
			}
			if d == 8 || d > 16 {
				return 160
			}
		}
		return 80
	case 2, 3:
		return 160 // 80+80 MHz is reported as 160
	}
	return 0
}

// parseHEOperation extracts the BSS colour and widths from the HE Operation
// element body (without the extension ID). width6 is the channel width from
// the 6 GHz Operation Information and vhtWidth the one from the embedded VHT
// Operation Information; both are 0 when absent.
func parseHEOperation(b []byte) (color, width6, vhtWidth int) {
	// HE Operation parameters (3), BSS colour info (1), basic HE-MCS and NSS
	// set (2), then optional fields.
	if len(b) < 6 {
		return 0, 0, 0
	}
	params := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
	if b[3]&0x80 == 0 { // bit 7: BSS colour disabled
		color = int(b[3] & 0x3f)
	}
	rest := b[6:]
	if params&(1<<14) != 0 { // VHT Operation Information present
		if len(rest) < 3 {
			return color, 0, 0
		}
		vhtWidth = vhtOperationWidth(rest[:3])
		rest = rest[3:]
	}
	if params&(1<<15) != 0 { // co-hosted BSS: max BSSID indicator
		if len(rest) < 1 {
			return color, 0, vhtWidth
		}
		rest = rest[1:]
	}
	if params&(1<<17) != 0 && len(rest) >= 2 { // 6 GHz Operation Information
		width6 = [...]int{20, 40, 80, 160}[rest[1]&3]
	}
	return color, width6, vhtWidth
}

// ehtOperationWidth returns the channel width from the EHT Operation element
// body, or 0 when it carries no operation information.
func ehtOperationWidth(b []byte) int {
	// Parameters (1), basic EHT-MCS and NSS set (4), then the optional EHT
	// Operation Information whose first byte is the control field.
	if len(b) < 6 || b[0]&1 == 0 {
		return 0
	}
	switch b[5] & 7 {
	case 0:
		return 20
	case 1:
		return 40
	case 2:
		return 80
	case 3:
		return 160
	case 4:
		return 320
	}
	return 0
}
