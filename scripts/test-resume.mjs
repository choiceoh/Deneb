#!/usr/bin/env node
/**
 * Re-run only the test files that failed in the previous `pnpm test` run.
 *
 * Usage:
 *   pnpm test:resume          # re-run failed tests from last run
 *   pnpm test:resume --list   # show which files would be re-run
 *   pnpm test:resume --clear  # clear the failed tests manifest
 *
 * The failed-tests manifest is written by test/failed-tests-reporter.ts
 * and lives at test/fixtures/failed-tests.json.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..");
const MANIFEST_PATH = path.join(REPO_ROOT, "test", "fixtures", "failed-tests.json");

function loadManifest() {
  try {
    const raw = fs.readFileSync(MANIFEST_PATH, "utf-8");
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed.files) ? parsed : null;
  } catch {
    return null;
  }
}

const args = process.argv.slice(2);

if (args.includes("--clear")) {
  try {
    fs.unlinkSync(MANIFEST_PATH);
    console.log("✓ Cleared failed tests manifest.");
  } catch {
    console.log("No manifest to clear.");
  }
  process.exit(0);
}

const manifest = loadManifest();
if (!manifest || manifest.files.length === 0) {
  console.log("✓ No failed tests to resume. All tests passed in the last run.");
  process.exit(0);
}

if (args.includes("--list")) {
  console.log(`Failed tests from ${manifest.generatedAt}:\n`);
  for (const file of manifest.files) {
    console.log(`  ${file}`);
  }
  console.log(`\nTotal: ${manifest.files.length} file(s)`);
  process.exit(0);
}

console.log(
  `\nResuming ${manifest.files.length} failed test file(s) from ${manifest.generatedAt}:\n`,
);
for (const file of manifest.files) {
  console.log(`  ${file}`);
}
console.log();

// Pass extra args (like --bail) through to the test runner
const extraArgs = args.filter((arg) => arg !== "--list" && arg !== "--clear");

try {
  execFileSync("node", ["scripts/test-parallel.mjs", "--", ...manifest.files, ...extraArgs], {
    cwd: REPO_ROOT,
    stdio: "inherit",
    env: process.env,
  });
  // If all tests pass, clean up the manifest
  try {
    fs.unlinkSync(MANIFEST_PATH);
  } catch {
    // ignore
  }
  console.log("\n✓ All previously failed tests now pass.");
} catch (err) {
  const code = err.status ?? 1;
  console.error(`\n✗ Some tests still failing. Run \`pnpm test:resume\` again after fixing.`);
  process.exit(code);
}
