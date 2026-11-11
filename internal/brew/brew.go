// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package brew loads the list of installed Homebrew formulae and casks.
package brew

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info is the subset of `brew info --json=v2 --installed` that brewdust uses.
type Info struct {
	Formulae []Formula `json:"formulae"`
	Casks    []Cask    `json:"casks"`
}

// Lifecycle is a formula's or cask's deprecation and disabling. brew
// deprecates a package to warn that it's going away, and disables it once
// it can no longer be installed or upgraded; installed copies keep working.
type Lifecycle struct {
	Deprecated                    bool   `json:"deprecated"`
	DeprecationDate               string `json:"deprecation_date"`
	DeprecationReason             string `json:"deprecation_reason"`
	DeprecationReplacementFormula string `json:"deprecation_replacement_formula"`
	DeprecationReplacementCask    string `json:"deprecation_replacement_cask"`
	Disabled                      bool   `json:"disabled"`
	DisableDate                   string `json:"disable_date"`
	DisableReason                 string `json:"disable_reason"`
	DisableReplacementFormula     string `json:"disable_replacement_formula"`
	DisableReplacementCask        string `json:"disable_replacement_cask"`
}

// Reason explains why the package is disabled or deprecated, in words.
func (l Lifecycle) Reason() string {
	if l.Disabled && l.DisableReason != "" {
		return ReasonText(l.DisableReason)
	}
	return ReasonText(l.DeprecationReason)
}

// Replacement names what brew suggests installing instead, if anything.
func (l Lifecycle) Replacement() string {
	for _, r := range []string{l.DisableReplacementFormula, l.DisableReplacementCask,
		l.DeprecationReplacementFormula, l.DeprecationReplacementCask} {
		if r != "" {
			return r
		}
	}
	return ""
}

// reasons are the wording `brew info` uses for its reason codes, which it
// prints after the package's name ("openssl@1.1 is not supported upstream").
var reasons = map[string]string{
	"does_not_build":           "does not build",
	"no_license":               "has no license",
	"repo_archived":            "has an archived upstream repository",
	"repo_removed":             "has a removed upstream repository",
	"unmaintained":             "is not maintained upstream",
	"unsupported":              "is not supported upstream",
	"deprecated_upstream":      "is deprecated upstream",
	"versioned_formula":        "is a versioned formula",
	"checksum_mismatch":        "had its source change checksum after release",
	"discontinued":             "is discontinued upstream",
	"no_longer_available":      "is no longer available upstream",
	"no_longer_meets_criteria": "no longer meets the criteria for acceptable casks",
	"unsigned":                 "is unsigned or does not meet signature requirements",
	"fails_gatekeeper_check":   "does not pass the macOS Gatekeeper check",
}

// ReasonText turns a brew reason code into words. Packages may give their
// own free-text reason instead, which passes through unchanged.
func ReasonText(code string) string {
	if r, ok := reasons[code]; ok {
		return r
	}
	return strings.ReplaceAll(code, "_", " ")
}

type Formula struct {
	Lifecycle
	Name      string             `json:"name"`
	FullName  string             `json:"full_name"`
	Oldnames  []string           `json:"oldnames"`
	Desc      string             `json:"desc"`
	KegOnly   bool               `json:"keg_only"`
	LinkedKeg string             `json:"linked_keg"`
	Installed []InstalledVersion `json:"installed"`
	Outdated  bool               `json:"outdated"`
	Pinned    bool               `json:"pinned"`
	Revision  int                `json:"revision"`
	Versions  struct {
		Stable string `json:"stable"`
	} `json:"versions"`
}

// LatestVersion is the newest version brew knows of, as brew writes it
// ("15.0.0_1" when the formula has a revision).
func (f Formula) LatestVersion() string {
	if f.Revision > 0 {
		return fmt.Sprintf("%s_%d", f.Versions.Stable, f.Revision)
	}
	return f.Versions.Stable
}

// CleanupSkipReason says why `brew cleanup` won't remove this formula's old
// versions, or "" if it will. brew skips pinned formulae and ones whose
// newest version isn't installed.
func (f Formula) CleanupSkipReason() string {
	switch {
	case f.Pinned:
		return "pinned"
	case f.Outdated:
		return "newer version " + f.LatestVersion() + " not installed; brew upgrade first"
	}
	return ""
}

type InstalledVersion struct {
	Version             string `json:"version"`
	Time                int64  `json:"time"`
	InstalledOnRequest  bool   `json:"installed_on_request"`
	RuntimeDependencies []Dep  `json:"runtime_dependencies"`
}

type Dep struct {
	FullName string `json:"full_name"`
	Version  string `json:"version"`
}

// Current returns the version that is linked, or the most recently installed one.
func (f Formula) Current() (InstalledVersion, bool) {
	if len(f.Installed) == 0 {
		return InstalledVersion{}, false
	}
	best := f.Installed[0]
	for _, iv := range f.Installed {
		if f.LinkedKeg != "" && iv.Version == f.LinkedKeg {
			return iv, true
		}
		if iv.Time > best.Time {
			best = iv
		}
	}
	return best, true
}

// Receipt is the INSTALL_RECEIPT.json brew writes into each keg. It's how
// brewdust learns about kegs whose formula brew can no longer find.
type Receipt struct {
	Time                int64 `json:"time"`
	InstalledOnRequest  bool  `json:"installed_on_request"`
	RuntimeDependencies []Dep `json:"runtime_dependencies"`
	Source              struct {
		Tap string `json:"tap"`
	} `json:"source"`
}

// ReadReceipt reads the receipt in a keg (a Cellar/<name>/<version> directory).
func ReadReceipt(keg string) (Receipt, error) {
	var r Receipt
	data, err := os.ReadFile(filepath.Join(keg, "INSTALL_RECEIPT.json"))
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(data, &r)
	return r, err
}

type Cask struct {
	Lifecycle
	Token         string     `json:"token"`
	FullToken     string     `json:"full_token"`
	OldTokens     []string   `json:"old_tokens"`
	Name          []string   `json:"name"`
	Desc          string     `json:"desc"`
	Installed     string     `json:"installed"`
	InstalledTime int64      `json:"installed_time"`
	Artifacts     []Artifact `json:"artifacts"`
	DependsOn     struct {
		Formula []string `json:"formula"`
	} `json:"depends_on"`
}

// Artifact is one entry of a cask's artifact list. Only app bundles matter here.
type Artifact map[string]json.RawMessage

// AppPath returns the installed location of an `app` artifact, if this is one.
func (a Artifact) AppPath() (string, bool) {
	if _, ok := a["app"]; !ok {
		return "", false
	}
	var target string
	if raw, ok := a["target"]; ok && json.Unmarshal(raw, &target) == nil && target != "" {
		return target, true
	}
	return "", false
}

// AppPaths lists every app bundle the cask installed.
func (c Cask) AppPaths() []string {
	var out []string
	for _, a := range c.Artifacts {
		if p, ok := a.AppPath(); ok {
			out = append(out, p)
		}
	}
	return out
}

// ShortName strips a tap prefix ("user/tap/name" -> "name").
func ShortName(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}

// Command builds a brew invocation that won't trigger an auto-update.
func Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "brew", args...)
	cmd.Env = append(os.Environ(),
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ENV_HINTS=1",
		"HOMEBREW_NO_ANALYTICS=1",
	)
	return cmd
}

func run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := Command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("brew %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Prefix returns `brew --prefix`.
func Prefix(ctx context.Context) (string, error) {
	out, err := run(ctx, "--prefix")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Load returns every installed formula and cask.
func Load(ctx context.Context) (*Info, error) {
	out, err := run(ctx, "info", "--json=v2", "--installed")
	if err != nil {
		return nil, err
	}
	var info Info
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parsing brew info output: %w", err)
	}
	return &info, nil
}
