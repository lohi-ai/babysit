# Feature Plan: Offline Editing for Collaborative Document Editor

## Scope

**In scope:**
- Local read/write access to documents the user has previously opened (cached on device), even with no network connection.
- Local edits queue durably (survive app kill/restart) and sync automatically when connectivity returns.
- Conflict resolution that merges offline edits with any concurrent server-side changes without silent data loss.
- Clear UI signaling of connection/sync state (offline, syncing, synced, conflict).
- Platform parity: web (via service worker + IndexedDB), iOS/Android (native local store).

**Out of scope (explicitly, for v1):**
- Offline *discovery* of new documents (browsing a full library, search) — only documents already opened/pinned are available offline.
- Offline creation of brand-new shared documents with new collaborators (new doc creation requires connectivity to establish server-side identity).
- Real-time cursor presence / live co-editing indicators while offline (no other users are present anyway).
- Rich media upload while offline (images/attachments) — text/structured content only in v1; media queues but doesn't block sync.
- Cross-device offline conflict (same user editing the same doc offline on two devices simultaneously) — rare, deferred to v2 with a "which version wins" prompt as a stopgap.

## Phased Milestones

**Phase 0 — Foundation (2–3 wks):** Introduce a local persistent store and an explicit "document version/op log" abstraction even for the online path. This is prerequisite plumbing, not a visible feature — it's the seam that offline plugs into later. Ships first because every subsequent phase depends on documents having a durable local representation and a mergeable edit representation (not just "last write wins" REST payloads).

**Phase 1 — Read-only offline (2 wks):** Cache last-synced content locally; users can *view* previously opened docs with no connection. No editing yet. Ships early because it's low-risk, immediately valuable (subway/flight scenario), and validates the caching/storage layer without touching write-conflict complexity.

**Phase 2 — Offline editing, single-user simple case (4–6 wks):** Allow edits while offline for docs with no concurrent server-side changes since last sync (the common case). Local edits are captured as an op-log (using CRDT or OT primitives — see risks) and applied optimistically. On reconnect, if the server version hasn't moved, apply cleanly. Ships next because it covers the majority of real usage (one person loses signal, keeps typing) before tackling the harder concurrent-edit merge.

**Phase 3 — Sync with concurrent remote changes (4–6 wks):** Handle the case where the doc changed on the server while the user was offline. Merge via CRDT/OT; surface unresolvable conflicts (e.g., same paragraph deleted vs. edited) as an inline, reviewable diff rather than silently picking a winner. This is the highest-risk, highest-value phase and comes last because Phases 1–2 must prove the storage/op-log model works before adding merge complexity on top.

**Phase 4 — Polish & scale (2–3 wks):** Background sync retries, large-doc performance, storage quota/eviction policy, telemetry dashboards, offline duration limits/warnings.

## Key Risks & De-risking

1. **Merge correctness (technical, highest risk).** Naive diff/patch or last-write-wins will silently destroy user work in multi-editor scenarios. *De-risk:* adopt an existing, battle-tested CRDT library (e.g., Yjs/Automerge) rather than inventing OT from scratch; Phase 0 explicitly builds the op-log seam so we're not retrofitting; Phase 2 ships with the *simple* no-concurrent-change case first so we validate the plumbing before merge logic is load-bearing.
2. **Storage growth/eviction (technical).** Caching every opened doc could blow device storage. *De-risk:* LRU eviction with an explicit "pin for offline" affordance so users control what's guaranteed available; scoped in Phase 4 but the storage abstraction (Phase 0) is built with quotas in mind from day one.
3. **Silent conflict loss eroding trust (product).** Users must never discover after the fact that edits vanished. *De-risk:* Phase 3 requires conflicts to be *visible and reviewable*, never auto-resolved when ambiguous; add a "recover from local history" safety net (undo/version history) as a backstop even if merge logic has bugs.
4. **User confusion about sync state (product).** Users won't trust the feature if they can't tell whether their edits are safe. *De-risk:* explicit, persistent sync-status UI (not just a toast) shipped alongside Phase 1, refined through later phases.
5. **Cross-platform divergence (technical).** Web/iOS/Android storage and background-sync primitives differ significantly. *De-risk:* define the op-log/CRDT format as the shared contract; platform teams own their local storage/sync-trigger implementation independently against that contract, tested via shared conformance tests.

## Success Criteria
- Read-only offline: 100% of previously-opened docs render fully offline in QA matrix (web/iOS/Android).
- Offline edit → reconnect (no concurrent change): edits present and byte-identical to expected merge in automated test suite, 0 data-loss bugs in beta.
- Concurrent-edit merge: conflicting edits surfaced to user in <2s after reconnect, resolvable without data loss in 95%+ of synthetic conflict test cases.
- No increase in P0/P1 data-loss support tickets post-launch vs. pre-launch baseline.
