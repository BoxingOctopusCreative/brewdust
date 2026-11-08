# brewdust

Find the Homebrew packages that are gathering dust: how much disk each installed
formula and cask uses, and how long it has been since you actually used it.

```
NAME            KIND     SIZE      INSTALLED   LAST USED          IDLE   NOTES
go@1.25         formula  6.1 GB    2026-08-21  2026-11-08         today  requested; +2 old version(s), 4.6 GB reclaimable
calibre         cask     1.9 GB    2024-03-05  not since install  2.7y   +2 old version(s), 1.9 GB reclaimable; app deleted outside brew
llvm@22         formula  1.6 GB    2026-08-21  not since install  2mo    dep of 1; active via rust
qemu            formula  729.5 MB  2026-08-21  not since install  2mo    requested
```

## Install

```sh
go install github.com/BoxingOctopusCreative/brewdust@latest
# or, from a checkout
go build -o brewdust .
```

Requires Go 1.25+ and `brew` on your `PATH`. No third-party dependencies.

## Usage

```sh
brewdust                               # everything, biggest first
brewdust -sort idle -stale 180         # unused for 6+ months, longest idle first
brewdust -requested -stale 90 -min-size 50MB   # things you installed and forgot
brewdust -orphans                      # dependencies nothing needs anymore
brewdust -type cask
brewdust -json | jq '.packages[] | select(.idle_days > 365) | .name'
```

| Flag | Meaning |
| --- | --- |
| `-sort size\|idle\|name` | Sort order (default `size`) |
| `-type all\|formula\|cask` | Limit to one package type |
| `-stale N` | Only packages idle for at least N days |
| `-requested` | Only packages you installed explicitly (casks always count) |
| `-orphans` | Only dependencies with no installed dependents |
| `-direct` | Judge idleness by a package's own use, ignoring its dependents |
| `-min-size 50MB` | Only packages at least this big (decimal units) |
| `-limit N` | Show at most N rows |
| `-json` | Machine-readable output |

## How it works

**Size** is the allocated size on disk (what `du` reports) of every installed
version under `$(brew --prefix)/Cellar/<formula>`, or for casks the
`Caskroom/<token>` directory plus any app bundles the cask installed. Hard links
are counted once. Versions other than the current one are reported as
reclaimable (`brew cleanup` removes them), as are leftover `*.upgrading`
directories in the Caskroom.

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
yesterday; the notes column says which dependent kept it active. Use `-direct`
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
  thinks it is installed; `brew uninstall --cask <name>` removes the leftovers.
