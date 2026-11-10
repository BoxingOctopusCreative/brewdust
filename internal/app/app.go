// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package app ties loading, filtering and sorting together for the CLI and TUI.
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
)

// Snapshot is one full scan of the Homebrew installation.
type Snapshot struct {
	Prefix   string
	Now      time.Time
	Packages []*analyze.Package
}

// Load asks brew for the installed packages and scans them.
func Load(ctx context.Context, workers int) (*Snapshot, error) {
	prefix, err := brew.Prefix(ctx)
	if err != nil {
		return nil, err
	}
	info, err := brew.Load(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	pkgs := analyze.Run(info, analyze.Options{
		Prefix:  prefix,
		Now:     now,
		Workers: workers,
		Grace:   10 * time.Minute,
	})
	return &Snapshot{Prefix: prefix, Now: now, Packages: pkgs}, nil
}

// Find returns the named package, preferring a formula when a formula and a
// cask share a name unless kind is given.
func (s *Snapshot) Find(name, kind string) *analyze.Package {
	var found *analyze.Package
	for _, p := range s.Packages {
		if p.Name != name || (kind != "" && p.Kind != kind) {
			continue
		}
		if found == nil || p.Kind == analyze.KindFormula {
			found = p
		}
	}
	return found
}

// Filter selects packages.
type Filter struct {
	Kind      string // "", "all", "formula" or "cask"
	Requested bool
	Orphans   bool
	Broken    bool
	Needed    bool // only packages something installed directly depends on
	StaleDays int
	MinBytes  int64
	Direct    bool
	Query     string // case-insensitive substring of name or description
}

// Idle returns the idle days the filter judges by.
func (f Filter) Idle(p *analyze.Package) int {
	if f.Direct {
		return p.DirectIdleDays
	}
	return p.IdleDays
}

func (f Filter) Match(p *analyze.Package) bool {
	switch {
	case f.Kind != "" && f.Kind != "all" && p.Kind != f.Kind,
		f.Requested && !p.OnRequest,
		f.Orphans && !p.Orphan,
		f.Broken && !p.Broken(),
		f.Needed && len(p.Dependents) == 0,
		f.Idle(p) < f.StaleDays,
		p.SizeBytes < f.MinBytes:
		return false
	}
	if q := strings.ToLower(f.Query); q != "" {
		return strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Desc), q)
	}
	return true
}

// Apply returns the matching packages, in their original order.
func (f Filter) Apply(pkgs []*analyze.Package) []*analyze.Package {
	var out []*analyze.Package
	for _, p := range pkgs {
		if f.Match(p) {
			out = append(out, p)
		}
	}
	return out
}

// SortKeys lists the accepted sort orders.
var SortKeys = []string{"size", "idle", "name", "deps"}

// Sort orders pkgs in place.
func Sort(pkgs []*analyze.Package, by string, f Filter) error {
	switch by {
	case "size":
		sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].SizeBytes > pkgs[j].SizeBytes })
	case "idle":
		sort.SliceStable(pkgs, func(i, j int) bool {
			if a, b := f.Idle(pkgs[i]), f.Idle(pkgs[j]); a != b {
				return a > b
			}
			return pkgs[i].SizeBytes > pkgs[j].SizeBytes
		})
	case "name":
		sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	case "deps": // most depended-on first
		sort.SliceStable(pkgs, func(i, j int) bool {
			if a, b := len(pkgs[i].AllDependents), len(pkgs[j].AllDependents); a != b {
				return a > b
			}
			return pkgs[i].SizeBytes > pkgs[j].SizeBytes
		})
	default:
		return fmt.Errorf("unknown sort %q (want %s)", by, strings.Join(SortKeys, ", "))
	}
	return nil
}

// ParseSize accepts plain bytes or a number with a k/M/G/T suffix (decimal).
func ParseSize(s string) (int64, error) {
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
