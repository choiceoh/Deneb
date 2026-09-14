/**
 * The login browser: where it lives, how it launches, how it gets installed.
 *
 * Playwright keeps browsers under $XDG_CACHE_HOME/ms-playwright by default.
 * Disk cleanups treat ~/.cache as disposable, and a deleted browser stays
 * invisible until the cached session expires up to 12 h later; from then on
 * every list, radar scan, and approval fails at login (2026-09-14: 결재 was
 * down for a day). The browser lives next to the session under ~/.deneb
 * instead, and every caller (gateway, CLI, deploy) goes through this module so
 * the install and the launch cannot disagree about the directory.
 */
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";

export const BROWSERS_PATH =
  process.env.PLAYWRIGHT_BROWSERS_PATH || path.join(os.homedir(), ".deneb", "ms-playwright");

const INSTALL_COMMAND = "node scripts/dev/groupware-reader/install-browser.mjs";

// Playwright fixes its browser directory when the package first loads, so the
// path has to be in the environment before that import, not merely before launch.
export async function loadPlaywright() {
  process.env.PLAYWRIGHT_BROWSERS_PATH = BROWSERS_PATH;
  return import("playwright");
}

export async function launchChromium(options) {
  const { chromium } = await loadPlaywright();
  try {
    return await chromium.launch(options);
  } catch (err) {
    // Playwright's own hint says `npx playwright install`, which installs into
    // whatever directory the shell running it resolves, not necessarily this one.
    if (/Executable doesn't exist/.test(String(err?.message))) {
      throw new Error(`login browser is not installed under ${BROWSERS_PATH}; run: ${INSTALL_COMMAND}`, {
        cause: err,
      });
    }
    throw err;
  }
}

/** Installs the headless shell into BROWSERS_PATH; Playwright skips a build already there. */
export function installBrowser() {
  const require = createRequire(import.meta.url);
  const cli = path.join(path.dirname(require.resolve("playwright/package.json")), "cli.js");
  const run = spawnSync(process.execPath, [cli, "install", "chromium-headless-shell"], {
    stdio: "inherit",
    env: { ...process.env, PLAYWRIGHT_BROWSERS_PATH: BROWSERS_PATH },
  });
  return run.status ?? 1;
}
