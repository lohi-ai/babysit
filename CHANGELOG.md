# Changelog

## 1.53.0 — 2026-07-09

### Changed

- **Builder bootstrap gate** — an unconfigured repo
  (`state_repo_configured=0`) no longer dead-ends in `NEEDS_CONTEXT`
  pointing at the developer-only `setup-project`. The builder workflow now
  seeds `.babysit/git-flow.yaml` with detected defaults (base from
  `origin/HEAD` → local `main`/`master` → current branch; `push` only when
  a remote exists; `mode: branch`) and keeps going, recommending
  `/bbs:setup-project` in the handoff for the QA harness only.
  `test_autopilot_readiness_gate.sh` now executes the seed block.
- **Git is autopilot's job, end to end** — step skills are infra-isolated:
  `implement` and `qa` never branch, commit, or push (QA fixes edit the
  checkout; the workflow commits and lands them). Autopilot init `git
  init`s a bare folder, and the builder workflow commits each skill's
  output itself. `CLAUDE.md` over-strict pattern #5 extended to cover all
  git mutations.
- **Plain-language guidance for humans** — with `INVOKER=developer`,
  autopilot leads every stop (handoff, `NEEDS_CONTEXT`, final status) with
  one plain sentence plus the exact next command to paste, so a
  non-technical user can drive a build end to end without knowing git.

## 1.52.1 — 2026-07-08

### Changed

- **Skill/reference condensation** — `autopilot` SKILL + `builder` workflow and
  the shared references (`archetypes`, `auto-decision-framework`, `git-flow`,
  `handoff-contracts`, `preamble`, `ticket-layout`, `ticket-size-rubric`)
  rewritten much shorter (net ≈ −850 lines) with the same contracts: gates,
  statuses, and file schemas are unchanged; prose and examples trimmed.
- New over-strict pattern #5 in `CLAUDE.md` (git-flow protocol belongs to
  workflows, not skills); `qa` operates on the current checkout as-is.

### Added

- **finding-unknowns reference** (`references/finding-unknowns.md`) — deriving
  unstated requirements from code and git history; wired into `plan-draft`,
  `investigate`, and `qa`. Companion blog posts under `blogs/`.

### Fixed

- **Inline `# comments` in YAML values** — `bbs-ticket` (git-flow `mode:`),
  `bbs-autopilot` (`base_branch:` / `branches.develop:`), and all
  `bbs-qa-config` scalar parsers (including the `credentials:` block, whose
  documented template carries an inline comment) now strip trailing comments
  before validating values. Covered by `test_bbs_ticket_git_flow_mode.sh` and
  `test_qa_config_loader.sh`.
- **qa/browse credential fallback drift** — `qa`'s snippet now falls back to
  the standard `QA_USER` / `QA_PASS` names like `browse`, instead of clobbering
  the values `bbs-secrets load` just exported.

## 1.51.2 — 2026-07-06

### Added

- **Standard QA credentials** — optional `credentials:` block in the qa.yaml
  simple shape naming the standard login env vars (`QA_USER` / `QA_PASS`), whose
  values live in the gitignored `.babysit/.env`. `bbs-qa-config probe` surfaces
  them as `QA_ENV_{USERNAME,PASSWORD}_ENV`; the `qa` and `browse` skills resolve
  login creds via `bbs-secrets load` and `BLOCK` (naming `.babysit/.env`) when a
  required credential resolves empty; `setup-project` seeds placeholders with
  `bbs-secrets seed`. Covered by `test_qa_config_loader.sh`.

## 1.51.0 — 2026-07-04

### Removed

- **Product mode** — deleted `bin/bbs-product`, `qa-product-mode.md`, the
  product-folder docs, `setup-workspace` templates, and the product-mode test
  suite (`test_bbs_product_*`, `test_qa_merge_*`, `test_qa_deploy_*`, the
  concurrency harness). Multi-repo work now runs through the trunk-worktree /
  merge-mode flow.
- **`babysitter` submodule** and its `.gitmodules` entry.

### Added

- **Merge mode / git-flow** — `bbs-ticket` and `bbs-slug` reworked around
  `bbs-ticket merge-base` plus cross-repo dispatch (`RELATED_*_REPO`), replacing
  product mode. New coverage: `test_bbs_ticket_git_flow_mode.sh`,
  `test_bbs_ticket_merge_base.sh`, `test_bbs_ticket_switch.sh`.

### Changed

- **review-pr** moves to a multi-agent finder/validator model; `qa` and
  `browse` SKILL docs updated; added `blogs/review-pr.md`.
