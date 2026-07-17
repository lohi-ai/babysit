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
behaves differently depending on the name it's invoked as. As of this release it
ships **two subcommands**, because only two of babysit's ~20 bins are ported to
Go so far:

| You run | Runs | What it does |
|---------|------|--------------|
| `bbs config …` / `bbs-config …` | `config` | read/write `~/.babysit/config.yaml` |
| `bbs env …` / `bbs-env …` | `env` | env resolution for babysit skills (`.env.base` auto-load) |

Everything else in babysit — `bbs-autopilot`, `bbs-ticket`, `bbs-slug`,
`bbs-db`, and the rest — is still a bash script and is **not** in the brew/tarball
artifact. Those install with the skill pack via `bin/setup-skills`. `brew install
bbs` does not, and is not meant to, give you the whole toolkit.

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
