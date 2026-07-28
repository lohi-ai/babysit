# P1 — Offline editing for a collaborative document editor

## Frame

**Task restated:** Write a feature plan for taking a server-authoritative,
online-only collaborative document editor (web + iOS/Android, live multi-user
co-editing over WebSocket today) to "users can edit offline, everything syncs
when back online." Deliverable: scope, phased milestones, risks +
de-risking, checkable success criteria, ≤800 words.

**Two materially different readings — must not pick silently:**
1. **Narrow reading:** a single user edits one document while offline (no
   other editor touched it during that window), then syncs — essentially
   "cache + replay," no real conflict resolution needed.
2. **Broad reading:** *any* number of users may be offline simultaneously on
   the *same* document, each editing concurrently, and all their changes must
   merge correctly on reconnect — this is a full distributed-merge problem,
   not just caching, because the product already supports concurrent
   multi-user editing online today, so users will expect the same when
   flaky/offline.

I plan for reading 2 as the eventual bar (product's "everything syncs" implies
no one's edits get silently dropped), but explicitly phase the narrow case
first — see Branch/Verify.

**Checkable success criteria** (not "works well"):
- Doc opened while online stays fully editable (edit/format/save) with zero
  connectivity, surviving app restart, on all 3 platforms.
- Zero silent data loss (an op accepted locally but dropped/never applied) in
  telemetry.
- ≥95% of single-editor offline sessions reconcile with no manual conflict
  prompt.
- P95 reconnect-sync latency < 3s for a typical offline session (<500 ops).
- No regression to existing live-collab p95 latency/error rate.

**Out of scope (stated up front, elaborated in the plan):** creating brand-new
documents offline; true simultaneous-offline-same-paragraph editing in v1;
offline permission/sharing changes; P2P device-to-device sync without the
server.

## Gather

**Facts** (cited from the task):
- System is server-authoritative today; REST for load/save, WebSocket for
  live multi-user co-editing (task lines 4–6).
- Multiple users already edit the same doc concurrently *online* — so some
  concurrency-control mechanism already exists (task line 6).
- Platforms: web + iOS/Android (task line 4).
- The explicit ask is edit-while-offline + sync-on-reconnect, phrased as a
  product outcome, not an implementation (task lines 7–8).

**Assumptions** (uncited — flagged, carried into the plan, not absorbed):
- *Unknown current conflict-resolution mechanism* (OT? CRDT? locking?
  last-write-wins?). This is the single most load-bearing unknown — it swings
  build cost by 3–5x — so the plan makes verifying it a Phase 0 gate rather
  than guessing.
- Document content is rich text (paragraphs/formatting/lists), possibly with
  images/tables/comments — assumed by "document editor" framing, not stated.
- Client-side persistent storage (IndexedDB / SQLite) is available and
  permitted on all 3 platforms — standard, but unverified for this app.
- Typical concurrent-editor count per doc is small (<20) — affects op-log
  scale math below.
- Network-loss detection is standard OS-level (no exotic captive-portal
  edge cases assumed away).

## Branch

Three genuinely different designs:

1. **Full CRDT core.** Replace the internal document representation with a
   CRDT (e.g., Yjs/Automerge-style RGA text). Offline edits are local CRDT
   ops; merge on reconnect is automatic and conflict-free by construction.
   - *For:* mathematically rules out silent data loss for text-level edits;
     scales to N concurrent offline editors without new merge logic per case.
   - *Against:* biggest rewrite; unknown compatibility with current model
     until Phase 0 spike runs.

2. **Extend existing OT with a client-side op queue.** Assume the live
   co-editing already does some form of operational transform; keep it, add
   a durable local queue of offline ops, replay/rebase against the server's
   ops on reconnect using the existing transform functions.
   - *For:* reuses tested logic, smaller diff if OT already exists.
   - *Against:* if positions are computed against a stale snapshot without a
     correct transform, rebase can silently corrupt or drop ops (see Attack).
     Entirely dependent on an unverified assumption.

3. **Snapshot-diff + manual conflict UI (Git-style).** Client caches
   last-synced snapshot; on reconnect, diff local vs. server, auto-merge
   non-overlapping sections, else show a "keep mine / keep theirs / merge"
   UI.
   - *For:* fastest to ship, lowest engineering risk, proven pattern
     (early Notion/Confluence-era tools shipped this way).
   - *Against:* frequent conflict prompts under real concurrent editing,
     which the product already has today — fails the "everything syncs"
     bar the moment two people touch the same doc offline.

**Pick:** Candidate 1 (CRDT core), phased so Phase 1 only needs to prove the
*single-editor* case (deferring true concurrent-offline merge to Phase 2).
One-line why: it's the only design where "no data loss" is a property of the
data structure rather than a heuristic that can be wrong under load.
**Switch trigger:** if the Phase 0 spike finds the current engine is already
OT-based with a mature, well-tested transform library, switch to Candidate 2
— reuse cost would then be lower than a CRDT migration.

## Attack

**Concrete failing input.** Users A and B are both online-editing Doc X,
then both lose connectivity (shared office Wi-Fi outage). Offline, A inserts
a paragraph at position 500; B deletes the range [400,600] — which contains
position 500. Both reconnect 10 minutes later.
- Naive position-based OT rebase (Candidate 2, done carelessly): B's delete
  is expressed as absolute offsets computed against a stale snapshot; once
  A's insert shifts everything after 500, the delete can either corrupt
  unrelated text or silently drop A's insert — exactly the "everything
  syncs" promise breaking, with no error surfaced.
- CRDT (Candidate 1): ops reference stable per-character IDs, not positions;
  the merge is well-defined (A's insert survives per RGA tie-break rules;
  B's delete removes only the characters that existed when issued). This
  validates the pick — but exposes a **residual gap**: range-anchored
  objects (an image inside the deleted paragraph, a comment anchored to
  it) don't have obvious CRDT semantics and need an explicit tombstone/
  resurrect UI, not silent "it just works."

**Scale check.** A 30-minute heavy-typing offline session ≈ 3,000 char ops;
CRDT metadata overhead (Yjs-class) runs ~1.5–3x content size — fine for
sub-1MB docs. A multi-day offline session (flight + delayed reconnect) could
grow the op log into MBs — needs periodic compaction, added as an explicit
Phase 2 task rather than assumed away.

**Re-check load-bearing assumption:** the current collab mechanism is still
unknown — Phase 0 is a hard gate, not a nice-to-have, precisely because this
Attack shows the wrong guess here silently breaks the "no data loss"
criterion.

**Steelman the rejected candidate (3, snapshot-diff):** if telemetry shows
same-section concurrent-offline edits are rare in practice, building full
CRDT merge is premature optimization — a cheap "conflict copy" UI, shipped
in weeks not a quarter, may satisfy leadership's actual usage pattern.
**This is the strongest surviving objection.** It doesn't kill the pick
because Phase 1 is scoped to ship the CRDT core for the single-editor case
regardless (useful even as a better live-collab engine), and explicitly
defers the *expensive* multi-editor-offline-merge hardening to Phase 2,
gated on real telemetry of how often concurrent offline edits actually
occur — which directly answers this objection with data instead of a guess.

## Verify

**Check that would fail if wrong:** post-Phase-1 telemetry should show ≥95%
of offline sessions reconcile with zero manual conflict UI and zero
data-loss reports. If either misses, the design pick (or its scope) was
wrong, not just under-tuned.

**Hand-trace** (the two-user paragraph example above): under CRDT, A's
insert and B's delete are independent, ID-keyed ops; merge result = paragraph
deleted except the inserted span, which survives — matches CRDT-expected
behavior, confirmed by tracing IDs rather than positions.

**Re-read Frame:** does the plan meet every stated criterion?
- Offline edit/save across restart, all platforms — yes, Phase 1.
- Zero silent data loss — yes for the single-editor case; explicitly flagged
  as *not yet proven* for concurrent-offline (Phase 2 gate) and for
  embed/table edges (tombstone fallback, not true merge).
- Conflicts surfaced, not dropped — partially: text is conflict-free by
  construction; non-text anchors get an explicit UI rather than silent loss.
- No regression to existing collab — explicit non-goal to touch the current
  path until CRDT parity is proven behind a flag.
No drift detected; the plan below reflects this.

---

## Deliverable: Offline Editing — Feature Plan

### Scope
Enable edit/view of previously-opened documents with zero connectivity, with
automatic sync on reconnect, for rich-text content (paragraphs, formatting,
lists) across web + iOS/Android.

### Out of scope (v1)
Creating brand-new documents offline (no server ID yet to sync against);
true simultaneous same-range offline editing by multiple users (deferred to
Phase 2 pending telemetry); offline permission/sharing changes; comment-
thread reconciliation and non-text embeds beyond a tombstone/resurrect
fallback; peer-to-peer sync without the server.

### Phase 0 — Spike (1–2 wks)
Confirm whether the existing live-collab engine is already OT/CRDT-based or
centralized locking. This fact alone swings cost 3–5x; it's a hard gate
before committing to Phase 1's design.

### Phase 1 — Single-editor offline, CRDT core (6–8 wks)
Ships first: it's what leadership actually asked for, and it avoids the
hardest problem (true concurrent offline merge) until we have usage data.
- Swap internal doc representation to a CRDT (Yjs-style RGA text) behind
  existing REST/WS APIs; clients persist local CRDT state + pending op log
  (IndexedDB/SQLite).
- Reconnect flow: send local op log, receive server's ops since last sync,
  merge via CRDT — automatic, no manual conflict UI for this case.
- Log (don't yet fully support) the case where two people were offline on
  the same doc: merge still runs via CRDT (correctly), but the session is
  tagged "concurrent offline edit occurred" — this is the data source for
  the Phase 2 go/no-go.

### Phase 2 — Concurrent offline editing + conflict UX (4–6 wks, gated)
Ship only if Phase 1 telemetry shows concurrent-offline-edit sessions above
a meaningful threshold (e.g. >2% of offline sessions). Adds tombstone/
resurrect UI for embed conflicts, a "what changed while you were away"
summary, and op-log compaction for long offline sessions.

### Key risks and de-risking
1. **Unknown current collab internals** (highest leverage risk). De-risked
   by Phase 0 as a hard gate, not a parallel track.
2. **Structural merge ambiguity** (edits inside a range another user
   deleted; comments/images anchored to deleted content). De-risked: CRDT
   text ops are conflict-free by construction; non-text anchors get an
   explicit tombstone+resurrect UI, named as a v1 edge case rather than
   silently swept in.
3. **Client storage growth on long offline sessions.** De-risked: Phase 2
   compaction milestone; Phase 1 caps/warns beyond a supported offline
   duration (e.g. 72h).
4. **Regression to existing live collab.** De-risked: CRDT core ships behind
   a flag, dogfooded first; legacy path stays default until parity is proven
   on a cohort.
5. **Premature investment if concurrent-offline-edit is rare** (steelmanned
   alternative: cheap snapshot-diff UI). De-risked: Phase 1's telemetry is
   the actual test; if the rate is low, Phase 2 narrows to the simpler
   manual-merge UI instead — no sunk cost, since Phase 1's CRDT core
   improves live-collab either way.

### Success criteria (checkable)
- 100% of documents opened online stay fully editable offline, surviving
  app restart, on web + iOS + Android.
- 0 data-loss incidents in Phase 1 telemetry over the first 30 days at scale.
- ≥95% of offline sessions reconcile with no user-facing conflict prompt
  (Phase 1, single-editor case).
- P95 reconnect-sync latency < 3s for a typical offline session (<500 ops).
- No measurable regression in existing live-collab p95 latency/error rate
  (canary comparison).
- Phase 2 ship/no-ship decided by data: proceed only if concurrent-offline
  sessions exceed 2% of total offline sessions in Phase 1's first 30 days.

```text
STATUS: DONE
CONFIDENCE: medium — would raise to high if Phase 0's answer (current
collab mechanism: OT vs CRDT vs locking) were known instead of assumed
CONFIDENCE
ASSUMPTIONS: current live-collab conflict-resolution mechanism is unverified
and load-bearing for cost estimates; document content model (rich text +
images/tables) is assumed, not confirmed; client persistent storage
availability on all 3 platforms is assumed
ATTACK: the steelmanned snapshot-diff alternative — if real usage shows
concurrent-offline-same-section edits are rare, full CRDT hardening (Phase 2)
may be over-engineering. Doesn't kill the plan because Phase 1's CRDT core
ships regardless (useful even as a better live-collab engine) and Phase 2's
scope is explicitly gated on the telemetry that would settle this objection.
```
