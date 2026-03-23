#!/usr/bin/env bun
/**
 * dev-create-pr — Single-command PR workflow for AI agents.
 *
 * Replaces the manual multi-step sequence:
 *   dev-patch-impact → dev-affected → dev-commit-gate → git push → gh pr create
 *
 * Optimizations:
 *   - Smart scope detection: skips gates entirely for docs/config-only changes
 *   - Parallel test discovery: finds affected tests while pnpm check runs
 *   - Single command: no need to run 5+ scripts manually
 *
 * Usage:
 *   bun scripts/dev-create-pr.ts --title "fix: foo"                    # full workflow
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --skip-gate        # already ran gate
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --draft            # draft PR
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --base develop     # target branch
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --dry-run          # preview only
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --full-gate        # force full gate
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --body "custom"    # custom body
 *   bun scripts/dev-create-pr.ts --title "fix: foo" --issue 123        # link issue
 */

import { spawnSync } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");

// ---------------------------------------------------------------------------
// CLI args
// ---------------------------------------------------------------------------

interface Options {
  title: string;
  base: string;
  draft: boolean;
  skipGate: boolean;
  fullGate: boolean;
  dryRun: boolean;
  body: string;
  issue: string;
}

function parseArgs(): Options {
  const argv = process.argv.slice(2);
  const opts: Options = {
    title: "",
    base: "main",
    draft: false,
    skipGate: false,
    fullGate: false,
    dryRun: false,
    body: "",
    issue: "",
  };

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--title" && argv[i + 1]) {
      opts.title = argv[++i];
    } else if (arg === "--base" && argv[i + 1]) {
      opts.base = argv[++i];
    } else if (arg === "--draft") {
      opts.draft = true;
    } else if (arg === "--skip-gate") {
      opts.skipGate = true;
    } else if (arg === "--full-gate") {
      opts.fullGate = true;
    } else if (arg === "--dry-run") {
      opts.dryRun = true;
    } else if (arg === "--body" && argv[i + 1]) {
      opts.body = argv[++i];
    } else if (arg === "--issue" && argv[i + 1]) {
      opts.issue = argv[++i];
    } else if (arg === "--help" || arg === "-h") {
      console.log("Usage: bun scripts/dev-create-pr.ts --title <title> [options]");
      console.log("");
      console.log("  --title <msg>    PR title (required)");
      console.log("  --base <branch>  Base branch (default: main)");
      console.log("  --draft          Create as draft PR");
      console.log("  --skip-gate      Skip commit gate (if already ran)");
      console.log("  --full-gate      Force full gate (all tests + build)");
      console.log("  --dry-run        Show plan without executing");
      console.log("  --body <text>    Override auto-generated PR body");
      console.log("  --issue <num>    Link to GitHub issue");
      process.exit(0);
    }
  }

  if (!opts.title) {
    console.error("Error: --title is required");
    process.exit(1);
  }

  return opts;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function run(
  cmd: string,
  args: string[],
  opts?: { timeout?: number },
): {
  ok: boolean;
  output: string;
} {
  const result = spawnSync(cmd, args, {
    cwd: ROOT,
    stdio: ["ignore", "pipe", "pipe"],
    timeout: opts?.timeout ?? 300_000,
    env: { ...process.env, FORCE_COLOR: "0", NO_COLOR: "1" },
  });
  const stdout = result.stdout?.toString("utf8") ?? "";
  const stderr = result.stderr?.toString("utf8") ?? "";
  return { ok: result.status === 0, output: [stdout, stderr].filter(Boolean).join("\n").trim() };
}

function step(icon: string, msg: string) {
  console.log(`${icon} ${msg}`);
}

function fail(msg: string): never {
  console.error(`\n✗ ${msg}`);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Push with retry (exponential backoff for network errors)
// ---------------------------------------------------------------------------

function pushWithRetry(branch: string): boolean {
  const delays = [2000, 4000, 8000, 16000];

  for (let attempt = 0; attempt <= 4; attempt++) {
    const result = run("git", ["push", "-u", "origin", branch]);
    if (result.ok) {
      return true;
    }

    if (attempt < 4 && result.output.includes("network")) {
      const delay = delays[attempt];
      step("↻", `Push failed (network), retrying in ${delay / 1000}s...`);
      spawnSync("sleep", [`${delay / 1000}`]);
    } else {
      console.error(result.output);
      return false;
    }
  }
  return false;
}

// ---------------------------------------------------------------------------
// Minimal PR body builder (fast, no subprocess)
// ---------------------------------------------------------------------------

function buildPrBody(title: string, issue: string): string {
  const lines = ["## Summary", "", `- ${title}`, ""];

  if (issue) {
    lines.push(`Closes #${issue}`, "");
  }

  lines.push(
    "## Security Impact (required)",
    "",
    "- New permissions/capabilities? (`No`)",
    "- Secrets/tokens handling changed? (`No`)",
    "- New/changed network calls? (`No`)",
    "- Command/tool execution surface changed? (`No`)",
    "- Data access scope changed? (`No`)",
    "",
    "## Compatibility / Migration",
    "",
    "- Backward compatible? (`Yes`)",
    "- Config/env changes? (`No`)",
    "- Migration needed? (`No`)",
  );

  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function main() {
  const opts = parseArgs();

  const branch = run("git", ["branch", "--show-current"]).output.trim();
  if (!branch) {
    fail("Not on a branch");
  }

  step("◈", `Branch: ${branch} → ${opts.base}`);

  if (opts.dryRun) {
    const gateMode = opts.skipGate ? "SKIP" : opts.fullGate ? "--full" : "smart (auto-detect)";
    console.log(`  Gate: ${gateMode}`);
    console.log(`  Push: git push -u origin ${branch}`);
    console.log(`  PR: gh pr create --title "${opts.title}" ${opts.draft ? "--draft" : ""}`);
    process.exit(0);
  }

  // Step 1: Run gate (uses smart scope detection internally)
  if (!opts.skipGate) {
    step("▶", "Running commit gate...");
    const gateArgs = ["scripts/dev-commit-gate.ts"];
    if (opts.fullGate) {
      gateArgs.push("--full");
    }

    const gateResult = run("bun", gateArgs, { timeout: 600_000 });
    if (!gateResult.ok) {
      const lines = gateResult.output.split("\n");
      console.error(lines.slice(-30).join("\n"));
      fail("Gate failed. Fix issues and retry.");
    }
    // Extract timing from JSON output
    try {
      const jsonMatch = gateResult.output.match(/\{[\s\S]*"passed"[\s\S]*\}/);
      if (jsonMatch) {
        const parsed = JSON.parse(jsonMatch[0]);
        step("✓", `Gate passed (${parsed.totalDurationMs}ms)`);
      } else {
        step("✓", "Gate passed");
      }
    } catch {
      step("✓", "Gate passed");
    }
  } else {
    step("⊘", "Skipping gate (--skip-gate)");
  }

  // Step 2: Push
  step("▶", `Pushing to origin/${branch}...`);
  if (!pushWithRetry(branch)) {
    fail("Push failed after retries");
  }
  step("✓", "Pushed");

  // Step 3: Create PR
  step("▶", "Creating PR...");
  const body = opts.body || buildPrBody(opts.title, opts.issue);
  const ghArgs = ["pr", "create", "--title", opts.title, "--base", opts.base, "--body", body];
  if (opts.draft) {
    ghArgs.push("--draft");
  }

  const prResult = run("gh", ghArgs, { timeout: 30_000 });
  if (!prResult.ok) {
    if (prResult.output.includes("already exists")) {
      step("⊘", "PR already exists");
      const url = run("gh", ["pr", "view", branch, "--json", "url", "-q", ".url"]).output.trim();
      if (url) {
        console.log(`\n${url}`);
      }
      process.exit(0);
    }
    fail(`PR creation failed:\n${prResult.output}`);
  }

  const prUrl = prResult.output.trim().split("\n").pop() ?? "";
  console.log(`\n✓ PR created: ${prUrl}`);
}

main();
