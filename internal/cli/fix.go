// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
)

func newFixCmd(ff *filterFlags) *cobra.Command {
	var rf runFlags
	var only string
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "Link unlinked kegs and repair package permissions",
		Long: `Fix two kinds of problems that stop packages working or brew managing them:

  unlinked     formulae that aren't keg-only but have no version linked into
               the prefix, so their commands aren't on PATH (brew link). brew
               refuses, and says why, if another package owns the same paths
  permissions  package files brew can't manage: anything owned by another
               user is given back to you (sudo chown -R, which asks for your
               password), then directories missing u+rwx and files missing u+r
               get them back, so brew can upgrade, clean up and uninstall

Only Cellar/<formula> and Caskroom/<cask> directories are changed. The plan is
shown first, and nothing runs until you confirm.`,
		Example: `  brewdust fix --dry-run
  brewdust fix --only permissions`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cats, err := actions.ParseFixCategories(only)
			if err != nil {
				return err
			}
			snap, err := load(cmd, ff)
			if err != nil {
				return err
			}
			return runSteps(cmd, snap.Prefix, actions.PlanCleanup(snap.Packages, cats), rf)
		},
	}
	rf.register(cmd)
	cmd.Flags().StringVar(&only, "only", "", "comma-separated fixes: unlinked, permissions (default all)")
	return cmd
}
