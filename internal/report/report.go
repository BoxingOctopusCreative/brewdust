// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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

// LastUsed formats the date a package was last used.
func LastUsed(p *analyze.Package) string {
	if p.LastUsed == nil {
		return "not since install"
	}
	return p.LastUsed.Local().Format("2006-01-02")
}

// NeededBy names what directly depends on a package: "—", "rust", or
// "qtgrpc +36" when there are several.
func NeededBy(p *analyze.Package) string {
	switch len(p.Dependents) {
	case 0:
		return "—"
	case 1:
		return p.Dependents[0]
	default:
		return fmt.Sprintf("%s +%d", p.Dependents[0], len(p.Dependents)-1)
	}
}

// Notes summarises status flags for a package.
func Notes(p *analyze.Package, direct bool) string {
	var n []string
	if p.Kind == analyze.KindFormula {
		if p.OnRequest {
			n = append(n, "requested")
		} else if p.Orphan {
			n = append(n, "orphan (brew autoremove)")
		} else {
			n = append(n, "dependency")
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
	if p.HeldVersions > 0 {
		why := "until upgraded"
		if p.HeldReason == "pinned" {
			why = "while pinned"
		}
		n = append(n, fmt.Sprintf("+%d old version(s), %s kept by brew cleanup %s", p.HeldVersions, Bytes(p.HeldBytes), why))
	}
	if len(p.MissingApps) > 0 {
		n = append(n, "app deleted outside brew")
	}
	return strings.Join(n, "; ")
}

// Table writes a human-readable report.
func Table(w io.Writer, pkgs []*analyze.Package, direct bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tSIZE\tINSTALLED\tLAST USED\tIDLE\tNEEDED BY\tNOTES")
	for _, p := range pkgs {
		idle := p.IdleDays
		if direct {
			idle = p.DirectIdleDays
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Name, p.Kind, Bytes(p.SizeBytes),
			p.InstalledAt.Local().Format("2006-01-02"),
			LastUsed(p), Idle(idle), NeededBy(p), Notes(p, direct))
	}
	return tw.Flush()
}

// Summary writes totals for the listed packages.
func Summary(w io.Writer, shown, all []*analyze.Package) {
	var shownBytes, allBytes, reclaim, orphanBytes, held int64
	orphans, heldPkgs := 0, 0
	for _, p := range shown {
		shownBytes += p.SizeBytes
	}
	for _, p := range all {
		allBytes += p.SizeBytes
		reclaim += p.ReclaimableBytes
		if p.HeldVersions > 0 {
			held += p.HeldBytes
			heldPkgs++
		}
		if p.Orphan {
			orphans++
			orphanBytes += p.SizeBytes
		}
	}
	fmt.Fprintf(w, "\n%d of %d packages shown, %s of %s total.\n", len(shown), len(all), Bytes(shownBytes), Bytes(allBytes))
	if reclaim > 0 {
		fmt.Fprintf(w, "Old versions: %s reclaimable.\n", Bytes(reclaim))
	}
	if held > 0 {
		fmt.Fprintf(w, "Old versions brew cleanup won't remove yet: %s in %d outdated or pinned formula(e) (`brew upgrade` first).\n",
			Bytes(held), heldPkgs)
	}
	broken := 0
	for _, p := range all {
		if p.Broken() {
			broken++
		}
	}
	if broken > 0 {
		fmt.Fprintf(w, "Casks whose app was deleted outside brew: %d.\n", broken)
	}
	if reclaim > 0 || orphans > 0 || broken > 0 {
		defer fmt.Fprintln(w, "Run `brewdust clean` to review and remove these.")
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
