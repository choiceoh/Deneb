import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const browserModule = fileURLToPath(new URL("../lib/browser.mjs", import.meta.url));

// Each case runs in a fresh node: Playwright fixes its browser directory at its
// first import, which a shared test process could only observe once.
function inFreshNode(body, env) {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "groupware-browser-"));
  try {
    const run = spawnSync(
      process.execPath,
      ["--input-type=module", "-e", `const m = await import(${JSON.stringify(browserModule)});\n${body}`],
      { encoding: "utf8", env: { PATH: process.env.PATH, HOME: home, ...env(home) } },
    );
    assert.equal(run.status, 0, run.stderr);
    return { home, out: JSON.parse(run.stdout) };
  } finally {
    fs.rmSync(home, { recursive: true, force: true });
  }
}

const whereLaunchLooks = `
const { chromium } = await m.loadPlaywright();
console.log(JSON.stringify({ path: m.BROWSERS_PATH, executable: chromium.executablePath() }));`;

test("the login browser stays under ~/.deneb whatever XDG_CACHE_HOME says", () => {
  // The gateway unit points XDG_CACHE_HOME at an agent cache; the deploy timer
  // leaves it unset. Both must land on the same directory.
  const { home, out } = inFreshNode(whereLaunchLooks, (h) => ({ XDG_CACHE_HOME: path.join(h, "agent-cache") }));
  assert.equal(out.path, path.join(home, ".deneb", "ms-playwright"));
  assert.ok(out.executable.startsWith(out.path + path.sep), out.executable);
});

test("PLAYWRIGHT_BROWSERS_PATH still overrides the default", () => {
  const { home, out } = inFreshNode(whereLaunchLooks, (h) => ({ PLAYWRIGHT_BROWSERS_PATH: path.join(h, "pinned") }));
  assert.equal(out.path, path.join(home, "pinned"));
  assert.ok(out.executable.startsWith(out.path + path.sep), out.executable);
});

test("a missing browser names the install command for this directory", () => {
  const { home, out } = inFreshNode(
    `try {
  const browser = await m.launchChromium({ headless: true });
  await browser.close();
  console.log(JSON.stringify({ launched: true }));
} catch (err) {
  console.log(JSON.stringify({ message: err.message }));
}`,
    () => ({}),
  );
  assert.equal(
    out.message,
    `login browser is not installed under ${path.join(home, ".deneb", "ms-playwright")}; ` +
      "run: node scripts/dev/groupware-reader/install-browser.mjs",
  );
});
