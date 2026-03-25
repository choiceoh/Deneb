#!/usr/bin/env bun
/**
 * Build-time extension compilation.
 *
 * Pre-compiles bundled extension plugins during `pnpm build` so they don't
 * need to be compiled at gateway startup. This significantly reduces
 * cold-start time (seconds instead of tens of seconds for heavy extensions).
 *
 * Each extension in extensions/* is compiled to dist-runtime/extensions/<id>/
 * as a single-file ESM bundle.
 *
 * Usage:
 *   bun scripts/compile-extensions.ts [--only telegram] [--verbose]
 */

import { execSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";

const ROOT = resolve(import.meta.dirname, "..");
const EXTENSIONS_DIR = join(ROOT, "extensions");
const OUTPUT_DIR = join(ROOT, "dist-runtime", "extensions");

const args = process.argv.slice(2);
const verbose = args.includes("--verbose");
const onlyIdx = args.indexOf("--only");
const onlyFilter = onlyIdx >= 0 ? args[onlyIdx + 1] : undefined;

function log(msg: string) {
  console.log(`[compile-extensions] ${msg}`);
}

function findBundledExtensions(): string[] {
  if (!existsSync(EXTENSIONS_DIR)) {
    return [];
  }
  return readdirSync(EXTENSIONS_DIR, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .filter((name) => {
      // Skip shared utilities directory.
      if (name === "shared") {
        return false;
      }
      // Check for package.json with an entry point.
      const pkgPath = join(EXTENSIONS_DIR, name, "package.json");
      if (!existsSync(pkgPath)) {
        return false;
      }
      if (onlyFilter && name !== onlyFilter) {
        return false;
      }
      return true;
    });
}

function compileExtension(name: string): boolean {
  const extDir = join(EXTENSIONS_DIR, name);
  const outDir = join(OUTPUT_DIR, name);
  const entryPoint = join(extDir, "index.ts");

  if (!existsSync(entryPoint)) {
    // Try src/index.ts
    const srcEntry = join(extDir, "src", "index.ts");
    if (!existsSync(srcEntry)) {
      if (verbose) {
        log(`  skipping ${name}: no entry point found`);
      }
      return false;
    }
  }

  mkdirSync(outDir, { recursive: true });

  try {
    // Use esbuild for fast single-file bundling.
    // External: all node_modules and deneb/plugin-sdk/* imports.
    const entry = existsSync(entryPoint) ? entryPoint : join(extDir, "src", "index.ts");

    execSync(
      [
        "npx",
        "esbuild",
        entry,
        "--bundle",
        "--format=esm",
        "--platform=node",
        "--target=node22",
        `--outdir=${outDir}`,
        "--sourcemap",
        "--external:deneb/*",
        "--external:@grammyjs/*",
        "--external:grammy",
        "--external:undici",
        "--external:node:*",
      ].join(" "),
      {
        cwd: ROOT,
        stdio: verbose ? "inherit" : "pipe",
        timeout: 60_000,
      },
    );

    log(`  ✓ ${name}`);
    return true;
  } catch (err) {
    log(`  ✗ ${name}: ${err instanceof Error ? err.message : String(err)}`);
    return false;
  }
}

function main() {
  const extensions = findBundledExtensions();
  if (extensions.length === 0) {
    log("no extensions to compile");
    return;
  }

  log(`compiling ${extensions.length} extension(s)...`);
  mkdirSync(OUTPUT_DIR, { recursive: true });

  let success = 0;
  let failed = 0;
  for (const name of extensions) {
    if (compileExtension(name)) {
      success++;
    } else {
      failed++;
    }
  }

  log(`done: ${success} compiled, ${failed} skipped/failed`);
}

main();
