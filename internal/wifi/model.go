// Package wifi scans for Wi-Fi access points and decodes what they advertise.
//
// Decoding of beacon information elements, PHY rate computation and security
// classification are platform independent; only the scanner itself is
// platform specific (scan_linux.go talks to the kernel through nl80211).
package wifi

import (
	"errors"
	"time"
)

// ErrUnsupported is returned on platforms without a scanner implementation.
var ErrUnsupported = errors.New("wifi scanning is not implemented on this platform yet")

// Band is a Wi-Fi frequency band.
type Band int

// Supported bands.
const (
	BandUnknown Band = iota
	Band2G
	Band5G
	Band6G
)

// Bands lists the known bands in display order.
func Bands() []Band { return []Band{Band2G, Band5G, Band6G} }

func (b Band) String() string {
	switch b {
	case Band2G:
		return "2.4 GHz"
	case Band5G:
		return "5 GHz"
	case Band6G:
		return "6 GHz"
	}
	return "unknown"
}

// MarshalText makes bands appear as "2.4 GHz" etc. in JSON.
func (b Band) MarshalText() ([]byte, error) { return []byte(b.String()), nil }

// BSSLoad is the QBSS Load element: how busy the access point is.
type BSSLoad struct {
	Stations    int `json:"stations"`
	Utilization int `json:"utilization_percent"`
}

// Security describes the authentication and encryption an AP advertises.
type Security struct {
	// Protocol is a short name: Open, WEP, WPA, WPA/WPA2, WPA2, WPA2/WPA3,
	// WPA3, OWE, WPA2-Enterprise, WPA3-Enterprise.
	Protocol   string   `json:"protocol"`
	Enterprise bool     `json:"enterprise,omitempty"`
	Ciphers    []string `json:"ciphers,omitempty"`
	AKMs       []string `json:"akm,omitempty"`
	PMF        string   `json:"pmf,omitempty"` // "capable" or "required"
	WPS        bool     `json:"wps,omitempty"`
}

// AP is one access point (one BSSID) seen in a scan.
type AP struct {
	SSID             string   `json:"ssid"`
	Hidden           bool     `json:"hidden"`
	BSSID            string   `json:"bssid"`
	SignalDBM        int      `json:"signal_dbm"`
	Band             Band     `json:"band"`
	FrequencyMHz     int      `json:"frequency_mhz"`
	Channel          int      `json:"channel"`
	WidthMHz         int      `json:"width_mhz"`
	Generation       int      `json:"wifi_generation"` // 4, 5, 6, 7; 0 for legacy
	Standard         string   `json:"standard"`        // a, b, g, n, ac, ax, be
	Streams          int      `json:"streams,omitempty"`
	MinRateMbps      float64  `json:"min_rate_mbps"`
	MaxRateMbps      float64  `json:"max_rate_mbps"`
	BeaconIntervalMS float64  `json:"beacon_interval_ms"`
	UptimeSeconds    int64    `json:"uptime_seconds,omitempty"`
	Country          string   `json:"country,omitempty"`
	Load             *BSSLoad `json:"bss_load,omitempty"`
	BSSColor         int      `json:"bss_color,omitempty"`
	Roaming          []string `json:"roaming,omitempty"` // any of "k", "v", "r"
	Security         Security `json:"security"`
	Connected        bool     `json:"connected,omitempty"`
	SeenAgoMS        int64    `json:"seen_ago_ms"` // -1 when the platform does not report it
}

// Uptime is the access point's uptime derived from its beacon timestamp, or
// zero when unknown.
func (a AP) Uptime() time.Duration { return time.Duration(a.UptimeSeconds) * time.Second }

// WiFiLabel returns the marketing generation: "4", "5", "6", "6E", "7", or ""
// for legacy 802.11a/b/g.
func (a AP) WiFiLabel() string {
	switch {
	case a.Generation == 6 && a.Band == Band6G:
		return "6E"
	case a.Generation >= 4:
		return string(rune('0' + a.Generation))
	}
	return ""
}

// BandSupport says whether an adapter can use a band.
type BandSupport string

// Band support values. The empty value means the platform cannot tell.
const (
	SupportUnknown  BandSupport = ""
	SupportNone     BandSupport = "none"     // the hardware has no such band
	SupportDisabled BandSupport = "disabled" // present, but off in the regulatory domain
	SupportOK       BandSupport = "ok"
)

// Interface is a wireless network interface.
type Interface struct {
	// Wiphy is the Linux wireless device the interface belongs to; zero on
	// other platforms.
	Wiphy int    `json:"-"`
	Index int    `json:"index"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	MAC   string `json:"mac,omitempty"`
}

// Result is the outcome of one scan.
type Result struct {
	Interface string
	Time      time.Time
	// Fresh is true when a new scan was triggered and completed for this
	// result; false means the kernel's cached results were read.
	Fresh bool
	// Notes holds non-fatal warnings to show to the user.
	Notes []string
	APs   []AP
	// Bands says which bands the adapter supports; nil when the platform
	// cannot tell (Windows).
	Bands map[Band]BandSupport
}

// ScanOptions controls a scan.
type ScanOptions struct {
	// Interface is the interface name; empty selects the first Wi-Fi station
	// interface.
	Interface string
	// Fresh asks for a new scan. This needs root or CAP_NET_ADMIN; without
	// it the cached results are returned together with a note.
	Fresh bool
	// Timeout bounds the wait for a fresh scan to finish.
	Timeout time.Duration
}
