---
name: review-pr
description: Review the current diff or a pull request before landing — a verbatim mirror of Claude Code's /code-review at a chosen effort level (low/medium/high/xhigh/max). Parallel finder angles, one-vote verify, sweep, ranked capped findings; --fix and --comment supported.
---
# review-pr

This skill is a **verbatim mirror** of Claude Code's built-in `/code-review`,
extracted in `docs/claude-code-2.1.201-code-review.md`. The shared fragments
(§1), inline level templates (§2), and flag appendices (§3) below are the exact
prompt strings the CLI emits — do not paraphrase them. Assemble the requested
level's template from the fragments it references and run it as written.
Minified identifiers are kept in parentheses so each region maps 1:1 to the
extraction doc.

## 0. Routing

Argument grammar: `[low|medium|high|xhigh|max] [--fix] [--comment] [<target>]`

1. Parse `--comment` / `--fix`. The first token is the level if it matches a
   known level name; everything after the level is the free-form `target`
   (a PR number, branch name, or file path to review instead of the diff).
2. No level given → use the session effort level; if undefined → `medium`.
3. Emit the level's inline template (§2), assembled from the shared fragments
   (§1), plus the `--comment` / `--fix` appendices (§3) when those flags are set.

Level table (drives the templates; the self-describing header is baked into
each prompt):

| level | pipeline |
|---|---|
| low | `1 diff pass → no verify → ≤4 findings` |
| medium | `3+5 angles × 6 candidates → 1-vote verify → ≤8 findings` (precision) |
| high | `3+5 angles × 6 candidates → 1-vote verify (recall-biased) → ≤10 findings` (recall) |
| xhigh | `5+5 angles × 8 candidates → 1-vote verify → sweep → ≤15 findings` (recall) |
| max | same as xhigh; only the API reasoning effort differs, not the fan-out |

## 1. Shared prompt fragments

### Phase 0 — Gather the diff (`LAt`)

```
## Phase 0 — Gather the diff

Run `git diff @{upstream}...HEAD` (or `git diff main...HEAD` / `git diff HEAD~1`
if there's no upstream) to get the unified diff under review. If there are
uncommitted changes, or the range diff is empty, also run `git diff HEAD` and
include the working-tree changes in scope — the review often runs before the
commit. If a PR number, branch name, or file path was passed as an argument,
review that target instead. Treat this diff as the review scope.
```

### Correctness angles (`IFl` = A–E; medium/high use A–C, xhigh/max use A–E)

```
### Angle A — line-by-line diff scan
Read every hunk in the diff, line by line. Then Read the enclosing function for
each hunk — bugs in unchanged lines of a touched function are in scope (the PR
re-exposes or fails to fix them). For every line ask: what input, state, timing,
or platform makes this line wrong? Look for inverted/wrong conditions,
off-by-one, null/undefined deref, missing `await`, falsy-zero checks,
wrong-variable copy-paste, error swallowed in catch, unescaped regex metachars.

### Angle B — removed-behavior auditor
For every line the diff DELETES or replaces, name the invariant or behavior it
enforced, then search the new code for where that invariant is re-established.
If you can't find it, that's a candidate: a removed guard, a dropped error
path, a narrowed validation, a deleted test that was covering a real case.

### Angle C — cross-file tracer
For each function the diff changes, find its callers (Grep for the symbol) and
check whether the change breaks any call site: a new precondition, a changed
return shape, a new exception, a timing/ordering dependency. Also check callees:
does a parallel change in the same PR make a call unsafe?

### Angle D — language-pitfall specialist
Scan for the classic pitfalls of the diff's language/framework — for example:
JS falsy-zero, `==` coercion, closure-captured loop var; Python mutable default
args, late-binding closures; Go nil-map write, range-var capture; SQL injection;
timezone/DST drift; float equality. Flag any instance the diff introduces.

### Angle E — wrapper/proxy correctness
When the PR adds or modifies a type that wraps another (cache, proxy, decorator,
adapter): check that every method routes to the wrapped instance and not back
through a registry/session/global — e.g. a caching provider holding a
`delegate` field that resolves IDs via `session.get(...)` instead of
`delegate.get(...)` will re-enter the cache or recurse. Also check that the
wrapper forwards all the methods the callers actually use.
```

### Cleanup angles (`jFl`: Reuse `MAt`, Simplification `VFe`, Efficiency `KFe`, Altitude `zFe`, Conventions `NAt`)

```
### Reuse
The angles above hunt for bugs; this one and the next two hunt for cleanup in
the changed code. Flag new code that re-implements something the codebase
already has — Grep shared/utility modules and files adjacent to the change,
and name the existing helper to call instead.

### Simplification
Flag unnecessary complexity the diff adds: redundant or derivable state,
copy-paste with slight variation, deep nesting, dead code left behind. Name
the simpler form that does the same job.

### Efficiency
Flag wasted work the diff introduces: redundant computation or repeated I/O,
independent operations run sequentially, blocking work added to startup or
hot paths. Also flag long-lived objects built from closures or captured
environments — they keep the entire enclosing scope alive for the object's
lifetime (a memory leak when that scope holds large values); prefer a
class/struct that copies only the fields it needs. Name the cheaper
alternative.

### Altitude
Check that each change is implemented at the right depth, not as a fragile
bandaid. Special cases layered on shared infrastructure are a sign the fix
isn't deep enough — prefer generalizing the underlying mechanism over adding
special cases.

### Conventions (CLAUDE.md)
Find the CLAUDE.md files that govern the changed code: the user-level
~/.claude/CLAUDE.md, the repo-root CLAUDE.md, plus any CLAUDE.md or
CLAUDE.local.md in a directory that is an ancestor of a changed file (a
directory's CLAUDE.md only applies to files at or below it). Read each one
that exists, then check the diff for clear violations of the rules they state.
Only flag a violation when you can quote the exact rule and the exact line
that breaks it — no style preferences, no vague "spirit of the doc"
inferences. In the finding, name the CLAUDE.md path and quote the rule so the
report can cite it. If no CLAUDE.md applies, return nothing for this angle.
```

### Cleanup precedence (`HQt`)

```
Cleanup, altitude, and conventions candidates use the same
`file`/`line`/`summary` shape; in `failure_scenario`, state the concrete
cost (what is duplicated, wasted, harder to maintain, or which CLAUDE.md rule
is broken) instead of a crash. Correctness bugs always outrank cleanup,
altitude, and conventions findings when the output cap forces a cut.
```

### Verdict ladder (`H2o`)

```
- **CONFIRMED** — can name the inputs/state that trigger it and the wrong
  output or crash. Quote the line.
- **PLAUSIBLE** — mechanism is real, trigger is uncertain (timing, env,
  config). State what would confirm it.
- **REFUTED** — factually wrong (code doesn't say that) or guarded elsewhere.
  Quote the line that proves it.
```

### Recall-biased verdict ladder (`D2o`, high and above)

```
**PLAUSIBLE by default** — do not refute a candidate for being "speculative" or
"depends on runtime state" when the state is realistic: concurrency races,
nil/undefined on a rare-but-reachable path (error handler, cold cache, missing
optional field), falsy-zero treated as missing, off-by-one on a boundary the
code does not exclude, retry storms / partial failures, regex/allowlist that
lost an anchor. These are PLAUSIBLE.
**REFUTED** only when constructible from the code: factually wrong (quote the
actual line); provably impossible (type/constant/invariant — show it); already
handled in this diff (cite the guard); or pure style with no observable effect.
```

### Verify phase — precision variant (`HFl`, medium/xhigh/max)

```
## Phase 2 — Verify (1-vote, 3-state)
Dedup candidates that point at the same line/mechanism, keeping the one with
the most concrete failure scenario. For each remaining candidate, run **one
verifier** via the Task tool: give it the diff, the relevant
file(s), and the candidate, and have it return exactly one of:
<verdict ladder H2o>
Keep candidates where the vote is CONFIRMED or PLAUSIBLE.
```

### Verify phase — recall variant (`w$m`, high)

```
## Phase 2 — Verify (1-vote, recall-biased)
Dedup near-duplicates (same defect, same location, same reason → keep one). For
each remaining candidate, run **one verifier** via the Task tool:
give it the diff, the relevant file(s), and the candidate; it returns exactly
one of **CONFIRMED / PLAUSIBLE / REFUTED**.
<recall ladder D2o>
Keep **CONFIRMED and PLAUSIBLE**. Drop REFUTED.
```

### Sweep gap-focus list (`P2o`, xhigh/max)

```
moved/extracted code that dropped a guard
or anchor; second-tier footguns (dataclass default evaluated once, `hash()`
non-determinism, lock-scope shrink, predicate methods with side effects);
setup/teardown asymmetry in tests; config defaults flipped.
```

### Sweep phase (`x$m`, xhigh/max)

```
## Phase 3 — Sweep for gaps
Run **one more finder** as a fresh reviewer who has the verified list. Re-read
the diff and enclosing functions looking ONLY for defects not already listed.
Do not re-derive or re-confirm anything already there — the job is gaps. Focus
on what the first pass tends to miss: <gap-focus list P2o>
Surface **up to 8 additional candidates**, each naming a defect not already on
the list. If nothing new, return an empty sweep — do not pad.
```

### Output — JSON variant (`$or(n)`, when ReportFindings is unavailable)

```
## Output
Return findings as a JSON array of at most <n> objects:
    "file": "path/to/file.ext",
    "line": 123,
    "summary": "one-sentence statement of the bug",
    "failure_scenario": "concrete inputs/state → wrong output/crash"
Ranked most-severe first. If more than <n> survive, keep the <n> most
severe. If nothing survives verification, return `[]`.
```

### Output — ReportFindings variant (`PFl(n)`)

```
## Output
Call the ReportFindings tool once to report this review's results
with `{level, findings}`. `findings` is at most <n> entries ranked
most-severe first; each entry has `file`, `line`, `summary`,
`failure_scenario`, and `category` — a short kebab-case slug for the angle
that produced it (`correctness`, `simplification`, `efficiency`,
`reuse`, `altitude`, `conventions`, or a more specific slug like
`test-coverage` when one fits better) — plus `verdict` when a verify pass
produced one. If more than <n> survive, keep the <n> most severe. If
nothing survives verification, call it with an empty array. Do not also print
the findings as text.
```

## 2. Inline level templates

### low (`OFl`) — verbatim, self-contained

```
`low effort → 1 diff pass → no verify → ≤4 findings`
## Turn 1 — read
One tool call: read the unified diff (`git diff @{upstream}...HEAD; git diff HEAD`
to cover both committed and uncommitted changes, or `git diff main...HEAD` /
the target passed as an argument). Skip test/fixture
hunks (`test/`, `spec/`, `__tests__/`, `*_test.*`, `*.test.*`,
`fixtures/`, `testdata/`) — test-file changes are not reviewed at this level.
No subagents, no full-file reads.
## Turn 2 — findings
Flag runtime-correctness bugs visible from the hunk alone: inverted/wrong
condition, off-by-one, null/undefined deref where adjacent lines show the value
can be absent, removed guard, falsy-zero check, missing `await`,
wrong-variable copy-paste, error swallowed in a catch that should propagate.
Also flag — still from the hunk alone — new code that duplicates an existing
helper visible in the diff context, and dead code the diff leaves behind.
Do **not** flag style, naming, perf, missing tests, or anything outside the
hunk.
Output at most **4 findings**, most-severe first, one line each:
`path/to/file.ext:123 — what's wrong and the concrete failure`. If nothing
qualifies, output exactly `(none)`.
```

### medium (`qor`) — assembled from fragments

```
`medium effort → 3+5 angles × 6 candidates → 1-vote verify → ≤8 findings`
You are reviewing for **precision** at medium effort: every finding you surface
should be one a maintainer would act on.
<Phase 0: gather the diff>
## Phase 1 — Find candidates (3 correctness angles + 3 cleanup angles + 1 altitude angle + 1 conventions angle, up to 6 each)
Run **8 independent finder angles** via the Task tool. Each
surfaces **up to 6 candidate findings** with `file`, `line`, a one-line
`summary`, and a concrete `failure_scenario`.
<Angles A, B, C>  <Reuse> <Simplification> <Efficiency> <Altitude> <Conventions>
<cleanup precedence>
Pass every candidate with a nameable failure scenario through — finders that
silently drop half-believed candidates bypass the verify step and are the
dominant cause of misses.
<Phase 2 — Verify (1-vote, 3-state), precision ladder>
<Output, cap 8>
```

### high (`LFl`) — same fan-out, recall posture

```
`high effort → 3+5 angles × 6 candidates → 1-vote verify (recall-biased) → ≤10 findings`
You are reviewing for **recall** at high effort: catch every real bug a careful
reviewer would catch in one sitting. At this level, catching real bugs matters
more than avoiding false positives. Err on the side of surfacing.
<Phase 0> <Phase 1 as medium> <cleanup precedence>
<pass-through instruction>
<Phase 2 — Verify (1-vote, recall-biased), PLAUSIBLE-by-default ladder>
<Output, cap 10>
```

### xhigh / max (`MFl("xhigh"|"max")`) — widest fan-out plus sweep

```
`xhigh|max effort → 5+5 angles × 8 candidates → 1-vote verify → sweep → ≤15 findings`
You are reviewing for **recall** at <extra-high|maximum> effort: catch every real bug. At
this level, catching real bugs matters more than avoiding false positives — a
missed bug ships. Err on the side of surfacing.
<Phase 0>
## Phase 1 — Find candidates (5 correctness angles + 3 cleanup angles + 1 altitude angle + 1 conventions angle, up to 8 each)
Run **10 independent finder angles** via the Task tool. Each
surfaces **up to 8 candidate findings**. Do NOT let one angle's conclusions
suppress another's — if two angles flag the same line for different reasons,
record both.
<Angles A–E> <cleanup angles> <cleanup precedence>
<Phase 2 — Verify (1-vote, 3-state)>
This is recall mode — a single non-REFUTED vote carries the finding. Do NOT
drop on uncertainty.
<Phase 3 — Sweep for gaps>
<Output, cap 15>
```

## 3. Flag appendices

### `--comment` (`WBc`)

```
## Posting to GitHub (--comment)
The `--comment` flag was passed. After producing the findings list, if the
review target is a GitHub PR, post each finding as an inline PR comment via
`mcp__github_inline_comment__create_inline_comment` (one call per finding;
include a suggestion block only when it fully fixes the issue). If that tool
is not available in this session, fall back to `gh api` (repos/{owner}/{repo}/pulls/{pr}/comments)
or print the findings instead. If the target is not a PR, print the findings
to the terminal and note that `--comment` was ignored.
```

### `--fix` (`GBc`)

```
## Applying fixes (--fix)
The `--fix` flag was passed. After producing the findings list, apply the
findings to the working tree instead of stopping at the report: fix each one
directly — correctness bugs and reuse/simplification/efficiency cleanups alike.
Skip any finding whose fix would change intended behavior, require changes well
outside the reviewed diff, or that you judge to be a false positive — note the
skip rather than arguing with it. Finish with a brief summary of what was fixed
and what was skipped.
```
