package app

import (
	"context"
	"strings"
	"testing"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/brewtest"
)

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

func pkgs() []*analyze.Package {
	return []*analyze.Package{
		{Name: "jq", Kind: analyze.KindFormula, Desc: "JSON processor", SizeBytes: 2_000, OnRequest: true, IdleDays: 5, DirectIdleDays: 5},
		{Name: "oniguruma", Kind: analyze.KindFormula, Desc: "Regex library", SizeBytes: 500, IdleDays: 5, DirectIdleDays: 200,
			Dependents: []string{"jq"}, AllDependents: []string{"jq", "tool"}},
		{Name: "leftover", Kind: analyze.KindFormula, SizeBytes: 300, IdleDays: 400, DirectIdleDays: 400, Orphan: true},
		{Name: "tool", Kind: analyze.KindCask, Desc: "A tool", SizeBytes: 9_000, OnRequest: true, IdleDays: 30, DirectIdleDays: 30,
			MissingApps: []string{"/Applications/Tool.app"}, AllDependents: []string{"x"}},
		{Name: "jq", Kind: analyze.KindCask, Desc: "A cask sharing a formula's name", SizeBytes: 1},
	}
}

func names(ps []*analyze.Package) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name+"/"+p.Kind[:1])
	}
	return strings.Join(out, " ")
}

func TestFilter(t *testing.T) {
	cases := []struct {
		name string
		f    Filter
		want string
	}{
		{"zero value matches all", Filter{}, "jq/f oniguruma/f leftover/f tool/c jq/c"},
		{"kind all", Filter{Kind: "all"}, "jq/f oniguruma/f leftover/f tool/c jq/c"},
		{"formulae", Filter{Kind: "formula"}, "jq/f oniguruma/f leftover/f"},
		{"casks", Filter{Kind: "cask"}, "tool/c jq/c"},
		{"requested", Filter{Requested: true}, "jq/f tool/c"},
		{"orphans", Filter{Orphans: true}, "leftover/f"},
		{"broken", Filter{Broken: true}, "tool/c"},
		{"needed", Filter{Needed: true}, "oniguruma/f"},
		{"stale", Filter{StaleDays: 30}, "leftover/f tool/c"},
		{"stale, direct", Filter{StaleDays: 30, Direct: true}, "oniguruma/f leftover/f tool/c"},
		{"min size", Filter{MinBytes: 1_000}, "jq/f tool/c"},
		{"query name", Filter{Query: "ONIG"}, "oniguruma/f"},
		{"query description", Filter{Query: "json"}, "jq/f"},
		{"combined", Filter{Kind: "formula", Query: "o", StaleDays: 100}, "leftover/f"},
		{"nothing", Filter{Orphans: true, Requested: true}, ""},
	}
	for _, c := range cases {
		if got := names(c.f.Apply(pkgs())); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSort(t *testing.T) {
	cases := []struct {
		by     string
		direct bool
		want   string
	}{
		{"size", false, "tool/c jq/f oniguruma/f leftover/f jq/c"},
		{"idle", false, "leftover/f tool/c jq/f oniguruma/f jq/c"}, // ties broken by size
		{"idle", true, "leftover/f oniguruma/f tool/c jq/f jq/c"},
		{"name", false, "jq/f jq/c leftover/f oniguruma/f tool/c"}, // stable
		{"deps", false, "oniguruma/f tool/c jq/f leftover/f jq/c"},
	}
	for _, c := range cases {
		ps := pkgs()
		if err := Sort(ps, c.by, Filter{Direct: c.direct}); err != nil {
			t.Fatal(err)
		}
		if got := names(ps); got != c.want {
			t.Errorf("sort %s (direct=%v): got %q, want %q", c.by, c.direct, got, c.want)
		}
	}
	if err := Sort(pkgs(), "age", Filter{}); err == nil || !strings.Contains(err.Error(), "size, idle, name, deps") {
		t.Errorf("unknown sort: err = %v", err)
	}
}

func TestFind(t *testing.T) {
	s := &Snapshot{Packages: pkgs()}
	if p := s.Find("jq", ""); p == nil || p.Kind != analyze.KindFormula {
		t.Errorf("jq should prefer the formula, got %+v", p)
	}
	if p := s.Find("jq", analyze.KindCask); p == nil || p.Kind != analyze.KindCask {
		t.Errorf("jq --cask got %+v", p)
	}
	if p := s.Find("tool", analyze.KindFormula); p != nil {
		t.Errorf("tool is not a formula, got %+v", p)
	}
	if s.Find("nope", "") != nil {
		t.Error("found a package that isn't installed")
	}
}

func TestLoad(t *testing.T) {
	fake := brewtest.Standard(t)
	fake.Install(t)
	snap, err := Load(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Prefix != fake.Prefix || len(snap.Packages) != 6 || snap.Now.IsZero() {
		t.Fatalf("snapshot prefix=%q packages=%d", snap.Prefix, len(snap.Packages))
	}
	if jq := snap.Find("jq", ""); jq == nil || jq.LastUsed == nil || jq.OldVersions != 1 {
		t.Errorf("jq = %+v", jq)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Load(context.Background(), 2); err == nil {
		t.Error("expected an error without brew")
	}
}
