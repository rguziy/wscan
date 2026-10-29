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

func TestStringList(t *testing.T) {
	var l stringList
	for _, v := range []string{"Home, Home-IoT", "", "AA:BB:CC:DD:EE:FF"} {
		if err := l.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"Home", "Home-IoT", "AA:BB:CC:DD:EE:FF"}
	if len(l) != len(want) {
		t.Fatalf("got %q, want %q", l, want)
	}
	for i := range want {
		if l[i] != want[i] {
			t.Errorf("item %d = %q, want %q", i, l[i], want[i])
		}
	}
}

func TestValidateRecommend(t *testing.T) {
	ok := func(c config) config { c.scans, c.width = 3, 80; return c }
	cases := []struct {
		name string
		cfg  config
		bad  bool
	}{
		{"plain scan needs nothing", config{}, false},
		{"ignore without recommend", config{ignore: stringList{"x"}}, true},
		{"country without recommend", config{country: "UA"}, true},
		{"include-connected without recommend", config{includeConnected: true}, true},
		{"valid", ok(config{recommend: true, country: "UA"}), false},
		{"with watch", ok(config{recommend: true, watch: true}), true},
		{"too many scans", func() config { c := ok(config{recommend: true}); c.scans = 99; return c }(), true},
		{"zero scans", func() config { c := ok(config{recommend: true}); c.scans = 0; return c }(), true},
		{"bad width", func() config { c := ok(config{recommend: true}); c.width = 100; return c }(), true},
		{"bad country", ok(config{recommend: true, country: "UKR"}), true},
	}
	for _, c := range cases {
		cfg := c.cfg
		if err := validateRecommend(&cfg); (err != nil) != c.bad {
			t.Errorf("%s: error = %v, want error %v", c.name, err, c.bad)
		}
	}
}
