//go:build linux

package genl

import (
	"fmt"
	"syscall"
	"time"
)

const (
	solNetlink           = 270 // SOL_NETLINK
	netlinkAddMembership = 1   // NETLINK_ADD_MEMBERSHIP

	defaultTimeout = 10 * time.Second
	recvBufferSize = 1 << 16
)

// Conn is a generic netlink socket. It is not safe for concurrent use.
type Conn struct {
	fd  int
	seq uint32
	buf []byte
}

// Dial opens a generic netlink socket.
func Dial() (*Conn, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_GENERIC)
	if err != nil {
		return nil, fmt.Errorf("genl: socket: %w", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("genl: bind: %w", err)
	}
	c := &Conn{fd: fd, buf: make([]byte, recvBufferSize)}
	if err := c.SetReadTimeout(defaultTimeout); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return c, nil
}

// Close releases the socket.
func (c *Conn) Close() error { return syscall.Close(c.fd) }

// SetReadTimeout bounds every blocking receive. Zero disables the timeout.
func (c *Conn) SetReadTimeout(d time.Duration) error {
	tv := syscall.NsecToTimeval(d.Nanoseconds())
	if err := syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("genl: set receive timeout: %w", err)
	}
	return nil
}

// JoinGroup subscribes the socket to a multicast group (see Family.Groups).
func (c *Conn) JoinGroup(id uint32) error {
	if err := syscall.SetsockoptInt(c.fd, solNetlink, netlinkAddMembership, int(id)); err != nil {
		return fmt.Errorf("genl: join multicast group: %w", err)
	}
	return nil
}

// recv reads one datagram and splits it into messages.
func (c *Conn) recv() ([]rawMsg, error) {
	for {
		n, _, err := syscall.Recvfrom(c.fd, c.buf, 0)
		switch err {
		case nil:
			return parseDatagram(c.buf[:n])
		case syscall.EINTR:
			continue
		case syscall.EAGAIN:
			return nil, ErrTimeout
		default:
			return nil, fmt.Errorf("genl: receive: %w", err)
		}
	}
}

// Receive reads one datagram and returns the generic netlink messages in it.
// It is meant for multicast event sockets. ErrTimeout is returned when the
// read timeout expires.
func (c *Conn) Receive() ([]Message, error) {
	raws, err := c.recv()
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, r := range raws {
		if r.typ == msgError || r.typ == msgDone {
			continue
		}
		if m, err := r.toMessage(); err == nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// Execute sends a request to the given family and collects the reply. Pass
// FlagDump to dump a table, FlagAck to wait for an acknowledgement. Kernel
// errors are returned as syscall.Errno values (wrapped).
func (c *Conn) Execute(family uint16, m Message, flags uint16) ([]Message, error) {
	c.seq++
	seq := c.seq
	req := marshal(family, flags, seq, m)
	if err := syscall.Sendto(c.fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, fmt.Errorf("genl: send: %w", err)
	}

	multipart := flags&FlagDump == FlagDump
	var (
		out         []Message
		interrupted bool
	)
	for {
		raws, err := c.recv()
		if err != nil {
			return nil, err
		}
		for _, r := range raws {
			if r.seq != seq {
				continue // stray multicast event or a reply to something else
			}
			if r.flags&flagDumpIntr != 0 {
				interrupted = true
			}
			switch r.typ {
			case msgError:
				code, err := r.errno()
				if err != nil {
					return nil, err
				}
				if code == 0 {
					return out, nil // ACK
				}
				return nil, fmt.Errorf("genl: kernel error: %w", syscall.Errno(-code))
			case msgDone:
				if interrupted {
					return out, ErrDumpInterrupted
				}
				return out, nil
			default:
				msg, err := r.toMessage()
				if err != nil {
					return nil, err
				}
				out = append(out, msg)
				if !multipart && flags&FlagAck == 0 {
					return out, nil // single reply, no terminator follows
				}
			}
		}
	}
}

// Family resolves a generic netlink family such as "nl80211". The error wraps
// syscall.ENOENT when the kernel does not provide the family.
func (c *Conn) Family(name string) (Family, error) {
	msgs, err := c.Execute(ctrlFamilyID, familyRequest(name), 0)
	if err != nil {
		return Family{}, err
	}
	if len(msgs) == 0 {
		return Family{}, fmt.Errorf("genl: no reply resolving family %q", name)
	}
	return parseFamily(msgs[0])
}
