# Installing the `bbs` CLI

This page covers the **standalone `bbs` command-line binary** — the Go CLI
distributed as release artifacts. Mac users install it with Homebrew; Linux
users use the tarball.

> **This is not how you install babysit.** Babysit is a Claude Code skill pack;
> it installs via `git clone` + `bin/setup-skills` (see the
> [README Quick start](../README.md#quick-start)) — which builds `bbs` and
> symlinks it into `~/.local/bin/`. This page is only for getting the compiled
> `bbs` binary onto your `PATH` on its own, with no checkout behind it.

## What `bbs` gives you today

`bbs` is a [multicall binary](#how-the-aliases-work): one executable that
behaves differently depending on the name it's invoked as. Most of babysit's
core bins are now Go and ship inside this one binary, reachable as `bbs <sub>`:

| You run | Runs | What it does |
|---------|------|--------------|
| `bbs config …` (alias `bbs-config`) | `config` | read/write `~/.babysit/config.yaml` |
| `bbs ticket …` | `ticket` | ticket identity core (`env`, `resolve`, `set-verdict`, `verdict-status`, `session`, `board`) — see the strangler note below |
| `bbs upgrade` | `upgrade` | `git pull` + `setup-skills`, writes a `JUST_UPGRADED` marker; `upgrade check` prints `UPGRADE_AVAILABLE` when a newer release exists |
| `bbs secrets …` (alias `bbs-env`) | `secrets` | project-local `.babysit/.env` credential loader (`load` / `seed` / `ensure-gitignore`), env resolution with `.env.base` auto-load (`resolve` / `is-set` / `list-prefix` / `prompt`), and `.babysit/qa.yaml` fields (`qa probe` / `qa list` / …) |
| `bbs design …` | `design` | design-intelligence broker (`tokens` / `suggest` / `components` / `ux-check`) — the CSV/DESIGN.md data files ship with the skill pack |

**Strangler note on `ticket`:** the Go `ticket` command owns the identity core
(resolve, verdicts, session, board), the index.json state-accessors
(`env`, `get`, `set-status`, `set-phase`, `set-parent`, `add-child`,
`add-relation`, `set-sibling`, `add-label`, `set-pointer`, `get-pointer`,
`ensure-size`, `append-history`), the file-only manifest.yaml ops
(`init`, `get-manifest`, `set-branch`), the base-ops family (`merge-base`,
`switch`, `reset-base`, `qa-lease`, `serve`, `refresh`), `ensure`, and
`path`/`list`/`reconcile`/`find-similar`. `bbs ticket` is now entirely
self-contained in the binary — a brew-only install runs every subcommand
without the skill pack.

**Note on `dashboard`:** released binaries carry the built SPA inside them
(`internal/webui`, staged by `scripts/build-webui.sh` in goreleaser's
before-hooks), so `bbs dashboard` on a brew-only install serves the real
dashboard with no checkout and no npm. A checkout's own `web/dist` takes
precedence when it exists, so `bbs dashboard build` still does what it always
did. `--snapshot` needs real files next to `index.html`, so it unpacks the
embedded copy into `~/.babysit/cache/dashboard` and writes `data.js` there.

**Note on `design`:** the `design` command itself is Go (ships in the binary),
but its CSV/DESIGN.md data files live in the skill pack, so a brew-only
`bbs design suggest` needs `--data <dir>` pointed at a skill-pack checkout.

Every subcommand is now Go, `ticket` included — no production bash
remains. `brew install bbs` still does not, and is not meant to, give you the
whole toolkit: the skill pack (skills, workflows, DESIGN.md/CSV data) comes
only from the clone + `bin/setup-skills`.

`bbs --version` (or `-v`) prints the version. A release binary reports the tag
it was built from, injected at build time; a clone install has no injected value
and reads `VERSION` from the checkout instead, so it stays accurate after a
`git pull` without a rebuild. `unknown` means neither source was available.

## macOS — Homebrew (primary)

```bash
brew tap lohi-ai/babysit https://github.com/lohi-ai/babysit
brew install bbs
```

The explicit tap URL is required because the repository is `lohi-ai/babysit`
rather than the conventional `homebrew-babysit` name.

Verify:

```bash
bbs --help          # babysit CLI
bbs config list     # prints ~/.babysit/config.yaml
```

Upgrade / uninstall:

```bash
brew upgrade bbs
brew uninstall bbs
```

## Linux — tarball (secondary)

No Homebrew formula path on Linux; download the tarball for your architecture
from the [latest release](https://github.com/lohi-ai/babysit/releases/latest)
and put `bbs` on your `PATH`.

```bash
VERSION=<version>          # e.g. 1.55.20, without a leading v
ARCH=amd64                 # or arm64
curl -fsSL -o bbs.tar.gz \
  "https://github.com/lohi-ai/babysit/releases/download/v${VERSION}/bbs_${VERSION}_linux_${ARCH}.tar.gz"

# Verify the checksum against checksums.txt from the same release, then:
tar -xzf bbs.tar.gz bbs
install -m 0755 bbs ~/.local/bin/bbs          # or /usr/local/bin

# Recreate the aliases the formula would make for you:
ln -sf bbs ~/.local/bin/bbs-config
ln -sf bbs ~/.local/bin/bbs-env
```

Checksums for every artifact are published as `checksums.txt` on the release.

## Platform matrix

Released artifacts (no Windows — see below):

| OS | arch | artifact |
|----|------|----------|
| macOS | arm64 (Apple Silicon) | `bbs_<version>_darwin_arm64.tar.gz` |
| macOS | amd64 (Intel) | `bbs_<version>_darwin_amd64.tar.gz` |
| Linux | arm64 | `bbs_<version>_linux_arm64.tar.gz` |
| Linux | amd64 | `bbs_<version>_linux_amd64.tar.gz` |

**Windows:** `bbs` is cross-compiled for Windows in CI purely as a regression
check (so a change that breaks the Windows build fails a PR). **No Windows
artifact is published.**

## How the aliases work

`bbs` inspects `argv[0]`: the alias `bbs-config` runs the `config` subcommand,
and the alias `bbs-env` runs the env resolver that now also answers to
`bbs secrets resolve`. The Homebrew formula installs the real binary once and
adds `bbs-config` / `bbs-env` as symlinks to it — so the `bbs-*` names work
exactly like the in-repo dev symlinks, without a separate build per bin.

`env` and `qa-config` are still reachable as top-level commands, but hidden
from `bbs --help`: they moved under `secrets`, and the old spellings stay only
so a brew-updated binary keeps working for a skill pack that hasn't upgraded
yet. New callers should use `bbs secrets …`.

## For maintainers: cutting a release

The release pipeline is built but fired by a human, never by automation.

1. Bump `VERSION` and its two mirrors in `.claude-plugin/marketplace.json`
   (`metadata.version`, `plugins[0].version`) — the 3-place rule in
   [CLAUDE.md](../CLAUDE.md#releasing--version-bumps).
2. Tag and push: `git tag "v$(cat VERSION)" && git push origin "v$(cat VERSION)"`.
3. `.github/workflows/release.yml` guards that the tag equals `v$(cat VERSION)`,
   runs goreleaser to build the four archives + `checksums.txt`, publishes a
   **draft** GitHub Release, then rewrites `Formula/bbs.rb` with the real
   per-platform checksums and commits it to the default branch.
4. Review and publish the draft release.

Validate the pipeline locally without tagging:

```bash
goreleaser check                       # config is valid
goreleaser build --snapshot --clean    # 4 binaries
brew style Formula/bbs.rb              # formula lint
```
