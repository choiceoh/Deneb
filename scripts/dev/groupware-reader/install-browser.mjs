#!/usr/bin/env node
/**
 * Install the login browser where lib/browser.mjs launches it.
 *
 *   node scripts/dev/groupware-reader/install-browser.mjs
 *
 * Idempotent. scripts/deploy/deploy.sh runs it after `npm ci`, so a Playwright
 * bump or a deleted browser directory is repaired by the next deploy instead of
 * surfacing as a login failure when the session expires.
 */
import { BROWSERS_PATH, installBrowser } from "./lib/browser.mjs";

console.error(`groupware-reader login browser: ${BROWSERS_PATH}`);
process.exit(installBrowser());
