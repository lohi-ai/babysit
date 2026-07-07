# Changelog

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
