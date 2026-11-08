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
	// Tasks is the same work split per package, in an order that runs
	// cleanly one at a time; the TUI uses it to show progress.
	Tasks []Task
}

// Command describes the step as a shell-like line.
func (s Step) Command() string {
	if len(s.Brew) > 0 {
		return "brew " + strings.Join(s.Brew, " ")
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
	OldVersions Category = "old"     // non-current formula kegs and stale Caskroom versions
	Orphans     Category = "orphans" // dependencies nothing installed needs
	Broken      Category = "broken"  // casks whose app was deleted outside brew
)

// Categories lists every cleanup category in the order they run.
var Categories = []Category{OldVersions, Orphans, Broken}

// Describe returns a human-readable label.
func (c Category) Describe() string {
	switch c {
	case OldVersions:
		return "Old versions"
	case Orphans:
		return "Orphaned dependencies"
	case Broken:
		return "Casks whose app was deleted outside brew"
	}
	return string(c)
}

// ParseCategories accepts a comma-separated list; empty means all.
func ParseCategories(s string) ([]Category, error) {
	if strings.TrimSpace(s) == "" {
		return Categories, nil
	}
	var out []Category
	for _, part := range strings.Split(s, ",") {
		c := Category(strings.TrimSpace(part))
		switch c {
		case OldVersions, Orphans, Broken:
			out = append(out, c)
		default:
			return nil, fmt.Errorf("unknown cleanup category %q (want old, orphans, broken)", part)
		}
	}
	return out, nil
}

// PlanCleanup builds the steps for the chosen categories.
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
	return steps
}

// Execute runs one step in the foreground, streaming brew's output to out
// and letting it prompt on the terminal.
func Execute(ctx context.Context, prefix string, s Step, out io.Writer) error {
	return run(ctx, prefix, s.Brew, s.Paths, out, false)
}

// ExecuteTask runs one task. With detached set, brew gets no terminal: no
// stdin and a new session, so a sudo password prompt fails with an error
// (see NeedsTerminal) instead of writing over the caller's screen.
func ExecuteTask(ctx context.Context, prefix string, t Task, out io.Writer, detached bool) error {
	return run(ctx, prefix, t.Brew, t.Paths, out, detached)
}

func run(ctx context.Context, prefix string, brewArgs, paths []string, out io.Writer, detached bool) error {
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
}

// Command describes the task as a shell-like line.
func (t Task) Command() string {
	return Step{Brew: t.Brew, Paths: t.Paths}.Command()
}

// AsStep wraps a task so it can run through Execute.
func (t Task) AsStep() Step {
	return Step{Title: t.Action + " " + t.Label, Items: []string{t.Label}, Bytes: t.Bytes, Brew: t.Brew, Paths: t.Paths}
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
