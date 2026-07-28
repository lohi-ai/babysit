# P2 — Async DAG task runner

Python 3.11, stdlib `asyncio` only. No code was executed — correctness is
argued by careful trace-through below the listings.

## Design decisions (read before the code)

1. **Cycle check is a separate, fully synchronous, iterative 3-color DFS**
   (`_check_for_cycles`), run as the *first* statement of `run_tasks`, before
   any `asyncio` construct (semaphore, events, tasks) is even created. This
   guarantees "raise `CycleError` … before running anything" — there is no
   `await` before it, so if it raises, the coroutine never yields to the
   event loop at all. It's iterative (explicit stack), not recursive, so it
   doesn't blow the recursion limit on large graphs. As a bonus it also
   catches a dependency name that isn't a key in `tasks` (raises `ValueError`)
   — not explicitly asked for, but the alternative is a silent deadlock
   (waiting forever on an `asyncio.Event` that nothing ever sets), which is
   worse.

2. **All task coroutines are scheduled up front** (one `asyncio.Task` per
   entry in `tasks`, all created inside a single `asyncio.TaskGroup` before
   any of them is awaited elsewhere). Each one immediately does
   `await done[dep].wait()` for its own deps. This is what makes "at most
   `concurrency` may be *executing*" correct: waiting on a dependency event
   costs nothing (no semaphore held), so an arbitrary number of tasks can be
   "pending on deps" at once — the semaphore only gates the actual `fn()`
   call.

3. **Skip propagation falls out for free.** A task becomes `SKIPPED` iff any
   *direct* dependency's terminal status isn't `SUCCEEDED`. Since a skipped
   task's own status is exactly the thing its dependents check, a failure
   three levels down naturally cascades: fail → skip → skip → skip, without
   any extra transitive-closure computation.

4. **Retries hold the semaphore slot for the task's entire lifetime**,
   including all retry attempts — a retry is still "that task executing", so
   it shouldn't release its concurrency slot to a sibling between attempts.
   `async with semaphore:` wraps the whole `while` retry loop, not each
   individual attempt.

5. **`except Exception`, not `except BaseException`**, gates retry/failure.
   `asyncio.CancelledError` is a `BaseException` in 3.8+, so it is *not*
   caught here — it propagates immediately out of `run_one`, which is exactly
   "cooperative cancellation must not be swallowed or retried". A rogue
   `SystemExit`/`KeyboardInterrupt` from inside a task fn also isn't caught
   (deliberate — those shouldn't be silently retried either); it would
   surface as a `BaseExceptionGroup` from the `TaskGroup`, which is
   acceptable/expected fallout for something that severe.

6. **Cancellation relies on documented `asyncio.TaskGroup` semantics**: if
   the task awaiting inside `async with TaskGroup()` is itself cancelled
   (i.e. the caller cancels the `run_tasks(...)` task), `TaskGroup.__aexit__`
   cancels every still-running child task, awaits all of them to finish
   unwinding, and then re-raises the *original* `CancelledError` (child
   `CancelledError`s from the cancelled-because-you-cancelled-them tasks are
   consumed internally by the group, not wrapped into an
   `ExceptionGroup`, since none of our `run_one` bodies ever produce a
   genuine non-cancellation exception — they all catch `Exception`
   internally). This gives exactly: no new task starts (any task still
   parked on `done[dep].wait()` or the semaphore gets `CancelledError`
   injected there and never reaches `fn()`), in-flight tasks are cancelled
   and awaited, and then cancellation propagates out of `run_tasks` as a
   plain `CancelledError`.

7. **On cancellation, `run_tasks` does not return a partial `results` dict**
   — it raises, per the "propagates" wording. This is a deliberate
   simplification; exposing partial results would need an out-parameter or a
   different return contract that the spec doesn't ask for.

---

## `taskrunner.py`

```python
"""Async DAG task runner (Python 3.11, stdlib asyncio only)."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from enum import Enum
from typing import Awaitable, Callable

TaskSpec = "dict[str, tuple[list[str], Callable[[], Awaitable]]]"


class Status(Enum):
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    SKIPPED = "SKIPPED"


@dataclass
class TaskResult:
    status: Status
    attempts: int
    exception: BaseException | None = None


class CycleError(Exception):
    """Raised when the task dependency graph contains a cycle."""

    def __init__(self, cycle: list[str]) -> None:
        self.cycle = cycle
        super().__init__(f"dependency cycle detected: {' -> '.join(cycle)}")


def _check_for_cycles(tasks: "dict[str, tuple[list, Callable]]") -> None:
    """Iterative 3-color DFS cycle detector.

    Raises CycleError naming an actual cycle, or ValueError if a task lists
    a dependency that isn't a key of `tasks`. Pure / synchronous / no side
    effects on the outside world.
    """
    WHITE, GRAY, BLACK = 0, 1, 2
    color = {name: WHITE for name in tasks}

    for start in tasks:
        if color[start] != WHITE:
            continue

        stack = [(start, iter(tasks[start][0]))]
        path = [start]
        color[start] = GRAY

        while stack:
            node, dep_iter = stack[-1]
            for dep in dep_iter:
                if dep not in tasks:
                    raise ValueError(
                        f"task {node!r} depends on unknown task {dep!r}"
                    )
                if color[dep] == WHITE:
                    color[dep] = GRAY
                    stack.append((dep, iter(tasks[dep][0])))
                    path.append(dep)
                    break
                if color[dep] == GRAY:
                    idx = path.index(dep)
                    raise CycleError(path[idx:] + [dep])
                # color[dep] == BLACK: fully explored via another branch,
                # not part of a cycle through here — keep scanning dep_iter.
            else:
                # for-loop exhausted with no break => all deps resolved.
                color[node] = BLACK
                stack.pop()
                path.pop()


async def run_tasks(
    tasks: "dict[str, tuple[list[str], Callable[[], Awaitable]]]",
    concurrency: int,
    max_retries: int,
) -> dict[str, TaskResult]:
    if concurrency < 1:
        raise ValueError("concurrency must be >= 1")
    if max_retries < 0:
        raise ValueError("max_retries must be >= 0")

    # Synchronous, no `await` yet: guarantees this raises before anything runs.
    _check_for_cycles(tasks)

    results: dict[str, TaskResult] = {}
    done: dict[str, asyncio.Event] = {name: asyncio.Event() for name in tasks}
    semaphore = asyncio.Semaphore(concurrency)

    async def run_one(name: str) -> None:
        deps, fn = tasks[name]

        # Wait for every dependency to reach a terminal state. If we get
        # cancelled here, we simply never proceed to run fn() at all.
        for dep in deps:
            await done[dep].wait()

        if any(results[dep].status is not Status.SUCCEEDED for dep in deps):
            results[name] = TaskResult(Status.SKIPPED, attempts=0)
            done[name].set()
            return

        attempts = 0
        last_exc: BaseException | None = None

        async with semaphore:  # held across all retry attempts
            while attempts < max_retries + 1:
                attempts += 1
                try:
                    await fn()
                except Exception as exc:
                    # NB: asyncio.CancelledError is a BaseException, not an
                    # Exception, so a cancellation is never caught/retried
                    # here — it propagates straight out of run_one.
                    last_exc = exc
                    continue
                else:
                    results[name] = TaskResult(Status.SUCCEEDED, attempts)
                    done[name].set()
                    return

        results[name] = TaskResult(Status.FAILED, attempts, last_exc)
        done[name].set()

    async with asyncio.TaskGroup() as tg:
        for name in tasks:
            tg.create_task(run_one(name), name=name)
        # __aexit__ awaits every child task. If *this* coroutine is
        # cancelled while suspended here, TaskGroup cancels every
        # still-running child, awaits them, then re-raises the
        # CancelledError — no ExceptionGroup wrapping, since none of our
        # run_one bodies leak a non-cancellation exception.

    return results
```

---

## `test_taskrunner.py`

```python
"""Tests for taskrunner.run_tasks. Run with: python -m unittest -v"""

import asyncio
import unittest

from taskrunner import CycleError, Status, run_tasks


def make_recording_fn(name, log):
    async def fn():
        log.append(name)
    return fn


class TestBasicDAG(unittest.IsolatedAsyncioTestCase):
    async def test_deps_run_before_dependents_and_all_succeed(self):
        order = []
        tasks = {
            "a": ([], make_recording_fn("a", order)),
            "b": (["a"], make_recording_fn("b", order)),
            "c": (["a"], make_recording_fn("c", order)),
            "d": (["b", "c"], make_recording_fn("d", order)),
        }

        results = await run_tasks(tasks, concurrency=3, max_retries=0)

        self.assertTrue(all(r.status == Status.SUCCEEDED for r in results.values()))
        self.assertEqual(order[0], "a")
        self.assertEqual(order[-1], "d")
        self.assertLess(order.index("a"), order.index("b"))
        self.assertLess(order.index("a"), order.index("c"))
        self.assertLess(order.index("b"), order.index("d"))
        self.assertLess(order.index("c"), order.index("d"))

    async def test_empty_graph(self):
        self.assertEqual(await run_tasks({}, concurrency=1, max_retries=0), {})


class TestConcurrencyLimit(unittest.IsolatedAsyncioTestCase):
    async def test_concurrency_is_bounded_and_reached(self):
        current = [0]
        max_seen = [0]

        def make_fn():
            async def fn():
                current[0] += 1
                max_seen[0] = max(max_seen[0], current[0])
                await asyncio.sleep(0.02)
                current[0] -= 1
            return fn

        tasks = {f"t{i}": ([], make_fn()) for i in range(6)}
        results = await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertTrue(all(r.status == Status.SUCCEEDED for r in results.values()))
        self.assertLessEqual(max_seen[0], 2)
        self.assertEqual(max_seen[0], 2)  # 6 equal-length independent tasks
                                          # must actually reach the cap once


class TestRetries(unittest.IsolatedAsyncioTestCase):
    async def test_retry_then_succeed(self):
        attempts = {"n": 0}

        async def flaky():
            attempts["n"] += 1
            if attempts["n"] < 3:
                raise RuntimeError(f"fail #{attempts['n']}")

        results = await run_tasks({"t": ([], flaky)}, concurrency=1, max_retries=5)
        r = results["t"]

        self.assertEqual(r.status, Status.SUCCEEDED)
        self.assertEqual(r.attempts, 3)
        self.assertIsNone(r.exception)

    async def test_retry_exhausted_reports_failure_and_final_exception(self):
        async def always_fails():
            raise ValueError("nope")

        results = await run_tasks(
            {"t": ([], always_fails)}, concurrency=1, max_retries=2
        )
        r = results["t"]

        self.assertEqual(r.status, Status.FAILED)
        self.assertEqual(r.attempts, 3)  # max_retries(2) + 1
        self.assertIsInstance(r.exception, ValueError)

    async def test_retry_does_not_reexecute_deps(self):
        dep_calls = []
        attempts = {"n": 0}

        async def dep():
            dep_calls.append("dep")

        async def flaky():
            attempts["n"] += 1
            if attempts["n"] < 2:
                raise RuntimeError("first attempt fails")

        tasks = {"dep": ([], dep), "t": (["dep"], flaky)}
        results = await run_tasks(tasks, concurrency=1, max_retries=3)

        self.assertEqual(results["t"].status, Status.SUCCEEDED)
        self.assertEqual(results["t"].attempts, 2)
        self.assertEqual(dep_calls, ["dep"])  # dep executed exactly once


class TestSkipPropagation(unittest.IsolatedAsyncioTestCase):
    async def test_failure_skips_transitive_dependents_unrelated_still_runs(self):
        ran = []

        async def fail_a():
            ran.append("a")
            raise RuntimeError("a failed")

        tasks = {
            "a": ([], fail_a),
            "b": (["a"], make_recording_fn("b", ran)),
            "c": (["b"], make_recording_fn("c", ran)),
            "d": ([], make_recording_fn("d", ran)),  # unrelated
        }

        results = await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(results["a"].status, Status.FAILED)
        self.assertEqual(results["a"].attempts, 1)
        self.assertIsInstance(results["a"].exception, RuntimeError)

        self.assertEqual(results["b"].status, Status.SKIPPED)
        self.assertEqual(results["b"].attempts, 0)
        self.assertIsNone(results["b"].exception)

        self.assertEqual(results["c"].status, Status.SKIPPED)
        self.assertEqual(results["c"].attempts, 0)

        self.assertEqual(results["d"].status, Status.SUCCEEDED)

        self.assertEqual(ran, ["a", "d"])  # b and c never executed


class TestCycleDetection(unittest.IsolatedAsyncioTestCase):
    async def test_cycle_raises_before_anything_runs(self):
        ran = []
        tasks = {
            "x": ([], make_recording_fn("x", ran)),   # unrelated to the cycle
            "a": (["c"], make_recording_fn("a", ran)),
            "b": (["a"], make_recording_fn("b", ran)),
            "c": (["b"], make_recording_fn("c", ran)),  # a -> c -> b -> a
        }

        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)

        self.assertEqual(ctx.exception.cycle, ["a", "c", "b", "a"])
        self.assertEqual(ran, [])  # nothing executed, including unrelated "x"

    async def test_self_dependency_is_a_cycle(self):
        tasks = {"a": (["a"], make_recording_fn("a", []))}
        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=1, max_retries=0)
        self.assertEqual(ctx.exception.cycle, ["a", "a"])

    async def test_unknown_dependency_raises_value_error(self):
        tasks = {"a": (["ghost"], make_recording_fn("a", []))}
        with self.assertRaises(ValueError):
            await run_tasks(tasks, concurrency=1, max_retries=0)


class TestCancellation(unittest.IsolatedAsyncioTestCase):
    async def test_cancel_stops_new_tasks_and_awaits_inflight(self):
        events = []
        started = asyncio.Event()
        release = asyncio.Event()  # never set -> "blocking" hangs until cancelled

        async def blocking():
            events.append("blocking:start")
            started.set()
            try:
                await release.wait()
            except asyncio.CancelledError:
                events.append("blocking:cancelled")
                raise
            events.append("blocking:done")  # must never be reached

        async def should_never_run():
            events.append("waiter:ran")

        tasks = {
            "blocking": ([], blocking),
            # concurrency=1 means "waiter" can't even acquire the semaphore
            # while "blocking" holds it, on top of depending on it directly.
            "waiter": (["blocking"], should_never_run),
        }

        run_task = asyncio.create_task(run_tasks(tasks, concurrency=1, max_retries=0))

        await started.wait()  # "blocking" is now in-flight, holding the slot
        run_task.cancel()

        with self.assertRaises(asyncio.CancelledError):
            await run_task

        self.assertIn("blocking:start", events)
        self.assertIn("blocking:cancelled", events)
        self.assertNotIn("blocking:done", events)
        self.assertNotIn("waiter:ran", events)  # new task never started


if __name__ == "__main__":
    unittest.main()
```

---

## Trace-throughs worth calling out explicitly

- **`test_cycle_raises_before_anything_runs`**: with `tasks` inserted in
  order `x, a, b, c`, `_check_for_cycles` visits `x` first (no deps, goes
  straight to `BLACK`), then starts DFS from `a`: `a → c → b → a`. `path`
  grows to `["a", "c", "b"]`; hitting `a` again while `a` is still `GRAY`
  yields `path[0:] + ["a"] == ["a", "c", "b", "a"]`. `x`'s recording fn is
  never invoked because the whole check is synchronous and precedes the
  `TaskGroup`.

- **`test_cancel_stops_new_tasks_and_awaits_inflight`**: after
  `tg.create_task` is called for both `blocking` and `waiter` (synchronously,
  no `await` in between), `run_tasks` reaches the end of the `async with`
  block and suspends inside `TaskGroup.__aexit__`, awaiting its children.
  Both child tasks then run: `blocking` acquires the uncontended semaphore
  and suspends on `release.wait()`; `waiter` suspends on
  `done["blocking"].wait()`. Once `started.wait()` unblocks the test and
  `run_task.cancel()` fires, the cancellation is delivered to `run_tasks`
  at that exact `__aexit__` suspension point, which is where
  `asyncio.TaskGroup` cancels every outstanding child: `blocking`'s
  `release.wait()` raises `CancelledError`, caught and re-raised by the
  `try/except` in the test's `blocking()`, so `"blocking:cancelled"` is
  logged; `waiter`'s `done["blocking"].wait()` raises `CancelledError` too,
  and since `run_one` has no handler around the dependency-wait loop, it
  propagates immediately — `should_never_run` (the task fn) is never called.
  `TaskGroup.__aexit__` awaits both before re-raising the original
  `CancelledError`, which is what `await run_task` surfaces.

## How to run

```
python -m unittest test_taskrunner -v
```

(Requires Python ≥ 3.11 for `asyncio.TaskGroup`, matching the problem's
target version.)
