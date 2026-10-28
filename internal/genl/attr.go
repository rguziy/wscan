// Package genl is a minimal generic netlink client built on the standard
// library only. It implements just enough of the protocol to talk to the
// Linux nl80211 (Wi-Fi) family: attribute encoding and decoding, message
// framing, family resolution, request/response exchanges and multicast
// events.
//
// The wire codec in this file and in message.go is platform independent so it
// can be tested everywhere; the socket code lives in conn_linux.go.
package genl

import (
	"encoding/binary"
	"errors"
)

// ErrShort is returned when a netlink buffer is truncated or malformed.
var ErrShort = errors.New("genl: short or malformed netlink data")

const (
	attrHeaderLen = 4
	attrAlign     = 4

	flagNested       = 0x8000 // NLA_F_NESTED
	flagNetByteorder = 0x4000 // NLA_F_NET_BYTEORDER
	attrTypeMask     = 0x3fff
)

func align(n int) int { return (n + attrAlign - 1) &^ (attrAlign - 1) }

// Attr is a single netlink attribute. Type has the NLA_F_* flag bits removed.
type Attr struct {
	Type uint16
	Data []byte
}

// ParseAttrs splits a buffer of netlink attributes. Attributes parsed before
// an error are still returned.
func ParseAttrs(b []byte) ([]Attr, error) {
	var out []Attr
	for len(b) > 0 {
		if len(b) < attrHeaderLen {
			return out, ErrShort
		}
		n := int(binary.NativeEndian.Uint16(b[0:2]))
		t := binary.NativeEndian.Uint16(b[2:4])
		if n < attrHeaderLen || n > len(b) {
			return out, ErrShort
		}
		out = append(out, Attr{Type: t & attrTypeMask, Data: b[attrHeaderLen:n]})
		adv := align(n)
		if adv > len(b) {
			adv = len(b)
		}
		b = b[adv:]
	}
	return out, nil
}

// Nested parses the payload of a nested attribute.
func (a Attr) Nested() ([]Attr, error) { return ParseAttrs(a.Data) }

// Uint8 returns the payload as a u8.
func (a Attr) Uint8() (uint8, bool) {
	if len(a.Data) != 1 {
		return 0, false
	}
	return a.Data[0], true
}

// Uint16 returns the payload as a host-endian u16.
func (a Attr) Uint16() (uint16, bool) {
	if len(a.Data) != 2 {
		return 0, false
	}
	return binary.NativeEndian.Uint16(a.Data), true
}

// Uint32 returns the payload as a host-endian u32.
func (a Attr) Uint32() (uint32, bool) {
	if len(a.Data) != 4 {
		return 0, false
	}
	return binary.NativeEndian.Uint32(a.Data), true
}

// Uint64 returns the payload as a host-endian u64.
func (a Attr) Uint64() (uint64, bool) {
	if len(a.Data) != 8 {
		return 0, false
	}
	return binary.NativeEndian.Uint64(a.Data), true
}

// String returns the payload as a string with any trailing NUL bytes removed.
func (a Attr) String() string {
	d := a.Data
	for len(d) > 0 && d[len(d)-1] == 0 {
		d = d[:len(d)-1]
	}
	return string(d)
}

// Builder assembles a buffer of netlink attributes.
type Builder struct{ buf []byte }

func (b *Builder) put(t uint16, data []byte) {
	var hdr [attrHeaderLen]byte
	binary.NativeEndian.PutUint16(hdr[0:2], uint16(attrHeaderLen+len(data)))
	binary.NativeEndian.PutUint16(hdr[2:4], t)
	b.buf = append(b.buf, hdr[:]...)
	b.buf = append(b.buf, data...)
	for len(b.buf)%attrAlign != 0 {
		b.buf = append(b.buf, 0)
	}
}

// Bytes appends an attribute with a raw payload (which may be empty).
func (b *Builder) Bytes(t uint16, data []byte) { b.put(t, data) }

// Uint16 appends a host-endian u16 attribute.
func (b *Builder) Uint16(t uint16, v uint16) {
	var d [2]byte
	binary.NativeEndian.PutUint16(d[:], v)
	b.put(t, d[:])
}

// Uint32 appends a host-endian u32 attribute.
func (b *Builder) Uint32(t uint16, v uint32) {
	var d [4]byte
	binary.NativeEndian.PutUint32(d[:], v)
	b.put(t, d[:])
}

// String appends a NUL-terminated string attribute.
func (b *Builder) String(t uint16, s string) { b.put(t, append([]byte(s), 0)) }

// Nested appends a nested attribute whose payload is built by fn.
func (b *Builder) Nested(t uint16, fn func(*Builder)) {
	var inner Builder
	fn(&inner)
	b.put(t|flagNested, inner.buf)
}

// Data returns the encoded attributes.
func (b *Builder) Data() []byte { return b.buf }
