package cli

import (
	"github.com/spf13/cobra"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
)

func newCleanCmd(ff *filterFlags) *cobra.Command {
	var rf runFlags
	var only string
	cmd := &cobra.Command{
		Use:     "clean",
		Aliases: []string{"cleanup"},
		Short:   "Remove old versions, orphaned dependencies and broken casks",
		Long: `Clean up three kinds of leftovers:

  old      non-current formula versions (brew cleanup <formula>...) and stale
           Caskroom version directories, including interrupted upgrades,
           which brew cleanup doesn't remove
  orphans  dependencies nothing installed needs anymore (brew autoremove)
  broken   casks whose app was deleted outside brew (brew uninstall --cask)

The plan is shown first, and nothing runs until you confirm.`,
		Example: `  brewdust clean --dry-run
  brewdust clean --only orphans,broken
  brewdust clean -y`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cats, err := actions.ParseCategories(only)
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
	cmd.Flags().StringVar(&only, "only", "", "comma-separated categories: old, orphans, broken (default all)")
	return cmd
}
