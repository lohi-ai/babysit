# P2 — async DAG task runner — five-move answer

## 1. Frame

Implement, stdlib-only (Python 3.11, `asyncio`):

```python
async def run_tasks(tasks, concurrency, max_retries) -> dict[str, TaskResult]
```

`tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]]`. Success criteria,
made checkable:

1. Returns a dict with one `TaskResult` per key in `tasks` (on normal, non-cancelled completion).
2. A task's `fn` is never invoked until every one of its deps has a `SUCCEEDED` result.
3. At no instant are more than `concurrency` `fn()` calls actually executing (waiting on deps does **not** count against this bound — only the function body running does).
4. A raising task is retried; total attempts (success or exhaustion) ≤ `max_retries + 1`; a retry re-invokes only that task's `fn`, never its deps' `fn`s.
5. If a task exhausts retries, it is `FAILED`; every transitive dependent becomes `SKIPPED` with `fn` never called; tasks outside that dependent-cone still run and can succeed.
6. Any cycle (anywhere in the graph, even in a component disjoint from the rest) raises `CycleError` naming the cyclic tasks, and this check happens strictly before any `fn` is invoked.
7. If the caller cancels the `run_tasks` coroutine/Task: no task that hasn't started `fn` may start it; every task currently inside `fn` is cancelled and **awaited** (not fire-and-forgotten) before `CancelledError` propagates out of `run_tasks`.
8. `TaskResult` exposes `status ∈ {SUCCEEDED, FAILED, SKIPPED}`, `attempts: int`, `exception: Exception | None`.

Out of scope (not asked, not added): per-task retry policy/backoff, task timeouts,
dynamic graph mutation mid-run, distributed/multi-process execution, logging,
a "why was this skipped" field beyond what's specified.

Two readings I did **not** silently pick between — resolved explicitly below
and re-litigated in Attack:

- **Reading A (chosen):** `attempts` for a task that *succeeds* is however many
  tries it actually took (1..max_retries+1), not always the max. Reading B
  (always report max_retries+1 attempts regardless of when it succeeded)
  contradicts "number of attempts **made**," so A wins — not a coin flip.
- **Reading A (chosen):** `CycleError` names only the nodes that are actually
  *on* a cycle (e.g. `x -> y -> x`), not every downstream node whose
  in-degree can never resolve because an ancestor is stuck in a cycle.
  Reading B (name everything Kahn's algorithm leaves un-removed) is easier to
  code but over-reports — a node three hops downstream of a cycle is not
  "in a cycle." Chosen reading matches the literal spec wording ("naming the
  tasks in a cycle") and is verified by construction in Attack/Verify below.

## 2. Gather

**Facts** (from the spec or Python/asyncio semantics, not opinion):

- F1. Total attempts on exhaustion = `max_retries + 1` (given).
- F2. A retry re-executes only the failed task, not its deps (given).
- F3. Skip propagation is transitive; unrelated tasks still complete (given).
- F4. Cycle detection must happen "before running anything" — a pure
  pre-flight check, no partial execution allowed even for the acyclic part
  of the graph (given, explicit).
- F5. `asyncio.gather(*awaitables)`, per the stdlib docs: if the `gather()`
  call itself is cancelled, every awaitable passed to it that hasn't
  completed is also cancelled, and `gather()` does not re-raise
  `CancelledError` until all of them have actually finished unwinding. This
  is the exact mechanism the spec's cancellation clause describes.
- F6. Since Python 3.8, `asyncio.CancelledError` inherits from
  `BaseException`, not `Exception`. A bare `except Exception:` around a
  task's `fn()` call therefore does **not** accidentally swallow
  cancellation — no special-casing needed to keep retry logic from
  eating a cancel.
- F7. asyncio is single-threaded/cooperative: two coroutines never execute
  concurrently between `await` points, so plain dict writes from different
  "tasks" (each mutating only its own key) need no lock.
- F8. `asyncio.Semaphore.acquire()` / `async with semaphore:` is itself a
  normal cancellable awaitable — cancelling a task blocked on it does not
  invoke the guarded body.

**Assumptions** (uncited — stated so they can be checked, not absorbed):

- A1. `TaskResult` for a `SKIPPED` task: `attempts=0`, `exception=None`
  (it never ran, so it has neither). No extra "which dep failed" field is
  added since the spec lists exactly three `TaskResult` fields.
- A2. No delay/backoff between retries — spec doesn't mention one.
- A3. Attempts-on-success = actual try count (Reading A above). Medium
  confidence — re-attacked below.
- A4. A dependency name that isn't a key in `tasks` is not defended against
  with a custom error; it surfaces as a natural `KeyError` while building
  the dependents map, which still happens before any `fn` runs. I chose not
  to add bespoke validation for this — the spec doesn't ask for it, and the
  natural failure mode already satisfies "don't run anything."
- A5. `concurrency` and `max_retries` are assumed non-negative valid inputs;
  `concurrency == 0` deadlocks by design (no permit ever available) rather
  than being special-cased into an error — not tested (would hang).
- A6. `CycleError` need only report *one* cycle if several exist — spec says
  "a cycle," singular.

## 3. Branch

**Candidate 1 — level-by-level (topological generations).** Compute
Kahn's-algorithm layers, run each layer to completion (bounded by a
semaphore) before starting the next.
- Cycle detection falls out of Kahn's algorithm for free.
- Fails to maximize legal concurrency: a task must wait for *every* node in
  the previous layer, not just its own deps — see the concrete break in
  Attack.

**Candidate 2 — dataflow scheduler (chosen).** Create one `asyncio.Task` per
node up front; each node's coroutine first `await`s its own deps' futures
(each future resolves to that dep's terminal `Status`), decides
`SUCCEEDED`-only-if-all-deps-`SUCCEEDED`, then runs a bounded (`Semaphore`)
retry loop only around the actual `fn()` call.
- Matches "runs only after all its deps have succeeded" exactly, per-task,
  not per-layer.
- Skip propagation is automatic: a node just checks its own deps' resolved
  `Status`, so a chain of SKIPPED cascades without any extra graph-walking
  code.
- Cancellation reduces to "cancel and await this list of `asyncio.Task`s,"
  which `asyncio.gather` already does (F5).

**Candidate 3 — explicit worker pool.** A fixed pool of `concurrency`
worker coroutines pulling off a ready-queue; a scheduler loop tracks
in-degree and pushes newly-ready tasks.
- Same power as Candidate 2, but re-implements what `asyncio.Semaphore` +
  per-node futures already give for free — a queue, in-degree bookkeeping,
  and idle-detection, for no correctness gain at this problem's scale.

**Scored against Frame's criteria:**
- Concurrency bound: all three satisfy it (semaphore or pool size).
- "Runs only after its own deps, as early as legally possible": Candidate 2
  and 3 satisfy; Candidate 1 does not (see Attack).
- Simplicity: Candidate 2 < Candidate 3 (no manual queue/worker-pool code).
- Cycle pre-check, cancellation: orthogonal to the choice; all three need a
  separate pre-flight pass; Candidate 2's cancellation story is simplest
  because it's one `gather` over a static task list.

**Pick: Candidate 2.** One-line why: it maps 1:1 onto the problem's actual
dataflow structure (a node's readiness is exactly "my deps' futures are all
`SUCCEEDED`"), giving maximal legal concurrency with no scheduler loop of
its own. **Switch trigger:** if the graph could have tens of thousands of
nodes, eagerly creating one `asyncio.Task` per node up front is wasteful
(O(N) live coroutines instead of O(concurrency)), and Candidate 3 would be
worth the extra bookkeeping. Nothing in the spec implies that scale, so I
didn't build for it.

## 4. Attack

**Attack 1 — does level-based (Candidate 1) actually break, concretely?**
`A` (10s), `B` (0.1s) both depend on nothing; `C` depends only on `B`.
Level 0 = `{A, B}`, level 1 = `{C}`. A level scheduler holds `C` until *both*
`A` and `B` finish (10s), even though `C`'s only real dependency, `B`,
finished at 0.1s. That's a real divergence from "runs only after all its
deps have succeeded" — it adds an unrequested extra gate (finish of
unrelated same-layer siblings). Candidate 2 starts `C` at ~0.1s. Attack
lands on Candidate 1, confirms Candidate 2.

**Attack 2 — concurrency bound, concrete counterexample attempt.**
Three independent slow tasks, `concurrency=2`. All three node-`Task`s are
created immediately and all reach `await semaphore.acquire()` in the same
synchronous burst (no deps to wait on). `Semaphore(2)` guarantees exactly 2
hold a permit; the third blocks until a release. No counterexample survives
— *provided* the semaphore wraps only `fn()`, not the dep-wait, which is
how it's built (dep-wait happens before the semaphore is even touched, so
tasks blocked on deps don't consume a concurrency slot — consistent with
"task **functions** executing," not "tasks pending").

**Attack 3 — retry/attempts edge cases.** `max_retries=2` (so `range(3)`
attempts): fails, fails, succeeds on the 3rd → `attempts=3`, `SUCCEEDED`,
`exception=None`. `max_retries=0` → `range(1)`, one try, no retry. Always-
failing task, `max_retries=2` → `attempts=3`, `FAILED`, `exception` = the
**last** attempt's exception (spec says "the final exception," singular —
last, not first, not a list). Matches F1 and Reading A. No break found.

**Attack 4 — cancellation mid-flight, does `gather`'s contract actually
deliver what the spec promises?** Trace: outer caller does
`t = asyncio.create_task(run_tasks(...)); t.cancel()` while a slow task `B`
is inside `await fn()` and a dependent `C` (`deps=['B']`) is blocked on
`await asyncio.gather(node_futures['B'])`. `run_tasks` itself is parked at
`await asyncio.gather(*node_tasks.values())`. Cancelling `t` delivers
`CancelledError` into that outer `gather`; per F5, `gather` cancels every
not-yet-done child node-`Task` (both `B`'s and `C`'s) and does not re-raise
until all of them finish unwinding. `B`'s coroutine: cancellation lands
inside `await fn()`; my retry loop has `except asyncio.CancelledError: raise`
(not swallowed by the broad `except Exception`, and F6 confirms
`except Exception` wouldn't have caught it anyway) — it re-raises, the
`async with semaphore:` block's `__aexit__` still runs (permit released,
no leak), `B`'s node-`Task` ends cancelled. `C`'s coroutine: cancellation
lands inside its inner `await asyncio.gather(node_futures['B'])`; nothing
to clean up, it's cancelled immediately, never reaches `fn()` (satisfies
"no new tasks start"). Once both unwind, the outer `gather` raises
`CancelledError` out of `run_tasks`, which is exactly "propagates" — no
extra `try/except` needed in `run_tasks` for this to work, because
`gather`'s own documented contract already does the waiting. This is a
load-bearing claim (F5) — flagged, and independently re-checked against the
stdlib `asyncio.gather` documentation, not just inferred.

**Attack 5 — steelman the rejected level-based candidate on cancellation.**
Level-based cancellation is arguably *simpler* to reason about (only the
current layer's tasks can ever be "in-flight," never a long queue of
not-yet-ready `asyncio.Task`s sitting around). This is a genuine point in
Candidate 1's favor, but it doesn't reopen the pick — the concurrency loss
from Attack 1 is a correctness-adjacent divergence from the spec's intent
("at most concurrency ... at any instant" implies keeping the pipe full
when legal), while Candidate 2's cancellation story, once traced through
`gather`'s actual contract, is no more complex in code, just less obvious
until you trace it (which Attack 4 now has). Surviving objection: **if**
`gather`'s cancel-propagation contract were misremembered, Candidate 2's
cancellation would silently break. I re-verified F5 against documented
`asyncio.gather` semantics rather than trusting recall alone; this is the
single highest-leverage fact in the whole design and is called out again in
Verify.

**Attack 6 — hand-trace the exact skip-propagation example used in
Verify** (diamond + unrelated branch) to make sure transitivity really is
automatic and not just plausible-sounding. Done below in Verify, since it's
also the correctness check.

## 5. Verify

**V1 — cycle check runs before anything executes.** `tasks = {'x': (['y'], fn_x), 'y': (['x'], fn_y)}`.
DFS: visit `x` → GRAY, push `x`; visit `y` (dep of `x`) → GRAY, push `y`;
`y`'s dep `x` is GRAY (on stack) → cycle found = `stack[index_of_x:]` =
`['x', 'y']`. `CycleError(['x','y'])` raised — and this happens in
`_find_cycle`, called as the very first statement of `run_tasks`, strictly
before any `asyncio.create_task` exists. No `fn` is ever called. Matches F4
and the "name only the true cycle" reading (not, e.g., some third node that
merely depends on `x`).

**V2 — skip propagation, full hand-trace** (Attack 6 promised): 
`A: ([], ok)`, `B: (['A'], always_fails)`, `C: (['B'], ok)`, `D: (['B'], ok)`
(diamond fan-out), `E: ([], ok)` (unrelated), `max_retries=1`,
`concurrency=2`.
- `A`, `E`'s node-Tasks have no deps → straight to retry loop → both
  `SUCCEEDED`, `attempts=1`.
- `B` waits on `A`'s future → resolves `SUCCEEDED` → `B` enters retry loop,
  `range(2)` attempts, both raise → `FAILED`, `attempts=2`,
  `exception=`last `RuntimeError`.
- `C` waits on `B`'s future → resolves to `Status.FAILED` (not
  `SUCCEEDED`) → `C` takes the skip branch: `TaskResult(SKIPPED, 0, None)`,
  `fn` for `C` **never invoked**.
- `D` — identical reasoning, independently — `SKIPPED`, `fn` never invoked.
- Final dict: `A=SUCCEEDED, B=FAILED(2), C=SKIPPED, D=SKIPPED, E=SUCCEEDED`.
  Matches criterion 5 exactly, and the transitivity (`D` also depending on
  the *same* failed `B`, not chained further) confirms fan-out, not just a
  linear chain, is handled — because each node only ever inspects its own
  direct deps' resolved status, and a SKIPPED/FAILED status is "not
  SUCCEEDED" either way, so a longer chain (`C2` depending on `C`) would
  cascade identically without extra code.

**V3 — cancellation hand-trace**, concrete: `long` (no deps, sleeps 10s),
`after` (`deps=['long']`). Start `run_tasks` as a task, wait until `long`
has actually entered `fn()` (via an `Event` the test sets), then cancel the
outer task. Per Attack 4's trace: `after` never starts (`fn` never called),
`long`'s `fn` observes `CancelledError` (test asserts this via a flag it
sets in an `except asyncio.CancelledError` inside the fn, then re-raises),
and awaiting the outer task raises `CancelledError` to the caller. This
directly exercises criterion 7, not just argues it.

**Re-reading Frame:** all 8 numbered criteria are hit by the design as
traced (V1–V3 plus Attacks 2/3 for concurrency-bound and retry counting).
The two flagged "two readings" are resolved and their chosen branch is
consistent with every hand-trace above, not just asserted.

---

## Deliverable

### Implementation

```python
"""Async DAG task runner — stdlib-only (Python 3.11, asyncio)."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from enum import Enum
from typing import Awaitable, Callable


class Status(Enum):
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    SKIPPED = "SKIPPED"


@dataclass(frozen=True)
class TaskResult:
    status: Status
    attempts: int
    exception: Exception | None


class CycleError(Exception):
    """Raised when the dependency graph contains a cycle.

    `.cycle` holds the node names that are actually on the cycle (not every
    node transitively blocked behind it).
    """

    def __init__(self, cycle: list[str]):
        self.cycle = cycle
        super().__init__(f"cycle detected among tasks: {' -> '.join(cycle)}")


def _find_cycle(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]]
) -> list[str] | None:
    """DFS with an explicit recursion stack (white/gray/black coloring).

    Returns the node sequence forming one cycle, or None if the graph is a
    DAG. Only the true cyclic nodes are returned, not downstream nodes that
    merely can never become ready because of it.
    """
    WHITE, GRAY, BLACK = 0, 1, 2
    color = {name: WHITE for name in tasks}
    stack: list[str] = []

    def visit(node: str) -> list[str] | None:
        color[node] = GRAY
        stack.append(node)
        for dep in tasks[node][0]:
            if color[dep] == WHITE:
                found = visit(dep)
                if found is not None:
                    return found
            elif color[dep] == GRAY:
                idx = stack.index(dep)
                return stack[idx:]
        stack.pop()
        color[node] = BLACK
        return None

    for name in tasks:
        if color[name] == WHITE:
            found = visit(name)
            if found is not None:
                return found
    return None


async def run_tasks(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]],
    concurrency: int,
    max_retries: int,
) -> dict[str, TaskResult]:
    # Pre-flight: no task fn may run if the graph has a cycle anywhere.
    cycle = _find_cycle(tasks)
    if cycle is not None:
        raise CycleError(cycle)

    semaphore = asyncio.Semaphore(concurrency)
    results: dict[str, TaskResult] = {}
    loop = asyncio.get_running_loop()
    # One future per node; its result is the node's terminal Status. Reading
    # a dep's future is the only synchronization a node needs.
    node_futures: dict[str, asyncio.Future] = {
        name: loop.create_future() for name in tasks
    }

    async def run_node(name: str) -> None:
        deps, fn = tasks[name]

        if deps:
            dep_statuses = await asyncio.gather(*(node_futures[d] for d in deps))
            if any(s is not Status.SUCCEEDED for s in dep_statuses):
                results[name] = TaskResult(Status.SKIPPED, 0, None)
                node_futures[name].set_result(Status.SKIPPED)
                return

        attempts = 0
        last_exc: Exception | None = None
        status = Status.FAILED
        for _ in range(max_retries + 1):
            attempts += 1
            async with semaphore:
                try:
                    await fn()
                except asyncio.CancelledError:
                    # BaseException, not Exception (Python 3.8+) — never
                    # caught below; re-raise so the node-task ends cancelled.
                    raise
                except Exception as e:
                    last_exc = e
                    status = Status.FAILED
                    continue
                else:
                    status = Status.SUCCEEDED
                    last_exc = None
                    break

        results[name] = TaskResult(status, attempts, last_exc)
        node_futures[name].set_result(status)

    node_tasks = [asyncio.create_task(run_node(name)) for name in tasks]
    # If this gather is cancelled, every not-yet-done node_task is cancelled
    # and awaited before CancelledError is re-raised here — this is the
    # entire cancellation contract, no extra try/except needed (see Attack 4).
    await asyncio.gather(*node_tasks)
    return results
```

### Tests (`unittest.IsolatedAsyncioTestCase`, stdlib only)

```python
import asyncio
import unittest

# from dag_runner import run_tasks, Status, CycleError  # adjust import to layout


class LinearChainTest(unittest.IsolatedAsyncioTestCase):
    async def test_linear_chain_runs_in_dependency_order(self):
        order = []

        async def a():
            order.append("A")

        async def b():
            order.append("B")

        async def c():
            order.append("C")

        tasks = {"A": ([], a), "B": (["A"], b), "C": (["B"], c)}
        results = await run_tasks(tasks, concurrency=3, max_retries=0)

        self.assertEqual(order, ["A", "B", "C"])
        for name in ("A", "B", "C"):
            self.assertEqual(results[name].status, Status.SUCCEEDED)
            self.assertEqual(results[name].attempts, 1)
            self.assertIsNone(results[name].exception)


class ConcurrencyLimitTest(unittest.IsolatedAsyncioTestCase):
    async def test_concurrency_never_exceeds_limit(self):
        current = 0
        peak = 0

        async def make_fn():
            async def fn():
                nonlocal current, peak
                current += 1
                peak = max(peak, current)
                await asyncio.sleep(0.05)
                current -= 1
            return fn

        tasks = {}
        for i in range(5):
            tasks[f"t{i}"] = ([], await make_fn())

        results = await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertLessEqual(peak, 2)
        self.assertEqual(peak, 2)  # 5 independent slow tasks, cap 2: reached deterministically
        for r in results.values():
            self.assertEqual(r.status, Status.SUCCEEDED)


class RetryTest(unittest.IsolatedAsyncioTestCase):
    async def test_retry_then_succeed(self):
        calls = {"n": 0}

        async def flaky():
            calls["n"] += 1
            if calls["n"] < 3:
                raise ValueError(f"fail {calls['n']}")

        results = await run_tasks({"t": ([], flaky)}, concurrency=1, max_retries=2)
        r = results["t"]
        self.assertEqual(r.status, Status.SUCCEEDED)
        self.assertEqual(r.attempts, 3)
        self.assertIsNone(r.exception)

    async def test_retry_exhausted_marks_failed(self):
        async def always_fails():
            raise RuntimeError("boom")

        results = await run_tasks({"t": ([], always_fails)}, concurrency=1, max_retries=2)
        r = results["t"]
        self.assertEqual(r.status, Status.FAILED)
        self.assertEqual(r.attempts, 3)
        self.assertIsInstance(r.exception, RuntimeError)


class SkipPropagationTest(unittest.IsolatedAsyncioTestCase):
    async def test_failed_task_skips_transitive_dependents_not_unrelated(self):
        executed = []

        async def a():
            executed.append("A")

        async def always_fails():
            executed.append("B")
            raise RuntimeError("boom")

        async def c():
            executed.append("C")

        async def d():
            executed.append("D")

        async def e():
            executed.append("E")

        tasks = {
            "A": ([], a),
            "B": (["A"], always_fails),
            "C": (["B"], c),
            "D": (["B"], d),
            "E": ([], e),
        }
        results = await run_tasks(tasks, concurrency=2, max_retries=1)

        self.assertEqual(results["A"].status, Status.SUCCEEDED)
        self.assertEqual(results["B"].status, Status.FAILED)
        self.assertEqual(results["B"].attempts, 2)
        self.assertEqual(results["C"].status, Status.SKIPPED)
        self.assertEqual(results["C"].attempts, 0)
        self.assertIsNone(results["C"].exception)
        self.assertEqual(results["D"].status, Status.SKIPPED)
        self.assertEqual(results["E"].status, Status.SUCCEEDED)

        self.assertNotIn("C", executed)
        self.assertNotIn("D", executed)
        self.assertIn("A", executed)
        self.assertIn("E", executed)


class CycleTest(unittest.IsolatedAsyncioTestCase):
    async def test_cycle_raises_before_running_anything(self):
        executed = []

        async def fn_x():
            executed.append("x")

        async def fn_y():
            executed.append("y")

        tasks = {"x": (["y"], fn_x), "y": (["x"], fn_y)}

        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(set(ctx.exception.cycle), {"x", "y"})
        self.assertEqual(executed, [])

    async def test_unknown_dependency_surfaces_before_running(self):
        # Not explicitly specified by the task; documents current behavior:
        # an unknown dep name surfaces as KeyError, still before any fn runs.
        async def fn():
            pass

        with self.assertRaises(KeyError):
            await run_tasks({"a": (["missing"], fn)}, concurrency=1, max_retries=0)


class CancellationTest(unittest.IsolatedAsyncioTestCase):
    async def test_cancel_stops_new_tasks_and_awaits_inflight(self):
        started = asyncio.Event()
        cancelled_seen = {"flag": False}
        never_started = {"flag": False}

        async def long_running():
            started.set()
            try:
                await asyncio.sleep(10)
            except asyncio.CancelledError:
                cancelled_seen["flag"] = True
                raise

        async def should_not_start():
            never_started["flag"] = True

        tasks = {
            "long": ([], long_running),
            "after": (["long"], should_not_start),
        }

        outer = asyncio.create_task(run_tasks(tasks, concurrency=2, max_retries=0))
        await started.wait()
        outer.cancel()

        with self.assertRaises(asyncio.CancelledError):
            await outer

        self.assertTrue(cancelled_seen["flag"])
        self.assertFalse(never_started["flag"])


if __name__ == "__main__":
    unittest.main()
```

---

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — would raise to high by actually executing the test
  suite; the design was hand-traced, not run, and asyncio scheduling
  interleaving (esp. the exact-peak-of-2 concurrency assertion) is argued
  from the cooperative-scheduling model rather than observed.
ASSUMPTIONS: (A1) SKIPPED → attempts=0, exception=None, no extra field;
  (A2) no delay between retries; (A3) attempts-on-success = actual try
  count, not always max_retries+1; (A4) unknown dep name surfaces as a
  natural KeyError rather than a custom validation error; (A6) CycleError
  reports one cycle, not all if several exist. All are carried into the
  Deliverable's docstrings/comments, not silently absorbed.
ATTACK: strongest surviving objection is Attack 4/5's dependency on
  asyncio.gather's documented cancel-propagation contract (F5) — if that
  contract were misremembered, the whole cancellation story would silently
  break, and it's the one place I could not execute to double-check. It
  doesn't kill the answer because F5 is the officially documented behavior
  of asyncio.gather (not inferred from analogy), it was re-verified
  explicitly rather than assumed, and the design adds no additional
  try/except around it that could mask a violation — a real regression here
  would surface immediately as `test_cancel_stops_new_tasks_and_awaits_inflight`
  hanging or failing to raise, not passing silently.
```
