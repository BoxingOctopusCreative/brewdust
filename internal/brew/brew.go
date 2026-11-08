// Package brew loads the list of installed Homebrew formulae and casks.
package brew

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Info is the subset of `brew info --json=v2 --installed` that brewdust uses.
type Info struct {
	Formulae []Formula `json:"formulae"`
	Casks    []Cask    `json:"casks"`
}

type Formula struct {
	Name      string             `json:"name"`
	FullName  string             `json:"full_name"`
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

type Cask struct {
	Token         string     `json:"token"`
	FullToken     string     `json:"full_token"`
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
