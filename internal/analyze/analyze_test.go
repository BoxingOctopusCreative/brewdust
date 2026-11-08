package analyze

import (
	"testing"
	"time"
)

func TestFormulaEligible(t *testing.T) {
	cases := map[string]bool{
		"1.0/bin/jq":                       true,
		"1.0/lib/libjq.dylib":              true,
		"1.0/include/jq.h":                 true,
		"1.0/INSTALL_RECEIPT.json":         false,
		"1.0/README.md":                    false,
		"1.0/.brew/jq.rb":                  false,
		"1.0/share/zsh/site-functions/_jq": false,
		"1.0/share/man/man1/jq.1":          false,
	}
	for rel, want := range cases {
		if got := formulaEligible(rel); got != want {
			t.Errorf("formulaEligible(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestCaskroomEligible(t *testing.T) {
	if !caskroomEligible("37.0/platform-tools/adb") {
		t.Error("CLI payload should count")
	}
	if caskroomEligible("6.29.0/calibre.app/Contents/MacOS/calibre") {
		t.Error("staged app bundle should not count")
	}
}

func TestResolveDependenciesPropagatesActivity(t *testing.T) {
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(-1, 0, 0)
	recent := now.AddDate(0, 0, -3)

	lib := &Package{Name: "openssl", Kind: KindFormula, InstalledAt: old}
	mid := &Package{Name: "curl", Kind: KindFormula, InstalledAt: old, deps: []string{"openssl"}}
	app := &Package{Name: "wget", Kind: KindFormula, InstalledAt: old, OnRequest: true, LastUsed: &recent, deps: []string{"curl"}}
	orphan := &Package{Name: "leftover", Kind: KindFormula, InstalledAt: old}

	resolveDependencies([]*Package{lib, mid, app, orphan}, now)

	if lib.IdleDays != 3 || lib.ActiveVia != "curl → wget" {
		t.Errorf("lib idle=%d via=%q, want 3 via curl → wget", lib.IdleDays, lib.ActiveVia)
	}
	if lib.DirectIdleDays != 365 {
		t.Errorf("lib direct idle = %d, want 365", lib.DirectIdleDays)
	}
	if lib.Orphan || !orphan.Orphan || app.Orphan {
		t.Errorf("orphan flags wrong: lib=%v orphan=%v app=%v", lib.Orphan, orphan.Orphan, app.Orphan)
	}
}
