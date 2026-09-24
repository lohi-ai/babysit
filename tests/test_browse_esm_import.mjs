// tests/test_browse_esm_import.mjs — run under real Node (`node --test`),
// not bun: bun's ESM loader normalizes %5C in file URLs, so only Node
// exercises the Windows-path semantics this guards.
//
// The browse skill imports cloakbrowser by absolute path. On Windows
// `npm root -g` returns a backslash path (C:\…) — neither a valid ESM
// specifier nor safe inside a JS string literal. The skill now imports via
// url.pathToFileURL with the path passed through env (audit bs-b3m7rnkw #10).
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

test("a bare Windows path fails import(); the file URL pathToFileURL produces works", async () => {
  // The bug: import("C:\\pkg\\index.js") parses as a URL with scheme "c".
  await assert.rejects(
    import("C:\\pkg\\index.js"),
    (e) => e.code === "ERR_UNSUPPORTED_ESM_URL_SCHEME" || e.code === "ERR_INVALID_MODULE_SPECIFIER",
  );

  // The fix: on Windows, pathToFileURL("C:\\pkg\\index.js") yields
  // file:///C:/pkg/index.js — backslashes become slashes. Simulate that
  // layout on POSIX and prove the URL form imports.
  const root = mkdtempSync(join(tmpdir(), "esm-win-"));
  try {
    const dir = join(root, "C:", "pkg");
    mkdirSync(dir, { recursive: true });
    const mod = join(dir, "index.js");
    writeFileSync(mod, "export const MARKER = 'file-url-ok';\n");
    const m = await import(pathToFileURL(mod).href);
    assert.equal(m.MARKER, "file-url-ok");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("browse SKILL.md imports via pathToFileURL, never embeds the path in JS", () => {
  const skill = readFileSync(join(here, "../.claude/skills/browse/SKILL.md"), "utf8");
  assert.match(skill, /pathToFileURL/);
  // The old shape interpolated $CB into the JS source — a backslash path
  // broke the string literal and the specifier.
  assert.doesNotMatch(skill, /import\('\$CB'\)/);
  assert.doesNotMatch(skill, /import\("\$CB"\)/);
  // The path must travel via env, not string interpolation.
  assert.match(skill, /process\.env\.CB/);
});
