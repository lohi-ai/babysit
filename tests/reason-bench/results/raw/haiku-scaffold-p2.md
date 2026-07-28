# P2 — Async DAG task runner: scaffolded answer

## 1. Frame

Restatement: implement `run_tasks(tasks, concurrency, max_retries) -> dict[str, TaskResult]`
in Python 3.11 stdlib (`asyncio` only) that:

- Runs each named task's `fn()` only after every task in its `deps` list has
  reached `SUCCEEDED`.
- Never has more than `concurrency` `fn()` bodies executing simultaneously.
- Retries a raising task up to `max_retries` times (≤ `max_retries + 1` total
  calls to that task's `fn()`), re-executing only `fn()`, never its deps.
- Marks every transitive dependent of an ultimately-failed task `SKIPPED`
  without calling its `fn()`, while tasks with no path to the failure still
  run to completion.
- Detects a dependency cycle and raises `CycleError` naming the cycle's
  tasks *before invoking any `fn()`*.
- On cancellation of `run_tasks` itself: no task not already executing may
  start `fn()`, all currently-executing `fn()` calls are cancelled, and the
  cancellation is not re-raised until those in-flight calls have finished
  unwinding.
- Returns one `TaskResult{status, attempts, exception}` per task name.

Checkable success criteria:

1. Given no cycle, every returned `TaskResult.status` is consistent with the
   dependency graph (SUCCEEDED only if fn() returned; SKIPPED only if ≥1 dep
   is not SUCCEEDED; FAILED only after `max_retries+1` raising attempts).
2. At every instant, the number of tasks inside their `fn()` call is
   `≤ concurrency` (hand-traceable via a shared in/out counter).
3. `attempts` equals the number of times `fn()` was actually invoked for that
   task (0 for SKIPPED).
4. A graph with a cycle raises `CycleError` whose payload names ⊇ the actual
   cycle members, and no task's `fn()` is ever called in that run.
5. Cancelling the coroutine driving `run_tasks` raises `asyncio.CancelledError`
   out of it only after every in-flight `fn()` has been cancelled and
   awaited; no new `fn()` starts after the cancel is delivered.

Out of scope (not asked for, not built): retry backoff/delay, priority
scheduling among simultaneously-ready tasks, timeouts, persistence/resume,
validation that `deps` names all exist in `tasks`, fairness guarantees beyond
"respect the concurrency bound."

Two materially different readings I had to pick between (stating both, not
picking silently):

- **(a) What does "concurrency" bound?** Reading A (chosen): only the actual
  `fn()` call counts toward the limit — waiting on deps or on the semaphore
  doesn't occupy a slot. Reading B: any task that has "started" (its asyncio
  Task object exists and is not finished) counts. I chose A because the spec
  says "task functions may be executing," which is the `fn()` body
  specifically, not bookkeeping around it.
- **(b) Skip condition with multiple deps.** Reading A (chosen): a task is
  skipped if *any* dep is not `SUCCEEDED` (mirrors "runs only after **all**
  deps have succeeded" — one non-success already breaks that precondition).
  Reading B: only skip if *all* deps failed. I chose A — it's the direct
  contrapositive of the stated run condition.

## 2. Gather

**Facts** (from the task text):
- Signature and types are given verbatim (`tasks`, `concurrency`,
  `max_retries` → `dict[str, TaskResult]`).
- Total attempts on a failing task = `max_retries + 1` (explicit).
- Retries re-run only the task, not its deps (explicit).
- Cycle → `CycleError` naming the cycle, raised before anything runs
  (explicit).
- Cancellation must be cooperative: no new starts, in-flight tasks cancelled
  *and awaited* before the cancellation propagates (explicit).
- Stdlib only, `asyncio` (explicit); Python 3.11 (explicit, no 3.12+ features
  like `TaskGroup`'s newer semantics assumed).

**Domain facts about `asyncio` I'm relying on (cited from documented
behavior, not guessed):**
- `asyncio.create_task(coro)` schedules the coroutine but does not run any of
  its body synchronously; the coroutine only starts executing at the next
  point the caller yields control (its own next `await`). Consequently, a
  loop/comprehension that calls `create_task` repeatedly with no `await`
  inside it completes in full, with every `Task` object stored, before any
  of those new tasks run a single line.
- An `asyncio.Task` can be awaited by multiple different coroutines; each
  await after the first returns the cached result (or re-raises the cached
  exception) without re-running the coroutine.
- `except Exception` does not catch `asyncio.CancelledError` (it derives from
  `BaseException`, not `Exception`, since Python 3.8), so a bare `except
  Exception:` retry-catch is automatically cancellation-safe without special
  casing — I add an explicit `except asyncio.CancelledError: raise` anyway,
  purely for readability/auditability, not because it changes behavior.
- `asyncio.Semaphore` is the standard stdlib primitive for "at most N
  concurrent holders of a resource," and `acquire()` is itself a cancellable
  await point.

**Assumptions (uncited, carried into the design/output explicitly, not
absorbed into narrative):**
- A1: All names in every `deps` list are keys of `tasks` (well-formed input).
  Not validated beyond what cycle detection incidentally tolerates; an
  unknown dep name would surface as a `KeyError` at schedule time, which is
  undefined-but-not-silent behavior.
- A2: `concurrency >= 1` and `max_retries >= 0`. `concurrency <= 0` would
  deadlock a `Semaphore` and is treated as caller error, not handled.
- A3: `fn()` raises only `Exception` subclasses in its "this attempt failed"
  case; non-`Exception` `BaseException`s (e.g. a task deliberately raising
  `SystemExit`) are not caught/retried, matching the normal Python convention
  that only `KeyboardInterrupt`/`SystemExit`-style signals stay uncaught by
  broad handlers. This is a real, surfaced edge case — see Attack.
- A4: Retrying a task releases the concurrency slot between attempts (a
  retry re-acquires the semaphore rather than holding it across the whole
  retry loop). The spec doesn't say either way; I pick the version that lets
  other ready tasks use the freed slot between a failing task's attempts.

## 3. Branch

**Candidate 1 — Recursive-await per node.** Every task gets its own
`asyncio.Task` wrapping a coroutine that first `await`s its dependencies'
`Task` objects directly (by name lookup in a dict built before any task
starts), decides SKIPPED vs. run from their statuses, then runs `fn()` under
a `Semaphore(concurrency)` with a retry loop. Scheduling order and "wait for
deps" fall directly out of asyncio awaiting another Task; no manual graph
bookkeeping at runtime (cycle detection is a separate, pre-flight, purely
synchronous pass).

**Candidate 2 — Worker pool + explicit ready queue.** Precompute in-degree
per node and a "dependents" adjacency list. A fixed pool of `concurrency`
worker coroutines pop ready task names off an `asyncio.Queue`, run them
(with retries), then on completion decrement dependents' in-degree (or push
a SKIPPED marker down the subtree), pushing newly-ready names back onto the
queue. Classic manual DAG scheduler, no reliance on "await someone else's
Task" as the coordination mechanism.

**Candidate 3 — Layered topological batches.** Kahn's-algorithm layering:
repeatedly peel off all zero-in-degree nodes as one "generation," run each
generation to completion (`gather`, semaphore-bounded) before starting the
next; cycle detection is a side effect of Kahn's algorithm not fully
draining the graph.

Scoring against Frame's criteria:

- *Correctness of dep-ordering / skip propagation*: 1 and 2 both correct by
  construction; 1 gets it "for free" (just check awaited results), 2 needs
  explicit bookkeeping code for decrementing/propagating skips — more
  surface area for an off-by-one or a forgotten SKIPPED-vs-FAILED branch. 3
  is also correct (a layer only starts once its own prerequisite layers
  are fully resolved) but see efficiency below.
- *Concurrency-bound correctness*: all three are equally correct — each
  bounds via a `Semaphore` (1, 3) or a fixed pool size (2).
- *Cancellation auditability*: 1 has exactly one place (the final
  `gather`) whose cancellation needs handling, making the contract easy to
  reason about; 2 needs to cancel worker loops and drain the queue; 3 needs
  to cancel mid-layer `gather`s specifically.
- *True parallel efficiency* (a scale check, not just correctness): with 3,
  a fast task in layer *N* that depends on only one specific task in layer
  *N-1* still waits for **every** task in layer *N-1* to finish, including
  unrelated stragglers — e.g. 999 short tasks + 1 slow straggler in layer 0,
  and layer-1 tasks that depend only on the 999 short ones still can't start
  until the straggler finishes too. That's a real wasted-concurrency defect
  relative to "runs only after **its** deps have succeeded," even though it
  doesn't violate any literal constraint in the spec.

**Pick: Candidate 1.** One-line why: correct dependency/skip semantics come
for free from asyncio's own Task/await machinery instead of hand-rolled
in-degree bookkeeping, and it reaches full legal parallelism (no artificial
generation barriers) with a single, auditable cancellation seam.

Switch trigger: if this had to be ported to a runtime where a shared future
can't be cheaply awaited by multiple independent waiters (so "await the
sibling's Task" is expensive or impossible), or if the number of tasks were
large enough that materializing one Task object per node up front were a
real memory/scheduling concern, I'd switch to Candidate 2.

## 4. Attack

Trying to break Candidate 1 before committing:

- **Ordering hazard**: does building `node_tasks = {name: create_task(...)
  for name in tasks}` really guarantee the dict is fully populated before any
  node's coroutine body runs and looks up a sibling by name? Re-checked
  against the cited fact above: `create_task` doesn't run any coroutine code
  synchronously, and a dict/comprehension with no `await` inside never
  yields control mid-iteration — so yes, the whole dict exists before the
  event loop gets a chance to run any of the new tasks. Holds.
- **Cycle-before-anything-runs**: `find_cycle()` must be plain synchronous
  code invoked *before* any `create_task` call, not merely "checked
  eventually." Confirmed my implementation puts it first, with no `await` in
  between, so raising `CycleError` there means zero `fn()` calls happened —
  satisfies "before running anything" literally, not just "early."
- **Concrete cycle trace**: `a→[b], b→[c], c→[a]`. DFS from `a`: color(a)=GRAY,
  push; visit dep `b`: WHITE → push, color GRAY; visit dep `c`: WHITE → push,
  color GRAY; visit dep `a`: GRAY → found back-edge, `stack.index("a")==0`,
  return `stack[0:] = ["a","b","c"]`. Matches the actual cycle. Self-loop
  case `x→[x]`: push x (GRAY), visit dep `x`: GRAY, `stack.index("x")==0`,
  returns `["x"]`. Correct.
- **Cancellation trace at production-shape scale**: chain `A→B→C`
  (`B` deps `[A]`, `C` deps `[B]`), concurrency=1, `A.fn` blocks on an event
  that's never set. Cancel the outer task wrapping `run_tasks` while `A` is
  mid-`fn()`. `run_tasks` is suspended at its own final
  `await asyncio.gather(*node_tasks.values())` → CancelledError raised
  there → caught by my `except asyncio.CancelledError` → I explicitly
  `.cancel()` every node task → `A`'s task is cancelled while inside
  `await fn()` (propagates out through the semaphore's `async with`, which
  releases on exit, and out of `run_node` since `except Exception` doesn't
  catch it) → `B` and `C` were never past `await asyncio.gather(node_tasks[dep])`
  waiting on their single dep, so cancelling them there means their `fn()`
  is **never called**, satisfying "no new tasks start" → my second
  `await asyncio.gather(*node_tasks.values(), return_exceptions=True)` waits
  for all three to actually finish unwinding before the outer `raise`
  re-propagates. Every clause of the cancellation requirement checks out by
  hand-trace.
- **The one place I chose not to rely on stdlib-internal cascade behavior**:
  `asyncio.gather` is documented to cascade-cancel its children when the
  `gather` future itself is cancelled, which would make my explicit
  cleanup block redundant in the common case — but relying on that alone
  felt like leaning on a subtlety I couldn't execute-verify, so I kept the
  explicit `except CancelledError: cancel all; await all; raise` block as a
  self-contained guarantee that doesn't depend on getting that nuance
  exactly right. This is a deliberate belt-and-suspenders choice, not
  dead code.
- **Steelman of Candidate 3** (layered batches): it's genuinely easier to
  *convince yourself* is correct by eyeballing it (finish layer, start next
  layer, done), and cycle detection is a one-liner side effect of Kahn's
  algorithm not draining the queue. If the task graph in practice is always
  shallow and roughly balanced (every task in a layer takes similar time),
  the efficiency loss versus Candidate 1 is negligible and the simplicity
  might win. I still reject it here because the spec explicitly cares about
  a `concurrency` knob, which only matters when task durations are
  heterogeneous — exactly the case where layering wastes slots.
- **Surviving objection (not fatal, documented as a real gap)**: A3's
  `except Exception` (not `BaseException`) means a task that raises a
  non-`Exception` `BaseException` (contrived, but legal Python) propagates
  out of that one `node_task` uncaught. If a dependent is awaiting exactly
  that task, the dependent's own `await asyncio.gather(node_tasks[dep])`
  re-raises it too — cascading up rather than being folded into a `FAILED`
  `TaskResult`. I accept this because (a) catching `BaseException` broadly
  is a known anti-pattern (it would also swallow real `KeyboardInterrupt`
  events from unrelated concurrent code sharing the loop), and (b) the spec's
  language ("a task that raises is retried") maps naturally onto the
  `Exception` hierarchy, which is the conventional "user code failure"
  boundary in Python. Documented as a known, deliberate boundary rather than
  an oversight.

No attack sent me back to Branch; the surviving objection above is scoped
and accepted rather than silently absorbed.

## 5. Verify

Check that would fail if wrong: hand-trace each success criterion from
Frame against the implementation and the test suite below; also re-read
Frame's two flagged ambiguous readings and confirm the code matches the
reading I declared.

- Criterion 1 (status consistency): traced in Attack for FAILED→SKIPPED
  cascade (`root` FAILED → `child` SKIPPED → `grandchild` SKIPPED,
  `other` unaffected) — matches reading (b)-A (any non-SUCCEEDED dep skips).
- Criterion 2 (concurrency bound): the semaphore wraps only the `await fn()`
  call, nothing else contends for it — matches reading (a)-A from Frame.
- Criterion 3 (`attempts` count): `attempts` increments once per loop
  iteration, each iteration is exactly one `fn()` call; SKIPPED path returns
  `attempts=0` without ever entering the loop. Verified via the retry trace:
  `max_retries=2` → `range(3)` → 3 possible calls; a task failing twice then
  succeeding reports `attempts=3`, `status=SUCCEEDED` — matches "total
  attempts = max_retries + 1" as the *ceiling*, not a fixed count.
- Criterion 4 (cycle before running): traced above; the moment `CycleError`
  is raised is inside a plain synchronous helper called before any
  `create_task`, so zero `fn()` invocations can have happened — a test
  asserts a shared call-counter is still `0` after catching `CycleError`.
- Criterion 5 (cancellation): fully hand-traced in Attack against a 3-node
  chain with an artificial concurrency=1 bottleneck; every sub-clause
  (no new starts / in-flight cancelled / awaited before propagate) checked
  individually.

Re-reading Frame once more end to end: the two ambiguous readings are both
explicitly stated (not silently picked) and the implementation matches the
declared choice in each case; out-of-scope items (backoff, timeouts,
fairness, dep-name validation) are not built and were declared out of scope
up front, so no silent scope creep. I did not drift from the original ask.

---

## Deliverable

```python
"""dag_runner.py — async DAG task runner, Python 3.11 stdlib only."""

from __future__ import annotations

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
    """Raised when the dependency graph has a cycle. `.cycle` names it."""

    def __init__(self, cycle: list[str]):
        self.cycle = cycle
        chain = " -> ".join(cycle + [cycle[0]])
        super().__init__(f"Cyclic dependency detected: {chain}")


def _find_cycle(tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]]) -> Optional[list[str]]:
    """Iterative DFS, 3-color, no recursion (avoids stack-depth limits on
    large graphs). Returns the cycle as a list of task names, or None."""
    WHITE, GRAY, BLACK = 0, 1, 2
    color = {name: WHITE for name in tasks}
    SENTINEL = object()

    for start in tasks:
        if color[start] != WHITE:
            continue
        stack = [start]
        iters = {start: iter(tasks[start][0])}
        color[start] = GRAY
        while stack:
            node = stack[-1]
            dep = next(iters[node], SENTINEL)
            if dep is SENTINEL:
                color[node] = BLACK
                stack.pop()
                continue
            if dep not in tasks:
                # Out of scope (Assumption A1): unresolved dep names are
                # not validated here; they will surface as KeyError when
                # scheduling actually starts.
                continue
            if color[dep] == WHITE:
                color[dep] = GRAY
                iters[dep] = iter(tasks[dep][0])
                stack.append(dep)
            elif color[dep] == GRAY:
                idx = stack.index(dep)
                return stack[idx:]
            # BLACK: already fully explored elsewhere, no cycle through it.
    return None


async def run_tasks(
    tasks: dict[str, tuple[list[str], Callable[[], Awaitable]]],
    concurrency: int,
    max_retries: int,
) -> dict[str, TaskResult]:
    cycle = _find_cycle(tasks)
    if cycle is not None:
        raise CycleError(cycle)

    semaphore = asyncio.Semaphore(concurrency)
    node_tasks: dict[str, asyncio.Task[TaskResult]] = {}

    async def run_node(name: str) -> TaskResult:
        deps, fn = tasks[name]

        dep_results = await asyncio.gather(*(node_tasks[d] for d in deps))
        if any(r.status != Status.SUCCEEDED for r in dep_results):
            return TaskResult(Status.SKIPPED, 0, None)

        attempts = 0
        last_exc: Optional[BaseException] = None
        for _ in range(max_retries + 1):
            attempts += 1
            async with semaphore:
                try:
                    await fn()
                    return TaskResult(Status.SUCCEEDED, attempts, None)
                except asyncio.CancelledError:
                    raise
                except Exception as e:  # noqa: BLE001 — see Attack (A3)
                    last_exc = e
        return TaskResult(Status.FAILED, attempts, last_exc)

    # No `await` between here and the loop's end, so every sibling Task is
    # registered in node_tasks before any of them can execute a single line
    # (create_task schedules, it does not run synchronously).
    for name in tasks:
        node_tasks[name] = asyncio.create_task(run_node(name))

    try:
        results = await asyncio.gather(*node_tasks.values())
    except asyncio.CancelledError:
        for t in node_tasks.values():
            t.cancel()
        await asyncio.gather(*node_tasks.values(), return_exceptions=True)
        raise

    return dict(zip(node_tasks.keys(), results))
```

```python
"""test_dag_runner.py — stdlib-only tests (unittest.IsolatedAsyncioTestCase)."""

import asyncio
import unittest

from dag_runner import CycleError, Status, run_tasks


async def noop():
    return None


class ConcurrencyTracker:
    def __init__(self):
        self.current = 0
        self.max_seen = 0

    async def run(self):
        self.current += 1
        self.max_seen = max(self.max_seen, self.current)
        await asyncio.sleep(0.01)
        self.current -= 1


class Flaky:
    def __init__(self, fail_times):
        self.calls = 0
        self.fail_times = fail_times

    async def __call__(self):
        self.calls += 1
        if self.calls <= self.fail_times:
            raise RuntimeError(f"fail {self.calls}")


async def always_fail():
    raise ValueError("boom")


def make_fn(name, log):
    async def _fn():
        log.append(f"start:{name}")
        await asyncio.sleep(0)
        log.append(f"end:{name}")
    return _fn


class DagRunnerTests(unittest.IsolatedAsyncioTestCase):

    async def test_concurrency_bound(self):
        tracker = ConcurrencyTracker()
        tasks = {f"t{i}": ([], tracker.run) for i in range(6)}
        await run_tasks(tasks, concurrency=2, max_retries=0)
        self.assertLessEqual(tracker.max_seen, 2)

    async def test_dependency_ordering(self):
        log = []
        tasks = {
            "A": ([], make_fn("A", log)),
            "B": (["A"], make_fn("B", log)),
        }
        await run_tasks(tasks, concurrency=2, max_retries=0)
        self.assertLess(log.index("end:A"), log.index("start:B"))

    async def test_retry_then_succeed(self):
        flaky = Flaky(fail_times=2)
        tasks = {"flaky": ([], flaky)}
        results = await run_tasks(tasks, concurrency=1, max_retries=2)
        self.assertEqual(results["flaky"].status, Status.SUCCEEDED)
        self.assertEqual(results["flaky"].attempts, 3)

    async def test_failure_skips_transitive_dependents_unrelated_unaffected(self):
        tasks = {
            "root": ([], always_fail),
            "child": (["root"], noop),
            "grandchild": (["child"], noop),
            "other": ([], noop),
        }
        results = await run_tasks(tasks, concurrency=2, max_retries=1)
        self.assertEqual(results["root"].status, Status.FAILED)
        self.assertEqual(results["root"].attempts, 2)  # max_retries + 1
        self.assertIsInstance(results["root"].exception, ValueError)
        self.assertEqual(results["child"].status, Status.SKIPPED)
        self.assertEqual(results["child"].attempts, 0)
        self.assertEqual(results["grandchild"].status, Status.SKIPPED)
        self.assertEqual(results["other"].status, Status.SUCCEEDED)

    async def test_cycle_detected_before_anything_runs(self):
        calls = {"n": 0}

        async def counting_noop():
            calls["n"] += 1

        tasks = {
            "a": (["b"], counting_noop),
            "b": (["c"], counting_noop),
            "c": (["a"], counting_noop),
            "d": ([], counting_noop),
        }
        with self.assertRaises(CycleError) as ctx:
            await run_tasks(tasks, concurrency=2, max_retries=0)
        self.assertEqual(set(ctx.exception.cycle), {"a", "b", "c"})
        self.assertEqual(calls["n"], 0)

    async def test_cancellation_stops_new_work_and_awaits_inflight(self):
        started = asyncio.Event()
        allow_finish = asyncio.Event()
        state = {"cancelled_seen": False, "downstream_ran": False}

        async def long_task():
            started.set()
            try:
                await allow_finish.wait()
            except asyncio.CancelledError:
                state["cancelled_seen"] = True
                raise

        async def never_started():
            state["downstream_ran"] = True

        tasks = {
            "long": ([], long_task),
            "downstream": (["long"], never_started),
        }
        outer = asyncio.create_task(run_tasks(tasks, concurrency=2, max_retries=0))
        await started.wait()
        outer.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await outer
        self.assertTrue(state["cancelled_seen"])
        self.assertFalse(state["downstream_ran"])


if __name__ == "__main__":
    unittest.main()
```

```text
STATUS: DONE_WITH_CONCERNS
CONFIDENCE: medium — would raise to high by actually executing the test
  suite (I could not run code in this exercise; all verification here is
  hand-traced against documented asyncio semantics, not observed).
ASSUMPTIONS: A1 (all dep names resolve to real task keys — unvalidated,
  would KeyError otherwise), A2 (concurrency>=1, max_retries>=0), A4
  (a retrying task releases its concurrency slot between attempts rather
  than holding it) are unverified-by-execution but stated explicitly in
  Gather/Attack rather than left implicit.
ATTACK: A3's boundary (only `Exception`, not all `BaseException`, is caught
  and retried) means a task raising a non-Exception BaseException escapes
  as an uncaught exception from that node's Task instead of becoming a
  FAILED TaskResult, and can propagate through a dependent's await. Doesn't
  kill the answer because it matches the conventional Python "don't blanket
  -catch BaseException" boundary and the spec's "a task that raises" reads
  naturally as the Exception hierarchy; flagged rather than silently
  swallowed.
```
