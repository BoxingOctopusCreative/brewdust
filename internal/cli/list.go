package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/BoxingOctopusCreative/brewdust/internal/app"
	"github.com/BoxingOctopusCreative/brewdust/internal/report"
)

type listFlags struct {
	json  bool
	limit int
}

func newListCmd(ff *filterFlags) *cobra.Command {
	lf := listFlags{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Print disk usage and last use for installed packages",
		Example: `  brewdust list --sort idle --stale 180
  brewdust list --requested --min-size 50MB
  brewdust list --json | jq '.packages[] | select(.idle_days > 365) | .name'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runList(cmd, ff, lf) },
	}
	cmd.Flags().BoolVar(&lf.json, "json", false, "output JSON")
	cmd.Flags().IntVarP(&lf.limit, "limit", "n", 0, "show at most N packages (0 = all)")
	return cmd
}

func runList(cmd *cobra.Command, ff *filterFlags, lf listFlags) error {
	f, err := ff.filter()
	if err != nil {
		return err
	}
	snap, err := load(cmd, ff)
	if err != nil {
		return err
	}
	shown := f.Apply(snap.Packages)
	if err := app.Sort(shown, ff.sortBy, f); err != nil {
		return err
	}
	if lf.limit > 0 && len(shown) > lf.limit {
		shown = shown[:lf.limit]
	}
	if lf.json {
		return report.JSON(os.Stdout, shown, snap.Now)
	}
	if err := report.Table(os.Stdout, shown, f.Direct); err != nil {
		return err
	}
	report.Summary(os.Stdout, shown, snap.Packages)
	return nil
}
