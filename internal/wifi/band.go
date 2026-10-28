package wifi

// BandOf returns the band a centre frequency (MHz) belongs to.
func BandOf(freqMHz int) Band {
	switch {
	case freqMHz >= 2400 && freqMHz <= 2500:
		return Band2G
	case freqMHz >= 4900 && freqMHz < 5925:
		return Band5G
	case freqMHz >= 5925 && freqMHz <= 7125:
		return Band6G
	}
	return BandUnknown
}

// ChannelOf converts a centre frequency (MHz) to its channel number, or 0 when
// the frequency is outside the known bands.
func ChannelOf(freqMHz int) int {
	switch BandOf(freqMHz) {
	case Band2G:
		if freqMHz == 2484 {
			return 14
		}
		return (freqMHz - 2407) / 5
	case Band5G:
		return (freqMHz - 5000) / 5
	case Band6G:
		if freqMHz == 5935 {
			return 2
		}
		return (freqMHz - 5950) / 5
	}
	return 0
}
