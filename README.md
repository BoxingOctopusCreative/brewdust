# brewdust

Find the Homebrew packages that are gathering dust: how much disk each installed
formula and cask uses, how long it has been since you actually used it, and
remove what you don't need, including the leftovers Homebrew doesn't clean up
on its own.

```
 brewdust   all   requested   idle ≥ 90d   idle ≥ 1y   orphans   broken casks                                429 packages · 21.4 GB
╭─ Packages · by size ─────────────────────────────────────────────────────────── 6/429 ─╮╭─ Details · ansible ────────────────────╮
│   NAME                       KIND          SIZE         LAST USED     IDLE  NOTES      ││ ansible  formula 14.3.1                │
│   go@1.25                    formula     6.1 GB ██████  2026-11-08   today  requested… ┃│ Automate deployment, configuration,    │
│   calibre                    cask        1.9 GB █████▍  —             2.7y  +2 old ve… ││ and upgrading                          │
│ ● llvm@22                    formula     1.6 GB █████▎  —              2mo  dep of 1;… ││                                        │
│   qemu                       formula   729.5 MB ████▊─  —              2mo  requested  ││ Size         551.2 MB ████████████▌─── │
│ ● podman-desktop             cask      552.7 MB ████▋─  2026-11-07   today             ││ Idle              2mo █▋────────────── │
│   ansible                    formula   551.2 MB ████▋─  2026-08-21     2mo  requested  ││ Last used   2026-08-21 15:18           │
│   gcc                        formula   503.9 MB ████▋─  —              2mo  requested  ││             atime 14.3.1/libexec/lib/… │
│   rust                       formula   458.2 MB ████▌─  —              2mo  requested  ││ Installed   2026-08-21                 │
│   qtwebengine                formula   394.0 MB ████▌─  —              3mo  dep of 2;… ││ Source      installed by you           │
│   balenaetcher               cask      392.5 MB ████▌─  2026-11-05      2d             ││                                        │
│   azure-cli                  formula   377.4 MB ████▍─  2026-10-03     35d  requested  ││                                        │
│   mono                       formula   325.1 MB ████▍─  —             18mo  requested  ││                                        │
│   go                         formula   283.3 MB ████▎─  —              2mo  requested  ││                                        │
│   pandoc                     formula   271.3 MB ████▎─  —              23d  requested  ││                                        │
│   gstreamer                  formula   224.9 MB ████▏─  —              2mo  requested  ││                                        │
│   qt@5                       formula   217.5 MB ████▏─  —              3mo  requested  │╰─ enter to zoom ────────────────────────╯
│   awscli                     formula   208.5 MB ████▏─  2026-09-22     46d  requested  │╭─ Disk ─────────────────────── 21.4 GB ─╮
│   trivy                      formula   203.7 MB ████▏─  —              2mo  requested  ││ Formulae   ██████████████▎──   18.0 GB │
│   solargraph                 formula   192.8 MB ████──  —              3mo  requested  ││ Casks      ██▋──────────────    3.4 GB │
│   mysqlworkbench             cask      187.6 MB ████──  —             2.7y  +1 old ve… ││ Idle ≥ 1y  ██───────────────    2.6 GB │
│   trufflehog                 formula   169.8 MB ████──  —              2mo  requested  ││ Old vers.  █████▍───────────    6.8 GB │
│   deno                       formula   157.4 MB ████──  —              2mo  dep of 1;… ││ Orphans    ─────────────────       0 B │
│   qtdeclarative              formula   156.2 MB ████──  —              2mo  dep of 27… ││ Broken                         5 casks │
│   multipass                  cask      151.9 MB ███▉──  —              3mo             ││ Marked     █▋───────────────    2.1 GB │
│   opencode                   formula   143.9 MB ███▉──  —              2mo  requested  ││                                        │
╰─ ● 2 marked · 2.1 GB · d remove · esc clear ───────────────────────────────────────────╯╰─ c to clean up ────────────────────────╯
 NORMAL   ↑↓  move   space  mark   d  remove   c  clean   /  search   v  view   t  type   s  sort   enter  zoom   ?  help
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

Run `brewdust` with no arguments. The layout borrows from
[Zellij](https://zellij.dev): a tab bar of views across the top, framed panes
with their titles in the border (the focused one in green), and a status bar of
key hints at the bottom.

- **Packages** lists everything in the current view with a log-scale size bar,
  colour-coded idle time, and the scroll position in its frame.
- **Details** follows the selection: size and idle gauges, the evidence for
  "last used", what depends on it, old versions, and missing apps. Press
  `enter` to zoom it to full screen.
- **Disk** breaks down where the space goes: formulae vs. casks, packages idle
  for a year or more, reclaimable old versions, orphans, broken casks, and what
  you've marked.
- Removal, cleanup and help open as floating panes over the dimmed list.

The side column hides on terminals narrower than 100 columns, and the Disk
pane on ones shorter than 24 rows.

| Key | Action |
| --- | --- |
| `↑`/`↓`, `j`/`k`, `pgup`/`pgdn`, `g`/`G` | Move |
| `space` / `a` | Mark the package / mark everything shown |
| `enter` | Zoom the details pane |
| `/` | Search names and descriptions |
| `v` | Next view tab: all, requested, dependencies, idle ≥ 90d, idle ≥ 1y, orphans, broken casks |
| `t` / `s` | Cycle type (all, formula, cask) / sort (size, idle, name, deps) |
| `D` | Toggle whether use by dependents counts |
| `d`, `x`, `delete` | Remove the marked packages, or the selected one |
| `c` | Clean up old versions, orphans and broken casks |
| `r` / `?` / `q` | Rescan / help / quit |

Removing or cleaning up always shows a confirmation listing exactly what will
run and roughly how much space it frees. Formulae that something else still
needs are skipped, with the reason.

When you confirm, the work runs inside the interface, in a floating progress
pane: one package at a time (dependents before what they depend on, so brew's
checks pass), with a progress bar, space freed so far, a ✓/✗ per package and
the tail of brew's output. `esc` stops after the current package rather than
interrupting brew mid-uninstall. Afterwards brewdust rescans; anything that
failed stays marked.

brew runs detached from the terminal so it can't scribble over the interface.
The catch is that it can't ask for a password either: a few pkg-based casks
(e.g. macfuse) need `sudo` to uninstall. Those steps fail with a note, and
pressing `t` re-runs just the failed ones in the terminal, where brew can
prompt; press Enter afterwards to come back.

The mouse works too:

| Mouse | Action |
| --- | --- |
| Click a package | Select it; double-click to zoom its details |
| Click the `●` column, right-click, or ctrl-click | Mark or unmark |
| Shift-click | Mark everything from the selection to here |
| Drag the scrollbar thumb | Scroll; click the track to jump there |
| Wheel | Scroll the list |
| Click a tab / column header | Switch view / sort by that column |
| Click a row in the Disk pane | Open cleanup for just that category |
| Click a key hint in the status bar | Same as pressing the key |
| Click outside a floating pane | Close it |

Some terminals reserve shift-click for selecting text; hold your terminal's
override key (e.g. ⌥ in iTerm2, fn in Terminal.app) to select text instead.

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
| `old` | Non-current formula versions | `brew cleanup <formula>...` (see note below) |
| `old` | Stale Caskroom version directories, including interrupted `*.upgrading` ones | deleted directly, since `brew cleanup` skips them. brewdust only deletes `Caskroom/<cask>/<version>` paths |
| `orphans` | Dependencies nothing installed needs | `brew autoremove` |
| `broken` | Casks whose app was deleted outside brew | `brew uninstall --cask` |

`brew cleanup` leaves a formula's old versions alone while it's pinned, or
while a newer version than the one installed is available. brewdust lists
those as "kept by brew cleanup" rather than reclaimable, leaves them out of
the cleanup plan and totals, and shows the reason in the details pane. Run
`brew upgrade <formula>` (or unpin it) and they become reclaimable.

### Filter flags

Shared by the interactive view and `list`.

| Flag | Meaning |
| --- | --- |
| `--sort size\|idle\|name\|deps` | Sort order (default `size`); `deps` puts the most depended-on first |
| `-t, --type all\|formula\|cask` | Limit to one package type |
| `--stale N` | Only packages idle for at least N days |
| `--requested` | Only packages you installed explicitly (casks always count) |
| `--orphans` | Only dependencies with no installed dependents |
| `--broken` | Only casks whose app was deleted outside brew |
| `--deps` | Only packages that other installed packages depend on |
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

**Dependencies.** The NEEDED BY column (in the TUI and `list`) shows what
directly depends on each package, e.g. `rust` or `qtconnectivity +7`. The
details pane lists those dependents, how many more need it indirectly, and
what the package itself depends on; `--json` includes `dependencies`,
`dependents` and `all_dependents`. Homebrew records each package's runtime
dependencies transitively, so brewdust works out the direct links from what is
actually installed. Removal checks the full indirect set: a formula is skipped
if anything you're keeping needs it, even through another package.

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

## Development

```sh
go test ./...
```

The tests don't touch your Homebrew installation. `internal/brewtest` builds a
fake one in a temporary directory (Cellar and Caskroom files with chosen
access times) and puts a stub `brew` on `PATH` that answers from it and logs
every call, so `list`, `remove` and `clean` are tested end to end.

## Releases

Every pull request merged into `main` is released automatically. The release
workflow runs the tests again, cross-compiles brewdust for macOS and Linux
(arm64 and amd64), tags the merge commit, and publishes a GitHub release with
the archives, a `checksums.txt` and notes generated from the merged PRs.

A label on the pull request picks the version bump:

| Label           | Effect                         |
|-----------------|--------------------------------|
| `release:major` | v1.4.2 → v2.0.0                |
| `release:minor` | v1.4.2 → v1.5.0                |
| `release:patch` | v1.4.2 → v1.4.3 (also no label) |
| `release:none`  | merge without releasing        |

The first release is v0.1.0 (or v1.0.0 for `release:major`). A check on each
pull request fails if it has more than one of these labels, and says what
merging it will release. Commits pushed straight to `main` aren't released;
run the Release workflow by hand (Actions → Release → Run workflow) to
release them.

To create the labels once, in a clone of the GitHub repository:

```sh
gh label create release:major --color B60205 --description "Breaking change: bump the major version"
gh label create release:minor --color FBCA04 --description "New feature: bump the minor version"
gh label create release:patch --color 0E8A16 --description "Fix: bump the patch version (the default)"
gh label create release:none  --color CCCCCC --description "Merge without making a release"
```

### Signing

The macOS binaries are signed with a Developer ID Application certificate and
notarized by Apple, so they run without a Gatekeeper warning when downloaded.
The release fails rather than publish unsigned macOS binaries. It needs these
repository secrets (Settings → Secrets and variables → Actions):

| Secret                       | What it is                                                     |
|------------------------------|----------------------------------------------------------------|
| `MACOS_CERTIFICATE_P12`      | The Developer ID Application certificate and its private key, as a base64 `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | The password the `.p12` was exported with                       |
| `NOTARY_KEY_P8`              | An App Store Connect API key (`AuthKey_….p8`), base64            |
| `NOTARY_KEY_ID`              | That key's ID                                                   |
| `NOTARY_ISSUER_ID`           | The issuer ID shown above the list of keys in App Store Connect |

To export the certificate, open Keychain Access, find "Developer ID
Application: …" under My Certificates, and choose File → Export Items… as a
`.p12` with a password. Then, from a clone of the GitHub repository:

```sh
base64 -i DeveloperID.p12 | gh secret set MACOS_CERTIFICATE_P12
gh secret set MACOS_CERTIFICATE_PASSWORD
base64 -i ~/.appstoreconnect/private_keys/AuthKey_XXXXXXXXXX.p8 | gh secret set NOTARY_KEY_P8
gh secret set NOTARY_KEY_ID --body XXXXXXXXXX
gh secret set NOTARY_ISSUER_ID --body xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

Delete the exported `.p12` afterwards. The workflow imports the certificate
into a temporary keychain on a macOS runner and deletes it when the job ends.
The certificate it signs with defaults to Ryan Draga's; set the
`SIGN_IDENTITY` repository variable to use another.

### Building locally

```sh
scripts/release/build.sh v0.0.0-dev dist
```

That leaves the macOS binaries unsigned (with a warning). To sign and
notarize them as a release does, with a stored notarytool profile:

```sh
SIGN_IDENTITY="Developer ID Application: Ryan Draga (QA32Z4XU99)" NOTARY_PROFILE=brewdust scripts/release/build.sh v0.0.0-dev dist
```

## License

brewdust is licensed under the [Mozilla Public License 2.0](LICENSE). You can
use it, modify it and ship it in other software, including proprietary
software; changes to brewdust's own files must be shared under the same
license.
