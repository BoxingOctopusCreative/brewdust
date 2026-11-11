// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package cli defines brewdust's command-line interface.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/BoxingOctopusCreative/brewdust/internal/app"
	"github.com/BoxingOctopusCreative/brewdust/internal/tui"
)

// filterFlags are shared by the TUI (as its starting view) and `list`.
type filterFlags struct {
	sortBy    string
	kind      string
	staleDays int
	requested bool
	orphans   bool
	broken    bool
	retiring  bool
	noFormula bool
	unlinked  bool
	badPerms  bool
	needed    bool
	direct    bool
	minSize   string
	workers   int
}

func (f *filterFlags) register(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.sortBy, "sort", "size", "sort by: size, idle, name, deps (most depended-on first)")
	pf.StringVarP(&f.kind, "type", "t", "all", "package type: all, formula, cask")
	pf.IntVar(&f.staleDays, "stale", 0, "only packages idle for at least this many days")
	pf.BoolVar(&f.requested, "requested", false, "only packages you installed explicitly (plus casks)")
	pf.BoolVar(&f.orphans, "orphans", false, "only dependencies nothing installed needs anymore")
	pf.BoolVar(&f.broken, "broken", false, "only casks whose app was deleted outside brew")
	pf.BoolVar(&f.retiring, "deprecated", false, "only formulae and casks brew has deprecated or disabled")
	pf.BoolVar(&f.noFormula, "no-formula", false, "only kegs whose formula brew can't find anymore")
	pf.BoolVar(&f.unlinked, "unlinked", false, "only formulae that aren't keg-only but aren't linked")
	pf.BoolVar(&f.badPerms, "bad-permissions", false, "only packages with files brew can't manage (wrong owner or missing permissions)")
	pf.BoolVar(&f.needed, "deps", false, "only packages that other installed packages depend on")
	pf.BoolVar(&f.direct, "direct", false, "judge idleness by each package's own use, ignoring its dependents")
	pf.StringVar(&f.minSize, "min-size", "", "only packages at least this big, e.g. 50MB, 1.5GB")
	pf.IntVar(&f.workers, "workers", runtime.NumCPU()*2, "parallel directory scanners")
}

func (f *filterFlags) filter() (app.Filter, error) {
	min, err := app.ParseSize(f.minSize)
	if err != nil {
		return app.Filter{}, err
	}
	switch f.kind {
	case "all", "formula", "cask":
	default:
		return app.Filter{}, fmt.Errorf("unknown --type %q (want all, formula, cask)", f.kind)
	}
	ok := false
	for _, k := range app.SortKeys {
		ok = ok || k == f.sortBy
	}
	if !ok {
		return app.Filter{}, fmt.Errorf("unknown --sort %q (want %s)", f.sortBy, strings.Join(app.SortKeys, ", "))
	}
	return app.Filter{
		Kind:      f.kind,
		Requested: f.requested,
		Orphans:   f.orphans,
		Broken:    f.broken,
		Retiring:  f.retiring,
		NoFormula: f.noFormula,
		Unlinked:  f.unlinked,
		BadPerms:  f.badPerms,
		Needed:    f.needed,
		StaleDays: f.staleDays,
		MinBytes:  min,
		Direct:    f.direct,
	}, nil
}

// Execute runs the CLI and returns the process exit code.
func Execute(version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := newRoot(version)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "brewdust:", err)
		return 1
	}
	return 0
}

func newRoot(version string) *cobra.Command {
	ff := &filterFlags{}
	root := &cobra.Command{
		Use:   "brewdust",
		Short: "Find, inspect and clean up Homebrew packages gathering dust",
		Long: `brewdust shows how much disk each installed Homebrew formula and cask uses
and how long it has been since you used it, and helps you remove what you
don't need.

Run with no command for the interactive interface, or use a subcommand for
scripting. When output isn't a terminal, brewdust prints the list instead.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isInteractive() {
				return runList(cmd, ff, listFlags{})
			}
			return runTUI(cmd, ff)
		},
	}
	ff.register(root)

	root.AddCommand(
		&cobra.Command{
			Use:   "tui",
			Short: "Open the interactive interface (the default)",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return runTUI(cmd, ff) },
		},
		newListCmd(ff),
		newRemoveCmd(ff),
		newCleanCmd(ff),
		newFixCmd(ff),
	)
	return root
}

func runTUI(cmd *cobra.Command, ff *filterFlags) error {
	f, err := ff.filter()
	if err != nil {
		return err
	}
	if !isInteractive() {
		return fmt.Errorf("the interactive interface needs a terminal; try `brewdust list`")
	}
	return tui.Run(cmd.Context(), tui.Options{Filter: f, SortBy: ff.sortBy, Workers: ff.workers})
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func load(cmd *cobra.Command, ff *filterFlags) (*app.Snapshot, error) {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprint(os.Stderr, "Scanning Homebrew packages…\r")
		defer fmt.Fprint(os.Stderr, "\033[K")
	}
	return app.Load(cmd.Context(), ff.workers)
}

// confirm asks a yes/no question on the terminal. Without a terminal it
// refuses, so scripts must pass --yes explicitly.
func confirm(in io.Reader, out io.Writer, question string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("not a terminal; pass --yes to proceed without confirmation")
	}
	fmt.Fprintf(out, "%s [y/N] ", question)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}
