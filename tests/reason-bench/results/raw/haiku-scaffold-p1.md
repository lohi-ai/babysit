# P1 Answer — Offline editing for a collaborative document editor

## Frame

**Restatement:** Design the feature plan to take a currently online-only,
server-authoritative collaborative document editor (web + iOS/Android, REST
load/save, WebSocket live co-editing with routinely-simultaneous multi-user
sessions) to a state where a user can keep editing while offline and have
those edits sync back in when connectivity returns.

**Two materially different readings — must not pick silently:**
1. **Solo-offline reading:** "Offline editing" = a single user can draft
   locally when disconnected, with best-effort sync on reconnect (like a
   local cache + upload queue). Multi-user offline concurrency is a rare
   edge case to handle safely, not optimize for.
2. **Multi-writer-offline reading:** Because the product already has
   "several users routinely edit the same document at once," offline must
   support *multiple* users independently editing the same doc while
   disconnected and merging automatically with intent preserved (CRDT-style,
   Figma/Linear-like local-first collaboration).

These require different architectures and different effort (reading 2 is
substantially larger). The plan below treats (1) as the phase-1 target and
(2) as a conditional, telemetry-gated follow-on — this choice is argued in
Branch, not assumed here.

**Checkable success criteria:**
- Edits made with no network connection persist locally and survive app
  restart.
- On reconnect, a single offline-editing session merges into the
  server-authoritative document automatically, with zero silent data loss.
- Concurrent multi-user offline edits to the same document never silently
  corrupt or overwrite each other — every collision is either auto-merged or
  surfaced as a recoverable fork.
- Feature parity across web, iOS, Android.
- Sync completes within a defined time bound for a defined doc size.

**Explicit out of scope:** offline creation of brand-new documents; offline
opening of a never-cached document; full CRDT-grade automatic convergence
for extended concurrent multi-user offline sessions (v1); offline handling
of permission/access changes; offline editing of non-text embeds; one user
editing the same doc offline on two devices at once.

## Gather

**Facts (from the task / established domain knowledge):**
- Current system is server-authoritative, REST load/save, WebSocket
  co-editing, with routine simultaneous multi-user editing today (given in
  task — so *some* concurrent-edit resolution already exists in production).
- Target platforms are web, iOS, Android (given).
- The ask is a leadership one-liner, not a spec — the plan must supply the
  missing structure (given).
- Operational Transform (OT) and CRDTs (e.g., Yjs, Automerge) are the two
  established families for collaborative-edit merge; OT is the classical
  Google-Docs-era approach (centrally-mediated transforms), CRDTs are the
  modern local-first approach (commutative merges, native offline support)
  (domain knowledge).
- IndexedDB (web) and SQLite (mobile) are the standard local-durability
  layers for offline-first apps (domain knowledge).
- Offline-first sync architectures standardly use a local write-ahead op-log
  replayed on reconnect (domain knowledge).

**Assumptions (uncited — carried into the plan, not absorbed into the
narrative):**
- **[HIGH-IMPACT, unverified]** Whether the existing concurrent-edit engine
  is OT or CRDT-based, and whether it's proven correct for ops separated by
  minutes-to-hours rather than the sub-second skew it was likely built for.
  This single fact drives most of the cost estimate below and is *not*
  something this plan can responsibly assume either way — it is carried
  forward as Milestone 0, a gating spike, rather than guessed at.
- Document content is primarily text/structured (not huge binary/CAD-style
  payloads) — affects sync payload-size assumptions.
- Auth/session tokens can be cached with some offline grace period.
- "Everything syncs" tolerates eventual consistency on the order of seconds,
  not a hard real-time guarantee.

## Branch

**Candidate A — CRDT rewrite.** Replace/wrap the sync core with a CRDT
library (Yjs/Automerge); server becomes relay + persistence rather than
transform authority. *Pro:* proven, native multi-writer offline convergence.
*Con:* large rewrite if current system is OT-based; touches document schema
and history; slow to ship.

**Candidate B — Offline op-queue over existing merge logic.** Local
write-ahead log of ops while offline; replay through the *existing*
server-side concurrent-edit resolution on reconnect, same as it already
resolves simultaneous online edits today. *Pro:* small, reuses proven
production logic, ships fast. *Con:* replay quality degrades as offline
divergence time/writer-count grows; the reused logic was likely tuned for
small time-skew, not long offline gaps.

**Candidate C — Snapshot + manual merge (Dropbox-style conflicted copy).**
Cache read-only snapshot; on reconnect, if server changed, fork a copy and
let the user manually reconcile. *Pro:* simplest, lowest risk, zero server
changes. *Con:* for a product where several users routinely co-edit, manual
conflict resolution would be the common case, not the exception — poor fit
for "everything syncs."

**Pick: B, phased, with A as a conditional follow-on.** One-line why: B
ships the dominant real-world case — one user loses connectivity mid-edit —
fast and cheaply by reusing logic that's already in production, while
deferring the much larger CRDT investment until data justifies it.
**Switch trigger:** if post-launch telemetry shows multi-writer simultaneous
offline sessions above an agreed threshold (e.g., >2% of offline sessions
involve 2+ users editing overlapping regions offline for >5 min), switch to
Candidate A, because op-replay merge quality is not proven to hold at that
divergence.

## Attack

**Concrete failing scenario against B:** Two users on the same document lose
connectivity together (e.g., an office wifi outage), both edit the same
paragraph for 10 minutes, and reconnect within seconds of each other. The
server replays each queue against current state. If the existing
concurrent-edit engine is OT built for sub-second skew, transforming a
10-minute-old insert against a paragraph that the other user deleted while
offline is exactly the class of case OT transform functions are typically
*not* proven correct for. This is not a rare corner case at scale — outages
of minutes to an hour (subway, flight, flaky wifi) are common for any team
with "several users routinely co-editing."

**Steelman of rejected Candidate A:** CRDTs solve exactly this by
construction (commutative merge regardless of divergence time), so the
strongest objection to picking B is that it defers rather than solves the
real problem, and phase-1 engineering may not carry over to phase 2.

**Does this send us back to Branch?** No — it sharpens B's scope rather than
invalidating it: Phase 1 must (a) make M0's spike a hard go/no-go gate
before building further, (b) explicitly scope the "seamless auto-merge"
guarantee to the single-active-offline-editor case, and (c) add a bounded
safety valve — an offline-duration cap and a detect-and-fork fallback — for
the multi-writer-collision case, so a failure mode that can't be fully
solved cheaply is at least never silent.

**Strongest surviving objection:** Even with the safety valve, if M0's spike
finds the current engine cannot safely transform arbitrarily-aged ops at
all, Phase 1's "auto-merge" claim only holds for short offline windows. This
doesn't kill the plan — it's why the duration cap and fork fallback are
load-bearing parts of the plan itself, not an afterthought.

## Verify

**Check applied (hand-traced):**
- *Single user, 5 min offline, no one else touched the doc:* op queue
  replays against unchanged server state via already-proven online
  concurrent-edit logic → clean merge. **Pass.**
- *Two users, both offline 10 min, overlapping edits, near-simultaneous
  reconnect:* per Attack, risks corruption unless bounded → mitigated by
  duration cap + fork-and-banner fallback; acceptance criterion is "no
  corruption, always a recovery path," not "seamless merge." **Pass against
  the phase-1 criterion, not against the harder phase-2 criterion — by
  design.**

**Re-read Frame:** persistence offline ✓ (M1), automatic single-writer
merge on reconnect ✓ (M2), no silent corruption on multi-writer collision ✓
(M3, fork fallback), cross-platform parity ✓ (M4), bounded sync time ✓
(M2 criterion). The unresolved OT/CRDT unknown is not hidden — it is named
as the plan's first gating step (M0) rather than papered over.

---

# Deliverable — Feature Plan: Offline Editing & Sync

### Scope
Single user can open a previously-synced document, edit it (text, formatting,
structure) fully offline, with edits durably queued on-device. On
reconnect, queued edits merge automatically into the server-authoritative
document via the existing concurrent-edit resolution path — no manual
conflict UI for the common single-offline-editor case. Ships on web
(IndexedDB) and iOS/Android (SQLite) with equal guarantees. Multi-user
simultaneous offline divergence is bounded by an explicit duration cap and a
safe fork fallback rather than silently resolved.

### Out of scope (v1)
Offline creation of new documents or first-time opening of an uncached
document; full automatic convergence for extended multi-user offline
sessions on the same doc; offline handling of permission changes; offline
editing of non-text embeds; one user editing the same doc offline on two
devices at once.

### Milestones
**M0 — Architecture spike (1–2 wks, gating).** Determine whether the
current co-editing engine is OT- or CRDT-based, and whether it's proven
correct for ops separated by minutes-to-hours. Drives every downstream
estimate; go/no-go gate before M1.

**M1 — Local durability.** Local op-log + snapshot cache. Full offline
editing with a visible "saved locally, offline" state. *Verify:* kill
network mid-edit, edits survive app restart.

**M2 — Reconnect replay.** Replay the local op queue through the existing
merge path, idempotently. Add an offline-duration cap forcing a
rebase/refresh prompt beyond it. *Verify:* single-user offline session
merges automatically within 5s for docs <1MB; duration-cap path tested
explicitly.

**M3 — Multi-writer safety valve.** Detect collisions with concurrent
server-side changes from the same window. Non-overlapping edits: auto-merge.
Colliding edits: never overwrite — fork a copy, show a clear banner, let the
user reconcile via revision history. *Verify:* zero corruption in
adversarial concurrent-offline tests.

**M4 — Mobile parity + rollout.** Port to iOS/Android, background sync on
reconnect, telemetry on offline frequency/duration/collision rate. Staged
rollout behind a flag, dogfood first.

**M5 — Conditional CRDT evaluation.** Triggered only if M4 telemetry shows
multi-writer-offline collisions above an agreed threshold. If triggered,
scope a separate, larger project to replace the merge core with a CRDT
library for true automatic multi-writer convergence.

### Key risks & de-risking
1. **Unknown merge-engine robustness at long op-age** (largest risk) — M0
   spike as a hard gate; M2 duration cap avoids relying on unproven behavior.
2. **Multi-user offline collisions causing data loss** — M3's detect-and-fork
   valve; nothing is ever silently overwritten.
3. **Local storage growth on mobile** — bounded op-log size, cache eviction
   for docs unopened beyond N days.
4. **Auth/session expiry while offline** — cached refresh tokens with a
   defined grace period; expiry blocks sync visibly, never silently.
5. **Cross-platform inconsistency** — one shared op-log schema and merge
   client library across web/iOS/Android, not three implementations.

### Success criteria (checkable)
- Edits to a cached document persist across app restart with airplane mode
  on (M1).
- Single offline-editing sessions merge automatically within 5s for docs
  <1MB, zero data loss, in 100% of non-colliding test runs (M2).
- Adversarial concurrent-offline tests (2+ users, overlapping edits, 10+ min
  offline) show zero silent data loss — every collision auto-merges or
  produces a visible, recoverable fork (M3).
- Feature ships with parity on web, iOS, Android (M4).
- Telemetry on offline-session frequency/duration/collision rate exists to
  drive the M5 go/no-go decision.

```text
STATUS: DONE
CONFIDENCE: medium — one clause: would raise to high once M0's spike
confirms whether the production merge engine is OT or CRDT-based, since
that fact (not guessed here) determines whether M2's "automatic merge"
claim holds as scoped or needs to shrink further.
ASSUMPTIONS: (1) current concurrent-edit engine's correctness bounds at
long op-age are unverified — carried forward as gating Milestone 0, not
resolved; (2) documents are primarily text/structured, not large binary
payloads — affects payload-size claims in M2's time bound; (3) auth tokens
can be cached with some offline grace period.
ATTACK: strongest surviving objection — if M0 finds the existing engine
cannot safely transform arbitrarily-aged ops at all, Phase 1's "automatic
merge" guarantee only holds for short offline windows, not arbitrary ones.
Doesn't kill the plan: the duration cap and fork-and-banner fallback in M2/M3
are the explicit, load-bearing answer to exactly this failure mode, not an
afterthought.
```
