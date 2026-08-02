# Workspaces — the multi-repo registry

A **workspace** is a named list of repos that make up one product. It answers
the question a foreman has to answer before it can hand out work: *which repos
am I responsible for, and where are they on this machine?*

```bash
bbs workspace create acme
bbs workspace add-repo acme --git-url git@github.com:acme/web.git --path ~/src/web --role fe
bbs workspace add-repo acme --git-url git@github.com:acme/api.git --path ~/src/api --role be
bbs workspace list
bbs workspace show                 # membership of the repo you are standing in
```

## Three things are called "workspace"

The word is overloaded in this codebase, and the overload is load-bearing —
all three exist and none is going away. Only one of them is *this* thing.

| Meaning | What it is | How it is named in output |
|---|---|---|
| 1. cmux workspace | a cmux pane group — one visible worker, its terminal and browser split | always written **"cmux workspace"**, never bare |
| 2. worktree pool | `<repo>/.babysit/worktrees/<ticket>_<slug>`, one git worktree per ticket | always written **"worktree"** — never "workspace" |
| 3. registry workspace | this file's subject: a named set of repos | always carries its name — **"workspace acme"** |

The rule for anything you write — CLI output, skill text, docs: **meaning 3
always appears with its name attached**, meaning 1 always carries the `cmux`
prefix, and meaning 2 is called a worktree. Bare "workspace" with no name and
no prefix is a bug in the message.

`foreman` gains no new field for this. A foreman record already carries
`ProjectDir`; its registry workspace is whatever that repo's `config.yaml`
declares. (`foreman.Record.WorkspaceDir` / `WorkspaceRef` / `WorkspaceTitle`
are meaning 1 — the cmux workspace the worker runs in.)

## Two files, two commands

| File | Committed? | Read by |
|---|---|---|
| `~/.babysit/workspaces/<name>.yaml` | no — machine-local | `bbs workspace list/show/add-repo` |
| `<repo>/.babysit/config.yaml` | **yes** | `bbs workspace config` |
| `~/.babysit/config.yaml` | no | `bbs config` — a *different* file that happens to share the basename |

The split is machine-locality. Local paths differ per developer, so they live
in the machine-local registry; workspace membership is a fact about the repo,
so it is committed. Nothing with a machine-local absolute path is ever written
to a committed file.

`<repo>/.babysit/config.yaml`:

```yaml
workspace: acme          # required — the back-pointer
harness_version: 1.55.9  # nullable; written by `bbs workspace config stamp`
name: Web App            # optional
description: customer-facing storefront   # optional
repo_type: polyrepo      # optional: monorepo | polyrepo
```

`harness_version` is **null-by-default and null is fine**: it is the correct
reading for every repo configured before this existed, so it never warns and
never blocks. It exists so `bbs workspace show` can say "set up by an older
babysit" when it *does* know.

`repo_type` is not decoration — `monorepo` makes `bbs ticket serve` skip the
sibling fan-out, because a monorepo's siblings are directories in the same
checkout and there is nothing to resolve.

## Sibling paths: one authority

`bbs ticket serve` needs local paths for a ticket's sibling repos. Two sources
can supply them:

1. the workspace registry, matched by role — **authoritative**
2. `RELATED_*_REPO` in `<repo>/.babysit/.env` — fallback

A repo with no `.babysit/config.yaml` uses (2) exactly as it always has; there
is no migration and nothing to opt into. A repo that is registered uses (1).
When both name a role and **disagree**, babysit blocks and prints both paths
rather than picking one — silently serving the wrong checkout is the failure
this authority exists to prevent.

## Tests

Any test that touches the registry must redirect `BABYSIT_HOME`, via
`workspace.TestHome(t)`. `workspace.Dir()` panics under `go test` when
`BABYSIT_HOME` is unset, so a test cannot silently write into the human's real
`~/.babysit/workspaces/`. Note `BABYSIT_STATE_DIR` does **not** redirect this
store — it only redirects `internal/config`.
