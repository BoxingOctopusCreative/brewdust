// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package actions plans and runs package removal and cleanup.
//
// Planning is pure: it turns a scan into a list of Steps, which the CLI prints
// for confirmation and the TUI shows in a dialog. Executing a step either runs
// a brew command or deletes Caskroom directories that brew itself leaves behind.
package actions

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
)

// Step is one unit of work.
type Step struct {
	Title    string
	Category Category // set for cleanup steps
	Items    []string // package names or paths affected, for display
	Bytes    int64    // estimated space freed
	Brew     []string // brew arguments, when the step is a brew command
	Paths    []string // directories to delete, when the step isn't
	Rmdir    bool     // Paths are empty directories, removed only if still empty
	Chown    []string // package roots to give back to Owner, with sudo
	Owner    string   // the user to chown to
	Modes    []string // package roots whose missing owner permissions to restore
	// Tasks is the same work split per package, in an order that runs
	// cleanly one at a time; the TUI uses it to show progress.
	Tasks []Task
}

// Command describes the step as a shell-like line.
func (s Step) Command() string {
	if len(s.Brew) > 0 {
		return "brew " + strings.Join(s.Brew, " ")
	}
	if len(s.Chown) > 0 {
		return "sudo chown -R " + s.Owner + " " + strings.Join(quoteAll(s.Chown), " ")
	}
	if len(s.Modes) > 0 {
		return "chmod u+rwx on directories and u+r on files missing them, under " + strings.Join(quoteAll(s.Modes), " ")
	}
	if s.Rmdir {
		return "rmdir " + strings.Join(quoteAll(s.Paths), " ")
	}
	return "rm -rf " + strings.Join(quoteAll(s.Paths), " ")
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		if strings.ContainsAny(s, " '\"") {
			s = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
		out[i] = s
	}
	return out
}

// TotalBytes sums the estimated space freed by steps.
func TotalBytes(steps []Step) int64 {
	var n int64
	for _, s := range steps {
		n += s.Bytes
	}
	return n
}

// RemoveOptions tunes PlanRemoval.
type RemoveOptions struct {
	Zap                bool // casks: also remove app support files, caches, prefs
	IgnoreDependencies bool // formulae: remove even if something still needs them
}

// Removal is the plan for uninstalling a chosen set of packages.
type Removal struct {
	Steps []Step
	// Blocked maps formulae that were left out to the installed packages,
	// outside the chosen set, that still depend on them.
	Blocked map[string][]string
}

// PlanRemoval builds the brew uninstall commands for pkgs.
func PlanRemoval(pkgs []*analyze.Package, opt RemoveOptions) Removal {
	chosen := map[string]bool{}
	for _, p := range pkgs {
		chosen[p.Name] = true
	}
	r := Removal{Blocked: map[string][]string{}}
	var formulae, casks []*analyze.Package
	for _, p := range pkgs {
		if p.Kind == analyze.KindCask {
			casks = append(casks, p)
			continue
		}
		var blockers []string
		for _, d := range p.AllDependents { // indirect ones break too
			if !chosen[d] {
				blockers = append(blockers, d)
			}
		}
		if len(blockers) > 0 && !opt.IgnoreDependencies {
			r.Blocked[p.Name] = blockers
			continue
		}
		formulae = append(formulae, p)
	}

	// Removed one at a time, a formula must go after everything chosen that
	// depends on it, or brew refuses. A formula's chosen dependents are a
	// strict superset of each of theirs, so fewer-first is a valid order.
	inSet := func(p *analyze.Package) int {
		n := 0
		for _, d := range p.AllDependents {
			if chosen[d] {
				n++
			}
		}
		return n
	}
	sort.SliceStable(formulae, func(i, j int) bool { return inSet(formulae[i]) < inSet(formulae[j]) })

	// Casks first: they can depend on formulae, never the other way round.
	if len(casks) > 0 {
		flags := []string{"uninstall", "--cask"}
		if opt.Zap {
			flags = append(flags, "--zap")
		}
		r.Steps = append(r.Steps, uninstallStep(fmt.Sprintf("Uninstall %d cask(s)", len(casks)), flags, casks))
	}
	if len(formulae) > 0 {
		flags := []string{"uninstall", "--formula"}
		if opt.IgnoreDependencies {
			flags = append(flags, "--ignore-dependencies")
		}
		r.Steps = append(r.Steps, uninstallStep(fmt.Sprintf("Uninstall %d formula(e)", len(formulae)), flags, formulae))
	}
	return r
}

func uninstallStep(title string, flags []string, pkgs []*analyze.Package) Step {
	s := Step{Title: title, Brew: append([]string(nil), flags...)}
	for _, p := range pkgs {
		s.Brew = append(s.Brew, p.Name)
		s.Items = append(s.Items, p.Name)
		s.Bytes += p.SizeBytes
		s.Tasks = append(s.Tasks, Task{
			Label: p.Name, Action: "Uninstalling", Bytes: p.SizeBytes,
			Brew: append(append([]string(nil), flags...), p.Name),
		})
	}
	return s
}

// Category is a kind of cleanup.
type Category string

const (
	OldVersions Category = "old"       // non-current formula kegs and stale Caskroom versions
	Orphans     Category = "orphans"   // dependencies nothing installed needs
	Broken      Category = "broken"    // casks whose app was deleted outside brew
	NoFormula   Category = "noformula" // kegs whose formula brew can't find

	// Fixes, which `brewdust fix` offers rather than `clean`.
	Unlinked    Category = "unlinked"    // formulae that should be linked but aren't
	Permissions Category = "permissions" // package files brew can't manage
)

// Categories lists every cleanup category in the order they run.
var Categories = []Category{OldVersions, Orphans, Broken, NoFormula}

// FixCategories lists every kind of fix in the order they run.
var FixCategories = []Category{Unlinked, Permissions}

// IsFix reports whether c is a fix, which repairs rather than frees space.
func (c Category) IsFix() bool { return slices.Contains(FixCategories, c) }

// Describe returns a human-readable label.
func (c Category) Describe() string {
	switch c {
	case OldVersions:
		return "Old versions"
	case Orphans:
		return "Orphaned dependencies"
	case Broken:
		return "Casks whose app was deleted outside brew"
	case NoFormula:
		return "Kegs with no formula"
	case Unlinked:
		return "Unlinked kegs"
	case Permissions:
		return "Broken permissions"
	}
	return string(c)
}

// ParseCategories accepts a comma-separated list of cleanup categories;
// empty means all.
func ParseCategories(s string) ([]Category, error) {
	return parseCategories(s, Categories, "cleanup category")
}

// ParseFixCategories is ParseCategories for fixes.
func ParseFixCategories(s string) ([]Category, error) {
	return parseCategories(s, FixCategories, "fix")
}

func parseCategories(s string, all []Category, what string) ([]Category, error) {
	if strings.TrimSpace(s) == "" {
		return all, nil
	}
	var out []Category
	for _, part := range strings.Split(s, ",") {
		c := Category(strings.TrimSpace(part))
		if !slices.Contains(all, c) {
			names := make([]string, len(all))
			for i, a := range all {
				names[i] = string(a)
			}
			return nil, fmt.Errorf("unknown %s %q (want %s)", what, part, strings.Join(names, ", "))
		}
		out = append(out, c)
	}
	return out, nil
}

// PlanCleanup builds the steps for the chosen categories, cleanups and
// fixes alike.
func PlanCleanup(pkgs []*analyze.Package, cats []Category) []Step {
	want := map[Category]bool{}
	for _, c := range cats {
		want[c] = true
	}
	sorted := append([]*analyze.Package(nil), pkgs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var steps []Step
	if want[OldVersions] {
		kegs := Step{Category: OldVersions, Title: "Remove old formula versions", Brew: []string{"cleanup"}}
		rooms := Step{Category: OldVersions, Title: "Remove stale Caskroom versions (brew cleanup skips these)"}
		for _, p := range sorted {
			if p.OldVersions == 0 {
				continue
			}
			if p.Kind == analyze.KindFormula {
				kegs.Brew = append(kegs.Brew, p.Name)
				kegs.Items = append(kegs.Items, p.Name)
				kegs.Bytes += p.ReclaimableBytes
				kegs.Tasks = append(kegs.Tasks, Task{Label: p.Name, Action: "Removing old versions of",
					Bytes: p.ReclaimableBytes, Brew: []string{"cleanup", p.Name}})
			} else {
				rooms.Paths = append(rooms.Paths, p.OldPaths...)
				rooms.Items = append(rooms.Items, p.OldPaths...)
				rooms.Bytes += p.ReclaimableBytes
				rooms.Tasks = append(rooms.Tasks, Task{Label: p.Name, Action: "Removing stale Caskroom versions of",
					Bytes: p.ReclaimableBytes, Paths: p.OldPaths})
			}
		}
		if len(kegs.Items) > 0 {
			steps = append(steps, kegs)
		}
		if len(rooms.Paths) > 0 {
			steps = append(steps, rooms)
		}
	}
	if want[Orphans] {
		s := Step{Category: Orphans, Title: "Remove orphaned dependencies", Brew: []string{"autoremove"}}
		for _, p := range sorted {
			if p.Orphan {
				s.Items = append(s.Items, p.Name)
				s.Bytes += p.SizeBytes
			}
		}
		if len(s.Items) > 0 {
			s.Tasks = []Task{{Label: fmt.Sprintf("%d orphaned dependencies", len(s.Items)), Action: "Removing",
				Bytes: s.Bytes, Brew: []string{"autoremove"}}}
			steps = append(steps, s)
		}
	}
	if want[Broken] {
		s := Step{Category: Broken, Title: "Uninstall casks whose app is already gone", Brew: []string{"uninstall", "--cask"}}
		for _, p := range sorted {
			if p.Broken() {
				s.Brew = append(s.Brew, p.Name)
				s.Items = append(s.Items, p.Name)
				s.Bytes += p.SizeBytes - p.ReclaimableBytes // leftovers counted under old versions
				s.Tasks = append(s.Tasks, Task{Label: p.Name, Action: "Uninstalling",
					Bytes: p.SizeBytes - p.ReclaimableBytes, Brew: []string{"uninstall", "--cask", p.Name}})
			}
		}
		if len(s.Items) > 0 {
			steps = append(steps, s)
		}
	}
	if want[NoFormula] {
		// brew uninstall finds kegs by their Cellar directory, so it works
		// without the formula. Kegs something installed still needs are left
		// alone, and empty directories, which brew can't uninstall, are
		// deleted directly.
		kegs := Step{Category: NoFormula, Title: "Uninstall kegs whose formula is gone", Brew: []string{"uninstall", "--formula"}}
		empty := Step{Category: NoFormula, Title: "Remove empty Cellar directories", Rmdir: true}
		for _, p := range sorted {
			switch {
			case !p.NoFormula || len(p.AllDependents) > 0:
			case p.EmptyKeg():
				empty.Paths = append(empty.Paths, p.Root)
				empty.Items = append(empty.Items, p.Root)
				empty.Tasks = append(empty.Tasks, Task{Label: p.Name, Action: "Removing the empty Cellar directory of",
					Paths: []string{p.Root}, Rmdir: true})
			default:
				kegs.Brew = append(kegs.Brew, p.Name)
				kegs.Items = append(kegs.Items, p.Name)
				kegs.Bytes += p.SizeBytes
				kegs.Tasks = append(kegs.Tasks, Task{Label: p.Name, Action: "Uninstalling",
					Bytes: p.SizeBytes, Brew: []string{"uninstall", "--formula", p.Name}})
			}
		}
		if len(kegs.Items) > 0 {
			steps = append(steps, kegs)
		}
		if len(empty.Paths) > 0 {
			steps = append(steps, empty)
		}
	}
	if want[Unlinked] {
		// brew link refuses, and says why, when another package already owns
		// a path; brewdust doesn't pass --overwrite.
		s := Step{Category: Unlinked, Title: "Link kegs into the prefix", Brew: []string{"link", "--formula"}}
		for _, p := range sorted {
			if p.Unlinked {
				s.Brew = append(s.Brew, p.Name)
				s.Items = append(s.Items, p.Name)
				s.Tasks = append(s.Tasks, Task{Label: p.Name, Action: "Linking", Brew: []string{"link", "--formula", p.Name}})
			}
		}
		if len(s.Items) > 0 {
			steps = append(steps, s)
		}
	}
	if want[Permissions] {
		// Ownership first: until a package is the user's again, its modes
		// can't be changed without sudo.
		chown := Step{Category: Permissions, Title: "Give packages back to " + owner + " (asks for your password)", Owner: owner}
		modes := Step{Category: Permissions, Title: "Restore missing owner permissions"}
		for _, p := range sorted {
			if !p.BadPermissions() || p.Root == "" {
				continue
			}
			if p.ForeignOwned+p.DeniedPaths > 0 {
				chown.Chown = append(chown.Chown, p.Root)
				chown.Items = append(chown.Items, fmt.Sprintf("%s (%d entries)", p.Name, p.ForeignOwned+p.DeniedPaths))
				chown.Tasks = append(chown.Tasks, Task{Label: p.Name, Action: "Taking back ownership of",
					Chown: []string{p.Root}, Owner: owner})
			}
			modes.Modes = append(modes.Modes, p.Root)
			modes.Items = append(modes.Items, fmt.Sprintf("%s (%d entries)", p.Name, p.ForeignOwned+p.LockedPaths+p.DeniedPaths))
			modes.Tasks = append(modes.Tasks, Task{Label: p.Name, Action: "Restoring permissions of", Modes: []string{p.Root}})
		}
		if len(chown.Chown) > 0 {
			steps = append(steps, chown)
		}
		if len(modes.Modes) > 0 {
			steps = append(steps, modes)
		}
	}
	return steps
}

// Execute runs one step in the foreground, streaming brew's output to out
// and letting it prompt on the terminal.
func Execute(ctx context.Context, prefix string, s Step, out io.Writer) error {
	return run(ctx, prefix, s, out, false)
}

// ExecuteTask runs one task. With detached set, brew gets no terminal: no
// stdin and a new session, so a sudo password prompt fails with an error
// (see NeedsTerminal) instead of writing over the caller's screen.
func ExecuteTask(ctx context.Context, prefix string, t Task, out io.Writer, detached bool) error {
	return run(ctx, prefix, t.work(), out, detached)
}

func run(ctx context.Context, prefix string, s Step, out io.Writer, detached bool) error {
	brewArgs, paths := s.Brew, s.Paths
	if len(s.Chown) > 0 || len(s.Modes) > 0 {
		return fixPermissions(ctx, prefix, s, out, detached)
	}
	if len(brewArgs) > 0 {
		cmd := brew.Command(ctx, brewArgs...)
		cmd.Stdout = out
		cmd.Stderr = out
		if detached {
			cmd.Stdin = nil
			cmd.Env = append(cmd.Env, "NONINTERACTIVE=1", "HOMEBREW_NO_COLOR=1")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		} else {
			cmd.Stdin = os.Stdin
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("brew %s: %w", strings.Join(brewArgs, " "), err)
		}
		return nil
	}
	for _, p := range paths {
		if isEmptyRack(prefix, p) {
			// os.Remove only deletes an empty directory, so a keg installed
			// since the scan is never lost.
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("removing %s: %w", p, err)
			}
			fmt.Fprintf(out, "Removed %s\n", p)
			continue
		}
		if err := checkCaskroomPath(prefix, p); err != nil {
			return err
		}
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		fmt.Fprintf(out, "Removed %s\n", p)
	}
	return nil
}

// isEmptyRack reports whether p is <prefix>/Cellar/<name>, the only kind of
// Cellar path a step deletes itself.
func isEmptyRack(prefix, p string) bool {
	rel, err := filepath.Rel(filepath.Join(prefix, "Cellar"), filepath.Clean(p))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") && !strings.Contains(rel, "/") && !strings.HasPrefix(rel, ".")
}

// checkCaskroomPath refuses to delete anything but <prefix>/Caskroom/<token>/<version>.
func checkCaskroomPath(prefix, p string) error {
	room := filepath.Join(prefix, "Caskroom")
	rel, err := filepath.Rel(room, filepath.Clean(p))
	if err != nil {
		return fmt.Errorf("refusing to delete %s: %w", p, err)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || parts[0] == ".." || parts[0] == "." || parts[1] == ".." || parts[1] == ".metadata" {
		return fmt.Errorf("refusing to delete %s: not a version directory under %s", p, room)
	}
	return nil
}

// Task is one package's share of a step.
type Task struct {
	Label  string // what it acts on, e.g. "qemu"
	Action string // e.g. "Uninstalling"
	Bytes  int64  // estimated space freed
	Brew   []string
	Paths  []string
	Rmdir  bool
	Chown  []string
	Owner  string
	Modes  []string
}

// Command describes the task as a shell-like line.
func (t Task) Command() string {
	return t.work().Command()
}

// AsStep wraps a task so it can run through Execute.
func (t Task) AsStep() Step {
	s := t.work()
	s.Title, s.Items, s.Bytes = t.Action+" "+t.Label, []string{t.Label}, t.Bytes
	return s
}

// AllTasks flattens the steps' tasks in order.
func AllTasks(steps []Step) []Task {
	var out []Task
	for _, s := range steps {
		out = append(out, s.Tasks...)
	}
	return out
}

// NeedsTerminal reports whether brew output shows a step failed only because
// it had no terminal to ask for a password on.
func NeedsTerminal(output string) bool {
	o := strings.ToLower(output)
	for _, sign := range []string{
		"a terminal is required", "no tty present", "a password is required",
		"askpass", "sudo: ", "must be run interactively",
	} {
		if strings.Contains(o, sign) {
			return true
		}
	}
	return false
}

// work is the task as a step, for describing and running it.
func (t Task) work() Step {
	return Step{Brew: t.Brew, Paths: t.Paths, Rmdir: t.Rmdir, Chown: t.Chown, Owner: t.Owner, Modes: t.Modes}
}
