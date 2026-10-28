//go:build windows

package wifi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// The scanner uses the Native Wifi API in wlanapi.dll through plain
// syscalls, so no CGO and no third-party packages are needed.

const (
	defaultScanTimeout = 15 * time.Second

	wlanClientVersion2 = 2
	bssTypeAny         = 3 // dot11_BSS_type_any

	wlanNotificationSourceACM = 0x00000008
	acmScanComplete           = 7
	acmScanFail               = 8

	wlanIntfOpcodeCurrentConnection = 7

	errAccessDenied     = 5
	errServiceNotActive = 1062
	errInvalidState     = 4318
)

var (
	wlanapi             = syscall.NewLazyDLL("wlanapi.dll")
	procOpenHandle      = wlanapi.NewProc("WlanOpenHandle")
	procCloseHandle     = wlanapi.NewProc("WlanCloseHandle")
	procEnumInterfaces  = wlanapi.NewProc("WlanEnumInterfaces")
	procScan            = wlanapi.NewProc("WlanScan")
	procGetBSSList      = wlanapi.NewProc("WlanGetNetworkBssList")
	procFreeMemory      = wlanapi.NewProc("WlanFreeMemory")
	procQueryInterface  = wlanapi.NewProc("WlanQueryInterface")
	procRegisterNotifyF = wlanapi.NewProc("WlanRegisterNotification")
)

// client is an open WLAN API handle.
type client struct{ handle uintptr }

func openClient() (*client, error) {
	if err := wlanapi.Load(); err != nil {
		return nil, errors.New("wlanapi.dll is not available (on Windows Server install the \"Wireless LAN Service\" feature)")
	}
	var negotiated uint32
	var h uintptr
	r, _, _ := procOpenHandle.Call(wlanClientVersion2, 0,
		uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&h)))
	switch r {
	case 0:
		return &client{handle: h}, nil
	case errServiceNotActive:
		return nil, errors.New("the WLAN AutoConfig service (wlansvc) is not running: there is no Wi-Fi adapter or the service is stopped")
	}
	return nil, fmt.Errorf("open WLAN handle: %w", syscall.Errno(r))
}

func (c *client) close() { procCloseHandle.Call(c.handle, 0) }

// copyAndFree copies size bytes of a WLAN API allocation and frees it.
func copyAndFree(p unsafe.Pointer, size int) []byte {
	out := append([]byte(nil), unsafe.Slice((*byte)(p), size)...)
	procFreeMemory.Call(uintptr(p))
	return out
}

func (c *client) interfaces() ([]winInterface, error) {
	var list unsafe.Pointer
	r, _, _ := procEnumInterfaces.Call(c.handle, 0, uintptr(unsafe.Pointer(&list)))
	if r != 0 {
		return nil, fmt.Errorf("list Wi-Fi interfaces: %w", syscall.Errno(r))
	}
	if list == nil {
		return nil, nil
	}
	// The list is variable sized: the header holds the entry count.
	n := int(*(*uint32)(list))
	size := wlanIfaceListHeader + n*wlanIfaceSize
	return parseInterfaceList(copyAndFree(list, size)), nil
}

// Interfaces lists the wireless interfaces. Windows does not expose the
// interface name here, so the adapter description is used.
func Interfaces() ([]Interface, error) {
	c, err := openClient()
	if err != nil {
		return nil, err
	}
	defer c.close()
	wi, err := c.interfaces()
	if err != nil {
		return nil, err
	}
	return toInterfaces(wi), nil
}

func toInterfaces(wi []winInterface) []Interface {
	out := make([]Interface, len(wi))
	for i, w := range wi {
		out[i] = Interface{Index: i + 1, Name: w.Desc, Type: "station"}
	}
	return out
}

// Scan scans for access points. When opt.Fresh is set it asks Windows for a
// new scan and waits for it to finish; otherwise Windows' own cached scan
// table is returned.
func Scan(ctx context.Context, opt ScanOptions) (*Result, error) {
	c, err := openClient()
	if err != nil {
		return nil, err
	}
	defer c.close()

	wi, err := c.interfaces()
	if err != nil {
		return nil, err
	}
	iface, err := pickInterface(toInterfaces(wi), opt.Interface)
	if err != nil {
		return nil, err
	}
	dev := wi[iface.Index-1]

	res := &Result{Interface: iface.Name, Time: time.Now()}
	if opt.Fresh {
		timeout := opt.Timeout
		if timeout <= 0 {
			timeout = defaultScanTimeout
		}
		fresh, note, err := c.triggerScan(ctx, dev.GUID, timeout)
		if err != nil {
			return nil, err
		}
		res.Fresh = fresh
		if note != "" {
			res.Notes = append(res.Notes, note)
		}
	}

	raw, err := c.bssList(dev.GUID)
	if err != nil {
		return nil, err
	}
	connected := c.connectedBSSID(dev.GUID)
	for _, r := range raw {
		if connected != nil && bytes.Equal(r.BSSID, connected) {
			r.Status, r.HasStatus = bssStatusAssociated, true
		}
		res.APs = append(res.APs, decode(r))
	}
	if len(res.APs) == 0 {
		res.Notes = append(res.Notes, "Windows returned no networks; on Windows 11 24H2 and later Wi-Fi scanning needs the location permission (Settings > Privacy & security > Location)")
	}
	res.Time = time.Now()
	return res, nil
}

func (c *client) bssList(guid [16]byte) ([]rawBSS, error) {
	var list unsafe.Pointer
	r, _, _ := procGetBSSList.Call(c.handle, uintptr(unsafe.Pointer(&guid[0])),
		0, bssTypeAny, 0, 0, uintptr(unsafe.Pointer(&list)))
	if r != 0 {
		return nil, fmt.Errorf("read scan results: %w", syscall.Errno(r))
	}
	if list == nil {
		return nil, nil
	}
	total := int(*(*uint32)(list))
	if total < wlanBSSListHeader {
		procFreeMemory.Call(uintptr(list))
		return nil, nil
	}
	return parseBSSList(copyAndFree(list, total)), nil
}

// connectedBSSID returns the BSSID of the current connection, or nil.
func (c *client) connectedBSSID(guid [16]byte) []byte {
	var size uint32
	var data unsafe.Pointer
	var valueType uint32
	r, _, _ := procQueryInterface.Call(c.handle, uintptr(unsafe.Pointer(&guid[0])),
		wlanIntfOpcodeCurrentConnection, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&valueType)))
	if r != 0 || data == nil {
		return nil // not connected (errInvalidState) or not available
	}
	return parseConnectedBSSID(copyAndFree(data, int(size)))
}

// Scan completion arrives as a notification on a thread owned by Windows.
// One callback is shared by the whole process and forwards events here.
type scanEvent struct {
	guid [16]byte
	code uint32
}

var (
	notifyOnce sync.Once
	notifyCB   uintptr
	scanEvents = make(chan scanEvent, 16)
)

// wlanNotification is the leading part of WLAN_NOTIFICATION_DATA.
type wlanNotification struct {
	source uint32
	code   uint32
	guid   [16]byte
}

func notifyCallback(n *wlanNotification, _ uintptr) uintptr {
	if n != nil && n.source == wlanNotificationSourceACM &&
		(n.code == acmScanComplete || n.code == acmScanFail) {
		select {
		case scanEvents <- scanEvent{guid: n.guid, code: n.code}:
		default: // nobody is waiting; drop the event
		}
	}
	return 0
}

// triggerScan starts a scan and waits for Windows to announce the results.
// Problems that merely mean "no fresh data" are reported through note.
func (c *client) triggerScan(ctx context.Context, guid [16]byte, timeout time.Duration) (fresh bool, note string, err error) {
	notifyOnce.Do(func() { notifyCB = syscall.NewCallback(notifyCallback) })
	var prev uint32
	r, _, _ := procRegisterNotifyF.Call(c.handle, wlanNotificationSourceACM, 0, notifyCB, 0, 0,
		uintptr(unsafe.Pointer(&prev)))
	listening := r == 0

	// Drop events left over from an earlier scan.
	for drained := false; !drained; {
		select {
		case <-scanEvents:
		default:
			drained = true
		}
	}

	r, _, _ = procScan.Call(c.handle, uintptr(unsafe.Pointer(&guid[0])), 0, 0, 0)
	if r != 0 {
		if r == errAccessDenied {
			return false, "Windows denied the scan (check the location permission); showing cached results", nil
		}
		return false, fmt.Sprintf("could not start a scan (%v); showing cached results", syscall.Errno(r)), nil
	}

	if !listening {
		// Without notifications, give a typical scan time to finish.
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(5 * time.Second):
			return true, "", nil
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-timer.C:
			return false, "timed out waiting for the scan to finish; showing cached results", nil
		case ev := <-scanEvents:
			if ev.guid != guid {
				continue
			}
			if ev.code == acmScanFail {
				return false, "the scan failed; showing cached results", nil
			}
			return true, "", nil
		}
	}
}
