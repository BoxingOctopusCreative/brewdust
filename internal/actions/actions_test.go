// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package actions

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
)

func TestPlanRemovalBlocksNeededFormulae(t *testing.T) {
	openssl := &analyze.Package{Name: "openssl", Kind: analyze.KindFormula, SizeBytes: 10, AllDependents: []string{"curl", "wget"}}
	curl := &analyze.Package{Name: "curl", Kind: analyze.KindFormula, SizeBytes: 5}
	app := &analyze.Package{Name: "calibre", Kind: analyze.KindCask, SizeBytes: 100}

	r := PlanRemoval([]*analyze.Package{openssl, curl, app}, RemoveOptions{Zap: true})
	if got := r.Blocked["openssl"]; !reflect.DeepEqual(got, []string{"wget"}) {
		t.Errorf("Blocked[openssl] = %v, want [wget]", got)
	}
	if len(r.Steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(r.Steps))
	}
	if want := []string{"uninstall", "--cask", "--zap", "calibre"}; !reflect.DeepEqual(r.Steps[0].Brew, want) {
		t.Errorf("cask step (first) = %v, want %v", r.Steps[0].Brew, want)
	}
	if want := []string{"uninstall", "--formula", "curl"}; !reflect.DeepEqual(r.Steps[1].Brew, want) {
		t.Errorf("formula step = %v, want %v", r.Steps[1].Brew, want)
	}

	r = PlanRemoval([]*analyze.Package{openssl, curl}, RemoveOptions{IgnoreDependencies: true})
	if len(r.Blocked) != 0 || r.Steps[0].Bytes != 15 {
		t.Errorf("with IgnoreDependencies: blocked=%v bytes=%d", r.Blocked, r.Steps[0].Bytes)
	}
	if got := r.Steps[0].Tasks[1].Brew; !reflect.DeepEqual(got, []string{"uninstall", "--formula", "--ignore-dependencies", "openssl"}) {
		t.Errorf("openssl task (after curl, which needs it) = %v", got)
	}
}

func TestPlanCleanup(t *testing.T) {
	pkgs := []*analyze.Package{
		{Name: "go", Kind: analyze.KindFormula, OldVersions: 2, ReclaimableBytes: 400},
		{Name: "leftover", Kind: analyze.KindFormula, Orphan: true, SizeBytes: 30},
		{Name: "calibre", Kind: analyze.KindCask, SizeBytes: 250, OldVersions: 1, ReclaimableBytes: 200,
			OldPaths: []string{"/opt/homebrew/Caskroom/calibre/6.29.0"}, MissingApps: []string{"/Applications/calibre.app"}},
	}
	steps := PlanCleanup(pkgs, Categories)
	var cmds []string
	for _, s := range steps {
		cmds = append(cmds, s.Command())
	}
	want := []string{
		"brew cleanup go",
		"rm -rf /opt/homebrew/Caskroom/calibre/6.29.0",
		"brew autoremove",
		"brew uninstall --cask calibre",
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands = %q\nwant %q", cmds, want)
	}
	if got := TotalBytes(steps); got != 400+200+30+50 {
		t.Errorf("TotalBytes = %d", got)
	}
	if only := PlanCleanup(pkgs, []Category{Orphans}); len(only) != 1 || only[0].Items[0] != "leftover" {
		t.Errorf("orphans only = %+v", only)
	}
}

func TestExecuteRefusesPathsOutsideCaskroomVersions(t *testing.T) {
	prefix := t.TempDir()
	good := filepath.Join(prefix, "Caskroom", "calibre", "6.29.0")
	if err := os.MkdirAll(filepath.Join(good, "calibre.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		filepath.Join(prefix, "Caskroom", "calibre"),
		filepath.Join(prefix, "Caskroom", "calibre", ".metadata"),
		filepath.Join(prefix, "Cellar", "go", "1.0"),
		filepath.Join(prefix, "Caskroom", "calibre", "..", "..", "Cellar"),
	}
	for _, p := range bad {
		if err := Execute(context.Background(), prefix, Step{Paths: []string{p}}, &bytes.Buffer{}); err == nil {
			t.Errorf("expected refusal for %s", p)
		}
	}
	if err := Execute(context.Background(), prefix, Step{Paths: []string{good}}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Errorf("%s still exists", good)
	}
}

func TestParseCategories(t *testing.T) {
	if c, _ := ParseCategories(""); len(c) != len(Categories) || c[3] != NoFormula {
		t.Errorf("empty should mean all, got %v", c)
	}
	if c, err := ParseCategories("orphans, broken,noformula"); err != nil || len(c) != 3 {
		t.Errorf("got %v, %v", c, err)
	}
	if _, err := ParseCategories("everything"); err == nil {
		t.Error("expected error")
	}
}

func TestRemovalTasksRunDependentsFirst(t *testing.T) {
	// wget → curl → openssl, all chosen, plus a cask.
	openssl := &analyze.Package{Name: "openssl", Kind: analyze.KindFormula, SizeBytes: 3, AllDependents: []string{"curl", "wget"}}
	curl := &analyze.Package{Name: "curl", Kind: analyze.KindFormula, SizeBytes: 2, AllDependents: []string{"wget"}}
	wget := &analyze.Package{Name: "wget", Kind: analyze.KindFormula, SizeBytes: 1}
	app := &analyze.Package{Name: "calibre", Kind: analyze.KindCask, SizeBytes: 9}

	r := PlanRemoval([]*analyze.Package{openssl, curl, wget, app}, RemoveOptions{})
	var order []string
	for _, task := range AllTasks(r.Steps) {
		order = append(order, task.Label)
		if want := task.Label; task.Brew[len(task.Brew)-1] != want {
			t.Errorf("task %s runs %v", want, task.Brew)
		}
	}
	if want := []string{"calibre", "wget", "curl", "openssl"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestCleanupTasks(t *testing.T) {
	pkgs := []*analyze.Package{
		{Name: "go", Kind: analyze.KindFormula, OldVersions: 2, ReclaimableBytes: 400},
		{Name: "xz", Kind: analyze.KindFormula, OldVersions: 1, ReclaimableBytes: 4},
		{Name: "leftover", Kind: analyze.KindFormula, Orphan: true, SizeBytes: 30},
		{Name: "calibre", Kind: analyze.KindCask, SizeBytes: 250, OldVersions: 1, ReclaimableBytes: 200,
			OldPaths: []string{"/opt/homebrew/Caskroom/calibre/6.29.0"}, MissingApps: []string{"/Applications/calibre.app"}},
	}
	var cmds []string
	var total int64
	for _, task := range AllTasks(PlanCleanup(pkgs, Categories)) {
		cmds = append(cmds, task.Command())
		total += task.Bytes
	}
	want := []string{
		"brew cleanup go", "brew cleanup xz",
		"rm -rf /opt/homebrew/Caskroom/calibre/6.29.0",
		"brew autoremove",
		"brew uninstall --cask calibre",
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("tasks = %q\nwant %q", cmds, want)
	}
	if total != TotalBytes(PlanCleanup(pkgs, Categories)) {
		t.Errorf("task bytes %d don't add up to the steps'", total)
	}
}

func TestNeedsTerminal(t *testing.T) {
	if !NeedsTerminal("==> Uninstalling Cask macfuse\nsudo: a terminal is required to read the password") {
		t.Error("sudo without a tty should need a terminal")
	}
	if NeedsTerminal("Error: No such keg: /opt/homebrew/Cellar/foo") {
		t.Error("an ordinary error shouldn't")
	}
}

func TestPlanCleanupKegsWithoutFormula(t *testing.T) {
	pkgs := []*analyze.Package{
		{Name: "gone", Kind: analyze.KindFormula, NoFormula: true, Version: "2.0", SizeBytes: 500, Root: "/opt/homebrew/Cellar/gone"},
		{Name: "libgone", Kind: analyze.KindFormula, NoFormula: true, Version: "1.0", SizeBytes: 90,
			Root: "/opt/homebrew/Cellar/libgone", AllDependents: []string{"gone", "wget"}},
		{Name: "wpscan", Kind: analyze.KindFormula, NoFormula: true, Root: "/opt/homebrew/Cellar/wpscan"},
		{Name: "jq", Kind: analyze.KindFormula, SizeBytes: 40},
	}
	steps := PlanCleanup(pkgs, []Category{NoFormula})
	var cmds []string
	for _, s := range steps {
		cmds = append(cmds, s.Command())
	}
	// libgone stays: wget, which isn't being removed, still needs it.
	want := []string{"brew uninstall --formula gone", "rmdir /opt/homebrew/Cellar/wpscan"}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands = %q\nwant %q", cmds, want)
	}
	if TotalBytes(steps) != 500 {
		t.Errorf("TotalBytes = %d", TotalBytes(steps))
	}
	tasks := AllTasks(steps)
	if len(tasks) != 2 || tasks[0].Command() != "brew uninstall --formula gone" || tasks[1].Command() != "rmdir /opt/homebrew/Cellar/wpscan" {
		t.Errorf("tasks = %+v", tasks)
	}
	if steps[1].Category != NoFormula || NoFormula.Describe() != "Kegs with no formula" {
		t.Errorf("category %q %q", steps[1].Category, NoFormula.Describe())
	}
}

func TestExecuteRemovesOnlyEmptyCellarDirectories(t *testing.T) {
	prefix := t.TempDir()
	empty := filepath.Join(prefix, "Cellar", "wpscan")
	full := filepath.Join(prefix, "Cellar", "refilled")
	for _, d := range []string{empty, filepath.Join(full, "1.0")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	step := Step{Paths: []string{empty}, Rmdir: true}
	if err := Execute(context.Background(), prefix, step, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Errorf("%s still exists", empty)
	}
	// A keg installed since the scan must survive.
	if err := Execute(context.Background(), prefix, Step{Paths: []string{full}, Rmdir: true}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error removing a Cellar directory that isn't empty")
	}
	if _, err := os.Stat(filepath.Join(full, "1.0")); err != nil {
		t.Errorf("keg was removed: %v", err)
	}
	// Kegs themselves are never deleted directly.
	if err := Execute(context.Background(), prefix, Step{Paths: []string{filepath.Join(full, "1.0")}}, &bytes.Buffer{}); err == nil {
		t.Error("expected a refusal for a keg path")
	}
	if _, err := os.Stat(filepath.Join(full, "1.0")); err != nil {
		t.Errorf("keg was removed: %v", err)
	}
}
