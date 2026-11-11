// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package brew_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
	"github.com/BoxingOctopusCreative/brewdust/internal/brewtest"
)

func TestCurrentPrefersLinkedKeg(t *testing.T) {
	f := brew.Formula{LinkedKeg: "1.0", Installed: []brew.InstalledVersion{
		{Version: "1.0", Time: 100}, {Version: "2.0", Time: 200},
	}}
	if cur, ok := f.Current(); !ok || cur.Version != "1.0" {
		t.Errorf("linked: got %s, want 1.0", cur.Version)
	}
	f.LinkedKeg = "" // keg-only or unlinked: newest install wins
	if cur, _ := f.Current(); cur.Version != "2.0" {
		t.Errorf("unlinked: got %s, want 2.0", cur.Version)
	}
	if _, ok := (brew.Formula{}).Current(); ok {
		t.Error("no installed versions should report !ok")
	}
}

func TestLatestVersion(t *testing.T) {
	var f brew.Formula
	f.Versions.Stable = "15.0.0"
	if got := f.LatestVersion(); got != "15.0.0" {
		t.Errorf("got %q", got)
	}
	f.Revision = 2
	if got := f.LatestVersion(); got != "15.0.0_2" {
		t.Errorf("with revision: got %q", got)
	}
}

func TestAppPaths(t *testing.T) {
	c := brew.Cask{Artifacts: []brew.Artifact{
		brewtest.AppArtifact("/Applications/One.app"),
		{"binary": json.RawMessage(`["one"]`)},
		{"app": json.RawMessage(`["Two.app"]`)}, // no target: unknown location
		brewtest.AppArtifact("/Applications/Three.app"),
	}}
	if got := c.AppPaths(); !slices.Equal(got, []string{"/Applications/One.app", "/Applications/Three.app"}) {
		t.Errorf("AppPaths = %v", got)
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{"jq": "jq", "homebrew/core/jq": "jq", "user/tap/tool": "tool"} {
		if got := brew.ShortName(in); got != want {
			t.Errorf("ShortName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeInfo(t *testing.T) {
	// A trimmed sample of real `brew info --json=v2 --installed` output.
	raw := `{"formulae":[{"name":"go@1.25","full_name":"go@1.25","desc":"Go","keg_only":true,
		"linked_keg":null,"outdated":false,"pinned":true,"revision":1,"versions":{"stable":"1.25.4"},
		"installed":[{"version":"1.25.4_1","time":1762560000,"installed_on_request":true,
		"runtime_dependencies":[{"full_name":"homebrew/core/zstd","version":"1.5"}]}]}],
		"casks":[{"token":"calibre","full_token":"calibre","name":["calibre"],"desc":"E-books",
		"installed":"8.0","installed_time":1762560000,"depends_on":{"formula":["python"]},
		"artifacts":[{"app":["calibre.app"],"target":"/Applications/calibre.app"},{"zap":[{"trash":"~/x"}]}]}]}`
	var info brew.Info
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}
	f := info.Formulae[0]
	if !f.Pinned || f.LatestVersion() != "1.25.4_1" || f.CleanupSkipReason() != "pinned" {
		t.Errorf("formula = %+v", f)
	}
	if cur, _ := f.Current(); cur.Version != "1.25.4_1" || cur.RuntimeDependencies[0].FullName != "homebrew/core/zstd" {
		t.Errorf("current = %+v", cur)
	}
	c := info.Casks[0]
	if c.DependsOn.Formula[0] != "python" || !slices.Equal(c.AppPaths(), []string{"/Applications/calibre.app"}) {
		t.Errorf("cask = %+v", c)
	}
}

func TestLoadAndPrefixUseBrew(t *testing.T) {
	fake := brewtest.Standard(t)
	fake.Install(t)
	ctx := context.Background()

	prefix, err := brew.Prefix(ctx)
	if err != nil || prefix != fake.Prefix {
		t.Fatalf("Prefix = %q, %v; want %q", prefix, err, fake.Prefix)
	}
	info, err := brew.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Formulae) != 4 || len(info.Casks) != 2 || info.Formulae[0].Name != "jq" {
		t.Errorf("loaded %d formulae, %d casks", len(info.Formulae), len(info.Casks))
	}
	if calls := fake.Calls(t); !slices.Equal(calls, []string{"--prefix", "info --json=v2 --installed"}) {
		t.Errorf("brew calls = %q", calls)
	}
}

func TestCommandDisablesAutoUpdate(t *testing.T) {
	env := brew.Command(context.Background(), "info").Env
	for _, want := range []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1"} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s", want)
		}
	}
}

func TestLoadReportsBrewErrors(t *testing.T) {
	fake := brewtest.Standard(t)
	fake.Install(t)
	// Point info.json at garbage: decoding fails with a clear message.
	if err := os.WriteFile(filepath.Join(fake.Dir, "info.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := brew.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "parsing brew info") {
		t.Errorf("err = %v", err)
	}

	t.Setenv("PATH", t.TempDir()) // no brew at all
	if _, err := brew.Prefix(context.Background()); err == nil || !strings.Contains(err.Error(), "brew --prefix") {
		t.Errorf("missing brew: err = %v", err)
	}
}

func TestLifecycle(t *testing.T) {
	var info brew.Info
	data := `{"formulae":[
		{"name":"openssl@1.1","oldnames":["openssl11"],"deprecated":true,"deprecation_date":"2023-11-11",
		 "deprecation_reason":"unsupported","deprecation_replacement_formula":null,"disabled":false,"disable_date":null},
		{"name":"libsoup@2","deprecated":true,"deprecation_reason":"unsupported","disabled":true,
		 "disable_date":"2026-09-05","disable_reason":"repo_archived","disable_replacement_formula":"libsoup"},
		{"name":"custom","deprecated":true,"deprecation_reason":"Use something newer"}],
	 "casks":[{"token":"oldapp","old_tokens":["old-app"],"deprecated":true,"deprecation_reason":"unsigned",
		 "deprecation_replacement_cask":"newapp"}]}`
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		t.Fatal(err)
	}
	ssl, soup, custom, cask := info.Formulae[0], info.Formulae[1], info.Formulae[2], info.Casks[0]
	if !ssl.Deprecated || ssl.Disabled || ssl.DisableDate != "" || ssl.Oldnames[0] != "openssl11" {
		t.Errorf("openssl@1.1 = %+v", ssl.Lifecycle)
	}
	if got := ssl.Reason(); got != "is not supported upstream" {
		t.Errorf("openssl@1.1 Reason = %q", got)
	}
	if ssl.Replacement() != "" {
		t.Errorf("a null replacement should be empty, got %q", ssl.Replacement())
	}
	// A disabled formula's own reason wins over its deprecation reason.
	if got := soup.Reason(); got != "has an archived upstream repository" {
		t.Errorf("libsoup@2 Reason = %q", got)
	}
	if soup.Replacement() != "libsoup" {
		t.Errorf("libsoup@2 Replacement = %q", soup.Replacement())
	}
	if got := custom.Reason(); got != "Use something newer" {
		t.Errorf("free-text reason = %q", got)
	}
	if cask.Reason() != "is unsigned or does not meet signature requirements" || cask.Replacement() != "newapp" ||
		cask.OldTokens[0] != "old-app" {
		t.Errorf("cask = %+v", cask)
	}
	if got := brew.ReasonText("some_new_code"); got != "some new code" {
		t.Errorf("unknown code = %q", got)
	}
}

func TestReadReceipt(t *testing.T) {
	keg := t.TempDir()
	receipt := `{"time":1700000000,"installed_on_request":true,"source":{"tap":"someone/tools"},
		"runtime_dependencies":[{"full_name":"openssl@3","version":"3.4.0"}]}`
	if err := os.WriteFile(filepath.Join(keg, "INSTALL_RECEIPT.json"), []byte(receipt), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := brew.ReadReceipt(keg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Time != 1700000000 || !r.InstalledOnRequest || r.Source.Tap != "someone/tools" ||
		len(r.RuntimeDependencies) != 1 || r.RuntimeDependencies[0].FullName != "openssl@3" {
		t.Errorf("receipt = %+v", r)
	}
	if _, err := brew.ReadReceipt(t.TempDir()); err == nil {
		t.Error("expected an error for a keg without a receipt")
	}
}
