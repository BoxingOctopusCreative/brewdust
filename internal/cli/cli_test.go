// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/term"

	"github.com/BoxingOctopusCreative/brewdust/internal/brewtest"
)

// brewdust runs the CLI against a fake Homebrew installation.
func brewdust(t *testing.T, fake *brewtest.Fake, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRoot("test")
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func setup(t *testing.T) *brewtest.Fake {
	t.Helper()
	fake := brewtest.Standard(t)
	fake.Install(t)
	return fake
}

// mutating lists the brew calls that would change something.
func mutating(t *testing.T, fake *brewtest.Fake) []string {
	t.Helper()
	var out []string
	for _, c := range fake.Calls(t) {
		if c != "--prefix" && !strings.HasPrefix(c, "info ") {
			out = append(out, c)
		}
	}
	return out
}

func TestList(t *testing.T) {
	fake := setup(t)
	out, _, err := brewdust(t, fake, "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("no header:\n%s", out)
	}
	// Default sort is by size: jq, with two versions, is the biggest.
	if !strings.HasPrefix(lines[1], "jq ") {
		t.Errorf("first row = %q, want jq", lines[1])
	}
	for _, want := range []string{
		"6 of 6 packages shown",
		"orphan (brew autoremove)",
		"kept by brew cleanup until upgraded",
		"app deleted outside brew",
		"Run `brewdust clean`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestListFiltersSortAndLimit(t *testing.T) {
	fake := setup(t)
	rows := func(args ...string) []string {
		t.Helper()
		out, _, err := brewdust(t, fake, append([]string{"list"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, l := range strings.Split(out, "\n")[1:] {
			if l == "" {
				break
			}
			names = append(names, strings.Fields(l)[0])
		}
		return names
	}
	check := func(got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	check(rows("--sort", "name"), "harfbuzz", "jq", "leftover", "oniguruma", "tool", "viewer")
	check(rows("--sort", "name", "-n", "2"), "harfbuzz", "jq")
	check(rows("-t", "cask", "--sort", "name"), "tool", "viewer")
	check(rows("--orphans"), "leftover")
	check(rows("--broken"), "tool")
	check(rows("--deps"), "oniguruma")
	check(rows("--requested", "-t", "formula", "--sort", "name"), "harfbuzz", "jq")
	check(rows("--min-size", "45kB", "--sort", "name"), "jq", "viewer")
	// oniguruma counts as active through jq unless judged directly.
	check(rows("--stale", "100", "-t", "formula", "--sort", "name"), "harfbuzz", "leftover")
	check(rows("--stale", "100", "-t", "formula", "--sort", "name", "--direct"), "harfbuzz", "leftover", "oniguruma")
}

func TestListJSON(t *testing.T) {
	fake := setup(t)
	out, _, err := brewdust(t, fake, "ls", "--json", "--sort", "name")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Packages []struct {
			Name             string   `json:"name"`
			Kind             string   `json:"kind"`
			ReclaimableBytes int64    `json:"reclaimable_bytes"`
			HeldBytes        int64    `json:"held_bytes"`
			Dependents       []string `json:"dependents"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(doc.Packages) != 6 {
		t.Fatalf("got %d packages", len(doc.Packages))
	}
	byName := map[string]int{}
	for i, p := range doc.Packages {
		byName[p.Name] = i
	}
	if p := doc.Packages[byName["jq"]]; p.ReclaimableBytes == 0 {
		t.Errorf("jq = %+v", p)
	}
	if p := doc.Packages[byName["harfbuzz"]]; p.HeldBytes == 0 || p.ReclaimableBytes != 0 {
		t.Errorf("harfbuzz = %+v", p)
	}
	if p := doc.Packages[byName["oniguruma"]]; !slices.Equal(p.Dependents, []string{"jq"}) {
		t.Errorf("oniguruma = %+v", p)
	}
}

func TestRootFallsBackToListWithoutTerminal(t *testing.T) {
	if isInteractive() {
		t.Skip("running on a terminal")
	}
	fake := setup(t)
	out, _, err := brewdust(t, fake)
	if err != nil || !strings.HasPrefix(out, "NAME") {
		t.Errorf("err=%v out=\n%s", err, out)
	}
	if _, _, err := brewdust(t, fake, "tui"); err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("tui without a terminal: err = %v", err)
	}
}

func TestFlagErrors(t *testing.T) {
	fake := setup(t)
	cases := map[string][]string{
		"unknown --sort":           {"list", "--sort", "age"},
		"unknown --type":           {"list", "-t", "keg"},
		"invalid size":             {"list", "--min-size", "lots"},
		"cask and formula":         {"remove", "--cask", "--formula", "jq"},
		"unknown cleanup category": {"clean", "--only", "everything"},
		"remove needs a package":   {"remove"},
		"list takes no arguments":  {"list", "jq"},
		"unknown flag":             {"clean", "--bogus"},
		"not installed":            {"remove", "jq", "nope", "-n"},
		"not installed as formula": {"remove", "--formula", "tool", "-n"},
	}
	for name, args := range cases {
		if _, _, err := brewdust(t, fake, args...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, _, err := brewdust(t, fake, "remove", "jq", "nope", "-n"); err == nil || !strings.Contains(err.Error(), "not installed: nope") {
		t.Errorf("err = %v", err)
	}
	if got := mutating(t, fake); len(got) != 0 {
		t.Errorf("errors still ran brew: %q", got)
	}
}

func TestCleanDryRun(t *testing.T) {
	fake := setup(t)
	out, _, err := brewdust(t, fake, "clean", "-n")
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(fake.Prefix, "Caskroom/tool/1.0")
	var cmds []string
	for _, l := range strings.Split(out, "\n") {
		if c, ok := strings.CutPrefix(l, "  $ "); ok {
			cmds = append(cmds, c)
		}
	}
	want := []string{"brew cleanup jq", "rm -rf " + stale, "brew autoremove", "brew uninstall --cask tool"}
	if !slices.Equal(cmds, want) {
		t.Errorf("commands = %q\nwant %q\n%s", cmds, want, out)
	}
	if strings.Contains(out, "harfbuzz") {
		t.Errorf("outdated harfbuzz should not be in the plan:\n%s", out)
	}
	if !strings.Contains(out, "Dry run: nothing was changed.") {
		t.Errorf("missing dry-run note:\n%s", out)
	}
	if got := mutating(t, fake); len(got) != 0 {
		t.Errorf("dry run ran brew: %q", got)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("dry run removed %s", stale)
	}

	out, _, _ = brewdust(t, fake, "clean", "-n", "--only", "orphans")
	if !strings.Contains(out, "brew autoremove") || strings.Contains(out, "cleanup jq") {
		t.Errorf("--only orphans:\n%s", out)
	}
}

func TestCleanRuns(t *testing.T) {
	fake := setup(t)
	out, _, err := brewdust(t, fake, "clean", "-y")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := []string{"cleanup jq", "autoremove", "uninstall --cask tool"}
	if got := mutating(t, fake); !slices.Equal(got, want) {
		t.Errorf("brew calls = %q, want %q", got, want)
	}
	stale := filepath.Join(fake.Prefix, "Caskroom/tool/1.0")
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("%s should be gone", stale)
	}
	if _, err := os.Stat(filepath.Join(fake.Prefix, "Caskroom/tool/2.0")); err != nil {
		t.Error("the current Caskroom version was removed")
	}
	for _, want := range []string{"Removed " + stale, "fake brew: autoremove"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestCleanReportsFailedSteps(t *testing.T) {
	fake := setup(t)
	t.Setenv("FAKE_BREW_FAIL", "autoremove")
	out, errOut, err := brewdust(t, fake, "clean", "-y")
	if err == nil || !strings.Contains(err.Error(), "1 step(s) failed") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(errOut, "brew autoremove") || !strings.Contains(out, "Error: it broke") {
		t.Errorf("stderr=%q\nstdout=%s", errOut, out)
	}
	// The other steps still ran.
	if got := mutating(t, fake); !slices.Equal(got, []string{"cleanup jq", "autoremove", "uninstall --cask tool"}) {
		t.Errorf("brew calls = %q", got)
	}
}

func TestRemoveBlocksNeededFormulae(t *testing.T) {
	fake := setup(t)
	out, _, err := brewdust(t, fake, "remove", "oniguruma", "-n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "oniguruma (needed by jq)") || !strings.Contains(out, "Nothing to do.") {
		t.Errorf("output:\n%s", out)
	}

	// Removing its dependent as well unblocks it, and jq goes first.
	out, _, err = brewdust(t, fake, "rm", "oniguruma", "jq", "-n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "$ brew uninstall --formula jq oniguruma") {
		t.Errorf("output:\n%s", out)
	}

	out, _, _ = brewdust(t, fake, "remove", "oniguruma", "--ignore-dependencies", "-n")
	if !strings.Contains(out, "--ignore-dependencies oniguruma") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRemoveRuns(t *testing.T) {
	fake := setup(t)
	if _, _, err := brewdust(t, fake, "uninstall", "viewer", "leftover", "--zap", "-y"); err != nil {
		t.Fatal(err)
	}
	want := []string{"uninstall --cask --zap viewer", "uninstall --formula leftover"}
	if got := mutating(t, fake); !slices.Equal(got, want) {
		t.Errorf("brew calls = %q, want %q", got, want)
	}
}

func TestRemoveAsksFirst(t *testing.T) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Skip("stdin is a terminal")
	}
	fake := setup(t)
	out, _, err := brewdust(t, fake, "remove", "leftover")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("err = %v\n%s", err, out)
	}
	if got := mutating(t, fake); len(got) != 0 {
		t.Errorf("ran without confirmation: %q", got)
	}
}

func TestConfirm(t *testing.T) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		if _, err := confirm(strings.NewReader("y\n"), &bytes.Buffer{}, "Go?"); err == nil {
			t.Error("confirm without a terminal should refuse")
		}
		return
	}
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false} {
		var out bytes.Buffer
		got, err := confirm(strings.NewReader(in), &out, "Go?")
		if err != nil || got != want || out.String() != "Go? [y/N] " {
			t.Errorf("confirm(%q) = %v, %v; prompt %q", in, got, err, out.String())
		}
	}
}
