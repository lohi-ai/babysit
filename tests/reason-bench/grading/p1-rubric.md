# P1 rubric — offline collaborative editing (7 pts)

GRADER ONLY. A trap scores only if the answer *resolves* it (names the problem
AND commits to a handling), not merely mentions it.

- **T1 — merge model is THE core decision.** Concurrent offline edits to the
  same doc require an explicit conflict model: CRDT/OT, or an honest
  lock/fork/copy fallback. An answer that says "changes sync when back online"
  without choosing a merge strategy fails this.
- **T2 — auth/permission drift offline.** Access revoked or token expired
  while offline; doc shared/unshared meanwhile. Plan must say what happens to
  queued local edits.
- **T3 — long-divergence UX.** Hours/days offline against an actively edited
  doc: automatic merge is not always safe; plan needs a user-visible
  conflict/review surface or a justified always-converge (CRDT) position that
  addresses semantic (not just structural) conflict.
- **T4 — phased scope cut.** Milestone 1 is something smaller than the full
  ask (e.g., read + queued comments; single-device offline; time-boxed offline
  window) with a why.
- **T5 — device durability.** Local persistence: storage quota, eviction
  (especially web/IndexedDB and iOS), crash-safety of the queued-ops log.
- **T6 — checkable success criteria.** At least 2 measurable criteria (sync
  convergence rate, data-loss = 0 in fault-injection tests, p95 sync time),
  not "works well offline".
- **T7 — protocol/version migration risk.** Server-authoritative REST/WS model
  must change (op-based sync, server version vectors); plan acknowledges
  old-client/new-client coexistence or API migration.
