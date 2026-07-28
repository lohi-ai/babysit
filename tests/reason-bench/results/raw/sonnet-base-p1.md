# Offline Editing — Feature Plan

## Scope

- Users can **view and continue editing documents they have previously opened** while offline, on web and mobile (iOS/Android), including across app restarts and device reboots during the offline period.
- Local edits are queued locally and **automatically synced and reconciled** with the server and other collaborators' edits when connectivity returns — no manual "resync" action required.
- Concurrent edits made by different users while one or both were offline are **merged without silent data loss**; genuine structural conflicts (e.g., one user deletes a table while another edits a cell in it) are surfaced to the user rather than auto-resolved incorrectly.
- Clear UI affordances: an offline indicator, a "changes pending sync" state, and an edit history that makes merges inspectable after the fact.

## Explicit out-of-scope (v1)

- **Opening a document for the first time while offline.** A document must have been loaded online at least once; we are not solving offline discovery/creation-from-scratch in v1.
- **Creating brand-new documents offline** (no server-assigned ID yet) — deferred; requires ID reservation and duplicate-creation handling.
- **Same-user, multi-device offline editing** (editing the same doc offline on phone and laptop before either reconnects) — flagged as a known limitation, revisited once the core reconciliation engine is proven.
- **Offline attachments/media caching** — offline mode covers text/rich-text content only.
- **Offline comments, mentions, and notifications** — these require server round-trips and are deferred.
- **Real-time presence while offline** is a non-goal by definition; it is not "offline collaboration," it is "edit now, sync later."

## Phased milestones

**Phase 0 — Local-authoritative editing engine (foundation, ships first, no user-visible change).**
Replace the current "REST load + WebSocket patch" model with a client-side CRDT (or extend the existing OT engine to support disconnected replay) as the local source of truth. Ship and validate this purely against the *existing online* co-editing flows first — it must not regress current behavior. This is the highest-risk, highest-leverage piece; everything downstream depends on it, and retrofitting it after building queuing logic on the old model would mean rebuilding reconciliation twice.

**Phase 1 — Single-device offline MVP.**
Cache last-synced state and local pending ops (IndexedDB on web, SQLite on mobile). Detect connectivity loss, permit continued editing against local state, queue ops, and auto-reconcile on reconnect. Restrict to previously-opened documents. Roll out behind a flag to a small cohort. This is the smallest slice that delivers the literal product ask.

**Phase 2 — Robustness at scale.**
Handle reconnection races (many offline users returning near-simultaneously), long offline sessions (days), local storage eviction/quota pressure, and an explicit conflict-resolution UI for the rare cases CRDT merge can't resolve automatically. Add offline document creation.

**Phase 3 — Expansion and polish.**
Cross-device same-user merge, offline attachments, offline comment/notification backlog, telemetry-driven tuning of sync batching and frequency.

## Key risks and de-risking

1. **Merge bugs causing silent data loss** — the single highest-severity risk, and the hardest to catch in normal QA. De-risk: property-based/fuzz testing of the merge function against generated concurrent-edit sequences, run to a very low failure tolerance before Phase 1 ships; a shadow-mode rollout that computes offline merges without acting on them and diffs against known-good server state.
2. **Unbounded local storage growth** on long offline stretches. De-risk: caps on queued-op size, periodic snapshot compaction, and user-visible warnings before hitting limits.
3. **User confusion from invisible conflict resolution** ("where did my edit go?"). De-risk: a visible sync-status indicator and an inspectable edit history, not just a "trust the algorithm" merge.
4. **Server load spikes on mass reconnection** (e.g., after a regional outage). De-risk: jittered/backoff reconnect scheduling and batched op upload.
5. **Product expectation mismatch** — users may assume true peer-to-peer offline collaboration, which is not feasible. De-risk: precise messaging ("your edits sync when you're back online," not "collaborate offline") in product copy and onboarding.

## Success criteria

- Zero data-loss failures across 10,000+ generated concurrent-merge fuzz scenarios before Phase 1 launch.
- No regression in existing online co-editing latency/correctness metrics after Phase 0 ships (regression budget: zero).
- p95 reconnect-to-fully-synced latency under 5 seconds for documents under ~50k characters.
- In Phase 1 beta, >99% of offline-edit sessions reconcile automatically with no manual conflict resolution; the remaining <1% surface through conflict UI, never as silent data loss.
- No increase in "lost edits" support tickets post-launch, tracked for at least one full release cycle.
