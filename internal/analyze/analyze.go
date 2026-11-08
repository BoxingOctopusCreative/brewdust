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
	Name             string     `json:"name"`
	Kind             string     `json:"kind"`
	Version          string     `json:"version"`
	Desc             string     `json:"desc,omitempty"`
	SizeBytes        int64      `json:"size_bytes"`
	OldVersions      int        `json:"old_versions,omitempty"`
	ReclaimableBytes int64      `json:"reclaimable_bytes,omitempty"`
	InstalledAt      time.Time  `json:"installed_at"`
	OnRequest        bool       `json:"on_request"`
	LastUsed         *time.Time `json:"last_used,omitempty"`
	LastUsedEvidence string     `json:"last_used_evidence,omitempty"`
	Dependents       []string   `json:"dependents,omitempty"`
	// LastActive is the newest of: own last use, install time, and the
	// LastActive of every installed dependent.
	LastActive     time.Time `json:"last_active"`
	ActiveVia      string    `json:"active_via,omitempty"`
	IdleDays       int       `json:"idle_days"`        // based on LastActive
	DirectIdleDays int       `json:"direct_idle_days"` // ignoring dependents
	Orphan         bool      `json:"orphan,omitempty"` // dependency nothing installed needs
	MissingApps    []string  `json:"missing_apps,omitempty"`
	ScanErrors     int       `json:"scan_errors,omitempty"`

	deps []string
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
		pkgs = append(pkgs, p)
		f := f
		jobs = append(jobs, func() { scanFormula(p, f, opt) })
	}

	for _, c := range info.Casks {
		p := &Package{Name: c.Token, Kind: KindCask, Desc: c.Desc}
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
			p.OldVersions++
			p.ReclaimableBytes += r.Bytes
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
	for _, p := range pkgs {
		for _, d := range p.deps {
			if dep, ok := formulae[d]; ok && dep != p {
				dep.Dependents = append(dep.Dependents, p.Name)
			}
		}
	}

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
		sort.Strings(p.Dependents)
		p.IdleDays = daysBetween(p.LastActive, now)
		direct := p.InstalledAt
		if p.LastUsed != nil && p.LastUsed.After(direct) {
			direct = *p.LastUsed
		}
		p.DirectIdleDays = daysBetween(direct, now)
		p.Orphan = p.Kind == KindFormula && !p.OnRequest && len(p.Dependents) == 0
	}
}

func daysBetween(t, now time.Time) int {
	if t.IsZero() || !now.After(t) {
		return 0
	}
	return int(now.Sub(t).Hours() / 24)
}
