// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
)

func TestBytes(t *testing.T) {
	cases := map[int64]string{
		0:             "0 B",
		999:           "999 B",
		1000:          "1.0 kB",
		1500:          "1.5 kB",
		13_500_000:    "13.5 MB",
		4_600_000_000: "4.6 GB",
		2e12:          "2.0 TB",
	}
	for n, want := range cases {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestIdle(t *testing.T) {
	cases := map[int]string{0: "today", 1: "1d", 59: "59d", 60: "2mo", 400: "13mo", 730: "2.0y", 1000: "2.7y"}
	for days, want := range cases {
		if got := Idle(days); got != want {
			t.Errorf("Idle(%d) = %q, want %q", days, got, want)
		}
	}
}

func TestLastUsed(t *testing.T) {
	if got := LastUsed(&analyze.Package{}); got != "not since install" {
		t.Errorf("got %q", got)
	}
	when := time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local)
	if got := LastUsed(&analyze.Package{LastUsed: &when}); got != "2026-03-04" {
		t.Errorf("got %q", got)
	}
}

func TestNeededBy(t *testing.T) {
	cases := []struct {
		deps []string
		want string
	}{
		{nil, "—"},
		{[]string{"rust"}, "rust"},
		{[]string{"qtgrpc", "a", "b"}, "qtgrpc +2"},
	}
	for _, c := range cases {
		if got := NeededBy(&analyze.Package{Dependents: c.deps}); got != c.want {
			t.Errorf("NeededBy(%v) = %q, want %q", c.deps, got, c.want)
		}
	}
}

func TestNotes(t *testing.T) {
	cases := []struct {
		name   string
		p      analyze.Package
		direct bool
		want   string
	}{
		{"requested", analyze.Package{Kind: analyze.KindFormula, OnRequest: true}, false, "requested"},
		{"orphan", analyze.Package{Kind: analyze.KindFormula, Orphan: true}, false, "orphan (brew autoremove)"},
		{"dependency", analyze.Package{Kind: analyze.KindFormula}, false, "dependency"},
		{"cask has no role", analyze.Package{Kind: analyze.KindCask}, false, ""},
		{"active via", analyze.Package{Kind: analyze.KindFormula, ActiveVia: "curl"}, false, "dependency; active via curl"},
		{"long chain shortened", analyze.Package{Kind: analyze.KindFormula, ActiveVia: "a → b → c → d"}, false,
			"dependency; active via a → … → d"},
		{"direct hides active via", analyze.Package{Kind: analyze.KindFormula, ActiveVia: "curl"}, true, "dependency"},
		{"old versions", analyze.Package{Kind: analyze.KindCask, OldVersions: 2, ReclaimableBytes: 4_600_000_000}, false,
			"+2 old version(s), 4.6 GB reclaimable"},
		{"held outdated", analyze.Package{Kind: analyze.KindCask, HeldVersions: 1, HeldBytes: 13_500_000, HeldReason: "newer version 2 not installed"}, false,
			"+1 old version(s), 13.5 MB kept by brew cleanup until upgraded"},
		{"held pinned", analyze.Package{Kind: analyze.KindCask, HeldVersions: 1, HeldBytes: 1000, HeldReason: "pinned"}, false,
			"+1 old version(s), 1.0 kB kept by brew cleanup while pinned"},
		{"broken", analyze.Package{Kind: analyze.KindCask, MissingApps: []string{"/Applications/X.app"}}, false, "app deleted outside brew"},
	}
	for _, c := range cases {
		if got := Notes(&c.p, c.direct); got != c.want {
			t.Errorf("%s: Notes = %q, want %q", c.name, got, c.want)
		}
	}
}

func fixture() []*analyze.Package {
	installed := time.Date(2025, 1, 2, 12, 0, 0, 0, time.Local)
	used := time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local)
	return []*analyze.Package{
		{Name: "jq", Kind: analyze.KindFormula, Version: "1.8", SizeBytes: 2_000_000, OnRequest: true,
			InstalledAt: installed, LastUsed: &used, IdleDays: 7, DirectIdleDays: 7,
			OldVersions: 1, ReclaimableBytes: 1_000_000},
		{Name: "oniguruma", Kind: analyze.KindFormula, SizeBytes: 500_000, InstalledAt: installed,
			Dependents: []string{"jq"}, AllDependents: []string{"jq"}, ActiveVia: "jq", IdleDays: 7, DirectIdleDays: 675},
		{Name: "leftover", Kind: analyze.KindFormula, SizeBytes: 300_000, InstalledAt: installed, Orphan: true},
		{Name: "harfbuzz", Kind: analyze.KindFormula, SizeBytes: 100_000, InstalledAt: installed, OnRequest: true,
			HeldVersions: 1, HeldBytes: 50_000},
		{Name: "tool", Kind: analyze.KindCask, SizeBytes: 9_000_000, InstalledAt: installed, OnRequest: true,
			MissingApps: []string{"/Applications/Tool.app"}},
	}
}

func TestTable(t *testing.T) {
	var buf bytes.Buffer
	pkgs := fixture()
	if err := Table(&buf, pkgs[:2], false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines:\n%s", len(lines), buf.String())
	}
	if f := strings.Fields(lines[0]); strings.Join(f, " ") != "NAME KIND SIZE INSTALLED LAST USED IDLE NEEDED BY NOTES" {
		t.Errorf("header = %q", lines[0])
	}
	for _, want := range []string{"jq", "formula", "2.0 MB", "2025-01-02", "2026-11-01", "7d", "requested"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("jq row lacks %q: %q", want, lines[1])
		}
	}
	if !strings.Contains(lines[2], "not since install") || !strings.Contains(lines[2], "active via jq") {
		t.Errorf("oniguruma row = %q", lines[2])
	}

	buf.Reset()
	if err := Table(&buf, pkgs[1:2], true); err != nil {
		t.Fatal(err)
	}
	if row := strings.Split(buf.String(), "\n")[1]; !strings.Contains(row, "22mo") || strings.Contains(row, "active via") {
		t.Errorf("direct row should use own idle time: %q", row)
	}
}

func TestSummary(t *testing.T) {
	var buf bytes.Buffer
	pkgs := fixture()
	Summary(&buf, pkgs[:1], pkgs)
	out := buf.String()
	for _, want := range []string{
		"1 of 5 packages shown, 2.0 MB of 11.9 MB total.",
		"Old versions: 1.0 MB reclaimable.",
		"Old versions brew cleanup won't remove yet: 50.0 kB in 1 outdated or pinned formula(e)",
		"Casks whose app was deleted outside brew: 1.",
		"Orphaned dependencies: 1 using 300.0 kB",
		"Run `brewdust clean` to review and remove these.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	// The clean hint comes after everything it refers to.
	if strings.Index(out, "brewdust clean") < strings.Index(out, "Orphaned") {
		t.Errorf("clean hint should come last:\n%s", out)
	}

	buf.Reset()
	Summary(&buf, pkgs[1:2], pkgs[1:2])
	if strings.Contains(buf.String(), "brewdust clean") || strings.Contains(buf.String(), "reclaimable") {
		t.Errorf("nothing to clean, but:\n%s", buf.String())
	}
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	if err := JSON(&buf, fixture(), now); err != nil {
		t.Fatal(err)
	}
	var got struct {
		GeneratedAt time.Time        `json:"generated_at"`
		Packages    []map[string]any `json:"packages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.GeneratedAt.Equal(now) || len(got.Packages) != 5 {
		t.Fatalf("generated_at=%v packages=%d", got.GeneratedAt, len(got.Packages))
	}
	jq := got.Packages[0]
	if jq["name"] != "jq" || jq["reclaimable_bytes"] != float64(1_000_000) || jq["last_used"] == nil {
		t.Errorf("jq = %v", jq)
	}
	if _, ok := got.Packages[2]["last_used"]; ok {
		t.Error("last_used should be omitted when unknown")
	}
	if got.Packages[3]["held_bytes"] != float64(50_000) {
		t.Errorf("harfbuzz = %v", got.Packages[3])
	}
}
