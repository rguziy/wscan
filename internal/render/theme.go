package render

import "github.com/rguziy/wscan/internal/wifi"

// Palette. Colours are chosen to stay readable on both dark and light
// terminals; neutral text uses the terminal default.
var (
	colDim     = fg(0x80, 0x88, 0x98)
	colFaint   = fg(0x4a, 0x50, 0x5c)
	colWarn    = fg(0xf5, 0xa6, 0x23)
	colGood    = fg(0x3d, 0xd6, 0x8c)
	colLime    = fg(0x9b, 0xe1, 0x5d)
	colYellow  = fg(0xf5, 0xd9, 0x0a)
	colOrange  = fg(0xff, 0x9f, 0x43)
	colRed     = fg(0xff, 0x5c, 0x5c)
	colCyan    = fg(0x4f, 0xc3, 0xf7)
	colBlue    = fg(0x7a, 0xa2, 0xf7)
	colViolet  = fg(0xc7, 0x92, 0xea)
	colPink    = fg(0xff, 0x6a, 0xc1)
	colGreen4  = fg(0x9e, 0xce, 0x6a)
	bandColors = map[wifi.Band]Style{
		wifi.Band2G: fg(0xf5, 0xa6, 0x23), // amber
		wifi.Band5G: fg(0x4f, 0xc3, 0xf7), // sky blue
		wifi.Band6G: fg(0xc7, 0x92, 0xea), // violet
	}
)

// signalLevel buckets a signal strength: 0 excellent .. 4 poor.
func signalLevel(dbm int) int {
	switch {
	case dbm >= -50:
		return 0
	case dbm >= -60:
		return 1
	case dbm >= -70:
		return 2
	case dbm >= -80:
		return 3
	}
	return 4
}

var signalStyles = [...]Style{colGood, colLime, colYellow, colOrange, colRed}

var signalNames = [...]string{"excellent", "good", "fair", "weak", "poor"}

func signalStyle(dbm int) Style { return signalStyles[signalLevel(dbm)] }

func generationStyle(gen int) Style {
	switch gen {
	case 7:
		return colPink
	case 6:
		return colCyan
	case 5:
		return colBlue
	case 4:
		return colGreen4
	}
	return colDim
}

func securityStyle(proto string) Style {
	switch proto {
	case "Open", "WEP":
		return colRed
	case "WPA":
		return colOrange
	case "WPA/WPA2", "WPA2", "WPA2-Enterprise", "WPA-Enterprise":
		return colYellow
	case "WPA2/WPA3":
		return colLime
	}
	return colGood // WPA3, WPA3-Enterprise, OWE
}

// congestionStyle colours the number of access points sharing a channel.
func congestionStyle(n int) Style {
	switch {
	case n >= 4:
		return colRed
	case n >= 2:
		return colYellow
	}
	return colGood
}

// Styles exported for the command's own status messages.
var (
	// ErrorStyle is used for error messages.
	ErrorStyle = colRed
	// DimStyle is used for secondary text.
	DimStyle = colDim
)
