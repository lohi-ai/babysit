# Installing the `bbs` CLI

This page covers the **standalone `bbs` command-line binary** — the Go CLI
distributed as release artifacts. Mac users install it with Homebrew; Linux
users use the tarball.

> **This is not how you install babysit.** Babysit is a Claude Code skill pack;
> it installs via `git clone` + `bin/setup-skills` (see the
> [README Quick start](../README.md#quick-start)). This page is only for getting
> the compiled `bbs` binary onto your `PATH` on its own.

## What `bbs` gives you today

`bbs` is a [multicall binary](#how-the-aliases-work): one executable that
behaves differently depending on the name it's invoked as. Most of babysit's
core bins are now Go and ship inside this one binary, reachable as `bbs <sub>`:

| You run | Runs | What it does |
|---------|------|--------------|
| `bbs config …` / `bbs-config …` | `config` | read/write `~/.babysit/config.yaml` |
| `bbs env …` / `bbs-env …` | `env` | env resolution for babysit skills (`.env.base` auto-load) |
| `bbs slug …` | `slug` | derive `<slug>` / `<ticket>` / `<branch>` from git remote + branch |
| `bbs ticket …` | `ticket` | ticket identity core (`resolve`, `set-verdict`, `verdict-status`, `session`, `board`) — see the strangler note below |
| `bbs update-check` | `update-check` | print `UPGRADE_AVAILABLE` when a newer release exists |
| `bbs upgrade` | `upgrade` | `git pull` + `setup-skills`, writes a `JUST_UPGRADED` marker |
| `bbs learnings-log …` / `learnings-search …` | `learnings-log` / `learnings-search` | append/query the decisions log |
| `bbs qa-config …` | `qa-config` | read `.babysit/qa.yaml` fields |
| `bbs telemetry-log …` | `telemetry-log` | append skill-usage telemetry rows |
| `bbs codex-competitive …` | `codex-competitive` | competitive-analysis helper |
| `bbs analytics-cron …` | `analytics-cron` | install/uninstall/run the weekly `/bbs:analytics-review` schedule |
| `bbs secrets …` | `secrets` | project-local `.babysit/.env` credential loader (`load` / `seed` / `ensure-gitignore`) |
| `bbs design …` | `design` | design-intelligence broker (`tokens` / `suggest` / `components` / `ux-check`) — the CSV/DESIGN.md data files ship with the skill pack |

**Strangler note on `ticket`:** the Go `ticket` command owns the identity core
(resolve, verdicts, session, board) and the index.json state-accessors
(`env`, `get`, `set-status`, `set-phase`, `set-parent`, `add-child`,
`add-relation`, `set-sibling`, `add-label`, `set-pointer`, `get-pointer`,
`ensure-size`, `append-history`). It still **delegates** the base-ops family
(`merge-base`, `switch`, `reset-base`, `qa-lease`, `serve`, `refresh`), the
manifest.yaml ops (`init`, `ensure`, `get-manifest`, `set-branch`), and
`path`/`list`/`reconcile` to a `bbs-ticket.bash` sitting next to the binary.
That bash sibling ships **only** with the skill pack (`bin/setup-skills`), so a
brew-only `bbs ticket <delegated-sub>` will not find it. Use the skill-pack
install for full ticket operations.

**Note on `design`:** the `design` command itself is Go (ships in the binary),
but its CSV/DESIGN.md data files live in the skill pack, so a brew-only
`bbs design suggest` needs `--data <dir>` pointed at a skill-pack checkout.

Every standalone bin is now Go. The only bash left is the `bbs-ticket.bash`
sibling the Go `ticket` command delegates its un-ported half to; it installs
with the skill pack via `bin/setup-skills`.
`brew install bbs` does not, and is not meant to, give you the whole toolkit.

There is no `bbs --version` yet.

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
bbs-config list     # prints ~/.babysit/config.yaml
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

`bbs` inspects `argv[0]`: invoked as `bbs-config` it runs the `config`
subcommand, as `bbs-env` it runs `env`. The Homebrew formula installs the real
binary once and adds `bbs-config` / `bbs-env` as symlinks to it — so the
`bbs-*` names work exactly like the in-repo dev symlinks, without a separate
build per bin.

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
