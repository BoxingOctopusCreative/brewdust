package main

import "testing"

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"":      0,
		"512":   512,
		"50MB":  50e6,
		"1.5GB": 1.5e9,
		"10k":   10e3,
		"2GiB":  2e9, // treated as decimal for simplicity
	}
	for in, want := range cases {
		got, err := parseSize(in)
		if err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := parseSize("lots"); err == nil {
		t.Error("expected error for invalid size")
	}
}
