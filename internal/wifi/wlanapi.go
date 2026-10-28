package wifi

import (
	"encoding/binary"
	"unicode/utf16"
)

// This file decodes the binary structures returned by the Windows WLAN API
// (wlanapi.dll). It is platform independent on purpose, so the layout logic
// can be unit tested on any OS; scan_windows.go does the actual calls.
//
// Offsets follow the 64-bit Windows layout (ULONGLONG fields are 8-byte
// aligned). 32-bit x86 Windows is therefore not supported.

const (
	// WLAN_INTERFACE_INFO_LIST: DWORD count, DWORD index, then the entries.
	wlanIfaceListHeader = 8
	// WLAN_INTERFACE_INFO: GUID, WCHAR[256] description, state.
	wlanIfaceSize     = 532
	wlanIfaceDescOff  = 16
	wlanIfaceDescLen  = 256
	wlanIfaceStateOff = 528

	// WLAN_BSS_LIST: DWORD total size, DWORD count, then the entries.
	wlanBSSListHeader = 8
	// WLAN_BSS_ENTRY.
	wlanBSSEntrySize    = 360
	wlanBSSOffBSSID     = 40
	wlanBSSOffRSSI      = 56
	wlanBSSOffBeacon    = 66
	wlanBSSOffTimestamp = 72
	wlanBSSOffCap       = 88
	wlanBSSOffFreqKHz   = 92
	wlanBSSOffIEOffset  = 352
	wlanBSSOffIESize    = 356

	// WLAN_CONNECTION_ATTRIBUTES: state, mode, WCHAR[256] profile name, then
	// WLAN_ASSOCIATION_ATTRIBUTES (SSID 36 bytes, BSS type 4, BSSID 6).
	wlanConnOffState = 0
	wlanConnOffBSSID = 560
	wlanConnLen      = wlanConnOffBSSID + 6

	// WLAN_INTERFACE_STATE value for a connected interface.
	wlanStateConnected = 1
)

// winInterface is one entry of WLAN_INTERFACE_INFO_LIST.
type winInterface struct {
	GUID  [16]byte
	Desc  string
	State uint32
}

// parseInterfaceList decodes a WLAN_INTERFACE_INFO_LIST.
func parseInterfaceList(buf []byte) []winInterface {
	if len(buf) < wlanIfaceListHeader {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[0:4]))
	var out []winInterface
	for i := 0; i < n; i++ {
		off := wlanIfaceListHeader + i*wlanIfaceSize
		if off+wlanIfaceSize > len(buf) {
			break
		}
		e := buf[off : off+wlanIfaceSize]
		var wi winInterface
		copy(wi.GUID[:], e[:16])
		wi.Desc = utf16String(e[wlanIfaceDescOff : wlanIfaceDescOff+2*wlanIfaceDescLen])
		wi.State = binary.LittleEndian.Uint32(e[wlanIfaceStateOff:])
		out = append(out, wi)
	}
	return out
}

// utf16String decodes a NUL-terminated little-endian UTF-16 string.
func utf16String(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// parseBSSList decodes a WLAN_BSS_LIST into platform independent entries.
// Malformed entries are skipped. The returned data does not alias buf.
func parseBSSList(buf []byte) []rawBSS {
	if len(buf) < wlanBSSListHeader {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[4:8]))
	var out []rawBSS
	for i := 0; i < n; i++ {
		off := wlanBSSListHeader + i*wlanBSSEntrySize
		if off+wlanBSSEntrySize > len(buf) {
			break
		}
		e := buf[off : off+wlanBSSEntrySize]
		le := binary.LittleEndian

		// The IE offset is relative to the start of the entry.
		ieOff := int(le.Uint32(e[wlanBSSOffIEOffset:]))
		ieSize := int(le.Uint32(e[wlanBSSOffIESize:]))
		var ies []byte
		if ieOff >= 0 && ieSize >= 0 && off+ieOff+ieSize <= len(buf) {
			ies = append([]byte(nil), buf[off+ieOff:off+ieOff+ieSize]...)
		}

		out = append(out, rawBSS{
			BSSID:       append([]byte(nil), e[wlanBSSOffBSSID:wlanBSSOffBSSID+6]...),
			FreqMHz:     int(le.Uint32(e[wlanBSSOffFreqKHz:]) / 1000),
			TSF:         le.Uint64(e[wlanBSSOffTimestamp:]),
			BeaconTU:    le.Uint16(e[wlanBSSOffBeacon:]),
			Capability:  le.Uint16(e[wlanBSSOffCap:]),
			SignalMBM:   int32(le.Uint32(e[wlanBSSOffRSSI:])) * 100,
			HasMBM:      true,
			SeenUnknown: true,
			IEs:         ies,
		})
	}
	return out
}

// parseConnectedBSSID extracts the BSSID the interface is associated with
// from a WLAN_CONNECTION_ATTRIBUTES, or nil when it is not connected.
func parseConnectedBSSID(buf []byte) []byte {
	if len(buf) < wlanConnLen {
		return nil
	}
	if binary.LittleEndian.Uint32(buf[wlanConnOffState:]) != wlanStateConnected {
		return nil
	}
	return append([]byte(nil), buf[wlanConnOffBSSID:wlanConnOffBSSID+6]...)
}
