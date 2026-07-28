# P2 — async DAG task runner

## Frame

Implement, Python 3.11 stdlib + asyncio only:

```python
async def run_tasks(tasks, concurrency, max_retries) -> dict[str, TaskResult]
```

`tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]]`, name → (deps, fn).

**Success criteria (checkable):**
1. A task only calls `fn()` after every dep in its list has a status of `SUCCEEDED`.
2. At every instant, the number of concurrently-executing `fn()` calls is ≤ `concurrency`.
3. A failing task is attempted up to `max_retries + 1` times total; a retry re-invokes only that task's `fn`, never re-runs its deps.
4. If a task's retries are exhausted (`FAILED`), every transitive dependent becomes `SKIPPED` with `fn` never called; tasks not downstream of the failure still run and can succeed.
5. A cycle in the dependency graph raises `CycleError` (naming the cycle's tasks) **before any `fn` anywhere is invoked** — including for tasks unrelated to the cycle.
6. If the `run_tasks` coroutine itself is cancelled: no task that hasn't started `fn()` yet is allowed to start it, every task whose `fn()` is mid-flight is cancelled and awaited, then `CancelledError` propagates out.
7. `TaskResult` exposes `status ∈ {SUCCEEDED, FAILED, SKIPPED}`, `attempts: int`, `exception: Exception | None`.

**Out of scope:** passing arguments to `fn`, inter-retry backoff/delay (not requested), task timeouts, logging, persistence, thread/process-based parallelism, priority scheduling beyond dependency+concurrency limits, defending against the caller cancelling `run_tasks` a *second* time while cleanup from the first cancellation is still in flight.

**Two readings worth naming, and how I resolved them:**
- *What exactly must stop on cancellation?* "no new tasks start" could mean no new `asyncio.Task` objects, or no new `fn()` invocations. I read it as the latter (fn-level), because the natural implementation creates one lightweight scheduling coroutine per graph node up front (cheap, no side effects) and only some of those go on to call `fn()`. See Attack/Verify for why this reading is also what falls out cleanly of the chosen design.
- *Does "transitive dependent" mean "descendant of the failed node" or "any node with an unresolved dep"?* These turn out to be the same set once you note the spec also says "a task runs only after all its deps have succeeded" — so a node with *any* non-`SUCCEEDED` dep must itself be `SKIPPED`, which cascades to exactly the transitive-dependent set. Not a real fork, just worth spelling out since it drives the implementation.

## Gather

**Facts** (from the task text):
- Exact signature and types are given.
- Total attempts on a task = `max_retries + 1` (explicit).
- Retry re-executes only that task, not its deps (explicit).
- Failing task ⇒ transitive dependents `SKIPPED`, unaffected tasks still complete (explicit).
- Cycle ⇒ `CycleError` naming the cycle's tasks, raised before running anything (explicit).
- Cancellation of `run_tasks`: no new starts; in-flight cancelled *and awaited* before the cancellation propagates (explicit).
- `TaskResult`: status, attempts, final exception (explicit).
- `asyncio.CancelledError` derives from `BaseException`, not `Exception`, since Python 3.8 (stdlib/PEP 3156 fact — matters below: a bare `except Exception` never intercepts cancellation).
- `asyncio.gather(...)`: if the awaiting task is itself cancelled, gather cancels every not-yet-done argument task (documented asyncio behavior).

**Assumptions** (uncited — carried into the code as comments/behavior, not silently absorbed):
- A) A dep name not present in `tasks` is a malformed-graph error, raised eagerly (like the cycle check) rather than treated as an implicit failure. Implemented as `ValueError`.
- B) No delay between retry attempts — spec doesn't ask for backoff, adding one would be speculative.
- C) `concurrency ≥ 1`, `max_retries ≥ 0`; invalid values are left to fail naturally (e.g. `asyncio.Semaphore` rejects negative values) rather than re-validated redundantly.
- D) "final exception" = the exception from the *last* attempt (most actionable), not the first.
- E) A double-cancellation of `run_tasks` (cancel arrives again while cleanup from the first cancel is still awaiting) is out of scope; documented as a known gap, not defended against.

## Branch

**Candidate 1 — Kahn's-algorithm queue + worker pool.** Track in-degree per node and a reverse-adjacency map; seed a queue with in-degree-0 nodes; a pool of `concurrency` workers pull ready names, run them (with retries), decrement dependents' counters, enqueue newly-ready ones (running or skipped).

**Candidate 2 — memoized recursive resolve per node.** One `async def resolve(name)` per task, created as an `asyncio.Task` for *every* name up front. `resolve` awaits its deps' Tasks (via a shared `node_tasks` dict, so each name is only ever computed once no matter how many dependents await it), then either skips or runs its own `fn` under a shared `asyncio.Semaphore(concurrency)`.

**Candidate 3 — generational/layered batches.** Compute topological "layers" via Kahn's algorithm, run each layer as one `asyncio.gather` bounded by a semaphore, repeat.

Scored against Frame's criteria:
- **C1**: correct, handles fan-in naturally via degree-counting; but is more bookkeeping (queue + degree map + manual re-enqueue logic) for no extra correctness benefit at the scale implied by the task.
- **C2**: correct-by-construction diamond handling (memoization ⇒ shared dep runs exactly once); cancellation story is the cleanest of the three — because every node's Task exists from the start, "no new `fn()` starts after cancel" is automatic (a parked `resolve` that hasn't reached its `fn()` call yet just gets cancelled at the `await gather(deps)` line and never proceeds).
- **C3**: layer barriers make independent later-layer tasks wait on unrelated earlier-layer stragglers, under-using the concurrency budget the spec asks to bound (not exceed) — and once you patch that by tracking per-node readiness inside a layer, you've reinvented C1. Rejected as a strictly worse variant of C1.

**Pick: Candidate 2.** One-line why: it gets diamond-correctness and the "no new task starts post-cancel" guarantee for free from the memoized-Task structure, with less custom bookkeeping than C1.
**Switch trigger:** graphs large enough (≫10⁵ nodes) that eagerly creating one `asyncio.Task` per node is itself a memory/scheduling concern — no such scale is stated here, so this doesn't fire.

## Attack

- **Race in naive memoization:** if node Tasks were created *lazily* on first await (`node_tasks.setdefault(name, asyncio.create_task(...))` called from inside multiple concurrent `resolve` bodies), two dependents could both observe "not yet created" and each spawn a Task for the same name — double-executing a shared dependency, breaking "each task attempted at most `max_retries+1` times." **Fix incorporated into the design:** create *all* node Tasks in a single synchronous dict comprehension before any `resolve` body runs. This is safe because `asyncio.create_task()` only schedules a callback — it doesn't yield control to the loop — so the whole `node_tasks` dict is fully populated before the first `resolve` body actually executes and looks up a dep by name.
- **Diamond, hand-traced:** `base` (no deps) → `left`, `right` (both dep on `base`) → `top` (deps `[left, right]`). `left` and `right` both `await node_tasks["base"]`, the *same* Task object — awaiting an already-scheduled Task twice returns its cached result without re-running the coroutine body. `base`'s `fn` executes exactly once. ✓.
- **Failure cascade, hand-traced:** `a` (no deps, succeeds), `b` (deps `[a]`, always fails), `c` (deps `[b]`), `d` (deps `[a,b]`), `e` (deps `[a]`, unrelated to `b`). `resolve(c)`: dep_results = `[FAILED]` → SKIPPED, `fn` never called. `resolve(d)`: dep_results = `[SUCCEEDED, FAILED]` → any-non-succeeded is True → SKIPPED (correctly cascades even though `d` also has a *succeeding* dep). `resolve(e)`: dep_results = `[SUCCEEDED]` → runs normally, SUCCEEDED. Matches criterion 4 exactly, including the "unrelated tasks still complete" clause.
- **Retry count at the boundary:** `max_retries=0` → `range(0+1)` → exactly 1 attempt. `max_retries=2`, task fails twice then succeeds → attempts recorded = 3 at success (loop breaks on the successful attempt, `attempts` already incremented to 3 for that iteration). Both check out against criterion 3.
- **Concurrency during a retry:** does a task pending retry hold its semaphore slot across attempts (wasting a concurrency slot on nothing) or release it between attempts? Design: `async with semaphore` wraps *only* the single `await fn()` call inside the retry loop, released before the loop's next iteration — matches "at most `concurrency` task **functions** may be executing," not "reserved."
- **Cancellation, hand-traced:** caller does `t = create_task(run_tasks(...)); await sleep(x); t.cancel()`. The outer `await asyncio.gather(*node_tasks.values())` receives the cancellation; per the cited asyncio fact, gather cancels every not-yet-done node Task itself — a node still parked at `await asyncio.gather(*(node_tasks[d] for d in deps))` (i.e. hasn't reached `fn()` yet) is cancelled right there and *never calls `fn()`*, satisfying "no new tasks start" under the fn-level reading from Frame. A node mid-`fn()` gets `CancelledError` injected at `fn`'s current await point (standard asyncio cancellation delivery). The `except asyncio.CancelledError` clause in `run_tasks` then explicitly re-cancels (belt-and-suspenders, harmless no-op if already cancelled) and does `await asyncio.gather(*node_tasks.values(), return_exceptions=True)` — this is the "awaited before propagating" requirement — before re-raising.
- **Steelman of the rejected C1:** for graphs orders of magnitude larger than anything implied here, C1's O(concurrency) live coroutines beats C2's O(N) live Tasks on memory. Correctly kills C2 *only* under a stated scale requirement, which this task doesn't give — doesn't flip the pick.
- **Surviving objection:** if the caller cancels `run_tasks` a *second* time while the except-block's cleanup `gather` is itself still awaiting, that second cancellation could interrupt cleanup before every task is fully awaited, technically breaking "in-flight tasks are... awaited before the cancellation propagates." I judge this out of the stated scope (the spec describes a single cancellation) and do not add defensive re-shielding for it — flagged in Assumptions (E) rather than silently ignored.

## Verify

Check applied: hand-traced every scenario above against the actual code (not vibes) — chain-succeeds, retry-then-succeed, retry-exhausted, diamond-shared-dep-once, failure-cascade-with-unrelated-survivor, cycle-detected-before-anything-runs, unknown-dependency, concurrency-bound, cancellation. All traced consistent with Frame's 7 criteria. Executable tests below encode the same scenarios so a reader who *can* run code gets a second, independent check.

Re-reading Frame: criteria 1–7 are each satisfied by a specific piece of the design — dep-status gating (1), semaphore scoped to the `fn()` call only (2), `range(max_retries+1)` attempt loop (3), any-non-succeeded-dep ⇒ SKIPPED cascade (4), `_validate_graph` runs fully before any `asyncio.create_task` (5), gather's automatic child-cancellation + explicit cancel-and-await in the except clause (6), `TaskResult` dataclass fields (7). No drift found.

## Deliverable

`dag_runner.py`:

```python
"""Concurrency-bounded, dependency-ordered async task runner.

Implements `run_tasks`: given a DAG of named async tasks, runs each task
only after its dependencies have succeeded, bounds concurrent execution,
retries failing tasks, and skips transitive dependents of a task that
ultimately fails.
"""

import asyncio
from dataclasses import dataclass
from enum import Enum
from typing import Awaitable, Callable, Optional


class Status(Enum):
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    SKIPPED = "SKIPPED"


@dataclass
class TaskResult:
    status: Status
    attempts: int
    exception: Optional[BaseException] = None


class CycleError(Exception):
    """Raised when the dependency graph contains a cycle.

    `cycle` lists the task names forming the cycle, in order, with the
    starting node repeated at the end (e.g. ["a", "b", "c", "a"]).
    """

    def __init__(self, cycle: list[str]):
        self.cycle = cycle
        super().__init__(f"Dependency cycle detected: {' -> '.join(cycle)}")


def _validate_graph(tasks: dict) -> None:
    # Unknown dependency names are a malformed-graph error (Assumption A),
    # raised eagerly, before the cycle check or any scheduling.
    for name, (deps, _fn) in tasks.items():
        for dep in deps:
            if dep not in tasks:
                raise ValueError(f"Task {name!r} depends on unknown task {dep!r}")

    # DFS cycle detection via white/gray/black coloring. Must fully
    # complete (raise or return) before any asyncio.Task is created, so a
    # cycle involving only some tasks still blocks unrelated tasks from
    # running (spec: "before running anything").
    WHITE, GRAY, BLACK = 0, 1, 2
    color = {name: WHITE for name in tasks}
    path: list[str] = []

    def visit(name: str) -> None:
        color[name] = GRAY
        path.append(name)
        for dep in tasks[name][0]:
            if color[dep] == GRAY:
                idx = path.index(dep)
                raise CycleError(path[idx:] + [dep])
            if color[dep] == WHITE:
                visit(dep)
        path.pop()
        color[name] = BLACK

    for name in tasks:
        if color[name] == WHITE:
            visit(name)


async def run_tasks(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]],
    concurrency: int,
    max_retries: int,
) -> dict[str, TaskResult]:
    _validate_graph(tasks)

    semaphore = asyncio.Semaphore(concurrency)
    results: dict[str, TaskResult] = {}

    async def resolve(name: str) -> TaskResult:
        deps, fn = tasks[name]

        # Wait for every dependency to reach a terminal state. Each dep is
        # looked up in `node_tasks`, a single asyncio.Task per name, so no
        # matter how many dependents await it, a task's fn runs at most
        # once (plus retries) — never once per dependent.
        dep_results = await asyncio.gather(*(node_tasks[d] for d in deps))

        if any(r.status is not Status.SUCCEEDED for r in dep_results):
            result = TaskResult(status=Status.SKIPPED, attempts=0)
            results[name] = result
            return result

        attempts = 0
        last_exc: Optional[BaseException] = None
        for _ in range(max_retries + 1):
            attempts += 1
            async with semaphore:  # slot held only for this one attempt
                try:
                    await fn()
                except Exception as exc:  # CancelledError is a BaseException,
                    last_exc = exc        # so it is never swallowed here.
                    continue
                result = TaskResult(status=Status.SUCCEEDED, attempts=attempts)
                results[name] = result
                return result

        result = TaskResult(status=Status.FAILED, attempts=attempts, exception=last_exc)
        results[name] = result
        return result

    # Create every node's Task up front, before awaiting anything. The dict
    # comprehension runs to completion synchronously (create_task only
    # schedules; it doesn't yield control), so `node_tasks` is fully
    # populated before any `resolve` body actually executes and looks up a
    # dependency by name — no double-create race on shared dependencies.
    node_tasks: dict[str, asyncio.Task] = {
        name: asyncio.create_task(resolve(name)) for name in tasks
    }

    try:
        await asyncio.gather(*node_tasks.values())
    except asyncio.CancelledError:
        # gather() already cancels every not-yet-done child on cancellation;
        # the explicit cancel() below is a harmless no-op in that case and
        # only matters if this code is ever refactored away from a single
        # gather call. Awaiting them (return_exceptions=True so one
        # CancelledError doesn't stop us collecting the rest) is what
        # satisfies "in-flight tasks are cancelled and awaited before the
        # cancellation propagates".
        for t in node_tasks.values():
            t.cancel()
        await asyncio.gather(*node_tasks.values(), return_exceptions=True)
        raise

    return results
```

`test_dag_runner.py`:

```python
import asyncio
import unittest

from dag_runner import CycleError, Status, run_tasks


def ok(record=None, name=None):
    async def _fn():
        if record is not None:
            record.append(name)
    return _fn


def fail_n_times(n, exc_factory=lambda: RuntimeError("transient")):
    calls = {"count": 0}

    async def _fn():
        calls["count"] += 1
        if calls["count"] <= n:
            raise exc_factory()

    return _fn


class RunTasksTests(unittest.IsolatedAsyncioTestCase):

    async def test_simple_chain_succeeds_in_order(self):
        order = []
        tasks = {
            "a": ([], ok(order, "a")),
            "b": (["a"], ok(order, "b")),
            "c": (["b"], ok(order, "c")),
        }
        results = await run_tasks(tasks, concurrency=2, max_retries=0)
        self.assertEqual(order, ["a", "b", "c"])
        for name in ("a", "b", "c"):
            self.assertEqual(results[name].status, Status.SUCCEEDED)
            self.assertEqual(results[name].attempts, 1)
            self.assertIsNone(results[name].exception)

    async def test_retry_then_succeed_counts_attempts(self):
        tasks = {"a": ([], fail_n_times(2))}  # fails twice, succeeds 3rd try
        results = await run_tasks(tasks, concurrency=1, max_retries=2)
        self.assertEqual(results["a"].status, Status.SUCCEEDED)
        self.assertEqual(results["a"].attempts, 3)

    async def test_retry_exhausted_fails_with_final_exception(self):
        tasks = {"a": ([], fail_n_times(99))}  # never succeeds within budget
        results = await run_tasks(tasks, concurrency=1, max_retries=2)
        self.assertEqual(results["a"].status, Status.FAILED)
        self.assertEqual(results["a"].attempts, 3)  # max_retries + 1
        self.assertIsInstance(results["a"].exception, RuntimeError)

    async def test_failure_skips_transitive_dependents_but_not_unrelated(self):
        order = []
        tasks = {
            "a": ([], ok(order, "a")),
            "b": (["a"], fail_n_times(99)),      # always fails
            "c": (["b"], ok(order, "c")),        # depends on failing b -> SKIPPED
            "d": (["a", "b"], ok(order, "d")),   # one succeeded dep, one failed -> SKIPPED
            "e": (["a"], ok(order, "e")),        # unrelated to b -> should run
        }
        results = await run_tasks(tasks, concurrency=3, max_retries=0)
        self.assertEqual(results["a"].status, Status.SUCCEEDED)
        self.assertEqual(results["b"].status, Status.FAILED)
        self.assertEqual(results["c"].status, Status.SKIPPED)
        self.assertEqual(results["c"].attempts, 0)
        self.assertEqual(results["d"].status, Status.SKIPPED)
        self.assertEqual(results["e"].status, Status.SUCCEEDED)
        self.assertNotIn("c", order)
        self.assertNotIn("d", order)
        self.assertIn("e", order)

    async def test_diamond_shared_dependency_runs_exactly_once(self):
        calls = {"count": 0}

        async def shared_fn():
            calls["count"] += 1

        tasks = {
            "base": ([], shared_fn),
            "left": (["base"], ok()),
            "right": (["base"], ok()),
            "top": (["left", "right"], ok()),
        }
        results = await run_tasks(tasks, concurrency=4, max_retries=0)
        self.assertEqual(calls["count"], 1)
        self.assertEqual(results["top"].status, Status.SUCCEEDED)

    async def test_concurrency_bound_is_respected_and_used(self):
        current = 0
        peak = 0
        guard = asyncio.Lock()

        async def track_fn():
            nonlocal current, peak
            async with guard:
                current += 1
                peak = max(peak, current)
            await asyncio.sleep(0.02)
            async with guard:
                current -= 1

        tasks = {f"t{i}": ([], track_fn) for i in range(6)}
        results = await run_tasks(tasks, concurrency=2, max_retries=0)
        self.assertLessEqual(peak, 2)
        self.assertEqual(peak, 2)  # confirms slots are actually used, not under-scheduled
        self.assertTrue(all(r.status == Status.SUCCEEDED for r in results.values()))

    async def test_cycle_raises_before_anything_runs(self):
        executed = []

        tasks = {
            "a": (["b"], ok(executed, "a")),
            "b": (["c"], ok(executed, "b")),
            "c": (["a"], ok(executed, "c")),
        }
        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)
        self.assertTrue(set(ctx.exception.cycle) >= {"a", "b", "c"})
        self.assertEqual(executed, [])

    async def test_unknown_dependency_raises_value_error(self):
        tasks = {"a": (["missing"], ok())}
        with self.assertRaises(ValueError):
            await run_tasks(tasks, concurrency=1, max_retries=0)

    async def test_cancellation_stops_new_work_and_awaits_inflight(self):
        started = []
        cancelled_cleanly = []

        async def slow_fn(name):
            started.append(name)
            try:
                await asyncio.sleep(5)
            except asyncio.CancelledError:
                cancelled_cleanly.append(name)
                raise

        tasks = {
            "a": ([], lambda: slow_fn("a")),
            "b": ([], lambda: slow_fn("b")),
            "later": (["a"], ok()),  # would only become ready once 'a' succeeds
        }
        runner = asyncio.ensure_future(run_tasks(tasks, concurrency=2, max_retries=0))
        await asyncio.sleep(0.01)  # let a, b actually start
        runner.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await runner
        self.assertIn("a", started)
        self.assertIn("b", started)
        self.assertNotIn("later", started)
        await asyncio.sleep(0)
        self.assertEqual(set(cancelled_cleanly), {"a", "b"})


if __name__ == "__main__":
    unittest.main()
```

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — hand-traced every listed scenario against the actual code; would raise to high by actually executing the test file (I have no code execution available in this run).
ASSUMPTIONS: (A) unknown dep name -> ValueError, raised eagerly; (B) no inter-retry delay; (C) concurrency>=1 / max_retries>=0 not separately re-validated beyond what asyncio.Semaphore enforces; (D) "final exception" = last attempt's exception; (E) a second cancellation arriving mid-cleanup from the first is not defended against.
ATTACK: strongest surviving objection is (E) — a caller that cancels run_tasks twice in quick succession could interrupt the cleanup gather before every in-flight task is fully awaited. Doesn't kill the answer because the spec describes a single cancellation and defending the re-entrant case would be speculative complexity beyond what was asked.
```
