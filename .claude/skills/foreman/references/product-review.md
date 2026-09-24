# Product evaluator assignment (Builder pilot)

Input: accepted parent outcome, audience, criterion IDs, design/prototype, exact
integrated revisions, runtime URL/probe, and existing QA evidence. Start a fresh
read-only Orca Task using the current orchestration contract. No code edits,
topology changes, nested workers, release authority, or approval authority.

Judge the running product from the user's entry point. Complete the first
usable journey without implementation narration, then check the remaining
accepted journeys at final review. Exercise real actions, persisted state after
reload, cross-feature transitions, empty/error/permission states relevant to the
scope, keyboard use and a narrow viewport for UI work. A polished screenshot
with a disconnected action is a material failure.

Use the accepted design and host product as the comparison. Name observable
defects rather than arbitrary aesthetic preferences. Capture steps, expected and
actual behavior, criterion ID, tested SHA/runtime, screenshot/log paths, severity
and the owning seed. Compare against the previously verified candidate after a
repair; avoid repeated cosmetic churn that worsens usability.

Return checks for every assigned criterion and unresolved findings. `material`
blocks acceptance; `minor` and `nit` are nonblocking only when the criterion still
works. Unavailable runtime or missing evidence is a gap, never PASS. Submit via
the parent's evidence attempt; Foreman dispatches fixes to the owning child and
reruns affected gates. Stop within the project's work budget and report remaining
gaps explicitly.

Measure this pilot on comparable projects before claiming a quality/cost gain:
accepted completion, escaped material defects, interventions, repeated work,
recovery correctness, time to first usable journey, and provider usage coverage.
Count blocked/failed runs. Do not equate deterministic contract fixtures with a
live model product benchmark.
