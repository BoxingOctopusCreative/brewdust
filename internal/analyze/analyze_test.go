// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package analyze

import (
	"strings"
	"testing"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
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

func TestLinkDependenciesFindsDirectEdges(t *testing.T) {
	now := time.Now()
	// wget → curl → openssl, and wget's receipt lists both (transitively).
	// A cask depends on wget only.
	openssl := &Package{Name: "openssl", Kind: KindFormula, InstalledAt: now}
	curl := &Package{Name: "curl", Kind: KindFormula, InstalledAt: now, deps: []string{"openssl"}}
	wget := &Package{Name: "wget", Kind: KindFormula, InstalledAt: now, OnRequest: true, deps: []string{"curl", "openssl"}}
	app := &Package{Name: "someapp", Kind: KindCask, InstalledAt: now, OnRequest: true, deps: []string{"wget"}}

	resolveDependencies([]*Package{openssl, curl, wget, app}, now)

	check := func(name string, got, want []string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("wget.Dependencies", wget.Dependencies, []string{"curl"})
	check("openssl.Dependents", openssl.Dependents, []string{"curl"})
	check("openssl.AllDependents", openssl.AllDependents, []string{"curl", "someapp", "wget"})
	check("wget.Dependents", wget.Dependents, []string{"someapp"})
	check("someapp.Dependencies", app.Dependencies, []string{"wget"})
	if openssl.Orphan || curl.Orphan {
		t.Error("needed formulae shouldn't be orphans")
	}
}

func TestCleanupSkipReason(t *testing.T) {
	var f brew.Formula
	if f.CleanupSkipReason() != "" {
		t.Error("current, unpinned formula should be cleanable")
	}
	f.Outdated, f.Revision = true, 1
	f.Versions.Stable = "15.0.0"
	if got := f.CleanupSkipReason(); !strings.Contains(got, "15.0.0_1") {
		t.Errorf("outdated reason = %q", got)
	}
	f.Pinned = true
	if f.CleanupSkipReason() != "pinned" {
		t.Error("pinned takes precedence")
	}
}
