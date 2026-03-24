#!/usr/bin/env bun
/**
 * Highway Test Runner — integration bridge between the Rust highway engine
 * and the existing pnpm test infrastructure.
 *
 * Usage:
 *   bun scripts/highway-test.ts                    # run affected tests (git diff)
 *   bun scripts/highway-test.ts --all              # run all tests
 *   bun scripts/highway-test.ts --files src/a.ts   # run tests affected by specific files
 *   bun scripts/highway-test.ts --dry-run           # preview only
 *   bun scripts/highway-test.ts --no-cache          # skip cache
 *   bun scripts/highway-test.ts --verbose           # show vitest output
 */

import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve, join } from "node:path";

const ROOT = resolve(import.meta.dirname, "..");
const HIGHWAY_BIN = join(ROOT, "tools/highway/target/release/highway");
const HIGHWAY_DEBUG_BIN = join(ROOT, "tools/highway/target/debug/highway");

function getHighwayBin(): string {
  if (existsSync(HIGHWAY_BIN)) {
    return HIGHWAY_BIN;
  }
  if (existsSync(HIGHWAY_DEBUG_BIN)) {
    return HIGHWAY_DEBUG_BIN;
  }

  console.error("⚡ Highway binary not found. Building...");
  const buildResult = spawnSync("cargo", ["build", "--release"], {
    cwd: join(ROOT, "tools/highway"),
    stdio: "inherit",
  });

  if (buildResult.status !== 0) {
    console.error("Failed to build highway. Falling back to pnpm test.");
    return "";
  }

  return HIGHWAY_BIN;
}

function parseArgs(): {
  all: boolean;
  files: string[];
  dryRun: boolean;
  noCache: boolean;
  verbose: boolean;
  format: string;
} {
  const args = process.argv.slice(2);
  const opts = {
    all: false,
    files: [] as string[],
    dryRun: false,
    noCache: false,
    verbose: false,
    format: "text",
  };

  let i = 0;
  while (i < args.length) {
    switch (args[i]) {
      case "--all":
        opts.all = true;
        break;
      case "--dry-run":
        opts.dryRun = true;
        break;
      case "--no-cache":
        opts.noCache = true;
        break;
      case "--verbose":
        opts.verbose = true;
        break;
      case "--format":
        opts.format = args[++i] || "text";
        break;
      case "--files":
        i++;
        while (i < args.length && !args[i].startsWith("--")) {
          opts.files.push(args[i]);
          i++;
        }
        continue;
      default:
        // Treat as file path
        if (!args[i].startsWith("--")) {
          opts.files.push(args[i]);
        }
    }
    i++;
  }

  return opts;
}

function main() {
  const bin = getHighwayBin();
  if (!bin) {
    // Fallback to pnpm test
    const result = spawnSync("pnpm", ["test"], {
      cwd: ROOT,
      stdio: "inherit",
    });
    process.exit(result.status ?? 1);
  }

  const opts = parseArgs();

  // Build highway command
  const args: string[] = ["--root", ROOT, "--format", opts.format];

  if (opts.all) {
    args.push("run");
  } else if (opts.files.length > 0) {
    args.push("run", ...opts.files);
  } else {
    args.push("run", "--git");
  }

  if (opts.dryRun) {
    args.push("--dry-run");
  }
  if (opts.noCache) {
    args.push("--no-cache");
  }
  if (opts.verbose) {
    args.push("--verbose");
  }

  console.log(`⚡ highway ${args.slice(3).join(" ")}`);

  const result = spawnSync(bin, args, {
    cwd: ROOT,
    stdio: "inherit",
    env: { ...process.env },
  });

  process.exit(result.status ?? 1);
}

main();
