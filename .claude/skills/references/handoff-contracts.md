# Handoff Contracts
What one skill leaves for the next actor (skill, human, or orchestrator).
Everything a downstream actor needs must be reachable from four surfaces: the
stdout status block ([preamble.md § Completion Status Protocol](preamble.md#completion-status-protocol)),
git state (branch + commits — the diff is the primary deliverable), the
ticket directory `~/.babysit/projects/<slug>/tickets/<ticket>/`
([ticket-layout.md](ticket-layout.md)), and — `developer` runs only —
conversation context. `<ticket>` is always re-derived from the branch, never
conversation memory
([preamble.md § Ticket consistency](preamble.md#ticket-consistency--the-four-layer-invariant)).
## Verdicts per skill
A verdict is a short fixed-shape string reported next to `STATUS`:

| Skill | Verdict shape |
|-------|--------------|
| `autopilot` | `PLANNED` \| `BUILT` \| `FIXED` \| `HANDOFF` |
| `browse` | `CHECKED` |
| `implement` | `BUILT` \| `FIXED` \| `CHANGED` |
| `investigate` | `FIXED` \| `INVESTIGATED` |
| `plan-draft` | `PLANNED(<XS\|S\|M\|L>)` \| `DECOMPOSED(<N>)` |
| `qa` | `PASS` \| `FIXED(<N>)` \| `FAIL` |
| `review-pr` | `PASS` \| `FINDINGS(<N>)` \| `FIXED(<N>)` |
| `create-pr` | `PR_CREATED` |
| `conversion-fix` | `FIXED` \| `AUDITED` |
| `copy-rewrite` | `REWRITTEN` |
| `design-ui` | `DESIGNED` |
| `growth-experiment` | `RANKED` \| `SCAFFOLDED` |
| `office-hours` | `DESIGNED` \| `NOT_READY` |
| `recon` | `STEAL(<approach>)` \| `PASS` |
| `social-content` | `SCRIPTS` |
| `setup-project` | `CONFIGURED` |
New skills pick a one-line verdict and document it in their own SKILL.md.
## CHANGE_BRIEF — the primary file artifact
One per finished skill, appended via `bbs-ticket` (never write the file
directly — the helper claims the sequence number and logs the event):
```bash
eval "$("${BBS_SLUG_BIN:-$HOME/.claude/bbs-slug}" env)"
cat > /tmp/<skill>-brief.md <<EOF
SUMMARY: <1–3 sentences: what changed and why>
FILES: <comma-separated changed files>
APPROACH: <one-line implementation approach>
BLAST_RADIUS: <what existing behavior could be affected>
EOF
"${BBS_TICKET_BIN:-$HOME/.claude/bbs-ticket}" add-handoff <skill> /tmp/<skill>-brief.md
```
Single-line fields, no markdown headers (downstream skills grep them). A
field that doesn't apply gets `none`, not omission.
## Evidence paths
| Path (under the ticket dir) | Contents | Writer |
|------|----------|--------|
| `handoffs/<NNN>-<skill>.md` | Append-only change briefs | `bbs-ticket add-handoff` |
| `verdicts/<skill>.md` | Latest status block per skill | `bbs-ticket set-verdict` |
| `reviews/<skill>.md` | Latest review per skill | `bbs-ticket set-review` |
| `plan.md`, `design.md`, `manifest.md` | Ticket-root canonicals | skill + `bbs-ticket set-pointer` |
| `evidence/*.{png,json}` | Screenshots, structured outputs | skill writes directly |
| `report.md` | Human-readable summary | skill writes directly |
Metadata mutations go through `bbs-ticket`. Don't write scratch files inside
the repo working tree — use the ticket dir; the diff stays clean.
### Typed evidence artifacts
Written through `bbs-ticket set-evidence` (validated; exit 2 on malformed →
retry once, then escalate) and read back categorically — presence +
structure, never a score, so a model can't self-grade past the gate. The
hard-stage gate still keys on `verdicts/`
(see [artifact-gated-approval](../../../docs/artifact-gated-approval.md)).

| Kind | Owner skill | Required | Optional |
|------|-------------|----------|----------|
| `verification` | `implement`, `browse`, `investigate` | `result` (`PASS`\|`FAIL`) | `checks:[{cmd,result}]`, `before`, `after` |
```bash
bbs-ticket set-evidence --kind verification \
  --json '{"result":"PASS","checks":[{"cmd":"eslint changed","result":"pass"}],"before":"3 errs","after":"0"}'
bbs-ticket evidence-status --kind verification   # → none | valid | malformed
```
## Git conventions
Orchestrator-specific conventions (ticket IDs, review cards, auto-merge)
belong to the orchestrator — a babysit skill works standalone with nothing
but a git repo.
