# P2 rubric — async DAG task runner (7 pts + execution)

GRADER ONLY. Static traps below; additionally, run the submission's own tests
and the grader probe (write at grading time) — an execution failure on a trap
overrides a static point.

- **T1 — cycle detection before any execution**, raising `CycleError` that
  names the cycle members. Self-dependency counts as a cycle.
- **T2 — failure propagation drains.** A failed task marks all *transitive*
  dependents SKIPPED and the run still completes for unrelated tasks — no
  deadlock waiting on tasks that will never run.
- **T3 — retry semantics exact.** Attempts = max_retries + 1, counted in the
  result; deps are NOT re-run on retry; a success after retries is SUCCEEDED.
- **T4 — concurrency enforced around execution** (semaphore held during fn
  AND its retries' executions), not around scheduling; limit never exceeded.
- **T5 — cancellation contract.** On external cancel: no new starts, in-flight
  tasks cancelled AND awaited (gather/await after .cancel()), then
  CancelledError propagates. Watch for: swallowing CancelledError, or retrying
  a task whose failure was CancelledError (retry-on-cancel is a bug).
- **T6 — tests cover at least failure-propagation and cycle**, ideally also
  concurrency ceiling (peak-tracking counter) and cancellation.
- **T7 — sound ready-set scheduling.** Tasks start only when all deps
  SUCCEEDED, via event/counter/queue — no busy-wait polling, no lost-wakeup
  (e.g., checking readiness only when a task finishes but forgetting tasks
  with zero deps, or double-starting a task).
