"""Grader probe for P2 (async DAG task runner).

Usage: python3 p2_probe.py <path-to-raw-result.md>

Extracts ```python blocks from the submission markdown, execs them in one
namespace, then runs probe tests against run_tasks. Prints one PASS/FAIL line
per probe and a final score line.
"""
import asyncio
import re
import sys
import traceback


def load_submission(path):
    text = open(path, encoding="utf-8").read()
    blocks = re.findall(r"```(?:python|py)[^\n]*\n(.*?)```", text, re.S)
    if not blocks:
        blocks = re.findall(r"```\n(.*?)```", text, re.S)
    ns = {}
    # Exec blocks independently: submissions often include a separate test
    # file that imports the impl by a module name that doesn't exist here
    # (e.g. `import dag_runner`). The impl block still loads; broken/aux
    # blocks are skipped with a note.
    for i, code in enumerate(blocks):
        try:
            exec(compile(code, f"{path}#block{i}", "exec"), ns)  # noqa: S102
        except Exception as e:
            print(f"NOTE: skipped block {i}: {type(e).__name__}: {e}")
    return ns


def status_name(result):
    st = getattr(result, "status", result)
    name = getattr(st, "name", None) or getattr(st, "value", None) or str(st)
    return str(name).upper()


async def probe_cycle(ns):
    ran = []

    async def t():
        ran.append(1)

    tasks = {"a": (["b"], t), "b": (["a"], t), "c": ([], t)}
    try:
        await ns["run_tasks"](tasks, concurrency=2, max_retries=0)
    except Exception as e:
        assert type(e).__name__ == "CycleError", f"raised {type(e).__name__}, not CycleError"
        assert not ran, "tasks executed despite cycle"
        return
    raise AssertionError("no CycleError raised for a<->b cycle")


async def probe_failure_propagation(ns):
    ran = []

    async def ok(name):
        async def f():
            ran.append(name)
        return f

    async def boom():
        raise RuntimeError("boom")

    async def c_fn():
        ran.append("c")

    async def d_fn():
        ran.append("d")

    tasks = {
        "a": ([], boom),
        "b": (["a"], d_fn),      # direct dependent -> SKIPPED
        "e": (["b"], d_fn),      # transitive dependent -> SKIPPED
        "c": ([], c_fn),         # unrelated -> runs
    }
    res = await ns["run_tasks"](tasks, concurrency=2, max_retries=1)
    assert status_name(res["a"]) == "FAILED", f'a={status_name(res["a"])}'
    assert status_name(res["b"]) == "SKIPPED", f'b={status_name(res["b"])}'
    assert status_name(res["e"]) == "SKIPPED", f'e={status_name(res["e"])}'
    assert status_name(res["c"]) == "SUCCEEDED", f'c={status_name(res["c"])}'
    assert "c" in ran and "d" not in ran


async def probe_retry_count(ns):
    calls = {"n": 0}

    async def flaky():
        calls["n"] += 1
        if calls["n"] < 3:
            raise ValueError("flaky")

    res = await ns["run_tasks"]({"a": ([], flaky)}, concurrency=1, max_retries=2)
    assert status_name(res["a"]) == "SUCCEEDED", status_name(res["a"])
    attempts = getattr(res["a"], "attempts", None)
    assert attempts == 3, f"attempts={attempts}, expected 3"
    assert calls["n"] == 3, f"fn called {calls['n']} times"


async def probe_concurrency(ns):
    state = {"cur": 0, "peak": 0}

    def mk():
        async def f():
            state["cur"] += 1
            state["peak"] = max(state["peak"], state["cur"])
            await asyncio.sleep(0.02)
            state["cur"] -= 1
        return f

    tasks = {f"t{i}": ([], mk()) for i in range(8)}
    await ns["run_tasks"](tasks, concurrency=2, max_retries=0)
    assert state["peak"] <= 2, f"peak concurrency {state['peak']} > 2"


async def probe_cancellation(ns):
    observed = {"cancelled": False, "started": False}

    async def slow():
        observed["started"] = True
        try:
            await asyncio.sleep(10)
        except asyncio.CancelledError:
            observed["cancelled"] = True
            raise

    async def never_runs():
        observed["late_start"] = True

    tasks = {"slow": ([], slow), "later": (["slow"], never_runs)}
    runner = asyncio.ensure_future(ns["run_tasks"](tasks, concurrency=1, max_retries=3))
    await asyncio.sleep(0.05)
    runner.cancel()
    try:
        await asyncio.wait_for(runner, timeout=2)
        raise AssertionError("run_tasks did not propagate cancellation")
    except asyncio.CancelledError:
        pass
    assert observed["started"], "slow task never started"
    assert observed["cancelled"], "in-flight task was not cancelled"
    assert "late_start" not in observed, "new task started after cancel"


PROBES = [
    ("cycle", probe_cycle),
    ("failure_propagation", probe_failure_propagation),
    ("retry_count", probe_retry_count),
    ("concurrency", probe_concurrency),
    ("cancellation", probe_cancellation),
]


async def main(path):
    ns = load_submission(path)
    if "run_tasks" not in ns:
        print("FATAL: no run_tasks in submission")
        return
    passed = 0
    for name, probe in PROBES:
        try:
            await asyncio.wait_for(probe(ns), timeout=10)
            print(f"PASS {name}")
            passed += 1
        except Exception as e:
            print(f"FAIL {name}: {type(e).__name__}: {e}")
            if "-v" in sys.argv:
                traceback.print_exc()
    print(f"SCORE {passed}/{len(PROBES)}")


if __name__ == "__main__":
    asyncio.run(main(sys.argv[1]))
