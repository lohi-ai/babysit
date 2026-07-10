# Changelog

## 1.55.0 — 2026-07-11

### Changed

- **Thin plan.md contract** — `plan-draft` now writes a **5–15 line** plan with
  inline bold labels (`**Goal:** / **Out of scope:** / **Approach:** /
  **Unknowns:** / **Verify:** / **Design:**`), replacing the old
  `## Goal/Scope/Approach/Reuse/Files/Verification/Risks/Next` headings. The
  plan carries only what a human must react to and what `implement` can't
  re-derive; task order, file lists, and step detail move to `implement`.
  - `implement` derives files and task order from the code (the plan no longer
    lists them) and reads reuse notes from the plan's `**Approach**`.
  - `qa` and the `builder` workflow read the plan's `**Verify:**` line (was
    `## Verification`).

### Added

- **PR reviewer quiz** — `create-pr` ends the PR body with 2–3 questions
  probing what the diff alone can't show (behavior riding on existing code
  paths, the consequence of a deviation, what else the change can reach), with
  answers collapsed in a `<details>` block so the reviewer self-checks before
  merging. `NEXT` reminds the human to pass the quiz first.
- **Token-skinned design prototypes** — `design-ui` mocks now inline the
  project's real tokens and imitate inventory components copied from the
  nearest real screen, so the prototype looks like the product `implement`
  builds to. Every element maps to a named `DESIGN.md` component or is flagged
  `NEW:` with a one-clause why; free-form styling only for projects with no UI
  yet.
- **review-pr living rulebook** — the review's learned rules
  (`<repo>/.babysit/review-pr.md`) now dedup against paraphrases, score by
  hits/misses with recency decay, and compact/expire when the file grows,
  sharing Swatter's entry format so both books stay mutually readable.

## 1.54.0 — 2026-07-09

### Added

- **Native task list mirroring** — multi-step skills now mirror their driving
  artifact into Claude Code's native task list (`TaskCreate`/`TaskUpdate`) as
  the visible progress view, while disk artifacts stay the durable state. New
  §Native task list in `references/preamble.md` sets the contract: seed tasks
  from the skill's artifact (`plan.md`, the QA flow matrix, workflow
  milestones), mark each `in_progress` on start and `completed` only when its
  check passes, and on cold resume rebuild the list from disk — never the
  reverse.
  - `autopilot` mirrors workflow milestones at loop entry (rebuilt from
    checkpoint + `plan.md` on cold re-entry); step skills add finer tasks to
    the same list.
  - `implement` derives its task list from `plan.md` (one task per verifiable
    unit) instead of an ad-hoc list beside the plan; deviations update both
    the task list and `## Deviations`.
  - `qa` mirrors its flow matrix one task per case, closed only when evidence
    lands.
- **Native plan mode handoff** — `plan-draft` in a developer session already
  in plan mode presents the finished draft through `ExitPlanMode` (native
  approval is the "plan accepted" checkpoint) and defers `plan.md` /
  `set-pointer` writes until after approval. Unattended runs never enter plan
  mode; `plan.md` on disk remains the accepted plan.

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
