import { expect, test } from "bun:test";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import type { ExtensionAPI, ExtensionContext, ExecResult } from "@oh-my-pi/pi-coding-agent";
import babysit from "../hooks/omp";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
type ExecCall = [command: string, args: string[], options?: Record<string, unknown>];

function fakeCtx(cwd: string, sessionId: string): ExtensionContext {
  // Minimal fake — the extension only reads cwd + sessionManager.getSessionId.
  return { cwd, sessionManager: { getSessionId: () => sessionId } } as unknown as ExtensionContext;
}

function harness(result: ExecResult = { code: 0, killed: false, stdout: "", stderr: "" }) {
  const handlers = new Map<string, Handler>();
  const calls: ExecCall[] = [];
  const pi = {
    on(name: string, handler: Handler) { handlers.set(name, handler); },
    async exec(command: string, args: string[], options?: Record<string, unknown>): Promise<ExecResult> {
      calls.push([command, args, options]);
      return result;
    },
  };
  babysit(pi as unknown as ExtensionAPI); // structural fake: only on/exec are used
  return { handlers, calls, ctx: fakeCtx("/tmp", "omp-test") };
}

test("OMP passes hook input as argv data and ignores other tools", async () => {
  const { handlers, calls, ctx } = harness();
  const command = 'echo "$(touch SHOULD_NOT_EXIST)"';
  expect(await handlers.get("tool_call")!({ toolName: "bash", input: { command } }, ctx)).toBeUndefined();
  // argv shape: bbs hooks <name> --payload <json> — the payload is an argv
  // value, never interpolated into a shell string.
  expect(calls[0][1]).toEqual(["hooks", "pre-tool-gate", "--payload", expect.any(String)]);
  expect(JSON.parse(calls[0][1][3]).tool_input.command).toBe(command);
  expect(calls[0][1].join(" ")).not.toContain(command);
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
    expect((await handlers.get("tool_call")!({ toolName: "bash", input: {} }, ctx)) as { block: boolean }).toMatchObject({ block: true });
  }
});

test("OMP refreshes session identity at start and after shell tools", async () => {
  const { handlers, calls, ctx } = harness();
  await handlers.get("session_start")!({}, ctx);
  await handlers.get("tool_result")!({ toolName: "bash" }, ctx);
  expect(calls).toHaveLength(2);
  expect(JSON.parse(calls[0][1][3])).toMatchObject({ agent: "omp", session_id: "omp-test" });
  expect(calls[0][1][1]).toBe("session-writer");
});

test("OMP executes the compiled hooks without a model or release command", async () => {
  // The extension prefers the sibling build at ../bin/bbs; build it if absent.
  const bbsPath = join(import.meta.dir, "..", "bin", "bbs");
  if (!existsSync(bbsPath)) {
    const build = spawnSync("go", ["build", "-o", bbsPath, "./cmd/bbs"],
      { cwd: join(import.meta.dir, ".."), encoding: "utf8" });
    if (build.status !== 0) return; // no Go toolchain — skip the exec half
  }
  const state = mkdtempSync(join(tmpdir(), "bbs-omp-hooks-"));
  try {
    const handlers = new Map<string, Handler>();
    const pi = {
      on(name: string, handler: Handler) { handlers.set(name, handler); },
      async exec(command: string, args: string[], options?: Record<string, unknown>): Promise<ExecResult> {
        const result = spawnSync(command, args, { ...options, encoding: "utf8",
          env: { ...process.env, BABYSIT_HOME: state } });
        return { code: result.status ?? 1, killed: !!result.signal, stdout: result.stdout, stderr: result.stderr };
      },
    };
    babysit(pi as unknown as ExtensionAPI);
    const ctx = fakeCtx(state, "real-omp");
    await handlers.get("session_start")!({}, ctx);
    expect(existsSync(join(state, "sessions/omp-real-omp.yaml"))).toBe(true);
    expect(await handlers.get("tool_call")!({ toolName: "bash", input: { command: "echo harmless" } }, ctx))
      .toBeUndefined();
  } finally {
    rmSync(state, { recursive: true });
  }
});
