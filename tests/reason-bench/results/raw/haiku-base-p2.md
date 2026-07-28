# P2 — Async DAG task runner

## Design notes (read before the code)

- **Scheduling model**: create one `asyncio.Task` per node up front (a
  "driver" coroutine `run_node(name)`). Each driver first `await`s an
  `asyncio.Event` per dependency, so the DAG's partial order is enforced by
  event-waiting, not by a hand-rolled scheduler loop. All nodes are "live"
  concurrently; the DAG only gates *when a node is allowed to call its own
  `fn`*.
- **Concurrency bound**: a single `asyncio.Semaphore(concurrency)` wraps only
  the `await fn()` call (one `async with semaphore:` per *attempt*). Waiting
  on dependency events or sitting idle between retries does not hold a
  permit, so "at most `concurrency` executing" is exact, not approximate.
- **Retries**: a plain `for _ in range(max_retries + 1)` loop around the
  semaphore/`fn()` call. Each iteration is a fresh attempt; the semaphore is
  released between attempts (via `async with`) so other ready tasks can use
  the freed slot while this task is "between attempts". Dependencies are
  looked up once, before the retry loop — a retry never touches `deps`.
- **Skip propagation**: a node is `SKIPPED` iff *any* immediate dependency's
  final status is not `SUCCEEDED` (covers both `FAILED` and `SKIPPED`
  deps). Because every node applies this same rule to its own immediate
  deps, transitive skipping falls out for free — no separate "walk
  descendants" pass is needed.
- **Cycle detection**: a standard white/gray/black iterative DFS (iterative,
  not recursive, so it doesn't hit Python's recursion limit on large
  graphs) run once, synchronously, before any `Task` is created. Since
  `run_tasks` is itself a coroutine, this check executes the instant the
  coroutine is driven (first `await`ed), strictly before any node task
  exists — satisfying "raise before running anything."
- **Unknown dependency names**: not explicitly specified, but left
  unvalidated it would surface as a confusing `KeyError` deep inside a
  driver coroutine. A tiny upfront validation raises a clear `ValueError`
  instead. This is the one behavior added beyond the literal spec; flagging
  it here rather than silently redefining semantics.
- **Cancellation**: when the caller cancels the `run_tasks()` task while it
  is suspended on `await asyncio.gather(*node_tasks.values())`, asyncio
  delivers the cancellation to that gather (cancelling the future the task
  is actually waiting on). The `except asyncio.CancelledError` handler
  explicitly cancels every node task and re-`gather`s them with
  `return_exceptions=True` — i.e. it **waits for every in-flight driver to
  actually finish unwinding** (releasing its semaphore permit, running its
  `finally`) before re-raising. A node blocked on `await
  done_events[dep].wait()` (i.e. not yet started) is cancelled at that
  suspension point and never reaches `await fn()` — so "no new tasks
  start" holds structurally, not by a flag check.
- **Result bookkeeping race**: `results[name] = TaskResult(...)` always
  happens (if it happens at all) strictly before `done_events[name].set()`
  in the same synchronous stretch of code (no `await` in between), so any
  waiter woken by the event always sees a populated entry — except in the
  one case where a node is cancelled *before* ever assigning a result, which
  only occurs during whole-run cancellation. To stay defensive there,
  dependents look up `results.get(d)` (never a bare `results[d]`) so a
  missing entry can't raise `KeyError`; it is simply treated as "not
  succeeded". This only matters transiently during a cancel, since in that
  path `run_tasks` never returns a results dict anyway (it re-raises
  `CancelledError`).
- `asyncio.CancelledError` is `BaseException`-derived in 3.8+, so the plain
  `except Exception` around `fn()` cannot accidentally swallow a
  cancellation; the explicit `except asyncio.CancelledError: raise` above it
  is intentionally redundant — kept for readability/documentation, not
  because it changes behavior.

## Implementation

```python
"""Async DAG task runner — Python 3.11, standard library only."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from enum import Enum
from typing import Awaitable, Callable, Optional


class Status(str, Enum):
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    SKIPPED = "SKIPPED"


@dataclass
class TaskResult:
    status: Status
    attempts: int
    exception: Optional[BaseException] = None


class CycleError(Exception):
    """Raised when the task dependency graph contains a cycle.

    ``cycle`` is the list of task names forming the cycle, in order, with
    the first name repeated at the end to make the loop explicit
    (e.g. ``["A", "B", "C", "A"]``).
    """

    def __init__(self, cycle: list[str]):
        self.cycle = cycle
        super().__init__(f"dependency cycle detected: {' -> '.join(cycle)}")


TaskMap = dict[str, tuple[list[str], Callable[[], Awaitable]]]


def _validate_deps(tasks: TaskMap) -> None:
    for name, (deps, _fn) in tasks.items():
        for d in deps:
            if d not in tasks:
                raise ValueError(
                    f"task {name!r} depends on unknown task {d!r}"
                )


def _find_cycle(tasks: TaskMap) -> Optional[list[str]]:
    """Iterative white/gray/black DFS. Returns a cycle (list of names,
    first repeated at the end) or None if the graph is acyclic."""
    WHITE, GRAY, BLACK = 0, 1, 2
    color = {name: WHITE for name in tasks}

    for start in tasks:
        if color[start] != WHITE:
            continue
        color[start] = GRAY
        path = [start]
        stack = [(start, iter(tasks[start][0]))]
        while stack:
            node, dep_iter = stack[-1]
            advanced = False
            for dep in dep_iter:
                if color[dep] == GRAY:
                    idx = path.index(dep)
                    return path[idx:] + [dep]
                if color[dep] == WHITE:
                    color[dep] = GRAY
                    path.append(dep)
                    stack.append((dep, iter(tasks[dep][0])))
                    advanced = True
                    break
                # BLACK: already fully explored, no cycle through here.
            if not advanced:
                color[node] = BLACK
                path.pop()
                stack.pop()
    return None


async def run_tasks(
    tasks: TaskMap,
    concurrency: int,
    max_retries: int,
) -> dict[str, TaskResult]:
    _validate_deps(tasks)

    cycle = _find_cycle(tasks)
    if cycle is not None:
        raise CycleError(cycle)

    semaphore = asyncio.Semaphore(concurrency)
    done_events: dict[str, asyncio.Event] = {name: asyncio.Event() for name in tasks}
    results: dict[str, TaskResult] = {}

    def _dep_status(d: str) -> Optional[Status]:
        r = results.get(d)
        return r.status if r is not None else None

    async def run_node(name: str) -> None:
        deps, fn = tasks[name]
        try:
            for d in deps:
                await done_events[d].wait()

            if any(_dep_status(d) is not Status.SUCCEEDED for d in deps):
                results[name] = TaskResult(Status.SKIPPED, 0, None)
                return

            attempts = 0
            last_exc: Optional[BaseException] = None
            for _ in range(max_retries + 1):
                attempts += 1
                async with semaphore:
                    try:
                        await fn()
                        results[name] = TaskResult(Status.SUCCEEDED, attempts, None)
                        return
                    except asyncio.CancelledError:
                        # BaseException, never caught by `except Exception`
                        # below anyway; explicit for readability.
                        raise
                    except Exception as exc:  # noqa: BLE001 - task-defined fn
                        last_exc = exc
            results[name] = TaskResult(Status.FAILED, attempts, last_exc)
        finally:
            done_events[name].set()

    node_tasks = {name: asyncio.create_task(run_node(name)) for name in tasks}

    try:
        await asyncio.gather(*node_tasks.values())
    except asyncio.CancelledError:
        for t in node_tasks.values():
            t.cancel()
        # Wait for every in-flight driver to actually unwind (release its
        # semaphore permit, run its `finally`) before propagating.
        await asyncio.gather(*node_tasks.values(), return_exceptions=True)
        raise

    return results
```

## Tests

```python
"""Tests for run_tasks — standard library only (unittest)."""

import asyncio
import unittest

# from runner import run_tasks, TaskResult, Status, CycleError


class TestBasicExecution(unittest.IsolatedAsyncioTestCase):
    async def test_linear_chain_runs_in_dependency_order(self):
        order = []

        def make_fn(name):
            async def fn():
                order.append(name)
            return fn

        tasks = {
            "A": ([], make_fn("A")),
            "B": (["A"], make_fn("B")),
            "C": (["B"], make_fn("C")),
        }
        results = await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(order, ["A", "B", "C"])
        for name in ("A", "B", "C"):
            self.assertEqual(results[name].status, Status.SUCCEEDED)
            self.assertEqual(results[name].attempts, 1)
            self.assertIsNone(results[name].exception)

    async def test_independent_tasks_all_succeed(self):
        tasks = {f"t{i}": ([], self._noop) for i in range(5)}
        results = await run_tasks(tasks, concurrency=3, max_retries=0)
        self.assertTrue(all(r.status == Status.SUCCEEDED for r in results.values()))

    @staticmethod
    async def _noop():
        return None


class TestConcurrency(unittest.IsolatedAsyncioTestCase):
    async def test_concurrency_is_bounded_and_saturated(self):
        concurrency = 2
        state = {"in_flight": 0, "max_in_flight": 0}

        async def fn():
            state["in_flight"] += 1
            state["max_in_flight"] = max(state["max_in_flight"], state["in_flight"])
            await asyncio.sleep(0.02)
            state["in_flight"] -= 1

        tasks = {f"t{i}": ([], fn) for i in range(6)}
        results = await run_tasks(tasks, concurrency=concurrency, max_retries=0)

        self.assertLessEqual(state["max_in_flight"], concurrency)
        self.assertEqual(state["max_in_flight"], concurrency)
        self.assertTrue(all(r.status == Status.SUCCEEDED for r in results.values()))


class TestRetries(unittest.IsolatedAsyncioTestCase):
    async def test_retry_then_succeed(self):
        calls = {"n": 0}

        async def flaky():
            calls["n"] += 1
            if calls["n"] < 3:
                raise RuntimeError(f"fail {calls['n']}")

        tasks = {"A": ([], flaky)}
        results = await run_tasks(tasks, concurrency=1, max_retries=5)

        self.assertEqual(results["A"].status, Status.SUCCEEDED)
        self.assertEqual(results["A"].attempts, 3)
        self.assertIsNone(results["A"].exception)

    async def test_retries_exhausted_marks_failed(self):
        calls = {"n": 0}

        async def always_fails():
            calls["n"] += 1
            raise ValueError("boom")

        tasks = {"A": ([], always_fails)}
        results = await run_tasks(tasks, concurrency=1, max_retries=2)

        self.assertEqual(results["A"].status, Status.FAILED)
        self.assertEqual(results["A"].attempts, 3)  # max_retries + 1
        self.assertIsInstance(results["A"].exception, ValueError)
        self.assertEqual(calls["n"], 3)

    async def test_retry_reexecutes_only_the_failing_task_not_its_deps(self):
        dep_calls = {"n": 0}
        target_calls = {"n": 0}

        async def dep():
            dep_calls["n"] += 1

        async def target():
            target_calls["n"] += 1
            if target_calls["n"] < 2:
                raise RuntimeError("retry me")

        tasks = {"D": ([], dep), "T": (["D"], target)}
        results = await run_tasks(tasks, concurrency=2, max_retries=3)

        self.assertEqual(results["T"].status, Status.SUCCEEDED)
        self.assertEqual(target_calls["n"], 2)
        self.assertEqual(dep_calls["n"], 1)  # dep ran exactly once


class TestSkipPropagation(unittest.IsolatedAsyncioTestCase):
    async def test_transitive_dependents_are_skipped_unrelated_still_runs(self):
        ran = set()

        async def bad():
            ran.add("A")
            raise RuntimeError("A fails")

        async def dependent_b():
            ran.add("B")  # must never execute

        async def dependent_c():
            ran.add("C")  # must never execute

        async def ok():
            ran.add("D")

        tasks = {
            "A": ([], bad),
            "B": (["A"], dependent_b),
            "C": (["B"], dependent_c),
            "D": ([], ok),
        }
        results = await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(results["A"].status, Status.FAILED)
        self.assertEqual(results["B"].status, Status.SKIPPED)
        self.assertEqual(results["B"].attempts, 0)
        self.assertEqual(results["C"].status, Status.SKIPPED)
        self.assertEqual(results["C"].attempts, 0)
        self.assertEqual(results["D"].status, Status.SUCCEEDED)
        self.assertNotIn("B", ran)
        self.assertNotIn("C", ran)
        self.assertIn("D", ran)

    async def test_diamond_any_failed_dep_causes_skip(self):
        ran = []

        async def a():
            ran.append("A")

        async def b_fails():
            ran.append("B")
            raise RuntimeError("b boom")

        async def c_ok():
            ran.append("C")

        async def d():
            ran.append("D")  # must never execute: B failed

        tasks = {
            "A": ([], a),
            "B": (["A"], b_fails),
            "C": (["A"], c_ok),
            "D": (["B", "C"], d),
        }
        results = await run_tasks(tasks, concurrency=3, max_retries=0)

        self.assertEqual(results["A"].status, Status.SUCCEEDED)
        self.assertEqual(results["B"].status, Status.FAILED)
        self.assertEqual(results["C"].status, Status.SUCCEEDED)
        self.assertEqual(results["D"].status, Status.SKIPPED)
        self.assertNotIn("D", ran)


class TestCycleDetection(unittest.IsolatedAsyncioTestCase):
    async def test_cycle_raises_before_running_anything(self):
        ran = []

        async def fn_a():
            ran.append("A")

        async def fn_b():
            ran.append("B")

        async def fn_c():
            ran.append("C")

        tasks = {
            "A": (["C"], fn_a),
            "B": (["A"], fn_b),
            "C": (["B"], fn_c),
        }
        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)

        # cycle names present (rotation-independent), and nothing executed.
        self.assertEqual(set(ctx.exception.cycle[:-1]), {"A", "B", "C"})
        self.assertEqual(ctx.exception.cycle[0], ctx.exception.cycle[-1])
        self.assertEqual(ran, [])

    async def test_self_dependency_is_a_cycle(self):
        async def fn():
            pass

        tasks = {"A": (["A"], fn)}
        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=1, max_retries=0)
        self.assertEqual(ctx.exception.cycle, ["A", "A"])

    async def test_unknown_dependency_raises_value_error(self):
        async def fn():
            pass

        tasks = {"A": (["ghost"], fn)}
        with self.assertRaises(ValueError):
            await run_tasks(tasks, concurrency=1, max_retries=0)


class TestCancellation(unittest.IsolatedAsyncioTestCase):
    async def test_cancel_stops_new_tasks_and_awaits_inflight(self):
        state = {"a_started": False, "a_cleaned_up": False, "b_started": False}

        async def a_long_running():
            state["a_started"] = True
            try:
                await asyncio.sleep(10)
            except asyncio.CancelledError:
                state["a_cleaned_up"] = True
                raise

        async def b_should_not_start():
            state["b_started"] = True  # depends on A; A never succeeds

        tasks = {
            "A": ([], a_long_running),
            "B": (["A"], b_should_not_start),
        }

        task = asyncio.create_task(run_tasks(tasks, concurrency=2, max_retries=0))
        await asyncio.sleep(0.01)  # let A actually start and reach its sleep
        task.cancel()

        with self.assertRaises(asyncio.CancelledError):
            await task

        self.assertTrue(state["a_started"])
        self.assertTrue(state["a_cleaned_up"])
        self.assertFalse(state["b_started"])

    async def test_cancel_while_blocked_on_concurrency_prevents_start(self):
        # concurrency=1, two tasks with no deps: the second is blocked on the
        # semaphore and must never enter its body once cancelled.
        state = {"first_started": False, "second_started": False}
        release = asyncio.Event()

        async def first():
            state["first_started"] = True
            await release.wait()  # held open until we cancel

        async def second():
            state["second_started"] = True  # must never happen

        tasks = {"first": ([], first), "second": ([], second)}
        task = asyncio.create_task(run_tasks(tasks, concurrency=1, max_retries=0))
        await asyncio.sleep(0.01)  # let `first` acquire the only permit
        task.cancel()

        with self.assertRaises(asyncio.CancelledError):
            await task

        self.assertTrue(state["first_started"])
        self.assertFalse(state["second_started"])


if __name__ == "__main__":
    unittest.main()
```

## Behavior-to-test cross reference

| Required behavior | Verified by |
|---|---|
| Runs only after deps succeed / respects DAG order | `test_linear_chain_runs_in_dependency_order` |
| At most `concurrency` executing at once | `test_concurrency_is_bounded_and_saturated` |
| Retry up to `max_retries` (total attempts = `max_retries + 1`) | `test_retry_then_succeed`, `test_retries_exhausted_marks_failed` |
| Retry re-executes only the task, not its deps | `test_retry_reexecutes_only_the_failing_task_not_its_deps` |
| Failed task ⇒ transitive dependents `SKIPPED`, unrelated tasks still run | `test_transitive_dependents_are_skipped_unrelated_still_runs`, `test_diamond_any_failed_dep_causes_skip` |
| Cycle ⇒ `CycleError` naming the cycle, before running anything | `test_cycle_raises_before_running_anything`, `test_self_dependency_is_a_cycle` |
| Cancellation: no new tasks start, in-flight tasks cancelled+awaited before propagating | `test_cancel_stops_new_tasks_and_awaits_inflight`, `test_cancel_while_blocked_on_concurrency_prevents_start` |
| `TaskResult` carries status / attempts / exception | asserted in every test above |

## Known limitations / assumptions

- Unknown dependency names raise `ValueError` — not in the spec, but the only
  sane behavior short of an obscure `KeyError`.
- `concurrency <= 0` is not guarded against; `asyncio.Semaphore(0)` would
  simply deadlock every task forever. Not specified as a required behavior,
  so left unhandled rather than adding speculative validation.
- Cycle detection reports the *first* cycle found (via DFS start-node
  iteration order over the dict), not all cycles in the graph — sufficient
  per the spec ("raise CycleError naming the tasks in a cycle").
