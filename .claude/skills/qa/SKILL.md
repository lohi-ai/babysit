---
name: qa
description: Systematically test a web application, fix issues caused by the current change, and re-verify. Use for full QA loops, critical user flows, release checks, or test-and-fix requests.
---
# qa
Exercise the application like a user and leave reproducible evidence.
## Flow
1. Load what to test — disk first, conversation as fallback. Resolve the
   ticket (`bbs-ticket resolve`); when it resolves, read whichever exist:
   - `bbs-ticket path requirement --read` — acceptance criteria
   - `bbs-ticket path plan --read` — especially its `**Verify:**` line
   - `bbs-ticket path handoff --skill implement --latest --read` — and its
     `## Deviations`: each deviation is where the plan diverged from reality,
     the likeliest home of a wrong guess
   - `bbs-ticket path verdict --skill review-pr --read` — unresolved
     `FINDINGS` go into the case matrix; `RISK_AREAS`/`FIXED` seed security
     and regression cases
   Missing docs are not a gate — fall back to conversation. Change surface:
   `BASE=$(bbs-autopilot base-branch)`;
   `git diff $(git merge-base origin/"$BASE" HEAD)`. Target URL and login
   from `.babysit/qa.yaml`:
   ```bash
   eval "$(bbs-secrets load)"                         # exports .babysit/.env values
   ENV=$(bbs-qa-config default-env); ENV=${ENV:-local}
   eval "$(bbs-qa-config probe --env "$ENV" 2>/dev/null)"  # QA_ENV_URL, QA_ENV_{USERNAME,PASSWORD}_ENV, …
   QA_USER=$(printenv "${QA_ENV_USERNAME_ENV:-QA_USER}" 2>/dev/null || true)  # standard: QA_USER / QA_PASS
   QA_PASS=$(printenv "${QA_ENV_PASSWORD_ENV:-QA_PASS}" 2>/dev/null || true)
   ```
   Target is `$QA_ENV_URL`; sign in with `$QA_USER`/`$QA_PASS`. A needed
   credential resolving empty means the value is missing from
   `.babysit/.env` — record that as the local-run blocker rather than testing
   signed-out. No `.babysit/qa.yaml` is not a gate: derive the target from
   conversation or the repo and note how credentials were obtained.
   `NEEDS_CONTEXT` only when neither docs nor conversation can name a single
   intended behavior to verify.
2. Boot or probe the local target first; hosted URLs only when local run is
   impossible and the reason is recorded. QA the current checkout as-is —
   landing a worktree branch onto a shared surface is the autopilot
   workflow's concern (`../references/git-flow.md` § QA loop). The dev
   server lives in the repo's **primary checkout only** — never npm-install
   or boot a server inside a ticket worktree (one heavy tree per repo); if
   the local target is down and can't be started there, that's the recorded
   blocker. Server prep: when `QA_ENV_PREPARE` is set (qa.yaml `prepare:`,
   idempotent install + migrate), run it in the serving checkout after the
   change lands, before probing. When leaving a shared surface (worktree
   mode) and the ticket's diff added DB migrations, run `QA_ENV_REVERT`
   (`revert:`) before releasing the qa-lease — reset-base drops the code
   but not the schema. Before
   trusting any surface, confirm it actually serves the change (probe a
   marker from the diff); if not, name the stale surface rather than testing
   blind. Fixes edit the files in the checkout under test — committing and
   landing them, like branching, stays the invoking workflow's job —
   re-verify on the updated surface.
3. Code-level checks (tests, typecheck, lint) first — they gate, they don't
   prove.
4. Build a flow matrix from the acceptance criteria — not just the diff —
   covering happy path, validation, empty/error states, failure/retry, and
   responsive behavior. Derive the change's reach independently: for each
   changed file/function, find callers and flows sharing its state or
   routes, and give each an adjacent-regression case (BLAST_RADIUS is the
   producer's own claim). Write the matrix before touching the app, each
   case anchored to its source ("criterion 2", "review-pr finding 1",
   "derived: shares session state"). Self-review: every criterion,
   BLAST_RADIUS entry, unresolved review finding, and implement deviation
   has a case; each touched flow has ≥1 non-happy-path case; behavior the
   code walk surfaced that the requirement never mentions gets a *derived
   criterion* case with the gap named in `SUMMARY`; an uncoverable criterion
   is named as a gap now, never silently dropped. Save the matrix:
   `bbs-ticket path evidence --skill qa --name test-matrix.md --write`.
   Mirror the matrix into the native task list (TaskCreate) — one task per
   case, closed only when its evidence lands.
5. Execute the flows end-to-end with a real client. Web UI: the `browse`
   engine — Read `../browse/SKILL.md` § Engine before the first browser
   command; its session-name export and setup are mandatory. Non-UI: a real
   call sequence (curl, CLI, the repo's e2e suite). Never "test" a flow by
   reading code or unit tests alone. Spend a few minutes off-script around
   the changed surface — findings feed back as derived cases.
6. Fix regressions owned by the current branch, then rerun the affected flow
   and checks.
7. Finish with one full end-to-end pass of the primary user journey on the
   final code state — any code change after it invalidates the verdict.
   Screenshot this verdict-bearing pass — and each failure or fixed
   reproducer — to
   `bbs-ticket path evidence --skill qa --name <f>.png --write`; list the
   paths in `EVIDENCE:`. (Ad-hoc `browse` checks stay screenshot-light; the
   QA verdict's screenshots are the durable proof a human audits later.)
## Case design
One case = user journey + expected observable + evidence — *as a user, do
`<steps>` → observe `<result>`*, not a component check. The **primary
journey** delivers the feature's core value end-to-end; it carries the
verdict and runs last. Tie every failure to the criterion it violates, with
reproduction steps.
## Coverage rubric
Grade every dimension A–D against the change's risk surface; a dimension the
change can't touch is `N/A` **with a one-line reason** — never a silent skip.

- **flow**: A = happy + every alternate/branch flow e2e · B = happy + ≥2 alternates · C = happy only · D = none e2e
- **boundary**: A = limits, invalid input, empty/error, failure/retry all exercised · B = ≥1 boundary + ≥1 error · C = mentioned, not executed · D = none
- **regression**: A = existing behavior around the change re-verified · B = adjacent flow spot-checked · C = assumed intact · D = not considered
- **data**: A = state correct across reload/nav/concurrent edits · B = persists across reload · C = not checked · D = loss/corruption seen
- **compat**: A = target browsers + responsive breakpoints · B = one extra viewport · C = default viewport only · D = broken layout seen
- **security**: A = permission gates + input safety (authz, injection) probed · B = auth-required paths checked · C = noted, not tested · D = access-control gap seen
- **a11y**: A = keyboard path, labels/roles, contrast, clear error copy · B = keyboard + visible focus · C = not checked · D = blocking defect
- **perf**: A = responsive under realistic data volume · B = no obvious lag on happy path · C = not observed · D = timeout/jank seen
- **freshness** (always applies): A = final full e2e pass on the *final* code state · B = e2e passed but code changed after · C = partial/stale e2e · D = unit/curl/code-read only
`PASS`/`FIXED` require **every applicable** dimension at B or better and
freshness at A. Any applicable dimension at C or D forces `VERDICT: FAIL`
(→ `STATUS: BLOCKED`), or `DONE_WITH_CONCERNS` naming the blocker if a real
blocker stopped coverage. Report the grade line for every dimension,
including `N/A: <reason>`.
## Rules
- Keep test data reversible; no destructive production actions.
- Distinguish current-change bugs from pre-existing failures.
- Do not report a fix until the original reproducer passes.
- `PASS`/`FIXED` require an executed end-to-end run as the *most recent*
  evidence — tool, journey steps, observed result — and the app proven
  running locally (or the local-run blocker named). Happy-path-only is not a
  PASS.
- Prefer 5-10 deep checks over many vague clicks.
- Every QA summary names the local target or blocker, the case matrix, and
  at least one non-happy-path result.
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
When a ticket resolves, persist the **full block** (`RUBRIC` and `EVIDENCE`
included) — `verdicts/qa.md` is the only artifact a human or orchestrator
sees after the fact. A `qa-evidence` audit re-checks the body on write and
the PR/merge gate **denies** a PASS that contradicts its own rubric
(freshness < A, any C/D dimension) or carries no e2e evidence. Record it even
when full QA was impossible (`DONE_WITH_CONCERNS` with the named blocker; a
concerns verdict with no named blocker is flagged `unexplained`):
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
