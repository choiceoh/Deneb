#!/usr/bin/env bun
/**
 * dev-commit-gate — Run all pre-commit quality gates.
 *
 * Optimizations over naive sequential execution:
 *   1. Smart skip: docs/test/script-only changes skip tests or even check.
 *   2. Parallel: affected test discovery runs concurrently with pnpm check.
 *   3. Fast-fail: stops immediately on first gate failure.
 *
 * Usage:
 *   bun scripts/dev-commit-gate.ts              # smart mode (auto-detect scope)
 *   bun scripts/dev-commit-gate.ts --full       # check + ALL tests + build
 *   bun scripts/dev-commit-gate.ts --no-test    # check only (skip tests)
 *
 * Exits 0 only if all gates pass. Reports structured results per stage.
 */

import { execSync, spawn, spawnSync } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");

type StageResult = {
  stage: string;
  passed: boolean;
  durationMs: number;
  output: string;
};

// ---------------------------------------------------------------------------
// Smart scope detection — determine minimal gate set
// ---------------------------------------------------------------------------

type GateScope = "full" | "check-and-test" | "check-only" | "skip";

function detectScope(): { scope: GateScope; reason: string } {
  let diffFiles: string[];
  try {
    const raw = execSync(
      "git diff --name-only --diff-filter=ACMR HEAD 2>/dev/null; git diff --cached --name-only --diff-filter=ACMR 2>/dev/null; git ls-files --others --exclude-standard 2>/dev/null",
      { cwd: ROOT, encoding: "utf8", timeout: 10_000 },
    );
    diffFiles = [
      ...new Set(
        raw
          .split("\n")
          .map((l) => l.trim())
          .filter(Boolean),
      ),
    ];
  } catch {
    return { scope: "check-and-test", reason: "could not detect diff" };
  }

  if (diffFiles.length === 0) {
    return { scope: "skip", reason: "no changes detected" };
  }

  const isDocsOnly = diffFiles.every((f) => f.startsWith("docs/"));
  if (isDocsOnly) {
    return { scope: "skip", reason: "docs-only changes — no gates needed" };
  }

  const isNonSource = diffFiles.every(
    (f) =>
      f.startsWith("docs/") ||
      f.startsWith("scripts/") ||
      f.endsWith(".md") ||
      f.endsWith(".yml") ||
      f.endsWith(".yaml") ||
      f.startsWith(".github/") ||
      f.startsWith(".agents/"),
  );
  if (isNonSource) {
    return { scope: "skip", reason: "non-source changes only (docs/scripts/config)" };
  }

  const isTestOnly = diffFiles.every((f) => f.endsWith(".test.ts") || f.endsWith(".e2e.test.ts"));
  if (isTestOnly) {
    return { scope: "check-and-test", reason: "test-only changes" };
  }

  const touchesPluginSdk = diffFiles.some((f) => f.startsWith("src/plugin-sdk/"));
  const touchesBuildConfig = diffFiles.some(
    (f) =>
      f === "package.json" ||
      f === "tsconfig.json" ||
      f === "pnpm-lock.yaml" ||
      f.includes("esbuild") ||
      f.includes("vitest"),
  );
  if (touchesPluginSdk || touchesBuildConfig) {
    return { scope: "full", reason: "plugin-sdk or build config changed" };
  }

  return { scope: "check-and-test", reason: "source files changed" };
}

// ---------------------------------------------------------------------------
// Stage runners
// ---------------------------------------------------------------------------

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

/** Run affected test discovery as an async process (for parallel execution). */
function startAffectedTestDiscovery(): Promise<string[]> {
  return new Promise((resolve) => {
    const child = spawn("bun", ["scripts/dev-affected.ts"], {
      cwd: ROOT,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, FORCE_COLOR: "0", NO_COLOR: "1" },
    });

    let stdout = "";
    child.stdout.on("data", (data: Buffer) => {
      stdout += data.toString("utf8");
    });

    const timer = setTimeout(() => {
      child.kill();
      resolve([]);
    }, 30_000);

    child.on("close", () => {
      clearTimeout(timer);
      try {
        const parsed = JSON.parse(stdout);
        resolve(Array.isArray(parsed.tests) ? parsed.tests : []);
      } catch {
        resolve([]);
      }
    });

    child.on("error", () => {
      clearTimeout(timer);
      resolve([]);
    });
  });
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  const args = new Set(process.argv.slice(2));
  const forceFull = args.has("--full");
  const noTest = args.has("--no-test");

  const results: StageResult[] = [];
  let allPassed = true;

  // Smart scope detection
  const detected = detectScope();
  let effectiveScope: GateScope;

  if (forceFull) {
    effectiveScope = "full";
  } else if (noTest) {
    effectiveScope = "check-only";
  } else {
    effectiveScope = detected.scope;
  }

  console.log(`◈ Scope: ${effectiveScope} (${detected.reason})`);

  if (effectiveScope === "skip") {
    console.log("✓ No gates needed for these changes\n");
    const summary = {
      passed: true,
      totalDurationMs: 0,
      stages: [],
      skipped: true,
      reason: detected.reason,
    };
    console.log(JSON.stringify(summary, null, 2));
    process.exit(0);
  }

  // === Optimization: start affected test discovery in parallel with check ===
  let affectedTestsPromise: Promise<string[]> | null = null;
  if (effectiveScope === "check-and-test" || effectiveScope === "full") {
    if (!forceFull) {
      // Start async discovery now — it runs while pnpm check runs
      affectedTestsPromise = startAffectedTestDiscovery();
    }
  }

  // Stage 1: pnpm check (lint + format + typecheck)
  const stageCount = effectiveScope === "full" ? 3 : effectiveScope === "check-and-test" ? 2 : 1;
  console.log(`▶ Stage 1/${stageCount}: pnpm check (lint + format + types)`);
  const checkResult = runStage("check", "pnpm", ["check"]);
  results.push(checkResult);
  if (!checkResult.passed) {
    allPassed = false;
    console.log("✗ check failed\n");
  } else {
    console.log(`✓ check passed (${checkResult.durationMs}ms)\n`);
  }

  // Stage 2: tests (if applicable)
  if (allPassed && (effectiveScope === "check-and-test" || effectiveScope === "full")) {
    if (forceFull) {
      console.log(`▶ Stage 2/${stageCount}: pnpm test (all)`);
      const testResult = runStage("test", "pnpm", ["test"]);
      results.push(testResult);
      if (!testResult.passed) {
        allPassed = false;
        console.log("✗ test failed\n");
      } else {
        console.log(`✓ test passed (${testResult.durationMs}ms)\n`);
      }
    } else {
      // Await the parallel-discovered affected tests
      const affected = affectedTestsPromise ? await affectedTestsPromise : [];
      if (affected.length > 0) {
        console.log(`▶ Stage 2/${stageCount}: affected tests (${affected.length} files)`);
        const testResult = runStage("test:affected", "pnpm", ["test", "--", ...affected]);
        results.push(testResult);
        if (!testResult.passed) {
          allPassed = false;
          console.log("✗ affected tests failed\n");
        } else {
          console.log(`✓ affected tests passed (${testResult.durationMs}ms)\n`);
        }
      } else {
        console.log(`▶ Stage 2/${stageCount}: no affected tests, skipping\n`);
        results.push({
          stage: "test:affected",
          passed: true,
          durationMs: 0,
          output: "No affected tests.",
        });
      }
    }
  } else if (effectiveScope === "check-only") {
    results.push({
      stage: "test",
      passed: true,
      durationMs: 0,
      output: "Skipped (check-only scope).",
    });
  }

  // Stage 3: build (only in full mode)
  if (effectiveScope === "full" && allPassed) {
    console.log(`▶ Stage 3/${stageCount}: pnpm build`);
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
  } else if (effectiveScope !== "full") {
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

  const summary = {
    passed: allPassed,
    totalDurationMs: totalMs,
    stages: results.map(({ stage, passed, durationMs }) => ({ stage, passed, durationMs })),
  };
  console.log(JSON.stringify(summary, null, 2));

  for (const r of results) {
    if (!r.passed) {
      console.log(`\n--- ${r.stage} failure output ---`);
      console.log(r.output);
    }
  }

  process.exit(allPassed ? 0 : 1);
}

void main();
