// Package report renders analysed packages as a table or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
)

// Bytes formats a size with decimal units, matching Finder.
func Bytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

// Idle formats a number of days compactly.
func Idle(days int) string {
	switch {
	case days < 1:
		return "today"
	case days < 60:
		return fmt.Sprintf("%dd", days)
	case days < 730:
		return fmt.Sprintf("%dmo", days/30)
	default:
		return fmt.Sprintf("%.1fy", float64(days)/365)
	}
}

func lastUsed(p *analyze.Package) string {
	if p.LastUsed == nil {
		return "not since install"
	}
	return p.LastUsed.Local().Format("2006-01-02")
}

func notes(p *analyze.Package, direct bool) string {
	var n []string
	if p.Kind == analyze.KindFormula {
		if p.OnRequest {
			n = append(n, "requested")
		} else if p.Orphan {
			n = append(n, "orphan (brew autoremove)")
		} else {
			n = append(n, fmt.Sprintf("dep of %d", len(p.Dependents)))
		}
	}
	if !direct && p.ActiveVia != "" {
		via := p.ActiveVia
		if parts := strings.Split(via, " → "); len(parts) > 2 {
			via = parts[0] + " → … → " + parts[len(parts)-1]
		}
		n = append(n, "active via "+via)
	}
	if p.OldVersions > 0 {
		n = append(n, fmt.Sprintf("+%d old version(s), %s reclaimable", p.OldVersions, Bytes(p.ReclaimableBytes)))
	}
	if len(p.MissingApps) > 0 {
		n = append(n, "app deleted outside brew")
	}
	return strings.Join(n, "; ")
}

// Table writes a human-readable report.
func Table(w io.Writer, pkgs []*analyze.Package, direct bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tSIZE\tINSTALLED\tLAST USED\tIDLE\tNOTES")
	for _, p := range pkgs {
		idle := p.IdleDays
		if direct {
			idle = p.DirectIdleDays
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Name, p.Kind, Bytes(p.SizeBytes),
			p.InstalledAt.Local().Format("2006-01-02"),
			lastUsed(p), Idle(idle), notes(p, direct))
	}
	return tw.Flush()
}

// Summary writes totals for the listed packages.
func Summary(w io.Writer, shown, all []*analyze.Package) {
	var shownBytes, allBytes, reclaim, orphanBytes int64
	orphans := 0
	for _, p := range shown {
		shownBytes += p.SizeBytes
	}
	for _, p := range all {
		allBytes += p.SizeBytes
		reclaim += p.ReclaimableBytes
		if p.Orphan {
			orphans++
			orphanBytes += p.SizeBytes
		}
	}
	fmt.Fprintf(w, "\n%d of %d packages shown, %s of %s total.\n", len(shown), len(all), Bytes(shownBytes), Bytes(allBytes))
	if reclaim > 0 {
		fmt.Fprintf(w, "Old versions: %s reclaimable with `brew cleanup`.\n", Bytes(reclaim))
	}
	if orphans > 0 {
		fmt.Fprintf(w, "Orphaned dependencies: %d using %s (see `brew autoremove --dry-run`).\n", orphans, Bytes(orphanBytes))
	}
	fmt.Fprintln(w, "Last-used dates are inferred from file access times and Spotlight; see README for caveats.")
}

// JSON writes the packages plus generation metadata.
func JSON(w io.Writer, pkgs []*analyze.Package, now time.Time) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		GeneratedAt time.Time          `json:"generated_at"`
		Packages    []*analyze.Package `json:"packages"`
	}{now, pkgs})
}
