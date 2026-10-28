package wifi

import (
	"testing"

	"github.com/rguziy/wscan/internal/genl"
)

func bssAttrs(freq uint32, mbm int32, ies []byte) []byte {
	var b genl.Builder
	b.Bytes(bssBSSID, []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x09})
	b.Uint32(bssFrequency, freq)
	b.Uint16(bssBeaconInterval, 100)
	b.Uint16(bssCapability, 0x0011)
	b.Uint32(bssSignalMBM, uint32(mbm))
	b.Uint32(bssSeenMSAgo, 750)
	b.Uint32(bssStatus, bssStatusAssociated)
	b.Bytes(bssInformationElems, ies)
	return b.Data()
}

func TestParseScanMessages(t *testing.T) {
	ies := cat(el(idSSID, []byte("Test")...), rsnEl([]byte{2}, 0), htCapEl(2, true), htOpEl(6, false))

	var msg genl.Builder
	msg.Uint32(attrIfIndex, 3)
	msg.Nested(attrBSS, func(n *genl.Builder) {
		// Re-use the already encoded attributes as the nested payload.
		attrs, _ := genl.ParseAttrs(bssAttrs(2437, -6350, ies))
		for _, a := range attrs {
			n.Bytes(a.Type, a.Data)
		}
	})
	// A second, malformed entry (no BSSID) must be skipped, not abort the dump.
	msg.Nested(attrBSS, func(n *genl.Builder) { n.Uint32(bssFrequency, 2412) })

	aps := parseScanMessages([]genl.Message{{Command: cmdNewScanResults, Version: 1, Data: msg.Data()}})
	if len(aps) != 1 {
		t.Fatalf("got %d access points, want 1", len(aps))
	}
	ap := aps[0]
	if ap.SSID != "Test" || ap.Channel != 6 || ap.Band != Band2G {
		t.Errorf("ap = %+v", ap)
	}
	if ap.SignalDBM != -64 && ap.SignalDBM != -63 {
		t.Errorf("signal = %d", ap.SignalDBM)
	}
	if ap.Security.Protocol != "WPA2" || !ap.Connected || ap.SeenAgoMS != 750 {
		t.Errorf("security %q connected %v seen %d", ap.Security.Protocol, ap.Connected, ap.SeenAgoMS)
	}
	if ap.Standard != "n" || ap.WidthMHz != 20 {
		t.Errorf("standard %s width %d", ap.Standard, ap.WidthMHz)
	}
}

func TestParseInterface(t *testing.T) {
	var b genl.Builder
	b.Uint32(attrWiphy, 0)
	b.Uint32(attrIfIndex, 3)
	b.String(attrIfName, "wlan0")
	b.Uint32(attrIfType, ifTypeStation)
	b.Bytes(attrMAC, []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x02})

	iface, ok := parseInterface(genl.Message{Data: b.Data()})
	if !ok || iface.Name != "wlan0" || iface.Index != 3 || iface.Type != "station" || iface.MAC != "00:00:5E:00:53:02" {
		t.Errorf("iface = %+v ok=%v", iface, ok)
	}

	var p2p genl.Builder // a P2P device has a wdev but no ifindex
	p2p.Uint32(attrWiphy, 0)
	if _, ok := parseInterface(genl.Message{Data: p2p.Data()}); ok {
		t.Error("interface without an index must be skipped")
	}
}

func TestPickInterface(t *testing.T) {
	ifaces := []Interface{
		{Index: 2, Name: "wlan1", Type: "monitor"},
		{Index: 3, Name: "wlan0", Type: "station"},
	}
	if i, err := pickInterface(ifaces, ""); err != nil || i.Name != "wlan0" {
		t.Errorf("default pick = %+v, %v (want the station interface)", i, err)
	}
	if i, err := pickInterface(ifaces, "wlan1"); err != nil || i.Name != "wlan1" {
		t.Errorf("named pick = %+v, %v", i, err)
	}
	if _, err := pickInterface(ifaces, "nope"); err == nil {
		t.Error("unknown interface accepted")
	}
	if _, err := pickInterface(nil, ""); err == nil {
		t.Error("no interfaces must be an error")
	}
	if i, _ := pickInterface([]Interface{{Index: 1, Name: "x", Type: "ap"}}, ""); i.Name != "x" {
		t.Error("with no station interface the first one is used")
	}
}
