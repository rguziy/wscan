package wifi

import (
	"encoding/binary"
	"strings"
)

// rsnInfo is the decoded content of an RSN element or of the WPA vendor
// element, which share one layout.
type rsnInfo struct {
	ciphers    []string
	akms       []string
	caps       uint16
	hasCaps    bool
	psk        bool
	sae        bool
	owe        bool
	enterprise bool
	suiteB     bool
}

var (
	ouiIEEE = [3]byte{0x00, 0x0f, 0xac}
	ouiWFA  = [3]byte{0x00, 0x50, 0xf2} // Microsoft/WFA, used by WPA and WPS
)

func cipherName(oui [3]byte, t byte) string {
	if oui != ouiIEEE && oui != ouiWFA {
		return ""
	}
	switch t {
	case 1:
		return "WEP-40"
	case 2:
		return "TKIP"
	case 4:
		return "CCMP"
	case 5:
		return "WEP-104"
	case 8:
		return "GCMP"
	case 9:
		return "GCMP-256"
	case 10:
		return "CCMP-256"
	}
	return ""
}

func akmName(oui [3]byte, t byte) string {
	switch oui {
	case ouiWFA:
		switch t {
		case 1:
			return "802.1X"
		case 2:
			return "PSK"
		}
	case ouiIEEE:
		switch t {
		case 1:
			return "802.1X"
		case 2:
			return "PSK"
		case 3:
			return "FT-802.1X"
		case 4:
			return "FT-PSK"
		case 5:
			return "802.1X-SHA256"
		case 6:
			return "PSK-SHA256"
		case 8:
			return "SAE"
		case 9:
			return "FT-SAE"
		case 11:
			return "Suite-B"
		case 12:
			return "Suite-B-192"
		case 18:
			return "OWE"
		case 24:
			return "SAE-EXT"
		case 25:
			return "FT-SAE-EXT"
		}
	}
	return ""
}

func addUnique(list []string, s string) []string {
	if s == "" {
		return list
	}
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// parseRSN decodes the body of an RSN element, or of a WPA vendor element
// after its OUI and type bytes.
func parseRSN(b []byte) (rsnInfo, bool) {
	var info rsnInfo
	// Version (2), group cipher suite (4), pairwise count (2).
	if len(b) < 8 || binary.LittleEndian.Uint16(b[0:2]) != 1 {
		return info, false
	}
	suite := func(s []byte) ([3]byte, byte) { return [3]byte{s[0], s[1], s[2]}, s[3] }

	oui, t := suite(b[2:6])
	info.ciphers = addUnique(info.ciphers, cipherName(oui, t))
	n := int(binary.LittleEndian.Uint16(b[6:8]))
	b = b[8:]
	if len(b) < 4*n {
		return info, true
	}
	for i := 0; i < n; i++ {
		oui, t := suite(b[4*i : 4*i+4])
		info.ciphers = addUnique(info.ciphers, cipherName(oui, t))
	}
	b = b[4*n:]

	if len(b) < 2 {
		return info, true
	}
	n = int(binary.LittleEndian.Uint16(b[0:2]))
	b = b[2:]
	if len(b) < 4*n {
		return info, true
	}
	for i := 0; i < n; i++ {
		oui, t := suite(b[4*i : 4*i+4])
		name := akmName(oui, t)
		info.akms = addUnique(info.akms, name)
		switch {
		case strings.Contains(name, "SAE"):
			info.sae = true
		case name == "OWE":
			info.owe = true
		case strings.Contains(name, "PSK"):
			info.psk = true
		case strings.Contains(name, "802.1X") || strings.HasPrefix(name, "Suite-B"):
			info.enterprise = true
			info.suiteB = info.suiteB || strings.HasPrefix(name, "Suite-B")
		}
	}
	b = b[4*n:]
	if len(b) >= 2 {
		info.caps = binary.LittleEndian.Uint16(b[0:2])
		info.hasCaps = true
	}
	return info, true
}

// decodeSecurity classifies the security of a BSS from its elements and its
// capability field (bit 4 is the Privacy bit, set for WEP).
func decodeSecurity(es *elementSet, capability uint16) Security {
	var sec Security

	var rsn rsnInfo
	hasRSN := false
	if body, ok := es.get(idRSN); ok {
		rsn, hasRSN = parseRSN(body)
	}

	var wpa rsnInfo
	hasWPA := false
	for _, v := range es.vendor {
		if len(v) < 4 || [3]byte{v[0], v[1], v[2]} != ouiWFA {
			continue
		}
		switch v[3] {
		case 1: // WPA information element
			if w, ok := parseRSN(v[4:]); ok && !hasWPA {
				wpa, hasWPA = w, true
			}
		case 4: // Wi-Fi Simple Configuration
			sec.WPS = true
		}
	}

	switch {
	case hasRSN:
		switch {
		case rsn.owe && !rsn.psk && !rsn.sae && !rsn.enterprise:
			sec.Protocol = "OWE"
		case rsn.enterprise:
			sec.Enterprise = true
			if rsn.suiteB || rsn.sae {
				sec.Protocol = "WPA3-Enterprise"
			} else {
				sec.Protocol = "WPA2-Enterprise"
			}
		case rsn.sae && rsn.psk:
			sec.Protocol = "WPA2/WPA3"
		case rsn.sae:
			sec.Protocol = "WPA3"
		default:
			sec.Protocol = "WPA2"
		}
		if hasWPA && !sec.Enterprise && sec.Protocol == "WPA2" {
			sec.Protocol = "WPA/WPA2"
		}
	case hasWPA:
		sec.Protocol = "WPA"
		if wpa.enterprise {
			sec.Protocol = "WPA-Enterprise"
			sec.Enterprise = true
		}
	case capability&0x0010 != 0:
		sec.Protocol = "WEP"
	default:
		sec.Protocol = "Open"
	}

	for _, info := range []rsnInfo{rsn, wpa} {
		for _, c := range info.ciphers {
			sec.Ciphers = addUnique(sec.Ciphers, c)
		}
		for _, a := range info.akms {
			sec.AKMs = addUnique(sec.AKMs, a)
		}
	}
	if rsn.hasCaps {
		switch {
		case rsn.caps&(1<<6) != 0:
			sec.PMF = "required"
		case rsn.caps&(1<<7) != 0:
			sec.PMF = "capable"
		}
	}
	return sec
}
