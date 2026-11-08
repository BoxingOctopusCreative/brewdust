// brewdust reports how much disk space each Homebrew package uses and how
// long it has been since it was last used.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
	"github.com/BoxingOctopusCreative/brewdust/internal/report"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "brewdust:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		sortBy    = flag.String("sort", "size", "sort by: size, idle, name")
		kind      = flag.String("type", "all", "package type: all, formula, cask")
		staleDays = flag.Int("stale", 0, "only show packages idle for at least this many days")
		requested = flag.Bool("requested", false, "only show packages you installed explicitly (plus casks)")
		orphans   = flag.Bool("orphans", false, "only show dependencies nothing installed needs anymore")
		direct    = flag.Bool("direct", false, "judge idleness by each package's own use, ignoring its dependents")
		limit     = flag.Int("limit", 0, "show at most N packages (0 = all)")
		minSize   = flag.String("min-size", "", "only show packages at least this big, e.g. 50MB, 1.5GB")
		asJSON    = flag.Bool("json", false, "output JSON")
		workers   = flag.Int("workers", runtime.NumCPU()*2, "parallel directory scanners")
		showVer   = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: brewdust [flags]\n\nShows disk usage and time since last use for installed Homebrew packages.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVer {
		fmt.Println("brewdust", version)
		return nil
	}

	minBytes, err := parseSize(*minSize)
	if err != nil {
		return err
	}
	if *kind != "all" && *kind != analyze.KindFormula && *kind != analyze.KindCask {
		return fmt.Errorf("unknown -type %q", *kind)
	}

	ctx := context.Background()
	prefix, err := brew.Prefix(ctx)
	if err != nil {
		return err
	}
	info, err := brew.Load(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	all := analyze.Run(info, analyze.Options{
		Prefix:  prefix,
		Now:     now,
		Workers: *workers,
		Grace:   10 * time.Minute,
	})

	idle := func(p *analyze.Package) int {
		if *direct {
			return p.DirectIdleDays
		}
		return p.IdleDays
	}

	var shown []*analyze.Package
	for _, p := range all {
		switch {
		case *kind != "all" && p.Kind != *kind,
			*requested && !p.OnRequest,
			*orphans && !p.Orphan,
			idle(p) < *staleDays,
			p.SizeBytes < minBytes:
			continue
		}
		shown = append(shown, p)
	}

	switch *sortBy {
	case "size":
		sort.SliceStable(shown, func(i, j int) bool { return shown[i].SizeBytes > shown[j].SizeBytes })
	case "idle":
		sort.SliceStable(shown, func(i, j int) bool {
			if a, b := idle(shown[i]), idle(shown[j]); a != b {
				return a > b
			}
			return shown[i].SizeBytes > shown[j].SizeBytes
		})
	case "name":
		sort.SliceStable(shown, func(i, j int) bool { return shown[i].Name < shown[j].Name })
	default:
		return fmt.Errorf("unknown -sort %q", *sortBy)
	}
	if *limit > 0 && len(shown) > *limit {
		shown = shown[:*limit]
	}

	if *asJSON {
		return report.JSON(os.Stdout, shown, now)
	}
	if err := report.Table(os.Stdout, shown, *direct); err != nil {
		return err
	}
	report.Summary(os.Stdout, shown, all)
	return nil
}

// parseSize accepts plain bytes or a number with a k/M/G/T suffix (decimal).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "I")
	mult := 1.0
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'K':
			mult = 1e3
		case 'M':
			mult = 1e6
		case 'G':
			mult = 1e9
		case 'T':
			mult = 1e12
		}
		if mult != 1 {
			s = s[:n-1]
		}
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(v * mult), nil
}
