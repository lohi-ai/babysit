# P1 — Offline editing for a collaborative document editor: reasoning + plan

## Frame

**Restated task:** The product is today server-authoritative, online-only: REST for load/save, WebSocket for live multi-user co-editing, with several users routinely editing the same document concurrently. Leadership wants offline editing that syncs automatically when connectivity returns. I need to hand the team a real feature plan: scope/out-of-scope, phased milestones, key risks with de-risking, and checkable success criteria, in ≤800 words.

**Two materially different readings — flagging, not picking silently:**
1. **Bounded offline continuity** — a user loses connection mid-session (elevator, subway, flight), keeps editing, and it reconciles on reconnect. Offline gaps are minutes-to-hours; concurrent divergence from other online editors is moderate.
2. **Full offline-first architecture** — documents are opened and edited without ever having synced this session, offline for arbitrarily long periods (days), possibly by multiple users simultaneously offline, requiring true CRDT-grade merge over long-diverged histories.

These imply very different engineering bets (incremental extension vs. core data-model rewrite). The plan below targets reading (1) as v1 — the literal, most defensible reading of "keep editing without a connection" for a live product — with an explicit, telemetry-gated path to (2) if evidence demands it.

**Checkable success criteria (not "works well"):**
- Automated test: disconnect, apply edits for the target offline window, kill/reopen the app, reconnect → final doc equals expected merge, zero dropped ops.
- Adversarial test: one offline client + heavy concurrent structural edits from an online collaborator → either correct auto-merge or an explicit conflict prompt, never silent data loss/corruption.
- No regression in existing online co-editing latency/correctness suite.

**Out of scope (flagged now, detailed below):** offline document creation, offline permission/ACL changes, same-user multi-device concurrent offline editing, offline sync of large binary attachments, unbounded-duration offline sessions.

## Gather

**Facts** (derived directly from the task):
- The system is already server-authoritative with live multi-user concurrent editing over WebSocket ⇒ some conflict-resolution mechanism already exists and works in production today (the task doesn't say which).
- Three platforms (web, iOS, Android) ⇒ any offline buffer needs a durable local store per platform (IndexedDB / SQLite / Room / CoreData) and one shared wire/queue format across them.
- "Several users routinely edit the same document at once" ⇒ reconnection-time conflicts will be common, not rare edge cases — the plan must treat conflict handling as a first-class path, not an afterthought.

**Assumptions** (uncited — load-bearing, carried into the plan, not absorbed into narrative):
- The current conflict engine is OT-like (rebase against a moving server version), typical of this product shape — **unverified, and the single biggest swing factor for the whole plan.** If it's actually last-write-wins or a bespoke non-rebasable scheme, the v1 approach below doesn't hold and a CRDT migration becomes mandatory, not optional.
- Realistic offline sessions are minutes-to-hours (commute/flight), not multi-day — unverified, should be confirmed with product telemetry before locking the phase-1 duration cap.
- Automatic merge is expected for text; structural conflicts (deletes/moves) may reasonably surface a manual-resolution UI rather than requiring perfect silent auto-merge.
- Incremental extension of the existing backend is in budget; a full rewrite is not what leadership is asking for this cycle.

## Branch

**A — Full CRDT rewrite** (e.g., Yjs/Automerge as the core doc model, server becomes relay/persistence only). Pros: true offline-first, arbitrary offline duration, native multi-user merge. Cons: rewrites the whole doc model, undo/redo, comments, and permission enforcement at write-time; discards a conflict engine that already works in production; 2–3+ quarters before first user-visible increment.

**B — Offline queue + rebase through the existing pipeline.** Keep the current server-authoritative engine; add a durable local op queue per platform; on reconnect, replay queued ops through the *existing* conflict-resolution path (the same mechanism already handling concurrent online edits, just applied as a batch after a gap). Pros: small blast radius, reuses proven logic, ships in weeks not quarters. Cons: only valid if the engine truly supports batch rebase; degrades on very long offline periods or heavy concurrent structural divergence.

**C — Hybrid: local CRDT shadow buffer, OT stays authoritative online.** Client stores offline edits in a mergeable local log (CRDT-flavored) so a user's *own* multiple offline devices could merge locally before ever touching the server; online path unchanged from B. Pros: covers same-user multi-device offline as a bonus. Cons: added complexity for a case explicitly out of scope for the leadership ask.

**Pick: B.** One-line why: it ships the actual ask with the least architectural risk by reusing conflict-resolution machinery already proven correct in production. **Switch trigger:** if Phase 0 discovery shows the engine can't rebase a batch of ops against an advanced server version, or telemetry shows offline sessions routinely span days with heavy concurrency, flip to A.

## Attack

**Concrete failing input:** Alice goes offline for 6 hours; meanwhile Bob (online) deletes two paragraphs and moves a table — 400+ structural ops — inside the same doc. Alice reconnects with her own queued edits anchored to text Bob deleted. Rebasing large batches of *structural* ops after long divergence is OT's classic weak spot ("edit position no longer exists") — this is a real failure mode, not a strawman.

**Magnitude check:** a single offline user might queue hundreds of ops over hours (fine), but a *busy shared document* can accumulate thousands of ops server-side from other online editors during that same window — rebasing that in one shot at reconnect risks both correctness (garbled structural merges) and latency (seconds-to-minutes).

**Re-checked assumption:** the OT-like-engine assumption is still unverified — Candidate B's entire premise rests on it, which is exactly why Phase 0 below is a hard go/no-go gate, not a formality.

**Steelman of A:** if leadership's "everything syncs" really means unbounded duration and heavy concurrency, only a CRDT rewrite is structurally sound — B's caps would then feel like a broken promise, not a reasonable v1 scope.

**Strongest surviving objection:** B is unsafe past a bounded offline window and divergence size. It survives *because* the plan makes that boundary explicit — capping v1 duration, adding a "can't auto-resolve, please review" fallback instead of silent merging, and re-evaluating A once real telemetry either confirms or breaks the bound — rather than quietly hoping the bad case never happens.

## Verify

**Check that would fail if wrong:** the adversarial test above (long offline + concurrent structural edits from another user) — under the v1 design, exceeding the cap must produce an explicit conflict prompt, never a silently corrupted or overwritten document. Hand-traced: with a 2-hour/divergence-size cap, Alice's 6-hour scenario exceeds the cap, triggers the fallback, and the "zero silent data loss" criterion still holds even though the merge itself isn't automatic.

**Re-read Frame:** phase-1 ship criteria are executable tests (not vibes), out-of-scope is explicit, and the plan answers leadership's literal ask rather than a straw "perfect universal merge" version of it. No drift detected.

---

# Feature Plan: Offline Editing & Sync

**Goal:** Users can keep editing a document without a network connection; edits sync automatically when connectivity returns, with no silent data loss, across web + iOS + Android.

**Scope (v1):** Continue editing offline on documents already opened this session/device. Local edits persist durably (survive app kill/reboot). On reconnect, edits merge automatically with server + collaborator changes via the existing conflict-resolution pipeline, batch-replayed. Offline duration bounded to a defined, telemetry-validated window (proposed ~2 hours) covering the dominant real use case. Non-destructive UX when auto-merge can't resolve — explicit conflict prompt, never silent overwrite.

**Out of scope (v1):** brand-new offline document creation; offline permission/ACL changes; same-user multi-device concurrent offline editing; offline sync of large binary attachments; unbounded/multi-day offline sessions with heavy concurrent divergence (capped, escalates to manual resolution).

**Milestones:**
- **Phase 0 — Discovery (1–2 wks):** Confirm whether the current conflict engine supports batch rebase against an advanced server version. Hard go/no-go gate on everything below.
- **Phase 1 — Single-user offline continuity (3–4 wks):** Durable local op queue per platform; offline indicator; reconnect replay through the existing sync path; conflict fallback UI instead of silent merge/overwrite on rebase failure.
- **Phase 2 — Multi-user hardening (3–4 wks):** Scale/load-test against real concurrent-editor divergence; divergence-size caps with graceful fallback; background retry/backoff; telemetry on real offline durations and conflict rates.
- **Phase 3 — Scope expansion (sized post-Phase-2):** If telemetry shows sessions/divergence routinely exceed the safe window, scope a CRDT migration (Candidate A) as its own project. Otherwise invest next in same-device-multi-device merge or offline document creation.

**Key risks & de-risking:**
1. Engine may not support batch rebase → Phase 0 is a hard gate before any product code.
2. Long offline + heavy concurrent structural edits can garble merges → duration/divergence caps plus a "flag, don't silently merge" fallback, proven via an adversarial test.
3. Three platforms need one consistent local-queue contract → define a shared wire format in Phase 0/1 so storage stays an implementation detail per platform.
4. Silent data loss erodes trust fastest → "no silent overwrite" is a release-blocking test every phase, not a nice-to-have.

**Success criteria (checkable):**
- Offline-then-reconnect test: edits over the target window survive app kill and reconnect with zero dropped ops.
- Adversarial test: offline client + concurrent structural edits from another online user → correct auto-merge or explicit conflict UI, never corruption.
- P95 reconnect-sync time < 3s for docs under 500KB (retuned after Phase 2 measurement).
- No regression in existing online co-editing latency/correctness suite.
- Phase-1 offline-duration cap is set from measured telemetry, not a guess.

```text
STATUS: DONE
CONFIDENCE: medium — one clause: would rise to high if Phase 0's discovery (current conflict engine's rebase capability) were actually confirmed rather than assumed
ASSUMPTIONS: current conflict-resolution engine is OT-like and supports batch rebase against an advanced server version; realistic offline sessions are minutes-to-hours rather than multi-day
ATTACK: long offline period + heavy concurrent structural edits from other collaborators can break batch rebase (OT's classic weak spot) — doesn't kill the answer because the plan makes the offline-duration/divergence bound explicit, adds a non-silent conflict fallback for the excess case, and gates a CRDT rewrite on Phase 2 telemetry rather than assuming Candidate B scales unboundedly
```
