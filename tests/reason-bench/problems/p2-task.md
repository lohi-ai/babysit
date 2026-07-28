# P2 — Coding: async DAG task runner

Implement, in Python 3.11 standard library only (asyncio), a task runner:

```python
async def run_tasks(tasks, concurrency, max_retries) -> dict[str, TaskResult]
```

- `tasks`: `dict[str, tuple[list[str], Callable[[], Awaitable]]]` — maps a task
  name to `(deps, fn)`. A task runs only after all its deps have succeeded.
- At most `concurrency` task functions may be executing at any instant.
- A task that raises is retried up to `max_retries` times (total attempts =
  `max_retries + 1`); a retry re-executes only that task, not its deps.
- If a task ultimately fails, every transitive dependent is marked `SKIPPED`
  without executing; unrelated tasks still run to completion.
- If the dependency graph has a cycle, raise `CycleError` naming the tasks in
  a cycle, before running anything.
- Cancellation: if the caller cancels `run_tasks`, no new tasks start, and
  in-flight tasks are cancelled and awaited before the cancellation
  propagates.
- `TaskResult` carries: status (`SUCCEEDED` / `FAILED` / `SKIPPED`), number of
  attempts made, and the final exception if any.

Deliver the full implementation plus tests demonstrating the behaviors above.
No external libraries. You cannot execute code — reason through correctness
carefully.
