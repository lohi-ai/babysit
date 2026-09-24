// OMP uses extension events, not Claude-style command-hook manifests.
import { existsSync, realpathSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI, ExtensionContext } from "@oh-my-pi/pi-coding-agent";

// The hooks are compiled into the bbs binary (`bbs hooks <name>`), so no bash
// or jq is required on any OS. A sibling build in this checkout wins over PATH
// so an in-repo extension exercises the same code the tests do; otherwise the
// installed `bbs` on PATH serves it (setup-skills / brew both put it there).
const binDir = resolve(dirname(realpathSync(fileURLToPath(import.meta.url))), "../bin");
const bbs = ["bbs", "bbs.exe"].map((n) => resolve(binDir, n)).find(existsSync) ?? "bbs";

export default function babysit(pi: ExtensionAPI) {
  async function run(name: string, ctx: ExtensionContext, input = {}) {
    const payload = JSON.stringify({
      agent: "omp", session_id: ctx.sessionManager.getSessionId(),
      cwd: ctx.cwd, tool_input: input,
    });
    // pi.exec has no stdin channel — the payload travels as an argv value,
    // never interpolated into shell code.
    return pi.exec(bbs, ["hooks", name, "--payload", payload], { cwd: ctx.cwd, timeout: 10000 });
  }

  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash") return;
    try {
      const result = await run("pre-tool-gate", ctx, event.input);
      if (result.killed || result.code !== 0) {
        return { block: true, reason: `Babysit release gate failed: ${result.stderr || "timeout or process failure"}` };
      }
      if (!result.stdout.trim()) return;
      const decision = JSON.parse(result.stdout).hookSpecificOutput;
      if (decision?.permissionDecision === "deny" || decision?.permissionDecision === "ask") {
        return { block: true, reason: decision.permissionDecisionReason };
      }
      return { block: true, reason: "Babysit release gate returned an unexpected response." };
    } catch (error) {
      return { block: true, reason: `Babysit release gate failed: ${error}` };
    }
  });

  async function heartbeat(_event: unknown, ctx: ExtensionContext) {
    // Session tracking is advisory; failure must not prevent tool execution.
    try { await run("session-writer", ctx); } catch { /* best effort */ }
  }
  pi.on("session_start", heartbeat);
  pi.on("tool_result", async (event, ctx) => {
    if (event.toolName === "bash") await heartbeat(event, ctx);
  });
}
