// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package analyze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/brewtest"
)

func run(t *testing.T) (*brewtest.Fake, map[string]*analyze.Package) {
	t.Helper()
	f := brewtest.Standard(t)
	pkgs := analyze.Run(&f.Info, analyze.Options{Prefix: f.Prefix, Now: f.Now, Workers: 2, Grace: 10 * time.Minute})
	byName := map[string]*analyze.Package{}
	for _, p := range pkgs {
		byName[p.Name] = p
	}
	if len(byName) != 6 {
		t.Fatalf("got %d packages, want 6", len(byName))
	}
	return f, byName
}

func TestRunFormulaSizesAndOldVersions(t *testing.T) {
	f, pkgs := run(t)
	jq := pkgs["jq"]
	if jq.Kind != analyze.KindFormula || jq.Version != "1.8" || !jq.OnRequest {
		t.Errorf("jq = %s %s requested=%v", jq.Kind, jq.Version, jq.OnRequest)
	}
	if jq.SizeBytes < 70000 {
		t.Errorf("jq size %d should include both versions (~71 kB of files)", jq.SizeBytes)
	}
	if jq.OldVersions != 1 || jq.ReclaimableBytes < 30000 || jq.ReclaimableBytes >= jq.SizeBytes {
		t.Errorf("jq old=%d reclaimable=%d size=%d", jq.OldVersions, jq.ReclaimableBytes, jq.SizeBytes)
	}
	if want := filepath.Join(f.Prefix, "Cellar/jq/1.7"); len(jq.OldPaths) != 1 || jq.OldPaths[0] != want {
		t.Errorf("jq OldPaths = %v, want [%s]", jq.OldPaths, want)
	}
	if jq.HeldVersions != 0 {
		t.Errorf("jq held = %d, want 0", jq.HeldVersions)
	}
	if !jq.InstalledAt.Equal(time.Unix(f.Days(200).Unix(), 0)) {
		t.Errorf("jq InstalledAt = %v, want the current version's install time", jq.InstalledAt)
	}
}

func TestRunHoldsOldVersionsOfOutdatedFormulae(t *testing.T) {
	f, pkgs := run(t)
	hb := pkgs["harfbuzz"]
	if hb.OldVersions != 0 || hb.ReclaimableBytes != 0 || len(hb.OldPaths) != 0 {
		t.Errorf("outdated formula counted as reclaimable: old=%d bytes=%d", hb.OldVersions, hb.ReclaimableBytes)
	}
	if hb.HeldVersions != 1 || hb.HeldBytes == 0 || hb.HeldPaths[0] != filepath.Join(f.Prefix, "Cellar/harfbuzz/1.0") {
		t.Errorf("held = %d, %d bytes, %v", hb.HeldVersions, hb.HeldBytes, hb.HeldPaths)
	}
	if !strings.Contains(hb.HeldReason, "3.0") {
		t.Errorf("HeldReason = %q, should name the version to upgrade to", hb.HeldReason)
	}
}

func TestRunLastUsedIgnoresDocsReceiptsAndInstallTime(t *testing.T) {
	f, pkgs := run(t)
	jq := pkgs["jq"]
	if jq.LastUsed == nil || !jq.LastUsed.Equal(f.Days(5)) {
		t.Fatalf("jq LastUsed = %v, want %v (bin/jq, not the newer man page or receipt)", jq.LastUsed, f.Days(5))
	}
	if jq.LastUsedEvidence != "atime 1.8/bin/jq" {
		t.Errorf("evidence = %q", jq.LastUsedEvidence)
	}
	if jq.DirectIdleDays != 5 || jq.IdleDays != 5 {
		t.Errorf("jq idle = %d / direct %d, want 5", jq.IdleDays, jq.DirectIdleDays)
	}
	for _, name := range []string{"oniguruma", "harfbuzz", "leftover", "tool", "viewer"} {
		if p := pkgs[name]; p.LastUsed != nil {
			t.Errorf("%s LastUsed = %v (%s), want none: only install-time accesses", name, p.LastUsed, p.LastUsedEvidence)
		}
	}
}

func TestRunDependencies(t *testing.T) {
	_, pkgs := run(t)
	onig, jq := pkgs["oniguruma"], pkgs["jq"]
	if strings.Join(jq.Dependencies, ",") != "oniguruma" || strings.Join(onig.Dependents, ",") != "jq" {
		t.Errorf("jq deps %v, oniguruma dependents %v", jq.Dependencies, onig.Dependents)
	}
	if onig.Orphan {
		t.Error("oniguruma is needed by jq")
	}
	if onig.ActiveVia != "jq" || onig.IdleDays != 5 || onig.DirectIdleDays != 200 {
		t.Errorf("oniguruma via=%q idle=%d direct=%d", onig.ActiveVia, onig.IdleDays, onig.DirectIdleDays)
	}
	if !pkgs["leftover"].Orphan {
		t.Error("leftover should be an orphan")
	}
	if pkgs["harfbuzz"].Orphan || pkgs["tool"].Orphan {
		t.Error("requested formulae and casks are never orphans")
	}
}

func TestRunCasks(t *testing.T) {
	f, pkgs := run(t)
	tool := pkgs["tool"]
	if tool.Kind != analyze.KindCask || tool.Version != "2.0" || !tool.OnRequest {
		t.Errorf("tool = %s %s requested=%v", tool.Kind, tool.Version, tool.OnRequest)
	}
	if tool.OldVersions != 1 || tool.OldPaths[0] != filepath.Join(f.Prefix, "Caskroom/tool/1.0") {
		t.Errorf("tool old = %d %v (.metadata must not count)", tool.OldVersions, tool.OldPaths)
	}
	if !tool.Broken() || tool.MissingApps[0] != filepath.Join(f.Apps, "Tool.app") {
		t.Errorf("tool MissingApps = %v", tool.MissingApps)
	}

	viewer := pkgs["viewer"]
	if viewer.Broken() {
		t.Error("viewer's app exists")
	}
	if viewer.SizeBytes < 50000 {
		t.Errorf("viewer size %d should include its app bundle", viewer.SizeBytes)
	}
}

func TestRunDefaults(t *testing.T) {
	// No workers or time given: still scans everything.
	f := brewtest.Standard(t)
	pkgs := analyze.Run(&f.Info, analyze.Options{Prefix: f.Prefix})
	if len(pkgs) != 6 {
		t.Fatalf("got %d packages", len(pkgs))
	}
	for _, p := range pkgs {
		if p.LastActive.IsZero() {
			t.Errorf("%s has no LastActive", p.Name)
		}
	}
}

func TestRunDeprecatedAndDisabled(t *testing.T) {
	f := brewtest.Standard(t)
	f.Info.Formulae[0].Deprecated = true // jq
	f.Info.Formulae[0].DeprecationDate = "2026-01-02"
	f.Info.Formulae[0].DeprecationReason = "versioned_formula"
	f.Info.Formulae[0].DisableDate = "2027-01-02"
	f.Info.Formulae[3].Deprecated = true // leftover
	f.Info.Formulae[3].Disabled = true
	f.Info.Formulae[3].DisableReason = "unmaintained"
	f.Info.Formulae[3].DisableReplacementFormula = "newer"
	f.Info.Casks[1].Deprecated = true // viewer
	f.Info.Casks[1].DeprecationReason = "unsigned"
	pkgs := map[string]*analyze.Package{}
	for _, p := range analyze.Run(&f.Info, analyze.Options{Prefix: f.Prefix, Now: f.Now}) {
		pkgs[p.Name] = p
	}

	jq := pkgs["jq"]
	if !jq.Deprecated || jq.Disabled || !jq.Retiring() || jq.StatusReason != "is a versioned formula" ||
		jq.DeprecationDate != "2026-01-02" || jq.DisableDate != "2027-01-02" {
		t.Errorf("jq = deprecated %v disabled %v %q %q %q", jq.Deprecated, jq.Disabled, jq.StatusReason, jq.DeprecationDate, jq.DisableDate)
	}
	if l := pkgs["leftover"]; !l.Disabled || l.StatusReason != "is not maintained upstream" || l.Replacement != "newer" {
		t.Errorf("leftover = disabled %v %q %q", l.Disabled, l.StatusReason, l.Replacement)
	}
	if v := pkgs["viewer"]; !v.Deprecated || v.StatusReason != "is unsigned or does not meet signature requirements" {
		t.Errorf("viewer = deprecated %v %q", v.Deprecated, v.StatusReason)
	}
	if o := pkgs["oniguruma"]; o.Retiring() || o.StatusReason != "" {
		t.Errorf("oniguruma should have no status, got %q", o.StatusReason)
	}
}

func TestRunKegsWithoutFormula(t *testing.T) {
	f := brewtest.Standard(t)
	// A requested tool whose tap was removed, its library (deleted from
	// homebrew/core) that it still uses, an empty leftover directory, and a
	// keg kept under a formula's old name, which brew still accounts for.
	f.OrphanKeg(t, "gone", "2.0", "someone/tools", 4000, true, "libgone")
	f.OrphanKeg(t, "libgone", "1.0", "homebrew/core", 2000, false)
	f.OrphanKeg(t, "manual", "0.1", "", 1000, true)
	f.OrphanKeg(t, "jq-old", "1.0", "homebrew/core", 1000, true)
	f.Info.Formulae[0].Oldnames = []string{"jq-old"}
	if err := os.MkdirAll(filepath.Join(f.Prefix, "Cellar", "wpscan"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkgs := map[string]*analyze.Package{}
	for _, p := range analyze.Run(&f.Info, analyze.Options{Prefix: f.Prefix, Now: f.Now}) {
		pkgs[p.Name] = p
	}

	if p := pkgs["jq-old"]; p != nil {
		t.Errorf("a formula's old name was reported as having no formula: %+v", p)
	}
	gone := pkgs["gone"]
	if gone == nil || !gone.NoFormula || gone.Kind != analyze.KindFormula || gone.Version != "2.0" || !gone.OnRequest {
		t.Fatalf("gone = %+v", gone)
	}
	if gone.Origin != "from untapped someone/tools" || gone.SizeBytes < 4000 || gone.Orphan {
		t.Errorf("gone origin %q size %d orphan %v", gone.Origin, gone.SizeBytes, gone.Orphan)
	}
	if gone.Rack != filepath.Join(f.Prefix, "Cellar", "gone") || !gone.InstalledAt.Equal(time.Unix(f.Days(300).Unix(), 0)) {
		t.Errorf("gone rack %q installed %v", gone.Rack, gone.InstalledAt)
	}
	lib := pkgs["libgone"]
	if lib.Origin != "deleted from homebrew/core" || len(lib.Dependents) != 1 || lib.Dependents[0] != "gone" || lib.Orphan {
		t.Errorf("libgone origin %q dependents %v orphan %v", lib.Origin, lib.Dependents, lib.Orphan)
	}
	if m := pkgs["manual"]; m.Origin != "no tap recorded" {
		t.Errorf("manual origin %q", m.Origin)
	}
	empty := pkgs["wpscan"]
	if empty == nil || !empty.EmptyKeg() || empty.SizeBytes != 0 || empty.InstalledAt.IsZero() {
		t.Errorf("wpscan = %+v", empty)
	}
	if jq := pkgs["jq"]; jq.NoFormula || jq.EmptyKeg() {
		t.Error("jq has a formula")
	}
}

func TestRunNoFormulaOriginForAnInstalledTap(t *testing.T) {
	f := brewtest.Standard(t)
	f.OrphanKeg(t, "removed", "1.0", "someone/tools", 100, true)
	// Intel and Linux keep taps in prefix/Homebrew.
	if err := os.MkdirAll(filepath.Join(f.Prefix, "Homebrew/Library/Taps/someone/homebrew-tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range analyze.Run(&f.Info, analyze.Options{Prefix: f.Prefix, Now: f.Now}) {
		if p.Name == "removed" && p.Origin != "deleted from someone/tools" {
			t.Errorf("origin = %q", p.Origin)
		}
	}
}
