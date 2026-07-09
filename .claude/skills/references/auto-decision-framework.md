# Auto-Decision Framework
Classify every decision as **Mechanical**, **Taste**, or **User Challenge**.
Mechanical → decide silently. Taste → decide using the 6 principles, log one
audit line. User Challenge → never auto-decide; emit `NEEDS_CONTEXT`
(delivery per [preamble.md](preamble.md#one-mode-two-escalation-channels)).
Auto-decide replaces the human's *judgment*, not the *analysis*. Order at
every decision point: classify → act → log.
## The 6 Decision Principles
1. **Correctness over completeness** — a guess that would land incorrect
   code is a User Challenge.
2. **Bounded blast radius** — the task plus direct importers; auto-approve
   expansions that are in-radius AND < 5 files AND no new infra, escalate
   wider.
3. **Pragmatic** — five seconds choosing between equivalent fixes.
4. **DRY** — grep before writing.
5. **Explicit over clever** — the 10-line obvious fix beats the 200-line
   abstraction.
6. **Bias toward verified action** — decide, ship, self-verify; flag
   concerns in the completion status instead of blocking on them.
Per-phase tiebreakers when principles collide: design → P1+P4;
implementation → P5+P3; quality → P1+P2; frontend → P1+P3;
ops/migration/deploy → P2+P6.
## The three tiers
**🟢 Mechanical** — one clearly right answer. No log entry. Output-mode
selection is Mechanical — route by consumer per
[preamble.md § Output style](preamble.md#output-style--terse-by-default).
**🟡 Taste** — reasonable engineers could disagree. Decide with the
principles, then append one JSON line — no silent Taste decisions, a human
audits unattended runs from this log:
```bash
printf '{"ts":"%s","session":"%s","skill":"%s","tier":"taste","principle":"%s","decision":"%s","rationale":"%s"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$_SESSION_ID" "$_SKILL_NAME" \
  "P5-explicit" "split into two files" "10 lines in each is easier to read than 200 in one" \
  >> ~/.babysit/analytics/decisions.jsonl
```
**🔴 User Challenge** — the stated direction is wrong, ambiguous, or would
land incorrect work: materially different plausible interpretations,
irreversible/high-blast-radius action without durable authorization, missing
config/credentials, security-sensitive with no obvious safe choice, or you'd
be rewriting what the user asked for. The original direction is the default;
the skill makes the case for change:
```
WHAT YOU SAID: <the original direction>
WHAT WE RECOMMEND: <the change, if any — else "no change, need clarification">
WHY: <1-2 sentences, citing which principle>
WHAT WE MIGHT BE MISSING: <explicit blind spots>
IF WE'RE WRONG, THE COST IS: <downside if the original direction was right>
OPTIONS: A) <option>  B) <option>  C) <option>  [2-4 labeled, mutually exclusive]
```
A security or feasibility *risk* (not a preference) gets the prefix
`⚠️ FLAGGED AS RISK, NOT PREFERENCE:`.
## The Final Gate (`INVOKER=developer` only)
After completing the work, surface all Taste decisions at once — the only
`AskUserQuestion` a correct run needs besides mid-run User Challenges:
```
## <skill-name> complete

### Summary
<1-3 sentences>

### Your Choices (taste decisions)
**Choice 1: <title>** — picked <X> (principle <N>). <Y> also viable:
  <1-sentence downstream impact if you pick Y>

### Auto-decided silently: <count> — see ~/.babysit/analytics/decisions.jsonl
```
Options: A) approve as-is, B) override — specify which, C) ask about one,
D) reject and redo. Zero taste decisions → skip the gate, report `DONE`.
Six or more → flag "High ambiguity run — review carefully".
Non-`developer` `INVOKER`: no gate — `decisions.jsonl` is the audit surface;
`NEEDS_CONTEXT` if User Challenges are unresolved.
