# P2 — Async DAG Task Runner — Solution

## Design notes

- **Graph validation before anything runs.** `_find_cycle` does a classic
  3-color (white/gray/black) DFS over the `tasks` dict. It runs synchronously
  at the top of `run_tasks`, before a single `asyncio.create_task` is issued,
  so `CycleError` is guaranteed to fire before any task function executes.
  As a side effect it also catches a dependency name that doesn't exist in
  `tasks` (raises `ValueError`) — not asked for explicitly, but needed so a
  malformed graph fails fast instead of hanging on an `Event` that is never
  set.
- **Per-task coroutine + `asyncio.Event` for "done".** Each task gets one
  `asyncio.Event` that is set exactly once, when that task reaches a
  terminal state (`SUCCEEDED` / `FAILED` / `SKIPPED`), with `results[name]`
  written *before* the event is set. Downstream tasks `await` their deps'
  events before looking at `results`. Because asyncio is single-threaded and
  cooperative, there is no race between the write and the wake-up.
- **Skip propagates transitively "for free".** A task only inspects its own
  *direct* deps' final status. If a dep is `SKIPPED` (itself already the
  product of *its* deps being skipped/failed), this task also becomes
  `SKIPPED`. Chaining that one-hop rule task-by-task reproduces full
  transitive skip propagation without any separate graph walk.
- **Concurrency gate wraps only actual execution.** The `asyncio.Semaphore`
  is acquired only once a task's deps have all resolved successfully — the
  wait-for-deps phase does not hold a slot. The semaphore is held across all
  retry attempts of one task (retries of the same task are still "one task
  function executing", so this is the natural reading of "at most
  `concurrency` executing at any instant").
- **Retries.** A plain `while attempts < max_retries + 1` loop; `except
  asyncio.CancelledError: raise` comes *before* the catch-all
  `except BaseException`, so cancellation is never mistaken for a retryable
  failure. Only the failing task's function is re-invoked — its deps are
  never touched again.
- **Cancellation.** `run_tasks` itself only ever suspends at
  `await asyncio.gather(*runners.values())`. If the caller cancels the
  `run_tasks` call/task, that `CancelledError` is delivered at the gather
  await; `asyncio.gather`'s documented behavior is to cancel all
  not-yet-done children when the gather itself is cancelled, and it does not
  resolve (re-raise) until every child has actually finished — so "in-flight
  tasks are cancelled and awaited before the cancellation propagates" holds
  even before adding anything extra. The code still does an explicit
  `t.cancel()` loop plus a `return_exceptions=True` re-`gather` in the
  `except` block, as a defensive belt-and-suspenders measure (harmless
  no-op in the common case, but guards the invariant even if gather's
  internals ever change). No new task ever starts after cancellation because
  every not-yet-running `run_one` is still suspended on either a dependency
  `Event.wait()` or `Semaphore.acquire()`, both of which are cancelled
  cleanly by asyncio without side effects.
- **Known limitation:** cycle detection is a recursive DFS, so extremely
  deep graphs (thousands of chained deps) could hit Python's recursion
  limit. Fine for realistic task graphs; would need an explicit stack to be
  fully general.

## Implementation (`task_runner.py`)

```python
"""Async DAG task runner (stdlib-only, Python 3.11)."""

from __future__ import annotations

import asyncio
import dataclasses
import enum
from typing import Awaitable, Callable


class Status(enum.Enum):
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    SKIPPED = "SKIPPED"


@dataclasses.dataclass
class TaskResult:
    status: Status
    attempts: int
    exception: BaseException | None = None


class CycleError(Exception):
    """Raised when the task dependency graph contains a cycle."""

    def __init__(self, cycle: list[str]):
        self.cycle = cycle
        super().__init__(
            "Dependency cycle detected: " + " -> ".join(cycle)
        )


def _find_cycle(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]]
) -> list[str] | None:
    """3-color DFS. Returns a list of task names forming a cycle (first
    and last entries equal), or None if the graph is acyclic. Also raises
    ValueError if a task lists a dependency that doesn't exist."""

    WHITE, GRAY, BLACK = 0, 1, 2
    color = {name: WHITE for name in tasks}
    stack: list[str] = []

    def visit(name: str) -> list[str] | None:
        color[name] = GRAY
        stack.append(name)
        deps, _fn = tasks[name]
        for dep in deps:
            if dep not in tasks:
                raise ValueError(
                    f"task {name!r} depends on unknown task {dep!r}"
                )
            if color[dep] == WHITE:
                found = visit(dep)
                if found is not None:
                    return found
            elif color[dep] == GRAY:
                idx = stack.index(dep)
                return stack[idx:] + [dep]
        stack.pop()
        color[name] = BLACK
        return None

    for name in tasks:
        if color[name] == WHITE:
            found = visit(name)
            if found is not None:
                return found
    return None


def _validate_graph(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]]
) -> None:
    cycle = _find_cycle(tasks)
    if cycle is not None:
        raise CycleError(cycle)


async def run_tasks(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]],
    concurrency: int,
    max_retries: int,
) -> dict[str, TaskResult]:
    """Run `tasks` respecting dependency order, with at most `concurrency`
    task functions executing at once, retrying a failing task function up
    to `max_retries` times, and skipping transitive dependents of a task
    that ultimately fails."""

    if concurrency < 1:
        raise ValueError("concurrency must be >= 1")
    if max_retries < 0:
        raise ValueError("max_retries must be >= 0")

    # Validate before starting anything: no create_task call happens above
    # this line, so a CycleError/ValueError here guarantees nothing ran.
    _validate_graph(tasks)

    results: dict[str, TaskResult] = {}
    done: dict[str, asyncio.Event] = {name: asyncio.Event() for name in tasks}
    sem = asyncio.Semaphore(concurrency)

    async def run_one(
        name: str, deps: list[str], fn: Callable[[], Awaitable]
    ) -> None:
        # Wait for every dependency to reach a terminal state. This await
        # is what makes the task cancellable before it ever starts running.
        for dep in deps:
            await done[dep].wait()

        if any(results[dep].status != Status.SUCCEEDED for dep in deps):
            results[name] = TaskResult(Status.SKIPPED, 0, None)
            done[name].set()
            return

        attempts = 0
        last_exc: BaseException | None = None
        async with sem:
            while attempts < max_retries + 1:
                attempts += 1
                try:
                    await fn()
                except asyncio.CancelledError:
                    # Never treat cancellation as a retryable failure.
                    raise
                except BaseException as exc:
                    last_exc = exc
                    continue
                else:
                    results[name] = TaskResult(Status.SUCCEEDED, attempts, None)
                    done[name].set()
                    return

        results[name] = TaskResult(Status.FAILED, attempts, last_exc)
        done[name].set()

    runners = {
        name: asyncio.create_task(run_one(name, deps, fn), name=name)
        for name, (deps, fn) in tasks.items()
    }

    try:
        await asyncio.gather(*runners.values())
    except asyncio.CancelledError:
        # Belt-and-suspenders: gather() already cancels not-yet-done
        # children when the gather itself is cancelled, and does not
        # resolve until they finish. This makes that explicit and robust
        # to any change in gather's internals.
        for t in runners.values():
            t.cancel()
        await asyncio.gather(*runners.values(), return_exceptions=True)
        raise

    return results
```

## Tests (`test_task_runner.py`)

```python
"""Tests for task_runner.run_tasks.

Run with:
    python -m unittest test_task_runner.py -v
"""

import asyncio
import unittest

from task_runner import CycleError, Status, run_tasks


class TestBasicOrderingAndConcurrency(unittest.IsolatedAsyncioTestCase):
    async def test_dependency_order_is_respected(self):
        order = []

        async def make(name, delay=0.01):
            async def fn():
                await asyncio.sleep(delay)
                order.append(name)
            return fn

        tasks = {
            "a": ([], await make("a")),
            "b": ([], await make("b")),
            "c": (["a", "b"], await make("c")),
            "d": (["c"], await make("d")),
        }

        results = await run_tasks(tasks, concurrency=3, max_retries=0)

        for name in tasks:
            self.assertEqual(results[name].status, Status.SUCCEEDED)
            self.assertEqual(results[name].attempts, 1)

        self.assertLess(order.index("a"), order.index("c"))
        self.assertLess(order.index("b"), order.index("c"))
        self.assertLess(order.index("c"), order.index("d"))

    async def test_concurrency_limit_is_enforced(self):
        active = 0
        max_active = 0
        lock = asyncio.Lock()

        async def make():
            async def fn():
                nonlocal active, max_active
                async with lock:
                    active += 1
                    max_active = max(max_active, active)
                await asyncio.sleep(0.03)
                async with lock:
                    active -= 1
            return fn

        tasks = {f"t{i}": ([], await make()) for i in range(8)}

        results = await run_tasks(tasks, concurrency=3, max_retries=0)

        self.assertTrue(all(r.status == Status.SUCCEEDED for r in results.values()))
        self.assertLessEqual(max_active, 3)
        self.assertGreater(max_active, 1)  # sanity: parallelism actually happened


class TestRetries(unittest.IsolatedAsyncioTestCase):
    async def test_retry_then_succeed(self):
        calls = {"n": 0}

        async def flaky():
            calls["n"] += 1
            if calls["n"] < 3:
                raise RuntimeError(f"attempt {calls['n']} failed")

        tasks = {"x": ([], flaky)}

        results = await run_tasks(tasks, concurrency=1, max_retries=5)

        self.assertEqual(results["x"].status, Status.SUCCEEDED)
        self.assertEqual(results["x"].attempts, 3)
        self.assertIsNone(results["x"].exception)

    async def test_retry_exhausted_marks_failed(self):
        calls = {"n": 0}

        async def always_fails():
            calls["n"] += 1
            raise ValueError("nope")

        tasks = {"x": ([], always_fails)}

        results = await run_tasks(tasks, concurrency=1, max_retries=2)

        self.assertEqual(results["x"].status, Status.FAILED)
        self.assertEqual(results["x"].attempts, 3)  # max_retries + 1
        self.assertEqual(calls["n"], 3)
        self.assertIsInstance(results["x"].exception, ValueError)

    async def test_retry_only_reruns_the_failing_task_not_its_deps(self):
        dep_calls = {"n": 0}
        target_calls = {"n": 0}

        async def dep():
            dep_calls["n"] += 1

        async def target():
            target_calls["n"] += 1
            if target_calls["n"] < 2:
                raise RuntimeError("first attempt fails")

        tasks = {
            "dep": ([], dep),
            "target": (["dep"], target),
        }

        results = await run_tasks(tasks, concurrency=1, max_retries=3)

        self.assertEqual(results["target"].status, Status.SUCCEEDED)
        self.assertEqual(dep_calls["n"], 1)     # dep ran exactly once
        self.assertEqual(target_calls["n"], 2)  # target retried once, dep not rerun


class TestFailurePropagation(unittest.IsolatedAsyncioTestCase):
    async def test_failed_task_skips_transitive_dependents_but_not_unrelated(self):
        executed = []

        async def root_fails():
            executed.append("root")
            raise ValueError("boom")

        async def child():
            executed.append("child")

        async def grandchild():
            executed.append("grandchild")

        async def unrelated():
            executed.append("unrelated")

        tasks = {
            "root": ([], root_fails),
            "child": (["root"], child),
            "grandchild": (["child"], grandchild),
            "unrelated": ([], unrelated),
        }

        results = await run_tasks(tasks, concurrency=2, max_retries=1)

        self.assertEqual(results["root"].status, Status.FAILED)
        self.assertEqual(results["root"].attempts, 2)  # max_retries + 1
        self.assertIsInstance(results["root"].exception, ValueError)

        self.assertEqual(results["child"].status, Status.SKIPPED)
        self.assertEqual(results["child"].attempts, 0)
        self.assertIsNone(results["child"].exception)

        self.assertEqual(results["grandchild"].status, Status.SKIPPED)
        self.assertEqual(results["grandchild"].attempts, 0)

        self.assertEqual(results["unrelated"].status, Status.SUCCEEDED)

        self.assertNotIn("child", executed)
        self.assertNotIn("grandchild", executed)
        self.assertIn("unrelated", executed)
        self.assertEqual(executed.count("root"), 2)  # ran once per attempt


class TestCycleDetection(unittest.IsolatedAsyncioTestCase):
    async def test_direct_cycle_raises_before_running_anything(self):
        executed = []

        async def fn_a():
            executed.append("a")

        async def fn_b():
            executed.append("b")

        tasks = {
            "a": (["b"], fn_a),
            "b": (["a"], fn_b),
        }

        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(executed, [])
        self.assertIn("a", ctx.exception.cycle)
        self.assertIn("b", ctx.exception.cycle)

    async def test_indirect_cycle_among_larger_graph(self):
        executed = []

        async def noop():
            executed.append("ran")

        tasks = {
            "start": ([], noop),
            "a": (["start", "c"], noop),
            "b": (["a"], noop),
            "c": (["b"], noop),
        }

        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(executed, [])
        for name in ("a", "b", "c"):
            self.assertIn(name, ctx.exception.cycle)


class TestCancellation(unittest.IsolatedAsyncioTestCase):
    async def test_cancel_awaits_inflight_and_prevents_new_starts(self):
        started = []
        finished = []
        cancelled = []

        async def make(name, delay):
            async def fn():
                started.append(name)
                try:
                    await asyncio.sleep(delay)
                    finished.append(name)
                except asyncio.CancelledError:
                    cancelled.append(name)
                    raise
            return fn

        tasks = {
            "long1": ([], await make("long1", 1.0)),
            "long2": ([], await make("long2", 1.0)),
            "late": ([], await make("late", 0.01)),  # blocked, concurrency=2
        }

        outer = asyncio.create_task(run_tasks(tasks, concurrency=2, max_retries=0))
        await asyncio.sleep(0.05)  # let long1/long2 grab both slots and start
        outer.cancel()

        with self.assertRaises(asyncio.CancelledError):
            await outer

        self.assertIn("long1", started)
        self.assertIn("long2", started)
        self.assertNotIn("late", started)  # never got a concurrency slot

        self.assertIn("long1", cancelled)
        self.assertIn("long2", cancelled)
        self.assertNotIn("long1", finished)
        self.assertNotIn("long2", finished)


if __name__ == "__main__":
    unittest.main()
```

## How to run

```bash
python -m unittest test_task_runner.py -v
```

(Requires `task_runner.py` and `test_task_runner.py` in the same directory,
Python 3.11, stdlib only.)

## Behaviors covered by the tests

| Requirement | Test |
|---|---|
| Deps run before dependents; concurrency cap respected | `test_dependency_order_is_respected`, `test_concurrency_limit_is_enforced` |
| Retry re-executes only the failing task, up to `max_retries` extra attempts | `test_retry_then_succeed`, `test_retry_exhausted_marks_failed`, `test_retry_only_reruns_the_failing_task_not_its_deps` |
| Ultimate failure marks transitive dependents `SKIPPED`; unrelated tasks still complete | `test_failed_task_skips_transitive_dependents_but_not_unrelated` |
| Cycle raises `CycleError` naming the cycle, before anything runs | `test_direct_cycle_raises_before_running_anything`, `test_indirect_cycle_among_larger_graph` |
| Cancelling `run_tasks` stops new starts and awaits in-flight tasks before propagating | `test_cancel_awaits_inflight_and_prevents_new_starts` |
