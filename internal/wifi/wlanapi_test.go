package wifi

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// buildBSSList assembles a WLAN_BSS_LIST with one entry the way Windows lays
// it out: header, fixed-size entry, then the information elements.
func buildBSSList(bssid [6]byte, rssi int32, freqKHz uint32, beaconTU uint16, tsf uint64, ies []byte) []byte {
	le := binary.LittleEndian
	entry := make([]byte, wlanBSSEntrySize)
	copy(entry[wlanBSSOffBSSID:], bssid[:])
	le.PutUint32(entry[wlanBSSOffRSSI:], uint32(rssi))
	le.PutUint16(entry[wlanBSSOffBeacon:], beaconTU)
	le.PutUint64(entry[wlanBSSOffTimestamp:], tsf)
	le.PutUint32(entry[wlanBSSOffFreqKHz:], freqKHz)
	le.PutUint32(entry[wlanBSSOffIEOffset:], wlanBSSEntrySize)
	le.PutUint32(entry[wlanBSSOffIESize:], uint32(len(ies)))

	buf := make([]byte, wlanBSSListHeader, wlanBSSListHeader+len(entry)+len(ies))
	buf = append(buf, entry...)
	buf = append(buf, ies...)
	le.PutUint32(buf[0:], uint32(len(buf)))
	le.PutUint32(buf[4:], 1)
	return buf
}

func TestParseBSSList(t *testing.T) {
	ssid := []byte("Lakeside")
	ies := append([]byte{0, byte(len(ssid))}, ssid...)
	buf := buildBSSList([6]byte{0x00, 0x00, 0x5E, 0x00, 0x53, 0x01}, -57, 5180000, 100, 3_600_000_000, ies)

	raw := parseBSSList(buf)
	if len(raw) != 1 {
		t.Fatalf("got %d entries, want 1", len(raw))
	}
	r := raw[0]
	if r.FreqMHz != 5180 || r.BeaconTU != 100 || r.SignalMBM != -5700 || r.TSF != 3_600_000_000 {
		t.Errorf("fields: freq=%d beacon=%d mbm=%d tsf=%d", r.FreqMHz, r.BeaconTU, r.SignalMBM, r.TSF)
	}
	if !r.SeenUnknown {
		t.Error("Windows entries must report the age as unknown")
	}

	ap := decode(r)
	if ap.SSID != "Lakeside" || ap.BSSID != "00:00:5E:00:53:01" {
		t.Errorf("ssid=%q bssid=%q", ap.SSID, ap.BSSID)
	}
	if ap.Band != Band5G || ap.Channel != 36 || ap.SignalDBM != -57 {
		t.Errorf("band=%v channel=%d signal=%d", ap.Band, ap.Channel, ap.SignalDBM)
	}
	if ap.SeenAgoMS != -1 {
		t.Errorf("SeenAgoMS = %d, want -1", ap.SeenAgoMS)
	}
	if ap.UptimeSeconds != 3600 {
		t.Errorf("uptime = %ds, want 3600", ap.UptimeSeconds)
	}
}

func TestParseBSSListTruncated(t *testing.T) {
	buf := buildBSSList([6]byte{1, 2, 3, 4, 5, 6}, -60, 2412000, 100, 0, nil)
	for _, n := range []int{0, 4, 7, 8, 100, len(buf) - 1} {
		// Must not panic; a cut entry is skipped.
		if got := parseBSSList(buf[:n]); len(got) != 0 {
			t.Errorf("len %d: got %d entries from truncated data", n, len(got))
		}
	}
}

func TestParseBSSListBadIEBounds(t *testing.T) {
	buf := buildBSSList([6]byte{1, 2, 3, 4, 5, 6}, -60, 2412000, 100, 0, []byte{0, 0})
	binary.LittleEndian.PutUint32(buf[wlanBSSListHeader+wlanBSSOffIESize:], 1<<30)
	raw := parseBSSList(buf)
	if len(raw) != 1 || len(raw[0].IEs) != 0 {
		t.Fatalf("entry with out-of-range IE size: %+v", raw)
	}
}

func TestParseInterfaceList(t *testing.T) {
	buf := make([]byte, wlanIfaceListHeader+wlanIfaceSize)
	binary.LittleEndian.PutUint32(buf[0:], 1)
	e := buf[wlanIfaceListHeader:]
	e[0] = 0xAB
	for i, c := range utf16.Encode([]rune("Intel(R) Wi-Fi 6 AX201")) {
		binary.LittleEndian.PutUint16(e[wlanIfaceDescOff+2*i:], c)
	}
	binary.LittleEndian.PutUint32(e[wlanIfaceStateOff:], wlanStateConnected)

	got := parseInterfaceList(buf)
	if len(got) != 1 || got[0].Desc != "Intel(R) Wi-Fi 6 AX201" || got[0].GUID[0] != 0xAB || got[0].State != wlanStateConnected {
		t.Fatalf("got %+v", got)
	}
	if n := len(parseInterfaceList(buf[:len(buf)-1])); n != 0 {
		t.Errorf("truncated list produced %d entries", n)
	}
}

func TestParseConnectedBSSID(t *testing.T) {
	buf := make([]byte, wlanConnLen)
	copy(buf[wlanConnOffBSSID:], []byte{1, 2, 3, 4, 5, 6})
	if got := parseConnectedBSSID(buf); got != nil {
		t.Errorf("disconnected state returned %v", got)
	}
	binary.LittleEndian.PutUint32(buf[wlanConnOffState:], wlanStateConnected)
	if got := parseConnectedBSSID(buf); len(got) != 6 || got[5] != 6 {
		t.Errorf("connected state returned %v", got)
	}
	if got := parseConnectedBSSID(buf[:10]); got != nil {
		t.Errorf("short buffer returned %v", got)
	}
}
