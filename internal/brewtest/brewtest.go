// Package brewtest builds a fake Homebrew installation for tests: a prefix
// with Cellar and Caskroom directories holding real files with chosen access
// times, the matching `brew info --json=v2` data, and optionally a `brew`
// script on PATH that answers from them and logs every call.
package brewtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/brew"
)

// Fake is a Homebrew installation under a temporary directory.
type Fake struct {
	Dir    string // holds the brew script, info.json and calls.log
	Prefix string
	Apps   string // stands in for /Applications
	Now    time.Time
	Info   brew.Info
}

// Days returns the time n days before f.Now.
func (f *Fake) Days(n int) time.Time { return f.Now.AddDate(0, 0, -n) }

// File creates a file of size bytes under the prefix whose access time is atime.
func (f *Fake) File(t testing.TB, rel string, size int, atime time.Time) string {
	t.Helper()
	p := filepath.Join(f.Prefix, rel)
	if strings.HasPrefix(rel, "/") {
		p = rel
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, atime, atime); err != nil {
		t.Fatal(err)
	}
	return p
}

// AppArtifact is a cask artifact entry installing an app at path.
func AppArtifact(path string) brew.Artifact {
	return brew.Artifact{
		"app":    json.RawMessage(`["` + filepath.Base(path) + `"]`),
		"target": json.RawMessage(`"` + path + `"`),
	}
}

// Standard builds the fixture most tests share:
//
//   - jq: requested, current 1.8 used 5 days ago, old 1.7 that brew cleanup
//     would remove; depends on oniguruma
//   - oniguruma: a dependency of jq, never used directly
//   - harfbuzz: requested but outdated, so its old version is held back
//   - leftover: a dependency nothing needs (an orphan)
//   - tool: a cask with a stale Caskroom version whose app was deleted
//   - viewer: a cask whose app is present
func Standard(t testing.TB) *Fake {
	t.Helper()
	dir := t.TempDir()
	f := &Fake{
		Dir:    dir,
		Prefix: filepath.Join(dir, "prefix"),
		Apps:   filepath.Join(dir, "Applications"),
		Now:    time.Now(),
	}
	installed := f.Days(200)
	old := f.Days(400)

	// Files touched only at install time don't count as use.
	f.File(t, "Cellar/jq/1.8/bin/jq", 40000, f.Days(5))
	f.File(t, "Cellar/jq/1.8/share/man/man1/jq.1", 1000, f.Days(1)) // not use
	f.File(t, "Cellar/jq/1.8/INSTALL_RECEIPT.json", 100, f.Days(1)) // not use
	f.File(t, "Cellar/jq/1.7/bin/jq", 30000, old)
	f.File(t, "Cellar/oniguruma/6.9/lib/libonig.dylib", 20000, installed)
	f.File(t, "Cellar/harfbuzz/1.0/lib/libharfbuzz.dylib", 10000, old)
	f.File(t, "Cellar/harfbuzz/2.0/lib/libharfbuzz.dylib", 10000, installed)
	f.File(t, "Cellar/leftover/0.1/lib/libleftover.dylib", 5000, old)
	f.File(t, "Caskroom/tool/1.0/tool-cli", 8000, old)
	f.File(t, "Caskroom/tool/2.0/tool-cli", 8000, installed)
	f.File(t, "Caskroom/tool/.metadata/2.0/tool.rb", 100, installed)
	f.File(t, filepath.Join(f.Apps, "Viewer.app/Contents/MacOS/Viewer"), 50000, f.Days(1))

	formula := func(name, linked string, outdated bool, versions ...brew.InstalledVersion) brew.Formula {
		fm := brew.Formula{Name: name, FullName: name, Desc: name + " description", LinkedKeg: linked, Installed: versions, Outdated: outdated}
		fm.Versions.Stable = linked
		return fm
	}
	iv := func(v string, at time.Time, onRequest bool, deps ...string) brew.InstalledVersion {
		out := brew.InstalledVersion{Version: v, Time: at.Unix(), InstalledOnRequest: onRequest}
		for _, d := range deps {
			out.RuntimeDependencies = append(out.RuntimeDependencies, brew.Dep{FullName: d})
		}
		return out
	}

	harfbuzz := formula("harfbuzz", "2.0", true, iv("1.0", old, true), iv("2.0", installed, true))
	harfbuzz.Versions.Stable = "3.0"
	f.Info.Formulae = []brew.Formula{
		formula("jq", "1.8", false, iv("1.7", old, true, "oniguruma"), iv("1.8", installed, true, "oniguruma")),
		formula("oniguruma", "6.9", false, iv("6.9", installed, false)),
		harfbuzz,
		formula("leftover", "0.1", false, iv("0.1", old, false)),
	}

	tool := brew.Cask{Token: "tool", Desc: "A tool", Installed: "2.0", InstalledTime: installed.Unix()}
	tool.Artifacts = []brew.Artifact{AppArtifact(filepath.Join(f.Apps, "Tool.app")), {"binary": json.RawMessage(`["tool-cli"]`)}}
	viewer := brew.Cask{Token: "viewer", Desc: "Views things", Installed: "3.1", InstalledTime: installed.Unix()}
	viewer.Artifacts = []brew.Artifact{AppArtifact(filepath.Join(f.Apps, "Viewer.app"))}
	f.Info.Casks = []brew.Cask{tool, viewer}
	return f
}

const script = `#!/bin/sh
d=%q
echo "$*" >> "$d/calls.log"
if [ -n "$FAKE_BREW_FAIL" ] && [ "$1" = "$FAKE_BREW_FAIL" ]; then
  echo "Error: it broke" >&2; exit 1
fi
case "$1" in
  --prefix) echo "$d/prefix" ;;
  info) cat "$d/info.json" ;;
  *) echo "fake brew: $*" ;;
esac
`

// Install writes f.Info to disk and puts a fake `brew` first on PATH for
// the rest of the test. It answers `--prefix` and `info`, fails when its
// subcommand is $FAKE_BREW_FAIL, and otherwise just echoes its arguments.
func (f *Fake) Install(t testing.TB) {
	t.Helper()
	data, err := json.Marshal(f.Info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.Dir, "info.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(f.Dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(script, "%q", shellQuote(f.Dir), 1)
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Calls returns the argument lists brew was run with, one per line.
func (f *Fake) Calls(t testing.TB) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.Dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
