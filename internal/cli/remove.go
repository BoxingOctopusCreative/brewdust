// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/report"
)

type runFlags struct {
	yes    bool
	dryRun bool
}

func (r *runFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&r.yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVarP(&r.dryRun, "dry-run", "n", false, "show what would be done without doing it")
}

func newRemoveCmd(ff *filterFlags) *cobra.Command {
	var rf runFlags
	var opt actions.RemoveOptions
	var asCask, asFormula bool
	cmd := &cobra.Command{
		Use:     "remove PACKAGE...",
		Aliases: []string{"rm", "uninstall"},
		Short:   "Uninstall formulae and casks",
		Long: `Uninstall the named formulae and casks with brew uninstall.

Formulae that other installed packages still depend on are skipped (and
listed) unless you remove those dependents too or pass --ignore-dependencies.`,
		Example: `  brewdust remove qemu mono
  brewdust remove --cask calibre --zap
  brewdust remove -n gcc`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if asCask && asFormula {
				return fmt.Errorf("--cask and --formula are mutually exclusive")
			}
			kind := ""
			if asCask {
				kind = analyze.KindCask
			} else if asFormula {
				kind = analyze.KindFormula
			}
			snap, err := load(cmd, ff)
			if err != nil {
				return err
			}
			var pkgs []*analyze.Package
			var missing []string
			for _, name := range args {
				if p := snap.Find(name, kind); p != nil {
					pkgs = append(pkgs, p)
				} else {
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("not installed: %s", strings.Join(missing, ", "))
			}

			plan := actions.PlanRemoval(pkgs, opt)
			printBlocked(cmd.OutOrStdout(), plan.Blocked)
			return runSteps(cmd, snap.Prefix, plan.Steps, rf)
		},
	}
	rf.register(cmd)
	cmd.Flags().BoolVar(&opt.Zap, "zap", false, "casks: also remove settings, caches and support files")
	cmd.Flags().BoolVar(&opt.IgnoreDependencies, "ignore-dependencies", false, "formulae: remove even if other packages depend on them")
	cmd.Flags().BoolVar(&asCask, "cask", false, "treat all names as casks")
	cmd.Flags().BoolVar(&asFormula, "formula", false, "treat all names as formulae")
	return cmd
}

func printBlocked(w io.Writer, blocked map[string][]string) {
	if len(blocked) == 0 {
		return
	}
	names := make([]string, 0, len(blocked))
	for n := range blocked {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "Skipping formulae that other packages still need:")
	for _, n := range names {
		fmt.Fprintf(w, "  %s (needed by %s)\n", n, strings.Join(blocked[n], ", "))
	}
	fmt.Fprintln(w)
}

// printPlan lists the steps and what they will free.
func printPlan(w io.Writer, steps []actions.Step) {
	for _, s := range steps {
		fmt.Fprintf(w, "%s — frees ~%s\n", s.Title, report.Bytes(s.Bytes))
		for _, it := range s.Items {
			fmt.Fprintf(w, "    %s\n", it)
		}
		fmt.Fprintf(w, "  $ %s\n\n", s.Command())
	}
	fmt.Fprintf(w, "Total: ~%s\n", report.Bytes(actions.TotalBytes(steps)))
}

func runSteps(cmd *cobra.Command, prefix string, steps []actions.Step, rf runFlags) error {
	out := cmd.OutOrStdout()
	if len(steps) == 0 {
		fmt.Fprintln(out, "Nothing to do.")
		return nil
	}
	printPlan(out, steps)
	if rf.dryRun {
		fmt.Fprintln(out, "Dry run: nothing was changed.")
		return nil
	}
	if !rf.yes {
		ok, err := confirm(cmd.InOrStdin(), out, "Proceed?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	}
	var failed []string
	for _, s := range steps {
		fmt.Fprintf(out, "\n==> %s\n", s.Title)
		if err := actions.Execute(cmd.Context(), prefix, s, out); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
			failed = append(failed, s.Title)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d step(s) failed: %s", len(failed), strings.Join(failed, "; "))
	}
	return nil
}
