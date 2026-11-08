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
		for _, d := range p.Dependents {
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

	if len(formulae) > 0 {
		args := []string{"uninstall", "--formula"}
		if opt.IgnoreDependencies {
			args = append(args, "--ignore-dependencies")
		}
		s := Step{Title: fmt.Sprintf("Uninstall %d formula(e)", len(formulae))}
		for _, p := range formulae {
			args = append(args, p.Name)
			s.Items = append(s.Items, p.Name)
			s.Bytes += p.SizeBytes
		}
		s.Brew = args
		r.Steps = append(r.Steps, s)
	}
	if len(casks) > 0 {
		args := []string{"uninstall", "--cask"}
		if opt.Zap {
			args = append(args, "--zap")
		}
		s := Step{Title: fmt.Sprintf("Uninstall %d cask(s)", len(casks))}
		for _, p := range casks {
			args = append(args, p.Name)
			s.Items = append(s.Items, p.Name)
			s.Bytes += p.SizeBytes
		}
		s.Brew = args
		r.Steps = append(r.Steps, s)
	}
	return r
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
			} else {
				rooms.Paths = append(rooms.Paths, p.OldPaths...)
				rooms.Items = append(rooms.Items, p.OldPaths...)
				rooms.Bytes += p.ReclaimableBytes
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
			}
		}
		if len(s.Items) > 0 {
			steps = append(steps, s)
		}
	}
	return steps
}

// Execute runs one step, streaming brew's output to out.
func Execute(ctx context.Context, prefix string, s Step, out io.Writer) error {
	if len(s.Brew) > 0 {
		cmd := brew.Command(ctx, s.Brew...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", s.Command(), err)
		}
		return nil
	}
	for _, p := range s.Paths {
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
