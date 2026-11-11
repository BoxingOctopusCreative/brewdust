// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package analyze turns installed-package metadata and filesystem scans into a report.
package analyze

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
	"github.com/BoxingOctopusCreative/brewdust/internal/scan"
)

const (
	KindFormula = "formula"
	KindCask    = "cask"
)

// Package is one installed formula or cask.
type Package struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Version          string   `json:"version"`
	Desc             string   `json:"desc,omitempty"`
	SizeBytes        int64    `json:"size_bytes"`
	OldVersions      int      `json:"old_versions,omitempty"`
	ReclaimableBytes int64    `json:"reclaimable_bytes,omitempty"`
	OldPaths         []string `json:"old_paths,omitempty"` // non-current version directories
	// Old versions that `brew cleanup` won't remove yet, and why.
	HeldVersions     int        `json:"held_versions,omitempty"`
	HeldBytes        int64      `json:"held_bytes,omitempty"`
	HeldPaths        []string   `json:"held_paths,omitempty"`
	HeldReason       string     `json:"held_reason,omitempty"`
	InstalledAt      time.Time  `json:"installed_at"`
	OnRequest        bool       `json:"on_request"`
	LastUsed         *time.Time `json:"last_used,omitempty"`
	LastUsedEvidence string     `json:"last_used_evidence,omitempty"`
	// Dependencies are the installed formulae this package directly needs.
	Dependencies []string `json:"dependencies,omitempty"`
	// Dependents are the installed packages that directly need this one.
	Dependents []string `json:"dependents,omitempty"`
	// AllDependents adds the ones that need it indirectly, through other
	// packages. Removing this package would break every one of them.
	AllDependents []string `json:"all_dependents,omitempty"`
	// LastActive is the newest of: own last use, install time, and the
	// LastActive of every installed dependent.
	LastActive     time.Time `json:"last_active"`
	ActiveVia      string    `json:"active_via,omitempty"`
	IdleDays       int       `json:"idle_days"`        // based on LastActive
	DirectIdleDays int       `json:"direct_idle_days"` // ignoring dependents
	Orphan         bool      `json:"orphan,omitempty"` // dependency nothing installed needs
	MissingApps    []string  `json:"missing_apps,omitempty"`
	ScanErrors     int       `json:"scan_errors,omitempty"`

	// Deprecated and Disabled are brew's status for the formula or cask: a
	// deprecated one is going away, a disabled one can no longer be
	// installed or upgraded. Either way the installed copy still works.
	Deprecated      bool   `json:"deprecated,omitempty"`
	Disabled        bool   `json:"disabled,omitempty"`
	StatusReason    string `json:"status_reason,omitempty"`
	DeprecationDate string `json:"deprecation_date,omitempty"`
	DisableDate     string `json:"disable_date,omitempty"` // when it was, or will be, disabled
	Replacement     string `json:"replacement,omitempty"`

	// NoFormula marks a keg in the Cellar whose formula brew can't find,
	// because it was deleted from its tap or the tap was removed. brew
	// can't upgrade it, and `brew info --installed` leaves it out.
	NoFormula bool `json:"no_formula,omitempty"`
	// Origin says where the formula went, from the tap the keg's install
	// receipt names: "deleted from homebrew/core", "from untapped user/tap"
	// or "no tap recorded".
	Origin string `json:"origin,omitempty"`
	// Rack is the keg's Cellar directory, set for NoFormula packages.
	Rack string `json:"rack,omitempty"`

	deps []string // as reported: transitive for formulae, direct for casks
}

// Broken reports a cask whose app was deleted without `brew uninstall`.
func (p *Package) Broken() bool { return len(p.MissingApps) > 0 }

// Retiring reports a formula or cask brew has deprecated or disabled.
func (p *Package) Retiring() bool { return p.Deprecated || p.Disabled }

// EmptyKeg reports a Cellar directory with no versions left in it.
func (p *Package) EmptyKeg() bool { return p.NoFormula && p.Version == "" }

func (p *Package) setLifecycle(l brew.Lifecycle) {
	p.Deprecated, p.Disabled = l.Deprecated, l.Disabled
	if !p.Retiring() {
		return
	}
	p.StatusReason = l.Reason()
	p.DeprecationDate, p.DisableDate = l.DeprecationDate, l.DisableDate
	p.Replacement = l.Replacement()
}

type Options struct {
	Prefix  string
	Now     time.Time
	Workers int
	// Grace ignores file accesses within this long after install, since
	// Homebrew itself reads files while pouring and relocating bottles.
	Grace time.Duration
}

// Run scans every installed package and returns them unsorted.
func Run(info *brew.Info, opt Options) []*Package {
	if opt.Workers < 1 {
		opt.Workers = 8
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}

	var pkgs []*Package
	var jobs []func()
	for _, f := range info.Formulae {
		p := &Package{Name: f.Name, Kind: KindFormula, Desc: f.Desc}
		p.setLifecycle(f.Lifecycle)
		pkgs = append(pkgs, p)
		f := f
		jobs = append(jobs, func() { scanFormula(p, f, opt) })
	}

	for _, k := range kegsWithoutFormula(info, opt.Prefix) {
		p := &Package{Name: k.Name, Kind: KindFormula, NoFormula: true}
		pkgs = append(pkgs, p)
		k := k
		jobs = append(jobs, func() { scanOrphanKeg(p, k, opt) })
	}

	for _, c := range info.Casks {
		p := &Package{Name: c.Token, Kind: KindCask, Desc: c.Desc}
		p.setLifecycle(c.Lifecycle)
		pkgs = append(pkgs, p)
		c := c
		jobs = append(jobs, func() { scanCask(p, c, opt) })
	}
	runAll(jobs, opt.Workers)

	resolveDependencies(pkgs, opt.Now)
	return pkgs
}

func runAll(jobs []func(), workers int) {
	ch := make(chan func())
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				j()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

// formulaEligible decides which keg files reflect real use. rel starts with
// the version directory, e.g. "1.8.2/bin/jq".
func formulaEligible(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		// Files at the keg root: INSTALL_RECEIPT.json, sbom.spdx.json,
		// README, LICENSE... `brew` reads these itself.
		return false
	}
	switch parts[1] {
	case ".brew", // formula source, read by `brew info`
		"share": // docs, man pages, and shell completions read at every shell start
		return false
	}
	return true
}

func scanFormula(p *Package, f brew.Formula, opt Options) {
	cur, ok := f.Current()
	if !ok {
		return
	}
	p.Version = cur.Version
	p.InstalledAt = time.Unix(cur.Time, 0)
	p.OnRequest = cur.InstalledOnRequest
	for _, d := range cur.RuntimeDependencies {
		p.deps = append(p.deps, brew.ShortName(d.FullName))
	}

	keg := filepath.Join(opt.Prefix, "Cellar", f.Name)
	var used scan.Result
	for _, iv := range f.Installed {
		r := scan.Dir(filepath.Join(keg, iv.Version), func(rel string) bool {
			return formulaEligible(iv.Version + "/" + rel)
		})
		p.SizeBytes += r.Bytes
		p.ScanErrors += r.Errors
		if iv.Version != cur.Version && r.Exists {
			path := filepath.Join(keg, iv.Version)
			if reason := f.CleanupSkipReason(); reason != "" {
				p.HeldVersions++
				p.HeldBytes += r.Bytes
				p.HeldPaths = append(p.HeldPaths, path)
				p.HeldReason = reason
			} else {
				p.OldVersions++
				p.ReclaimableBytes += r.Bytes
				p.OldPaths = append(p.OldPaths, path)
			}
		}
		if r.LastAccess.After(time.Unix(iv.Time, 0).Add(opt.Grace)) && r.LastAccess.After(used.LastAccess) {
			used.LastAccess, used.AccessPath = r.LastAccess, r.AccessPath
		}
	}
	if !used.LastAccess.IsZero() {
		t := used.LastAccess
		p.LastUsed = &t
		p.LastUsedEvidence = "atime " + strings.TrimPrefix(used.AccessPath, keg+"/")
	}
}

// kegsWithoutFormula finds Cellar directories that no installed formula
// accounts for, by name or by an old name it was renamed from. For each it
// builds a stand-in formula from the kegs' install receipts, newest first.
func kegsWithoutFormula(info *brew.Info, prefix string) []brew.Formula {
	known := map[string]bool{}
	for _, f := range info.Formulae {
		known[f.Name] = true
		for _, old := range f.Oldnames {
			known[old] = true
		}
	}
	cellar := filepath.Join(prefix, "Cellar")
	entries, _ := os.ReadDir(cellar)
	var out []brew.Formula
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || known[e.Name()] {
			continue
		}
		f := brew.Formula{Name: e.Name(), FullName: e.Name()}
		versions, _ := os.ReadDir(filepath.Join(cellar, e.Name()))
		for _, v := range versions {
			if !v.IsDir() || strings.HasPrefix(v.Name(), ".") {
				continue
			}
			iv := brew.InstalledVersion{Version: v.Name()}
			if r, err := brew.ReadReceipt(filepath.Join(cellar, e.Name(), v.Name())); err == nil {
				iv.Time, iv.InstalledOnRequest, iv.RuntimeDependencies = r.Time, r.InstalledOnRequest, r.RuntimeDependencies
				if f.FullName == e.Name() && r.Source.Tap != "" {
					f.FullName = r.Source.Tap + "/" + e.Name()
				}
			} else if fi, err := v.Info(); err == nil {
				iv.Time = fi.ModTime().Unix()
			}
			f.Installed = append(f.Installed, iv)
		}
		// The keg opt/<name> points at is the one in use, as for any formula.
		if dest, err := os.Readlink(filepath.Join(prefix, "opt", e.Name())); err == nil {
			f.LinkedKeg = filepath.Base(dest)
		}
		out = append(out, f)
	}
	return out
}

// scanOrphanKeg scans a keg whose formula is gone. All of it goes when it's
// uninstalled, and `brew cleanup` can't trim its old versions, so none of it
// counts as reclaimable on its own.
func scanOrphanKeg(p *Package, f brew.Formula, opt Options) {
	p.Rack = filepath.Join(opt.Prefix, "Cellar", f.Name)
	scanFormula(p, f, opt)
	p.OldVersions, p.ReclaimableBytes, p.OldPaths = 0, 0, nil
	p.HeldVersions, p.HeldBytes, p.HeldPaths, p.HeldReason = 0, 0, nil, ""
	if p.Version == "" { // an empty Cellar directory
		if fi, err := os.Stat(p.Rack); err == nil {
			p.InstalledAt = fi.ModTime()
		}
	}

	switch tap, _, _ := strings.Cut(f.FullName, "/"+f.Name); {
	case !strings.Contains(f.FullName, "/"):
		p.Origin = "no tap recorded"
	case tapInstalled(opt.Prefix, tap) || tap == "homebrew/core" || tap == "homebrew/cask":
		p.Origin = "deleted from " + tap
	default:
		p.Origin = "from untapped " + tap
	}
}

// tapInstalled reports whether user/repo is tapped. Taps live in brew's
// repository: the prefix itself on Apple Silicon, prefix/Homebrew elsewhere.
func tapInstalled(prefix, tap string) bool {
	user, repo, ok := strings.Cut(tap, "/")
	if !ok {
		return false
	}
	for _, repository := range []string{prefix, filepath.Join(prefix, "Homebrew")} {
		if _, err := os.Stat(filepath.Join(repository, "Library", "Taps", user, "homebrew-"+repo)); err == nil {
			return true
		}
	}
	return false
}

// caskroomEligible counts command-line payloads (e.g. adb, flutter) but not
// app bundles staged in the Caskroom, for the same reason as above.
func caskroomEligible(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasSuffix(part, ".app") {
			return false
		}
	}
	return true
}

func scanCask(p *Package, c brew.Cask, opt Options) {
	p.Version = c.Installed
	p.InstalledAt = time.Unix(c.InstalledTime, 0)
	p.OnRequest = true
	for _, f := range c.DependsOn.Formula {
		p.deps = append(p.deps, brew.ShortName(f))
	}

	var last time.Time
	var evidence string
	consider := func(t time.Time, ev string, needGrace bool) {
		if needGrace && !t.After(p.InstalledAt.Add(opt.Grace)) {
			return
		}
		if t.After(last) {
			last, evidence = t, ev
		}
	}

	// Caskroom/<token>/ holds the current version, possibly leftovers from
	// older or interrupted upgrades ("1.2.3.upgrading"), and .metadata.
	room := filepath.Join(opt.Prefix, "Caskroom", c.Token)
	entries, _ := os.ReadDir(room)
	for _, e := range entries {
		dir := filepath.Join(room, e.Name())
		r := scan.Dir(dir, caskroomEligible)
		p.SizeBytes += r.Bytes
		p.ScanErrors += r.Errors
		if e.Name() == ".metadata" {
			continue
		}
		if e.Name() != c.Installed {
			p.OldVersions++
			p.ReclaimableBytes += r.Bytes
			p.OldPaths = append(p.OldPaths, dir)
		}
		consider(r.LastAccess, "atime "+strings.TrimPrefix(r.AccessPath, room+"/"), true)
	}

	for _, app := range c.AppPaths() {
		// Executable atimes inside .app bundles are bumped by macOS's
		// periodic malware scans, so only size them; launches come from
		// LaunchServices via Spotlight.
		ar := scan.Dir(app, nil)
		if !ar.Exists {
			p.MissingApps = append(p.MissingApps, app)
			continue
		}
		p.SizeBytes += ar.Bytes
		p.ScanErrors += ar.Errors
		// Spotlight's date is a real launch even if it predates an upgrade.
		if t, ok := scan.AppLastUsed(app); ok {
			consider(t, "Spotlight: "+filepath.Base(app)+" opened", false)
		}
	}
	if !last.IsZero() {
		p.LastUsed, p.LastUsedEvidence = &last, evidence
	}
}

// resolveDependencies links dependents and propagates activity so that a
// library is considered active whenever something depending on it is.
func resolveDependencies(pkgs []*Package, now time.Time) {
	formulae := map[string]*Package{}
	for _, p := range pkgs {
		if p.Kind == KindFormula {
			formulae[p.Name] = p
		}
	}
	linkDependencies(pkgs, formulae)

	done := map[*Package]bool{}
	visiting := map[*Package]bool{}
	byName := map[string]*Package{}
	for _, p := range pkgs {
		byName[p.Kind+":"+p.Name] = p
	}
	var resolve func(p *Package) time.Time
	resolve = func(p *Package) time.Time {
		if done[p] || visiting[p] {
			return p.LastActive
		}
		visiting[p] = true
		p.LastActive = p.InstalledAt
		if p.LastUsed != nil && p.LastUsed.After(p.LastActive) {
			p.LastActive = *p.LastUsed
		}
		for _, name := range p.Dependents {
			dp := byName[KindFormula+":"+name]
			if dp == nil {
				dp = byName[KindCask+":"+name]
			}
			if dp == nil {
				continue
			}
			if t := resolve(dp); t.After(p.LastActive) {
				p.LastActive, p.ActiveVia = t, name
				if dp.ActiveVia != "" {
					p.ActiveVia = name + " → " + dp.ActiveVia
				}
			}
		}
		visiting[p] = false
		done[p] = true
		return p.LastActive
	}

	for _, p := range pkgs {
		resolve(p)
		p.IdleDays = daysBetween(p.LastActive, now)
		direct := p.InstalledAt
		if p.LastUsed != nil && p.LastUsed.After(direct) {
			direct = *p.LastUsed
		}
		p.DirectIdleDays = daysBetween(direct, now)
		p.Orphan = p.Kind == KindFormula && !p.NoFormula && !p.OnRequest && len(p.AllDependents) == 0
	}
}

func daysBetween(t, now time.Time) int {
	if t.IsZero() || !now.After(t) {
		return 0
	}
	return int(now.Sub(t).Hours() / 24)
}

// linkDependencies fills Dependencies, Dependents and AllDependents.
//
// A formula's install receipt lists its runtime dependencies transitively
// (everything it pulls in, at the versions actually installed). The direct
// edges are recovered by transitive reduction: d is a direct dependency of p
// unless it is also a dependency of another of p's dependencies. This uses
// only installed data, so it isn't thrown off by formula aliases or
// definitions that changed since install.
func linkDependencies(pkgs []*Package, formulae map[string]*Package) {
	// closure[p] = every installed formula p needs, directly or not.
	closure := map[*Package]map[string]bool{}
	var close func(p *Package, seen map[*Package]bool) map[string]bool
	close = func(p *Package, seen map[*Package]bool) map[string]bool {
		if c, ok := closure[p]; ok {
			return c
		}
		c := map[string]bool{}
		if seen[p] {
			return c
		}
		seen[p] = true
		for _, d := range p.deps {
			dep, ok := formulae[d]
			if !ok || dep == p {
				continue
			}
			c[d] = true
			if p.Kind == KindCask { // cask deps are direct only; expand them
				for dd := range close(dep, seen) {
					c[dd] = true
				}
			}
		}
		closure[p] = c
		return c
	}
	for _, p := range pkgs {
		close(p, map[*Package]bool{})
	}

	for _, p := range pkgs {
		all := closure[p]
		for d := range all {
			indirect := false
			for e := range all {
				if e != d && closure[formulae[e]][d] {
					indirect = true
					break
				}
			}
			dep := formulae[d]
			dep.AllDependents = append(dep.AllDependents, p.Name)
			if !indirect {
				p.Dependencies = append(p.Dependencies, d)
				dep.Dependents = append(dep.Dependents, p.Name)
			}
		}
	}
	for _, p := range pkgs {
		sort.Strings(p.Dependencies)
		sort.Strings(p.Dependents)
		sort.Strings(p.AllDependents)
	}
}
