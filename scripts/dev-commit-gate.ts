#!/usr/bin/env bun
/**
 * dev-commit-gate — Run all pre-commit quality gates in sequence.
 *
 * Usage:
 *   bun scripts/dev-commit-gate.ts              # check + affected tests
 *   bun scripts/dev-commit-gate.ts --full       # check + ALL tests + build
 *   bun scripts/dev-commit-gate.ts --no-test    # check only (skip tests)
 *
 * Exits 0 only if all gates pass. Reports structured results per stage.
 */

import { execSync, spawnSync } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");

type StageResult = {
  stage: string;
  passed: boolean;
  durationMs: number;
  output: string;
};

function runStage(stage: string, cmd: string, args: string[]): StageResult {
  const start = Date.now();
  const result = spawnSync(cmd, args, {
    cwd: ROOT,
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 300_000,
    env: { ...process.env, FORCE_COLOR: "0", NO_COLOR: "1" },
  });
  const stdout = result.stdout?.toString("utf8") ?? "";
  const stderr = result.stderr?.toString("utf8") ?? "";
  const combined = [stdout, stderr].filter(Boolean).join("\n");
  // Keep last 80 lines to avoid flooding
  const lines = combined.split("\n");
  const output =
    lines.length > 80
      ? [
          ...lines.slice(0, 20),
          `\n... (${lines.length - 80} lines omitted) ...\n`,
          ...lines.slice(-60),
        ].join("\n")
      : combined;

  return {
    stage,
    passed: result.status === 0,
    durationMs: Date.now() - start,
    output,
  };
}

function getAffectedTests(): string[] {
  try {
    const raw = execSync("bun scripts/dev-affected.ts", {
      cwd: ROOT,
      encoding: "utf8",
      timeout: 30_000,
    });
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed.tests) ? parsed.tests : [];
  } catch {
    return [];
  }
}

function main() {
  const args = new Set(process.argv.slice(2));
  const full = args.has("--full");
  const noTest = args.has("--no-test");

  const results: StageResult[] = [];
  let allPassed = true;

  // Stage 1: pnpm check (lint + format + typecheck)
  console.log("▶ Stage 1/3: pnpm check (lint + format + types)");
  const checkResult = runStage("check", "pnpm", ["check"]);
  results.push(checkResult);
  if (!checkResult.passed) {
    allPassed = false;
    console.log("✗ check failed\n");
  } else {
    console.log(`✓ check passed (${checkResult.durationMs}ms)\n`);
  }

  // Stage 2: tests
  if (!noTest && allPassed) {
    if (full) {
      console.log("▶ Stage 2/3: pnpm test (all)");
      const testResult = runStage("test", "pnpm", ["test"]);
      results.push(testResult);
      if (!testResult.passed) {
        allPassed = false;
        console.log("✗ test failed\n");
      } else {
        console.log(`✓ test passed (${testResult.durationMs}ms)\n`);
      }
    } else {
      const affected = getAffectedTests();
      if (affected.length > 0) {
        console.log(`▶ Stage 2/3: affected tests (${affected.length} files)`);
        const testResult = runStage("test:affected", "pnpm", ["test", "--", ...affected]);
        results.push(testResult);
        if (!testResult.passed) {
          allPassed = false;
          console.log("✗ affected tests failed\n");
        } else {
          console.log(`✓ affected tests passed (${testResult.durationMs}ms)\n`);
        }
      } else {
        console.log("▶ Stage 2/3: no affected tests, skipping\n");
        results.push({
          stage: "test:affected",
          passed: true,
          durationMs: 0,
          output: "No affected tests.",
        });
      }
    }
  } else if (noTest) {
    results.push({ stage: "test", passed: true, durationMs: 0, output: "Skipped (--no-test)." });
  }

  // Stage 3: build (only in --full mode or if check passed)
  if (full && allPassed) {
    console.log("▶ Stage 3/3: pnpm build");
    const buildResult = runStage("build", "pnpm", ["build"]);
    results.push(buildResult);
    if (!buildResult.passed) {
      allPassed = false;
      console.log("✗ build failed\n");
    } else {
      const hasWarning = buildResult.output.includes("INEFFECTIVE_DYNAMIC_IMPORT");
      console.log(
        `✓ build passed${hasWarning ? " (⚠ INEFFECTIVE_DYNAMIC_IMPORT warning)" : ""} (${buildResult.durationMs}ms)\n`,
      );
    }
  } else if (!full) {
    results.push({
      stage: "build",
      passed: true,
      durationMs: 0,
      output: "Skipped (use --full for build).",
    });
  }

  // Summary
  const totalMs = results.reduce((sum, r) => sum + r.durationMs, 0);
  console.log("─".repeat(60));
  console.log(allPassed ? "✓ All gates passed" : "✗ GATE FAILED");
  console.log(`Total: ${totalMs}ms\n`);

  // Print JSON for machine consumption
  const summary = {
    passed: allPassed,
    totalDurationMs: totalMs,
    stages: results.map(({ stage, passed, durationMs }) => ({ stage, passed, durationMs })),
  };
  console.log(JSON.stringify(summary, null, 2));

  // Print failure details
  for (const r of results) {
    if (!r.passed) {
      console.log(`\n--- ${r.stage} failure output ---`);
      console.log(r.output);
    }
  }

  process.exit(allPassed ? 0 : 1);
}

main();
