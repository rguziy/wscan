//go:build linux

package wifi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"syscall"
	"time"

	"github.com/rguziy/wscan/internal/genl"
)

const defaultScanTimeout = 15 * time.Second

// dialNL80211 opens a netlink socket and resolves the nl80211 family.
func dialNL80211() (*genl.Conn, genl.Family, error) {
	c, err := genl.Dial()
	if err != nil {
		return nil, genl.Family{}, err
	}
	fam, err := c.Family(nl80211Family)
	if err != nil {
		_ = c.Close()
		if errors.Is(err, syscall.ENOENT) {
			return nil, genl.Family{}, errors.New("nl80211 is not available: the kernel has no Wi-Fi (cfg80211) support or no wireless driver is loaded")
		}
		return nil, genl.Family{}, err
	}
	return c, fam, nil
}

// Interfaces lists the wireless interfaces.
func Interfaces() ([]Interface, error) {
	c, fam, err := dialNL80211()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return listInterfaces(c, fam)
}

func listInterfaces(c *genl.Conn, fam genl.Family) ([]Interface, error) {
	msgs, err := c.Execute(fam.ID, genl.Message{Command: cmdGetInterface, Version: 1}, genl.FlagDump)
	if err != nil {
		return nil, fmt.Errorf("list Wi-Fi interfaces: %w", err)
	}
	var out []Interface
	for _, m := range msgs {
		if iface, ok := parseInterface(m); ok {
			out = append(out, iface)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

// Scan scans for access points. It reads the kernel's scan results and, when
// opt.Fresh is set and permissions allow, triggers a new scan first.
func Scan(ctx context.Context, opt ScanOptions) (*Result, error) {
	c, fam, err := dialNL80211()
	if err != nil {
		return nil, err
	}
	defer c.Close()

	ifaces, err := listInterfaces(c, fam)
	if err != nil {
		return nil, err
	}
	iface, err := pickInterface(ifaces, opt.Interface)
	if err != nil {
		return nil, err
	}

	res := &Result{Interface: iface.Name, Time: time.Now()}
	if opt.Fresh {
		timeout := opt.Timeout
		if timeout <= 0 {
			timeout = defaultScanTimeout
		}
		fresh, note, err := triggerScan(ctx, c, fam, iface.Index, timeout)
		if err != nil {
			return nil, err
		}
		res.Fresh = fresh
		if note != "" {
			res.Notes = append(res.Notes, note)
		}
	}

	// The scan table can change while it is being dumped; retry then.
	var msgs []genl.Message
	for attempt := 0; attempt < 3; attempt++ {
		msgs, err = c.Execute(fam.ID, scanRequest(cmdGetScan, iface.Index), genl.FlagDump)
		if !errors.Is(err, genl.ErrDumpInterrupted) {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read scan results: %w", err)
	}
	res.APs = parseScanMessages(msgs)
	if len(res.APs) == 0 && !res.Fresh {
		res.Notes = append(res.Notes, "the kernel's scan cache is empty (entries expire after about 30 s); run with sudo or grant CAP_NET_ADMIN (sudo setcap cap_net_admin+ep wscan) for a fresh scan")
	}
	res.Time = time.Now()
	return res, nil
}

func scanRequest(cmd byte, ifindex int) genl.Message {
	var b genl.Builder
	b.Uint32(attrIfIndex, uint32(ifindex))
	return genl.Message{Command: cmd, Version: 1, Data: b.Data()}
}

// triggerScan starts an active scan and waits for the kernel to announce the
// results. Problems that merely mean "no fresh data" (no permission, device
// busy) are reported through note, not as errors.
func triggerScan(ctx context.Context, c *genl.Conn, fam genl.Family, ifindex int, timeout time.Duration) (fresh bool, note string, err error) {
	// Subscribe before triggering so the completion event cannot be missed.
	events, err := genl.Dial()
	if err != nil {
		return false, "", err
	}
	defer events.Close()
	if id, ok := fam.Groups["scan"]; ok {
		if err := events.JoinGroup(id); err != nil {
			return false, "could not listen for scan events; showing cached results", nil
		}
	} else {
		return false, "kernel offers no scan event group; showing cached results", nil
	}
	if err := events.SetReadTimeout(250 * time.Millisecond); err != nil {
		return false, "", err
	}

	var b genl.Builder
	b.Uint32(attrIfIndex, uint32(ifindex))
	// An empty SSID in the list requests an active scan with a wildcard
	// probe request, which also finds hidden networks.
	b.Nested(attrScanSSIDs, func(n *genl.Builder) { n.Bytes(1, nil) })
	req := genl.Message{Command: cmdTriggerScan, Version: 1, Data: b.Data()}

	if _, err := c.Execute(fam.ID, req, genl.FlagAck); err != nil {
		switch {
		case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
			return false, "a fresh scan needs root or CAP_NET_ADMIN; showing cached results", nil
		case errors.Is(err, syscall.EBUSY):
			// A scan is already running (NetworkManager, wpa_supplicant);
			// wait for it and use its results.
		case errors.Is(err, syscall.ENETDOWN), errors.Is(err, syscall.ERFKILL):
			return false, "the Wi-Fi interface is down or blocked; showing cached results", nil
		default:
			return false, fmt.Sprintf("could not start a scan (%v); showing cached results", err), nil
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}
		msgs, err := events.Receive()
		if errors.Is(err, genl.ErrTimeout) {
			continue
		}
		if err != nil {
			return false, "", err
		}
		for _, m := range msgs {
			if m.Command != cmdNewScanResults && m.Command != cmdScanAborted {
				continue
			}
			if !eventForInterface(m, ifindex) {
				continue
			}
			if m.Command == cmdScanAborted {
				return false, "the scan was aborted; showing cached results", nil
			}
			return true, "", nil
		}
	}
	return false, "timed out waiting for the scan to finish; showing cached results", nil
}

func eventForInterface(m genl.Message, ifindex int) bool {
	attrs, _ := genl.ParseAttrs(m.Data)
	for _, a := range attrs {
		if a.Type == attrIfIndex {
			v, ok := a.Uint32()
			return ok && int(v) == ifindex
		}
	}
	return false
}
