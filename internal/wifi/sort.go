package wifi

import (
	"fmt"
	"sort"
	"strings"
)

// SortKey selects how access points are ordered within a band.
type SortKey string

// Sort keys.
const (
	SortSignal  SortKey = "signal"  // strongest first
	SortChannel SortKey = "channel" // lowest channel first
	SortSSID    SortKey = "ssid"    // alphabetical, hidden networks last
	SortBSSID   SortKey = "bssid"
)

// ParseSortKey validates a sort key given on the command line.
func ParseSortKey(s string) (SortKey, error) {
	switch k := SortKey(strings.ToLower(s)); k {
	case SortSignal, SortChannel, SortSSID, SortBSSID:
		return k, nil
	}
	return "", fmt.Errorf("unknown sort key %q (use signal, channel, ssid or bssid)", s)
}

// Sort orders aps in place.
func Sort(aps []AP, key SortKey) {
	bySignal := func(a, b *AP) bool {
		if a.SignalDBM != b.SignalDBM {
			return a.SignalDBM > b.SignalDBM
		}
		return a.BSSID < b.BSSID
	}
	sort.SliceStable(aps, func(i, j int) bool {
		a, b := &aps[i], &aps[j]
		switch key {
		case SortChannel:
			if a.Channel != b.Channel {
				return a.Channel < b.Channel
			}
		case SortSSID:
			if a.Hidden != b.Hidden {
				return !a.Hidden
			}
			if la, lb := strings.ToLower(a.SSID), strings.ToLower(b.SSID); la != lb {
				return la < lb
			}
		case SortBSSID:
			return a.BSSID < b.BSSID
		}
		return bySignal(a, b)
	})
}

// Select keeps the access points in the wanted bands (all when bands is
// empty) and sorts them.
func Select(aps []AP, bands map[Band]bool, key SortKey) []AP {
	out := make([]AP, 0, len(aps))
	for _, ap := range aps {
		if len(bands) == 0 || bands[ap.Band] {
			out = append(out, ap)
		}
	}
	Sort(out, key)
	return out
}

// GroupByBand splits aps by band, keeping their order.
func GroupByBand(aps []AP) map[Band][]AP {
	g := make(map[Band][]AP)
	for _, ap := range aps {
		g[ap.Band] = append(g[ap.Band], ap)
	}
	return g
}
