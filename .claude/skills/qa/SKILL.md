---
name: qa
description: Systematically test a web application, fix issues caused by the current change, and re-verify. Use for full QA loops, critical user flows, release checks, or test-and-fix requests.
---

# qa

Exercise the application like a user and leave reproducible evidence.

## Flow

1. Load what to test — disk first, conversation as fallback. Resolve the
   ticket (`bbs-ticket resolve`); when it resolves, read whichever of these
   exist:
   - requirement: `bbs-ticket path requirement --read` — the acceptance criteria
   - plan: `bbs-ticket path plan --read` — especially its `## Verification` section
   - implement brief: `bbs-ticket path handoff --skill implement --latest --read`
     — SUMMARY / FILES / APPROACH / BLAST_RADIUS
   - review verdict: `bbs-ticket path verdict --skill review-pr --read` —
     carry unresolved `FINDINGS` into the case matrix; `RISK_AREAS` and
     `FIXED` spots seed the security and regression cases
   Missing docs are not a gate — fall back to conversation context. Then take
   the change surface from the diff (`BASE=$(bbs-autopilot base-branch)`;
   `git diff $(git merge-base origin/"$BASE" HEAD)`), and note the target URL
   and available credentials/config. `NEEDS_CONTEXT` only when neither docs
   nor conversation can name a single intended behavior to verify.
2. Boot or probe the local project target first. Hosted URLs are allowed only when local run is impossible and the reason is recorded.
   **Worktree detection — before booting or probing anything.** The ticket
   under test lives in a worktree when cwd is under `.babysit/worktrees/` or
   the ticket manifest records a `worktree:` value other than `.` (`.` means
   no worktree — trunk or in-place tickets are already served directly; skip
   the landing step). If so, the shared dev
   server in the primary checkout does NOT serve the change yet — land it
   first:
   - from the worktree: `bbs-ticket merge-base`
   - from the primary: `bbs-ticket switch <ticket>` (resets the surface to
     base + exactly this ticket)
   Both BLOCK loudly on a dirty/off-base surface or a merge conflict —
   surface that verbatim rather than testing a stale surface. Fixes go in
   the worktree — commit, re-run `merge-base`/`switch`, re-test; never edit
   the primary checkout (see `../references/git-flow.md` § QA loop).
3. Run code-level checks (tests, typecheck, lint) first — cheapest signal
   first; they gate, they don't prove.
4. Build a focused flow matrix from the acceptance criteria — not just the
   diff — covering happy path, validation, empty/error states, failure/retry
   behavior, and responsive behavior (see § Case design). Write it down
   before touching the app, each case anchored to its source ("criterion 2",
   "BLAST_RADIUS: settings", "review-pr finding 1"), then self-review the
   matrix against step 1's context:
   - every acceptance criterion has ≥1 case; every BLAST_RADIUS entry has a
     regression case; every unresolved review finding has a probe;
   - each touched flow has at least one non-happy-path case;
   - a criterion the matrix can't cover is named as a gap now (it becomes a
     finding or an `N/A: <reason>`), never silently dropped.
   Fix matrix gaps before executing; save the reviewed matrix to
   `bbs-ticket path evidence --skill qa --name test-matrix.md --write`.
5. Execute the flows end-to-end with a real client. For web UI that means
   the `browse` engine (agent-browser — see `../browse/SKILL.md` § Engine):
   open, snapshot, click/type through the actual user journey; capture
   snapshots, console errors, and failed requests. For non-UI targets the
   equivalent is a real call sequence — curl against the running API, a CLI
   invocation, or the repo's own e2e suite. Never "test" a flow by reading
   code or by unit tests alone.
6. Fix regressions owned by the current branch, then rerun the affected flow and checks.
7. Finish with one full end-to-end pass of the primary user journey on the
   final code state. The last test executed is the one the verdict stands
   on — any code change after it invalidates the verdict; rerun before
   reporting. Screenshot this verdict-bearing pass — and each failure or
   fixed reproducer — to a durable path
   (`bbs-ticket path evidence --skill qa --name <f>.png --write`), and list
   the paths in the verdict's `EVIDENCE:` line. Ad-hoc `browse` checks stay
   screenshot-light; the QA verdict is the exception — its screenshots are the
   durable proof a human audits after the fact.

## Case design

Turn each acceptance criterion (and the plan's `## Verification`) into a user
journey, not a component check: *as a user, do `<steps>` → observe `<result>`*.
One case = journey + expected observable + evidence to capture.

- The **primary journey** is the one path a user takes to get the feature's
  core value end-to-end. It carries the verdict and runs last (Flow step 7).
- E2E earns its cost at component boundaries, where unit tests are blind.
  Prefer cases that cross a layer: data shape between UI and API, state that
  must survive navigation/reload, a backend error actually surfacing in the
  UI, behavior in a production-like run (built app, real config) when it
  differs from dev.
- Tie every failure to the criterion it violates ("criterion 3: export
  completes — stuck at 99%") with reproduction steps — evidence mapped to
  the requirement, never "it doesn't feel right".

## Coverage rubric

Before reporting, score the run. This exists because the failure mode is
quitting early on a green happy path — the rubric turns "good enough?" into a
gate you must clear, not a feeling.

Grade every dimension A–D against the change's risk surface. The split is the
standard functional / non-functional QA breakdown; a dimension the change can't
touch is `N/A` **with a one-line reason** — never a silent skip.

**Functional**

| Dimension | A | B | C | D |
|-----------|---|---|---|---|
| Flow coverage | Happy + every alternate/branch flow run e2e | Happy + ≥2 alternate flows | Happy only | No flow run e2e |
| Boundary & negative | Limits, invalid input, empty/error, failure/retry all exercised | ≥1 boundary + ≥1 error case | Mentioned, not executed | None |
| Regression | Existing behavior around the change re-verified | Adjacent flow spot-checked | Assumed intact | Not considered |
| Data integrity | State correct across reload, nav, and concurrent edits | Persists across reload | Not checked | Data loss/corruption seen |

**Non-functional**

| Dimension | A | B | C | D |
|-----------|---|---|---|---|
| Compatibility | Target browsers + responsive breakpoints verified | One extra viewport checked | Default viewport only | Broken layout seen |
| Security & authz | Permission gates + input safety (authz, injection) probed | Auth-required paths checked | Noted, not tested | Access-control gap seen |
| Accessibility & UX | Keyboard path, labels/roles, contrast, clear error copy | Keyboard + visible focus | Not checked | Blocking a11y defect |
| Performance | Interactions responsive under realistic data volume | No obvious lag on happy path | Not observed | Timeout/jank seen |

**Evidence freshness** (always applies): A = final full e2e pass on the *final*
code state · B = e2e passed but code changed after · C = partial/stale e2e ·
D = unit/curl/code-read only.

`PASS`/`FIXED` require **every applicable** dimension at B or better and
freshness at A. Any applicable dimension at C or D forces `VERDICT: FAIL`
(→ `STATUS: BLOCKED`), or `DONE_WITH_CONCERNS` naming the blocker if a real
blocker stopped coverage. Report the grade line for every dimension (including
`N/A: <reason>`) so the gate is auditable.

## Rules

- Keep test data reversible and avoid destructive production actions.
- Distinguish current-change bugs from pre-existing failures.
- Do not report a fix until the original reproducer passes.
- Do not return `PASS` from happy-path-only testing.
- `PASS`/`FIXED` require an executed end-to-end run as the *most recent*
  evidence: name the tool, the journey steps, and the observed result.
  A verdict whose freshest evidence is a unit test, a homepage curl, or
  code reading is not a QA verdict.
- Do not return `PASS` if the app was never proven running locally or the local-run blocker is missing.
- Prefer 5-10 deep checks over many vague clicks.
- Every QA summary must name the local target or blocker, the case matrix, and at least one non-happy-path result.
- `VERDICT: FAIL` always pairs with `STATUS: BLOCKED` — never `DONE*`. The
  PR gate treats any `DONE*` status as ready; a failing QA that reports
  `DONE_WITH_CONCERNS` silently opens the gate.

## Output

```text
STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
VERDICT: PASS | FIXED(<N>) | FAIL
SUMMARY: <local target/blocker + flow matrix + findings>
SOURCES: <context read in step 1: requirement, plan, implement-handoff, review-pr — or conversation-only>
RUBRIC: flow=<> boundary=<> regression=<> data=<> compat=<> security=<> a11y=<> perf=<> freshness=<>  (grade or N/A each)
EVIDENCE: <last e2e run: tool + journey + result; screenshot + errors/report paths under evidence/qa/>
```

When a ticket resolves, persist the verdict so the PR/merge gate can read it —
this is what makes QA a hard gate instead of advice. Persist the **full block**
(`RUBRIC` and `EVIDENCE` included, not just STATUS/SUMMARY): `verdicts/qa.md` is
the only artifact a human or orchestrator sees after the fact, so the freshness
grade and the screenshot/log paths must live there, not just in the transcript.
A `qa-evidence` audit re-checks this body on write and the PR/merge gate
**denies** a PASS that contradicts its own rubric (freshness < A, any C/D
dimension) or carries no e2e evidence — a well-formed but thin verdict will not
ship. Record it even when full QA was impossible (set `DONE_WITH_CONCERNS` with
the named blocker; a concerns verdict with no named blocker is flagged
`unexplained`):

```bash
bbs-ticket set-verdict --skill qa --body "$(cat <<'EOF'
STATUS: ...
VERDICT: ...
SUMMARY: ...
SOURCES: ...
RUBRIC: flow=<> boundary=<> regression=<> data=<> compat=<> security=<> a11y=<> perf=<> freshness=<>
EVIDENCE: <last e2e run: tool + journey + result; screenshot/log paths under evidence/qa/>
EOF
)"
```
