package genl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestAttrRoundTrip(t *testing.T) {
	var b Builder
	b.Uint16(1, 0xbeef)
	b.Uint32(2, 0xdeadbeef)
	b.String(3, "nl80211")
	b.Bytes(4, []byte{1, 2, 3}) // not a multiple of 4: padded
	b.Bytes(5, nil)
	b.Nested(6, func(n *Builder) {
		n.Uint32(1, 7)
		n.String(2, "inner")
	})

	attrs, err := ParseAttrs(b.Data())
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 6 {
		t.Fatalf("got %d attributes, want 6", len(attrs))
	}
	if v, ok := attrs[0].Uint16(); !ok || v != 0xbeef {
		t.Errorf("u16 = %#x %v", v, ok)
	}
	if v, ok := attrs[1].Uint32(); !ok || v != 0xdeadbeef {
		t.Errorf("u32 = %#x %v", v, ok)
	}
	if s := attrs[2].String(); s != "nl80211" {
		t.Errorf("string = %q (the NUL terminator must be removed)", s)
	}
	if !bytes.Equal(attrs[3].Data, []byte{1, 2, 3}) {
		t.Errorf("bytes = %v", attrs[3].Data)
	}
	if len(attrs[4].Data) != 0 {
		t.Errorf("empty attribute has %d bytes", len(attrs[4].Data))
	}
	inner, err := attrs[5].Nested()
	if err != nil || len(inner) != 2 || inner[1].String() != "inner" {
		t.Fatalf("nested = %+v, %v", inner, err)
	}
	if attrs[5].Type != 6 {
		t.Errorf("nested flag not masked out of the type: %#x", attrs[5].Type)
	}
	if _, ok := attrs[0].Uint32(); ok {
		t.Error("Uint32 accepted a 2-byte payload")
	}
}

func TestParseAttrsMalformed(t *testing.T) {
	var b Builder
	b.Uint32(1, 1)
	good := b.Data()

	cases := map[string][]byte{
		"short header":     good[:2],
		"length too large": append(append([]byte(nil), good...), 0xff, 0x00, 0x01, 0x00),
		"length too small": {2, 0, 1, 0},
	}
	for name, data := range cases {
		if _, err := ParseAttrs(data); !errors.Is(err, ErrShort) {
			t.Errorf("%s: err = %v, want ErrShort", name, err)
		}
	}
	// Attributes before the damage are still returned.
	attrs, err := ParseAttrs(cases["length too large"])
	if err == nil || len(attrs) != 1 {
		t.Errorf("got %d attrs, err %v; want the leading attribute and an error", len(attrs), err)
	}
}

func TestMessageFraming(t *testing.T) {
	var data Builder
	data.Uint32(3, 42)
	req := marshal(0x1234, FlagDump, 77, Message{Command: 32, Version: 1, Data: data.Data()})

	raws, err := parseDatagram(req)
	if err != nil || len(raws) != 1 {
		t.Fatalf("parse = %v, %v", raws, err)
	}
	r := raws[0]
	if r.typ != 0x1234 || r.seq != 77 || r.flags&FlagRequest == 0 || r.flags&FlagDump != FlagDump {
		t.Errorf("header = type %#x seq %d flags %#x", r.typ, r.seq, r.flags)
	}
	m, err := r.toMessage()
	if err != nil || m.Command != 32 || m.Version != 1 {
		t.Fatalf("message = %+v, %v", m, err)
	}
	if !bytes.Equal(m.Data, data.Data()) {
		t.Errorf("payload = %v", m.Data)
	}

	// Two messages in one datagram, the second one padded.
	two := append(append([]byte(nil), req...), marshal(5, 0, 78, Message{Command: 1, Data: []byte{1}})...)
	raws, err = parseDatagram(two)
	if err != nil || len(raws) != 2 || raws[1].seq != 78 {
		t.Errorf("two messages: %v, %v", raws, err)
	}

	// The payload is copied, so the receive buffer can be reused.
	buf := append([]byte(nil), req...)
	raws, _ = parseDatagram(buf)
	for i := range buf {
		buf[i] = 0xaa
	}
	if m, _ := raws[0].toMessage(); m.Command != 32 {
		t.Error("payload aliases the input buffer")
	}

	if _, err := parseDatagram(req[:10]); err != nil && !errors.Is(err, ErrShort) {
		t.Errorf("short datagram: %v", err)
	}
	bad := append([]byte(nil), req...)
	binary.NativeEndian.PutUint32(bad[0:4], uint32(len(bad)+100))
	if _, err := parseDatagram(bad); !errors.Is(err, ErrShort) {
		t.Errorf("oversized length: %v", err)
	}
}

func TestParseFamily(t *testing.T) {
	var b Builder
	b.String(ctrlAttrFamilyName, "nl80211")
	b.Uint16(ctrlAttrFamilyID, 28)
	b.Nested(ctrlAttrMcastGroups, func(g *Builder) {
		g.Nested(1, func(e *Builder) {
			e.String(ctrlAttrMcastGrpName, "scan")
			e.Uint32(ctrlAttrMcastGrpID, 5)
		})
		g.Nested(2, func(e *Builder) {
			e.String(ctrlAttrMcastGrpName, "mlme")
			e.Uint32(ctrlAttrMcastGrpID, 7)
		})
	})

	fam, err := parseFamily(Message{Data: b.Data()})
	if err != nil {
		t.Fatal(err)
	}
	if fam.ID != 28 || fam.Groups["scan"] != 5 || fam.Groups["mlme"] != 7 {
		t.Errorf("family = %+v", fam)
	}

	var noID Builder
	noID.String(ctrlAttrFamilyName, "x")
	if _, err := parseFamily(Message{Data: noID.Data()}); err == nil {
		t.Error("a reply without a family ID must fail")
	}

	req := familyRequest("nl80211")
	attrs, _ := ParseAttrs(req.Data)
	if req.Command != ctrlCmdGetFamily || len(attrs) != 1 || attrs[0].String() != "nl80211" {
		t.Errorf("family request = %+v", req)
	}
}
