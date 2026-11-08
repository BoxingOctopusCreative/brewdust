package app

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
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseSize("lots"); err == nil {
		t.Error("expected error for invalid size")
	}
}
