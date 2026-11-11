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
	"strings"
	"testing"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
)

func TestPlanFixes(t *testing.T) {
	saved := owner
	owner = "me"
	defer func() { owner = saved }()
	pkgs := []*analyze.Package{
		{Name: "glide", Kind: analyze.KindFormula, Unlinked: true},
		{Name: "ctags", Kind: analyze.KindFormula, Unlinked: true},
		{Name: "go", Kind: analyze.KindFormula, Root: "/opt/homebrew/Cellar/go", LockedPaths: 30},
		{Name: "rooted", Kind: analyze.KindCask, Root: "/opt/homebrew/Caskroom/rooted", ForeignOwned: 2, DeniedPaths: 1},
		{Name: "fine", Kind: analyze.KindFormula, Root: "/opt/homebrew/Cellar/fine"},
	}
	steps := PlanCleanup(pkgs, FixCategories)
	var cmds []string
	for _, s := range steps {
		cmds = append(cmds, s.Command())
	}
	want := []string{
		"brew link --formula ctags glide",
		"sudo chown -R me /opt/homebrew/Caskroom/rooted",
		"chmod u+rwx on directories and u+r on files missing them, under /opt/homebrew/Cellar/go /opt/homebrew/Caskroom/rooted",
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands = %q\nwant %q", cmds, want)
	}
	var tasks []string
	for _, task := range AllTasks(steps) {
		tasks = append(tasks, task.Command())
	}
	wantTasks := []string{
		"brew link --formula ctags", "brew link --formula glide",
		"sudo chown -R me /opt/homebrew/Caskroom/rooted",
		"chmod u+rwx on directories and u+r on files missing them, under /opt/homebrew/Cellar/go",
		"chmod u+rwx on directories and u+r on files missing them, under /opt/homebrew/Caskroom/rooted",
	}
	if !reflect.DeepEqual(tasks, wantTasks) {
		t.Errorf("tasks = %q\nwant %q", tasks, wantTasks)
	}
	if steps[1].Items[0] != "rooted (3 entries)" || TotalBytes(steps) != 0 {
		t.Errorf("items %q bytes %d", steps[1].Items, TotalBytes(steps))
	}
	if only := PlanCleanup(pkgs, []Category{Unlinked}); len(only) != 1 {
		t.Errorf("unlinked only = %d steps", len(only))
	}
	if steps := PlanCleanup(pkgs, Categories); len(steps) != 0 {
		t.Errorf("cleanup shouldn't include fixes: %+v", steps)
	}
	if !Permissions.IsFix() || !Unlinked.IsFix() || OldVersions.IsFix() || Unlinked.Describe() != "Unlinked kegs" {
		t.Error("IsFix or Describe")
	}
}

func TestParseFixCategories(t *testing.T) {
	if c, _ := ParseFixCategories(""); !reflect.DeepEqual(c, FixCategories) {
		t.Errorf("empty = %v", c)
	}
	if c, err := ParseFixCategories("permissions"); err != nil || len(c) != 1 || c[0] != Permissions {
		t.Errorf("got %v, %v", c, err)
	}
	if _, err := ParseFixCategories("old"); err == nil || !strings.Contains(err.Error(), "want unlinked, permissions") {
		t.Errorf("a cleanup category isn't a fix: %v", err)
	}
	if _, err := ParseCategories("permissions"); err == nil {
		t.Error("a fix isn't a cleanup category")
	}
}

func TestFixPermissions(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "Cellar", "go")
	mod := filepath.Join(root, "1.25/pkg/mod/example.com@v1")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(mod, "go.mod")
	if err := os.WriteFile(file, []byte("module x"), 0o444); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "1.25/secret")
	if err := os.WriteFile(secret, []byte("x"), 0o200); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "1.25/link")
	if err := os.Symlink("go.mod", link); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{mod, filepath.Dir(mod)} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(mod, 0o755); _ = os.Chmod(filepath.Dir(mod), 0o755) })

	// sudo is a stub that records its arguments.
	log := filepath.Join(t.TempDir(), "sudo.log")
	stub := filepath.Join(t.TempDir(), "sudo")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := sudo
	sudo = stub
	defer func() { sudo = saved }()

	var out bytes.Buffer
	step := Step{Chown: []string{root}, Owner: "me", Modes: []string{root}}
	if err := Execute(context.Background(), prefix, step, &out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(log); strings.TrimSpace(string(got)) != "chown -R me "+root {
		t.Errorf("sudo ran with %q", got)
	}
	for path, want := range map[string]os.FileMode{
		mod: 0o755, filepath.Dir(mod): 0o755, // directories get u+rwx
		file:   0o444, // read-only files stay read-only
		secret: 0o600, // unreadable files get u+r
	} {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v (%v)", path, fi.Mode().Perm(), want, err)
		}
	}
	if !strings.Contains(out.String(), "Restored owner permissions on 3 entries") {
		t.Errorf("output:\n%s", out.String())
	}

	// Detached, sudo gets -n so it fails instead of prompting.
	_ = os.Remove(log)
	task := Task{Chown: []string{root}, Owner: "me"}
	if err := ExecuteTask(context.Background(), prefix, task, &out, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(log); strings.TrimSpace(string(got)) != "-n chown -R me "+root {
		t.Errorf("detached sudo ran with %q", got)
	}
}

func TestFixPermissionsRefusesOtherPaths(t *testing.T) {
	prefix := t.TempDir()
	for _, p := range []string{
		prefix,
		filepath.Join(prefix, "Cellar"),
		filepath.Join(prefix, "Cellar", "go", "1.25"),
		filepath.Join(prefix, "Cellar", ".."),
		filepath.Join(prefix, "bin"),
		"/etc",
	} {
		if err := Execute(context.Background(), prefix, Step{Modes: []string{p}}, &bytes.Buffer{}); err == nil ||
			!strings.Contains(err.Error(), "refusing") {
			t.Errorf("%s: err = %v", p, err)
		}
	}
	if err := Execute(context.Background(), prefix, Step{Chown: []string{filepath.Join(prefix, "Caskroom", "x")}}, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "which user") {
		t.Errorf("chown without an owner: %v", err)
	}
}
