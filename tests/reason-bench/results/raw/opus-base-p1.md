# Feature Plan: Offline Editing for Collaborative Document Editor

## Scope

- Local persistence of the currently-open (and recently-opened) documents on
  device: IndexedDB on web, SQLite/Realm on iOS/Android.
- Editing while offline: user can type, format, and structurally edit
  (add/delete blocks) with no connection; edits queue locally.
- Automatic merge on reconnect between the offline edit queue and whatever
  changed on the server (including edits from *other* users) while offline —
  this is the hard problem, not just "upload my queued edits."
- Visible sync status (offline / syncing / synced / needs attention).
- A narrow conflict-resolution UI for the rare cases automatic merge can't
  resolve confidently (e.g., a section you edited was deleted by someone
  else).

## Out of scope (v1)

- Two users editing the *same* document while *both* are offline
  simultaneously (true offline multiplayer) — requires peer-to-peer sync;
  server-mediated merge assumes at least one side reconnects to authority.
- Offline creation of brand-new documents with sharing/permission setup.
- Offline comments, mentions, and notifications.
- Full offline version history/restore.
- Live server-computed embeds (e.g., data widgets) while offline.

## Phased milestones

**Phase 0 — CRDT foundation (no user-facing change).** Today's editor is
server-authoritative with WebSocket co-editing, which likely already uses
OT or a simple last-write-wins per-op model. Offline requires edits to
survive an unbounded reconnection gap and merge deterministically with
concurrent server-side changes — this needs a CRDT (or OT extended with
durable operation logs) as the underlying document model. Do this first,
ship as an invisible refactor, and validate it doesn't regress today's live
co-editing. This is the highest-risk piece; everything else depends on it.

**Phase 1 — Offline read + status plumbing.** Cache last-opened documents
for offline *viewing* only, build the online/offline detection and sync
status UI. Low risk, but it exercises the storage layer and quota
management the write path will need, without exposing merge risk yet.

**Phase 2 — Single-user offline editing (web first).** Allow editing while
offline, queue ops locally, replay through the Phase 0 CRDT merge on
reconnect. Ship on web before mobile — web has simpler storage/network APIs
and no app-store release latency, so it's the cheaper place to find bugs in
the merge logic before doubling exposure.

**Phase 3 — Concurrent-edit convergence.** Explicitly test and harden the
case where other users edited the doc on the server while you were offline.
Add the conflict-resolution UI, scoped only to structural conflicts CRDTs
can't auto-resolve (e.g., "this section was deleted — keep yours, keep
theirs, or keep both").

**Phase 4 — Mobile parity + resilience.** Bring iOS/Android to feature
parity; add background sync retry with backoff, foreground-triggered sync
fallback (mobile OSes kill background tasks unpredictably), and storage
quota/eviction (LRU on cached docs).

**Phase 5 — Stretch.** Offline document creation, deeper offline history.

## Key risks and de-risking

1. **Merge correctness (data loss/corruption)** — the dominant risk. De-risk
   by front-loading Phase 0 and gating Phase 2's launch on a large
   property-based/fuzz test suite that simulates randomized concurrent
   offline+online edit sequences and asserts convergence with no lost
   content. No user-facing offline editing ships until this suite is green.
2. **Perceived data loss / trust** — even a correct but slow or opaque sync
   feels like loss to users. De-risk with always-visible sync status, never
   deleting local queued edits until the server acknowledges them, and
   retry-with-backoff instead of silent failure.
3. **Storage growth on mobile** — unbounded local caches hurt battery/disk.
   De-risk with caps and LRU eviction starting in Phase 1, before any write
   path exists.
4. **Conflict UX complexity** — most conflicts should never reach the user;
   CRDTs converge concurrent text edits automatically. De-risk by scoping
   the manual-resolution UI narrowly to structural conflicts, and treating a
   rising rate of manual prompts as a signal the merge model needs work.
5. **Scope creep into full offline multiplayer** — stated out-of-scope up
   front so the team doesn't silently absorb peer-to-peer sync complexity.
6. **Mobile OS background execution limits** — don't rely on background
   completion; sync-on-foreground is the reliable path, background is best
   effort.

## Success criteria

- Convergence fuzz suite: 10k+ randomized concurrent-edit scenarios, zero
  data-loss failures, required before Phase 2 ships.
- P95 reconnect-sync time under 2s for typical document sizes.
- Manual conflict-resolution prompts appear in <0.1% of reconnect syncs.
- 24-hour offline editing session with app kill/restart survives with zero
  edit loss (verified by automated kill tests, not just manual QA).
- "Lost my edits" support ticket volume does not increase post-launch
  (tracked weekly for 8 weeks).
- Mobile offline cache stays under an agreed size cap (e.g., 200MB) with
  eviction verified under sustained multi-document use.
