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
	if want := []string{"uninstall", "--formula", "curl"}; !reflect.DeepEqual(r.Steps[0].Brew, want) {
		t.Errorf("formula step = %v, want %v", r.Steps[0].Brew, want)
	}
	if want := []string{"uninstall", "--cask", "--zap", "calibre"}; !reflect.DeepEqual(r.Steps[1].Brew, want) {
		t.Errorf("cask step = %v, want %v", r.Steps[1].Brew, want)
	}

	r = PlanRemoval([]*analyze.Package{openssl, curl}, RemoveOptions{IgnoreDependencies: true})
	if len(r.Blocked) != 0 || r.Steps[0].Bytes != 15 {
		t.Errorf("with IgnoreDependencies: blocked=%v bytes=%d", r.Blocked, r.Steps[0].Bytes)
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
	if c, _ := ParseCategories(""); len(c) != 3 {
		t.Errorf("empty should mean all, got %v", c)
	}
	if c, err := ParseCategories("orphans, broken"); err != nil || len(c) != 2 {
		t.Errorf("got %v, %v", c, err)
	}
	if _, err := ParseCategories("everything"); err == nil {
		t.Error("expected error")
	}
}
