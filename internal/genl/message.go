package genl

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Netlink header flags used by this package.
const (
	FlagRequest  = 0x01
	FlagMulti    = 0x02
	FlagAck      = 0x04
	FlagDump     = 0x300 // NLM_F_ROOT | NLM_F_MATCH
	flagDumpIntr = 0x10
)

const (
	msgError = 2 // NLMSG_ERROR
	msgDone  = 3 // NLMSG_DONE

	nlmsgHeaderLen = 16
	genlHeaderLen  = 4
)

// Errors reported by the connection.
var (
	// ErrTimeout is returned when a receive times out.
	ErrTimeout = errors.New("genl: receive timeout")
	// ErrDumpInterrupted is returned when the kernel changed its tables while
	// a dump was running; the caller should repeat the request.
	ErrDumpInterrupted = errors.New("genl: dump interrupted")
)

// Message is a generic netlink message: a command plus its attributes.
type Message struct {
	Command byte
	Version byte
	Data    []byte // encoded attributes
}

// rawMsg is one netlink message as read from the socket.
type rawMsg struct {
	typ     uint16
	flags   uint16
	seq     uint32
	payload []byte
}

// marshal encodes a generic netlink request.
func marshal(family, flags uint16, seq uint32, m Message) []byte {
	total := nlmsgHeaderLen + genlHeaderLen + len(m.Data)
	b := make([]byte, total)
	binary.NativeEndian.PutUint32(b[0:4], uint32(total))
	binary.NativeEndian.PutUint16(b[4:6], family)
	binary.NativeEndian.PutUint16(b[6:8], flags|FlagRequest)
	binary.NativeEndian.PutUint32(b[8:12], seq)
	// b[12:16]: port ID, 0 lets the kernel fill it in.
	b[16] = m.Command
	b[17] = m.Version
	copy(b[nlmsgHeaderLen+genlHeaderLen:], m.Data)
	return b
}

// parseDatagram splits one received datagram into netlink messages. Payloads
// are copied, so the input buffer may be reused.
func parseDatagram(b []byte) ([]rawMsg, error) {
	var out []rawMsg
	for len(b) >= nlmsgHeaderLen {
		n := int(binary.NativeEndian.Uint32(b[0:4]))
		if n < nlmsgHeaderLen || n > len(b) {
			return out, ErrShort
		}
		out = append(out, rawMsg{
			typ:     binary.NativeEndian.Uint16(b[4:6]),
			flags:   binary.NativeEndian.Uint16(b[6:8]),
			seq:     binary.NativeEndian.Uint32(b[8:12]),
			payload: append([]byte(nil), b[nlmsgHeaderLen:n]...),
		})
		adv := align(n)
		if adv > len(b) {
			adv = len(b)
		}
		b = b[adv:]
	}
	return out, nil
}

// toMessage interprets a data message payload as generic netlink.
func (r rawMsg) toMessage() (Message, error) {
	if len(r.payload) < genlHeaderLen {
		return Message{}, ErrShort
	}
	return Message{
		Command: r.payload[0],
		Version: r.payload[1],
		Data:    r.payload[genlHeaderLen:],
	}, nil
}

// errnoOf extracts the error code of an NLMSG_ERROR payload (0 is an ACK).
func (r rawMsg) errno() (int32, error) {
	if len(r.payload) < 4 {
		return 0, ErrShort
	}
	return int32(binary.NativeEndian.Uint32(r.payload[0:4])), nil
}

// Family describes a resolved generic netlink family.
type Family struct {
	ID     uint16
	Groups map[string]uint32 // multicast group name -> group ID
}

// Controller (GENL_ID_CTRL) constants.
const (
	ctrlFamilyID         = 0x10
	ctrlCmdGetFamily     = 3
	ctrlAttrFamilyID     = 1
	ctrlAttrFamilyName   = 2
	ctrlAttrMcastGroups  = 7
	ctrlAttrMcastGrpName = 1
	ctrlAttrMcastGrpID   = 2
)

// familyRequest builds the controller request that resolves a family name.
func familyRequest(name string) Message {
	var b Builder
	b.String(ctrlAttrFamilyName, name)
	return Message{Command: ctrlCmdGetFamily, Version: 1, Data: b.Data()}
}

// parseFamily decodes the controller's reply to familyRequest.
func parseFamily(m Message) (Family, error) {
	attrs, err := ParseAttrs(m.Data)
	if err != nil {
		return Family{}, err
	}
	fam := Family{Groups: make(map[string]uint32)}
	for _, a := range attrs {
		switch a.Type {
		case ctrlAttrFamilyID:
			id, ok := a.Uint16()
			if !ok {
				return Family{}, fmt.Errorf("genl: bad family ID attribute")
			}
			fam.ID = id
		case ctrlAttrMcastGroups:
			groups, _ := a.Nested()
			for _, g := range groups {
				fields, _ := g.Nested()
				var name string
				var id uint32
				for _, f := range fields {
					switch f.Type {
					case ctrlAttrMcastGrpName:
						name = f.String()
					case ctrlAttrMcastGrpID:
						id, _ = f.Uint32()
					}
				}
				if name != "" {
					fam.Groups[name] = id
				}
			}
		}
	}
	if fam.ID == 0 {
		return Family{}, fmt.Errorf("genl: family ID missing in reply")
	}
	return fam, nil
}
