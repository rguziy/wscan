package wifi

import "time"

// Demo returns a made-up scan result that exercises every column and colour
// of the output. It is what "wscan --demo" shows, so the tool can be tried
// (and screenshots taken) on a machine without Wi-Fi hardware. All BSSIDs are
// in the IANA documentation range 00:00:5E:00:53:xx.
func Demo() *Result {
	day := int64(24 * 3600)
	mk := func(id byte, ssid string, freq, dbm, gen, width, nss int, sec Security, up int64) AP {
		ap := AP{
			SSID:             ssid,
			Hidden:           ssid == "",
			BSSID:            formatMAC([]byte{0x00, 0x00, 0x5e, 0x00, 0x53, id}),
			SignalDBM:        dbm,
			Band:             BandOf(freq),
			FrequencyMHz:     freq,
			Channel:          ChannelOf(freq),
			WidthMHz:         width,
			Generation:       gen,
			Streams:          nss,
			BeaconIntervalMS: 102.4,
			UptimeSeconds:    up,
			Country:          "DE",
			Security:         sec,
			SeenAgoMS:        int64(300 + int(id)*37),
		}
		switch gen {
		case 4:
			ap.Standard = "n"
			ap.MinRateMbps = phyRate(4, width, 1, 0, true)
			ap.MaxRateMbps = phyRate(4, width, nss, 7, true)
		case 5:
			ap.Standard = "ac"
			ap.MinRateMbps = phyRate(5, width, 1, 0, true)
			ap.MaxRateMbps = phyRate(5, width, nss, 9, true)
		case 6:
			ap.Standard = "ax"
			ap.MinRateMbps = phyRate(6, width, 1, 0, false)
			ap.MaxRateMbps = phyRate(6, width, nss, 11, false)
		case 7:
			ap.Standard = "be"
			ap.MinRateMbps = phyRate(7, width, 1, 0, false)
			ap.MaxRateMbps = phyRate(7, width, nss, 13, false)
		default:
			ap.Standard, ap.Streams = "g", 0
			ap.MinRateMbps, ap.MaxRateMbps = 1, 54
		}
		ap.MinRateMbps, ap.MaxRateMbps = round1(ap.MinRateMbps), round1(ap.MaxRateMbps)
		return ap
	}

	wpa23 := Security{Protocol: "WPA2/WPA3", Ciphers: []string{"CCMP"}, AKMs: []string{"PSK", "SAE"}, PMF: "capable"}
	wpa3 := Security{Protocol: "WPA3", Ciphers: []string{"CCMP"}, AKMs: []string{"SAE"}, PMF: "required"}
	wpa2 := Security{Protocol: "WPA2", Ciphers: []string{"CCMP"}, AKMs: []string{"PSK"}}
	ent := Security{Protocol: "WPA2-Enterprise", Enterprise: true, Ciphers: []string{"CCMP"}, AKMs: []string{"802.1X"}, PMF: "capable"}
	owe := Security{Protocol: "OWE", Ciphers: []string{"CCMP"}, AKMs: []string{"OWE"}, PMF: "required"}
	open := Security{Protocol: "Open"}
	wep := Security{Protocol: "WEP"}

	aps := []AP{
		mk(1, "Lakeside", 2412, -48, 6, 20, 2, wpa23, 3*day+4*3600),
		mk(2, "Lakeside-IoT", 2412, -49, 6, 20, 2, wpa2, 3*day+4*3600),
		mk(3, "CoffeeHouse Guest", 2437, -63, 4, 20, 2, open, 12*3600+25*60),
		mk(4, "Neighbor-7F", 2462, -71, 4, 40, 2, wpa2, 41*day+9*3600),
		mk(5, "", 2422, -77, 4, 20, 2, wpa2, 9*day),
		mk(6, "DIRECT-Printer", 2437, -82, 4, 20, 1, wpa2, 2*3600+10*60),
		mk(7, "OldRouter", 2462, -86, 0, 20, 0, wep, 200*day),

		mk(8, "Lakeside", 5180, -55, 6, 80, 2, wpa23, 3*day+4*3600),
		mk(9, "Lakeside", 5745, -61, 6, 80, 2, wpa23, 3*day+4*3600),
		mk(10, "Office-AC", 5220, -67, 5, 80, 3, ent, 88*day),
		mk(11, "Guest-WiFi", 5500, -72, 5, 40, 2, wpa2, 15*day),
		mk(12, "AirFiber-5G", 5260, -74, 6, 160, 2, wpa3, 7*day),

		mk(13, "Lakeside-6G", 5975, -58, 6, 160, 2, wpa3, 3*day+4*3600),
		mk(14, "Studio-7", 6135, -69, 7, 320, 2, wpa3, 19*day),
		mk(15, "Cafe-Enhanced-Open", 6295, -76, 6, 80, 2, owe, 5*day),
	}

	aps[0].Load = &BSSLoad{Stations: 12, Utilization: 18}
	aps[0].BSSColor = 12
	aps[0].Roaming = []string{"k", "v", "r"}
	aps[7].Load = &BSSLoad{Stations: 5, Utilization: 7}
	aps[7].BSSColor = 12
	aps[7].Roaming = []string{"k", "v", "r"}
	aps[7].Connected = true
	aps[7].Security.WPS = true
	aps[12].BSSColor = 12
	aps[12].Roaming = []string{"k", "v"}
	aps[9].Load = &BSSLoad{Stations: 41, Utilization: 63}
	aps[6].Country = ""
	aps[10].Country = "NL"

	return &Result{Interface: "wlan0", Time: time.Now(), Fresh: true, APs: aps}
}
