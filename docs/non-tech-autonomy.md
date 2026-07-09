# Non-tech autonomy: why the v1.53.0 design works

**Status:** BUILT (2026-07-09, v1.53.0). Changes live in
`.claude/skills/autopilot/SKILL.md`, `workflows/builder.md`, the `implement`
and `qa` skills, and `CLAUDE.md` over-strict pattern #5. Contract pinned by
`tests/test_autopilot_readiness_gate.sh`.

## The problem

A non-technical user pointing autopilot at a fresh folder hit three walls:

1. **A dead-end gate.** An unconfigured repo (no `.babysit/git-flow.yaml`)
   stopped with `NEEDS_CONTEXT` recommending `/bbs:setup-project` — a
   `developer`-only skill that asks about branch modes and QA harnesses.
   The one user who most needs autopilot to keep going was the one user
   who could not answer the question it stopped on.
2. **Git scattered across layers.** `qa` committed its own fixes, the
   workflow committed at the end, and nothing owned `git init`. Whether a
   run survived depended on which layer happened to touch git last.
3. **Jargon at every stop.** Handoffs ended in ticket ids, branch names,
   and skill names. A human who doesn't know git could not tell what had
   happened or what to type next.

## Why seeding defaults works (instead of stopping)

The Auto-Decision Framework classifies decisions as Mechanical, Taste, or
User Challenge. The old gate treated branch policy as a User Challenge. It
is actually **Mechanical**: the default shape is already documented in
`references/git-flow.md` (`base_branch` from the remote, `branch_prefix:
feat`, `mode: branch`), and every input is detectable from the repo —
`origin/HEAD` names the base, falling back to a local `main`/`master`, and
`push` is only true when a remote exists to push to. A decision whose
answer is fully derivable from the repo must be derived, not escalated
("anything derivable from the codebase, look up" — preamble.md).

The stop only ever protected the *QA harness* half of setup — the URL,
credentials, and flows that genuinely cannot be guessed. But `qa` already
treats a missing `qa.yaml` as a fallback path, not a gate. So stopping the
whole run bought nothing the downstream skill wasn't already handling; the
bootstrap gate keeps the one non-guessable ask (`/bbs:setup-project` for
QA config) as a handoff recommendation, where it belongs.

Two properties keep this safe:

- **Wrong guesses are cheap.** Every seeded value is a plain-text line in
  a config file the human (or `setup-project`) can correct later; nothing
  irreversible keys off it. `push: false` without a remote means the worst
  case of a wrong seed is a local branch, not a bad push.
- **The seed is executable, not aspirational.** The bash block in
  `builder.md` is extracted verbatim and run by
  `test_autopilot_readiness_gate.sh` in a throwaway repo — the doc cannot
  drift from what actually happens.

## Why centralizing git in autopilot works

Skills are **infra-isolated** by design: the same skill runs composed by a
workflow, typed as `/bbs:qa` on someone's dirty checkout, or mid-recovery
after a crash. A skill that commits behaves correctly in exactly one of
those shapes and destructively in the others (committing a user's
unrelated staged work, committing on the wrong branch, committing in the
base checkout instead of the worktree). A skill that only edits the
working tree is correct in *all* of them — the caller who knows the git
context applies the git consequence.

Autopilot is the only layer that has that context: it ran `bbs-ticket
ensure`, so it knows the mode (trunk/branch/worktree), which checkout is
the worktree vs. the shared test surface, and what `push:` policy allows.
Putting every mutation there (repo init → branch cut → commit per
milestone → `merge-base` landing → push) means:

- **One writer, one protocol.** The worktree QA loop ("fix in the
  worktree, commit, re-run `merge-base`, never fix in base") only holds if
  a single layer performs it. When `qa` also committed, the skill could
  commit in the checkout under test — which in worktree mode is the *base*
  checkout, exactly the state the loop forbids.
- **The crash contract stays intact.** Autopilot's resume story is
  "disk state is sufficient": checkpoint + branch encode where the run
  was. That only works if commits happen at autopilot's milestones, where
  the checkpoint is written in the same breath. Skill-side commits created
  states no checkpoint described.
- **Direct invocations match user expectation.** The harness rule is
  "commit only when asked." A standalone `implement` leaving the tree
  dirty is the same behavior a human gets from a direct ask.

## Why plain-language stops work

Autopilot's checkpoint model already concentrates human involvement at
four points (requirement, plan, QA-ready, PR). The human never needs to
*understand* git — they only need to take one correct action at each
checkpoint. So the fix is not simplifying the workflow, it is making each
stop carry: one plain sentence of what happened, and one copy-paste
command. The command is the interface; git stays an implementation detail
on autopilot's side of the line. This is a delivery-surface rule (the
`INVOKER=developer` channel), not a behavior branch — orchestrator runs
still get the structured blocks unchanged.

## The invariant, in one line

Every decision a repo can answer is answered from the repo; every git
mutation happens in the one layer that knows the git context; every human
stop names the next command. What remains for the human is exactly the set
of things only the human knows.
