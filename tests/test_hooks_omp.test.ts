import { expect, test } from "bun:test";
import { mkdtempSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import babysit from "../hooks/omp";

function harness(result = { code: 0, killed: false, stdout: "", stderr: "" }) {
  const handlers = new Map<string, Function>();
  const calls: any[] = [];
  babysit({
    on(name: string, handler: Function) { handlers.set(name, handler); },
    async exec(...args: any[]) { calls.push(args); return result; },
  } as any);
  const ctx = { cwd: "/tmp", sessionManager: { getSessionId: () => "omp-test" } };
  return { handlers, calls, ctx };
}

test("OMP passes shell input as data and ignores other tools", async () => {
  const { handlers, calls, ctx } = harness();
  const command = 'echo "$(touch SHOULD_NOT_EXIST)"';
  expect(await handlers.get("tool_call")!({ toolName: "bash", input: { command } }, ctx)).toBeUndefined();
  expect(JSON.parse(calls[0][1][3]).tool_input.command).toBe(command);
  expect(calls[0][1][1]).not.toContain(command);
  await handlers.get("tool_call")!({ toolName: "read", input: {} }, ctx);
  expect(calls).toHaveLength(1);
});

for (const permissionDecision of ["deny", "ask"]) {
  test(`OMP blocks ${permissionDecision} with the check's reason`, async () => {
    const { handlers, ctx } = harness({ code: 0, killed: false, stderr: "",
      stdout: JSON.stringify({ hookSpecificOutput: { permissionDecision, permissionDecisionReason: "Run review" } }) });
    expect(await handlers.get("tool_call")!({ toolName: "bash", input: {} }, ctx))
      .toEqual({ block: true, reason: "Run review" });
  });
}

test("OMP blocks process failure, timeout, and malformed responses", async () => {
  for (const result of [
    { code: 127, killed: false, stdout: "", stderr: "missing" },
    { code: 0, killed: true, stdout: "", stderr: "" },
    { code: 0, killed: false, stdout: "invalid", stderr: "" },
    { code: 0, killed: false, stdout: "{}", stderr: "" },
  ]) {
    const { handlers, ctx } = harness(result);
    expect((await handlers.get("tool_call")!({ toolName: "bash", input: {} }, ctx)).block).toBe(true);
  }
});

test("OMP refreshes session identity at start and after shell tools", async () => {
  const { handlers, calls, ctx } = harness();
  await handlers.get("session_start")!({}, ctx);
  await handlers.get("tool_result")!({ toolName: "bash" }, ctx);
  expect(calls).toHaveLength(2);
  expect(JSON.parse(calls[0][1][3])).toMatchObject({ agent: "omp", session_id: "omp-test" });
  expect(calls[0][1][4]).toEndWith("/bin/hooks/session-writer");
});

test("OMP executes the actual shell hooks without a model or release command", async () => {
  const state = mkdtempSync(join(tmpdir(), "bbs-omp-hooks-"));
  try {
    const handlers = new Map<string, Function>();
    babysit({
      on(name: string, handler: Function) { handlers.set(name, handler); },
      async exec(command: string, args: string[], options: any) {
        const result = spawnSync(command, args, { ...options, encoding: "utf8",
          env: { ...process.env, BABYSIT_HOME: state } });
        return { code: result.status, killed: !!result.signal, stdout: result.stdout, stderr: result.stderr };
      },
    } as any);
    const ctx = { cwd: state, sessionManager: { getSessionId: () => "real-omp" } };
    await handlers.get("session_start")!({}, ctx);
    expect(existsSync(join(state, "sessions/omp-real-omp.yaml"))).toBe(true);
    expect(await handlers.get("tool_call")!({ toolName: "bash", input: { command: "echo harmless" } }, ctx))
      .toBeUndefined();
  } finally {
    rmSync(state, { recursive: true });
  }
});
