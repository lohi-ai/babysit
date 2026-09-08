// OMP uses extension events, not Claude-style command-hook manifests.
import { realpathSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI, ExtensionContext } from "@oh-my-pi/pi-coding-agent";

const scripts = resolve(dirname(realpathSync(fileURLToPath(import.meta.url))), "../bin/hooks");

export default function babysit(pi: ExtensionAPI) {
  async function run(name: string, ctx: ExtensionContext, input = {}) {
    const payload = JSON.stringify({
      agent: "omp", session_id: ctx.sessionManager.getSessionId(),
      cwd: ctx.cwd, tool_input: input,
    });
    // Pass the payload as data, never interpolate tool commands into shell code.
    return pi.exec("bash", ["-c", 'printf "%s" "$1" | bash "$2"',
      "babysit-hook", payload, resolve(scripts, name)], { cwd: ctx.cwd, timeout: 10000 });
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
