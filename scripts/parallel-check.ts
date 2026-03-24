#!/usr/bin/env bun
/**
 * parallel-check — Run all `pnpm check` gates in parallel.
 *
 * Groups checks into three tiers:
 *   1. Heavy tools (format:check, tsgo, lint) — run in parallel.
 *   2. Lightweight boundary checks — all run in parallel.
 *   3. plugin-sdk:check-exports — runs alongside everything else.
 *
 * All tiers execute concurrently. Fails fast on first error, printing
 * the failed command's output. On success, prints a summary with timings.
 *
 * Usage:
 *   bun scripts/parallel-check.ts
 *   pnpm check                      # (after wiring in package.json)
 */

import { spawn } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");

// All the pnpm script names that make up `pnpm check`, in no particular order.
// Every one of these is independent and can run fully in parallel.
const ALL_CHECKS = [
  // Heavy tools
  "check:host-env-policy:swift",
  "check:bundled-provider-auth-env-vars",
  "format:check",
  "tsgo",
  "lint",
  "plugin-sdk:check-exports",

  // Boundary lint checks
  "lint:tmp:no-random-messaging",
  "lint:tmp:channel-agnostic-boundaries",
  "lint:tmp:no-raw-channel-fetch",
  "lint:agent:ingress-owner",
  "lint:plugins:no-register-http-handler",
  "lint:plugins:no-monolithic-plugin-sdk-entry-imports",
  "lint:plugins:no-extension-src-imports",
  "lint:plugins:no-extension-test-core-imports",
  "lint:plugins:no-extension-imports",
  "lint:plugins:plugin-sdk-subpaths-exported",
  "lint:extensions:no-src-outside-plugin-sdk",
  "lint:extensions:no-plugin-sdk-internal",
  "lint:extensions:no-relative-outside-package",
  "lint:web-search-provider-boundaries",
  "lint:webhook:no-low-level-body-read",
  "lint:auth:no-pairing-store-group",
  "lint:auth:pairing-account-scope",
];

type CheckResult = {
  name: string;
  passed: boolean;
  aborted: boolean;
  durationMs: number;
  output: string;
};

function runCheck(name: string, signal: AbortSignal): Promise<CheckResult> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve({ name, passed: false, aborted: true, durationMs: 0, output: "" });
      return;
    }

    const start = performance.now();
    const chunks: Buffer[] = [];
    let wasAborted = false;

    const proc = spawn("pnpm", [name], {
      cwd: ROOT,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, FORCE_COLOR: "1" },
    });

    proc.stdout.on("data", (d: Buffer) => chunks.push(d));
    proc.stderr.on("data", (d: Buffer) => chunks.push(d));

    signal.addEventListener(
      "abort",
      () => {
        wasAborted = true;
        proc.kill("SIGTERM");
      },
      { once: true },
    );

    proc.on("close", (code) => {
      resolve({
        name,
        passed: code === 0,
        aborted: wasAborted,
        durationMs: Math.round(performance.now() - start),
        output: Buffer.concat(chunks).toString("utf8"),
      });
    });
  });
}

async function main() {
  const totalStart = performance.now();
  const controller = new AbortController();

  console.log(`\x1b[1m▶ Running ${ALL_CHECKS.length} checks in parallel…\x1b[0m\n`);

  const results = await Promise.all(
    ALL_CHECKS.map(async (name) => {
      const result = await runCheck(name, controller.signal);
      if (!result.passed) {
        controller.abort();
      }
      return result;
    }),
  );

  const totalMs = Math.round(performance.now() - totalStart);
  const failed = results.filter((r) => !r.passed && !r.aborted);
  const passed = results.filter((r) => r.passed);

  // Print summary
  console.log(`\x1b[1m── Results ──\x1b[0m\n`);

  for (const r of results.toSorted((a, b) => b.durationMs - a.durationMs)) {
    const icon = r.passed
      ? "\x1b[32m✓\x1b[0m"
      : r.aborted
        ? "\x1b[33m⊘\x1b[0m"
        : "\x1b[31m✗\x1b[0m";
    const ms = `${r.durationMs}ms`.padStart(7);
    console.log(`  ${icon} ${ms}  ${r.name}`);
  }

  console.log();

  if (failed.length > 0) {
    console.log(
      `\x1b[31m✗ ${failed.length} check(s) failed\x1b[0m (${passed.length} passed) in ${totalMs}ms\n`,
    );
    for (const f of failed) {
      console.log(`\x1b[1m\x1b[31m── ${f.name} ──\x1b[0m`);
      console.log(f.output.trimEnd());
      console.log();
    }
    process.exit(1);
  }

  console.log(`\x1b[32m✓ All ${ALL_CHECKS.length} checks passed\x1b[0m in ${totalMs}ms`);
}

void main();
