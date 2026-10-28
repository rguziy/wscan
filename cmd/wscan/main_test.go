package main

import (
	"testing"

	"github.com/rguziy/wscan/internal/wifi"
)

func TestParseBands(t *testing.T) {
	cases := []struct {
		in   string
		want []wifi.Band // nil means "all bands" (empty set)
		err  bool
	}{
		{"all", nil, false},
		{"", nil, false},
		{"2.4", []wifi.Band{wifi.Band2G}, false},
		{"5,6", []wifi.Band{wifi.Band5G, wifi.Band6G}, false},
		{" 2.4GHz , 5g ", []wifi.Band{wifi.Band2G, wifi.Band5G}, false},
		{"5,all", nil, false},
		{"7", nil, true},
		{"wifi", nil, true},
	}
	for _, c := range cases {
		got, err := parseBands(c.in)
		if (err != nil) != c.err {
			t.Errorf("parseBands(%q) error = %v, want error %v", c.in, err, c.err)
			continue
		}
		if err != nil {
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("parseBands(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for _, b := range c.want {
			if !got[b] {
				t.Errorf("parseBands(%q) is missing %v", c.in, b)
			}
		}
	}
}
