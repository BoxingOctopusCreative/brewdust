# brewdust

Find the Homebrew packages that are gathering dust: how much disk each installed
formula and cask uses, how long it has been since you actually used it, and
remove what you don't need, including the leftovers Homebrew doesn't clean up
on its own.

```
brewdust  429 packages · 21.4 GB · 6.8 GB in old versions
view all  │  type all  │  sort size
  NAME            KIND          SIZE  LAST USED     IDLE  NOTES
  go@1.25         formula     6.1 GB  2026-11-08   today  requested; +2 old version(s), 4.6 GB reclaimable
● calibre         cask        1.9 GB  —             2.7y  +2 old version(s), 1.9 GB reclaimable; app deleted outside brew
  llvm@22         formula     1.6 GB  —              2mo  dep of 1; active via rust
● qemu            formula   729.5 MB  —              2mo  requested
2 marked · 2.6 GB  (d to remove, a to toggle all shown, esc to clear)
↑↓ move · space mark · d remove · c clean up · / search · v view · t type · s sort · enter details · ? help · q quit
```

## Install

```sh
go install github.com/BoxingOctopusCreative/brewdust@latest
# or, from a checkout
go build -o brewdust .
```

Requires Go 1.25+ and `brew` on your `PATH`. The interface is built with
[Phoenix](https://github.com/phoenix-tui/phoenix) and the CLI with
[Cobra](https://github.com/spf13/cobra).

## Interactive use

Run `brewdust` with no arguments. Scanning takes a few seconds.

| Key | Action |
| --- | --- |
| `↑`/`↓`, `j`/`k`, `pgup`/`pgdn`, `g`/`G` | Move |
| `space` / `a` | Mark the package / mark everything shown |
| `enter` | Details: evidence for "last used", dependents, old version paths |
| `/` | Search names and descriptions |
| `v` | Cycle views: all, requested, idle ≥ 90d, idle ≥ 1y, orphans, broken casks |
| `t` / `s` | Cycle type (all, formula, cask) / sort (size, idle, name) |
| `D` | Toggle whether use by dependents counts |
| `d`, `x`, `delete` | Remove the marked packages, or the selected one |
| `c` | Clean up old versions, orphans and broken casks |
| `r` / `?` / `q` | Rescan / help / quit |

Removing or cleaning up always shows a confirmation listing exactly what will
run and roughly how much space it frees. Formulae that something else still
needs are skipped, with the reason. When you confirm, brewdust hands the
terminal over to `brew` so you can see its output and answer any prompts (e.g.
`sudo` for pkg-based casks), then rescans.

Filter flags (below) set the starting view, e.g. `brewdust --stale 180 --sort idle`.

## Commands

```sh
brewdust list --sort idle --stale 180         # unused for 6+ months, longest idle first
brewdust list --requested --min-size 50MB     # things you installed, biggest first
brewdust list --json | jq '.packages[] | select(.idle_days > 365) | .name'

brewdust remove qemu mono                     # asks before running brew uninstall
brewdust remove --cask calibre --zap          # also delete the app's settings and caches
brewdust remove -n gcc                        # dry run

brewdust clean --dry-run                      # show the cleanup plan
brewdust clean --only orphans,broken -y       # no prompt
```

When output isn't a terminal, plain `brewdust` behaves like `brewdust list`.
`remove` and `clean` refuse to run without a terminal unless you pass `--yes`.

### Cleanup categories

| Category | What | How |
| --- | --- | --- |
| `old` | Non-current formula versions | `brew cleanup <formula>...` |
| `old` | Stale Caskroom version directories, including interrupted `*.upgrading` ones | deleted directly, since `brew cleanup` skips them. brewdust only deletes `Caskroom/<cask>/<version>` paths |
| `orphans` | Dependencies nothing installed needs | `brew autoremove` |
| `broken` | Casks whose app was deleted outside brew | `brew uninstall --cask` |

### Filter flags

Shared by the interactive view and `list`.

| Flag | Meaning |
| --- | --- |
| `--sort size\|idle\|name` | Sort order (default `size`) |
| `-t, --type all\|formula\|cask` | Limit to one package type |
| `--stale N` | Only packages idle for at least N days |
| `--requested` | Only packages you installed explicitly (casks always count) |
| `--orphans` | Only dependencies with no installed dependents |
| `--broken` | Only casks whose app was deleted outside brew |
| `--direct` | Judge idleness by a package's own use, ignoring its dependents |
| `--min-size 50MB` | Only packages at least this big (decimal units) |

`list` also takes `--json` and `-n/--limit N`.

## How it works

**Size** is the allocated size on disk (what `du` reports) of every installed
version under `$(brew --prefix)/Cellar/<formula>`, or for casks the
`Caskroom/<token>` directory plus any app bundles the cask installed. Hard links
are counted once. Versions other than the current one are reported as
reclaimable, as are leftover `*.upgrading` directories in the Caskroom.

**Last used** is inferred, since Homebrew doesn't track it:

- *Formulae*: the newest access time (atime) of any file in the keg, so running
  a binary, loading a library, or compiling against a header all count. Files
  Homebrew reads itself (`INSTALL_RECEIPT.json`, `.brew/`, the keg root) and
  `share/` (shell completions are read by every new shell) are ignored, as are
  accesses within 10 minutes of install.
- *Casks with apps*: Spotlight's `kMDItemLastUsedDate`, which LaunchServices
  updates each time an app opens.
- *Casks with CLI payloads*: atime of files in the Caskroom, excluding `.app`
  bundles.

**Idle** counts from the most recent of: the package's own last use, its install
date, and the last activity of anything installed that depends on it. A library
nobody calls directly is still "active" if a tool that links it was used
yesterday; the notes column says which dependent kept it active. Use `--direct`
to turn that off.

## Caveats

- macOS updates atime lazily (at most about once a day per file), so precision
  is roughly a day.
- Anything that reads files counts as use: backups, antivirus, a recursive
  `grep` through `/opt/homebrew`, or Homebrew itself checking linkage during an
  upgrade. A suspicious cluster of packages "used" at the same minute is usually
  one of these. App executables are skipped for this reason, since macOS's
  periodic malware scan reads them.
- Upgrading a formula replaces the keg, so its history starts over and it shows
  as "not since install" until you use the new version.
- Scanning only reads metadata (`lstat`), so running brewdust does not change
  the results. The `brew info` call it makes does read Homebrew's own files,
  which is why those files are ignored.
- "app deleted outside brew" means the cask's app is gone but Homebrew still
  thinks it is installed; `brewdust clean --only broken` (or `brew uninstall --cask <name>`) removes the leftovers.
