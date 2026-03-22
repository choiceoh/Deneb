#!/usr/bin/env bun
/**
 * dev-affected — Analyze changed files and find affected modules + tests.
 *
 * Usage:
 *   bun scripts/dev-affected.ts              # uses git diff (unstaged + staged)
 *   bun scripts/dev-affected.ts src/foo.ts   # analyze specific files
 *
 * Output: JSON with affected files, their test files, and importers (dependents).
 */

import { execSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");

// -------------------------------------------------------------------------
// Collect changed files from git or CLI args
// -------------------------------------------------------------------------

function getChangedFiles(args: string[]): string[] {
  if (args.length > 0) {
    return args.map((f) => (path.isAbsolute(f) ? path.relative(ROOT, f) : f));
  }
  const staged = execSync("git diff --cached --name-only --diff-filter=ACMR", {
    cwd: ROOT,
    encoding: "utf8",
  }).trim();
  const unstaged = execSync("git diff --name-only --diff-filter=ACMR", {
    cwd: ROOT,
    encoding: "utf8",
  }).trim();
  const untracked = execSync("git ls-files --others --exclude-standard", {
    cwd: ROOT,
    encoding: "utf8",
  }).trim();
  const all = [staged, unstaged, untracked]
    .flatMap((s) => s.split("\n"))
    .map((l) => l.trim())
    .filter((l) => l && /\.(?:[cm]?ts|[cm]?js|tsx|jsx)$/u.test(l));
  return [...new Set(all)];
}

// -------------------------------------------------------------------------
// Import graph helpers
// -------------------------------------------------------------------------

const IMPORT_RE =
  /(?:import|export)\s.*?from\s+["']([^"']+)["']|(?:import|require)\s*\(\s*["']([^"']+)["']\s*\)/g;

function extractImports(filePath: string): string[] {
  let content: string;
  try {
    content = fs.readFileSync(path.resolve(ROOT, filePath), "utf8");
  } catch {
    return [];
  }
  const imports: string[] = [];
  let m: RegExpExecArray | null;
  while ((m = IMPORT_RE.exec(content)) !== null) {
    const spec = m[1] ?? m[2];
    if (spec) {
      imports.push(spec);
    }
  }
  return imports;
}

/** Resolve a relative import specifier to a repo-root-relative path. */
function resolveRelativeImport(fromFile: string, spec: string): string | null {
  if (!spec.startsWith(".")) {
    return null;
  }
  const dir = path.dirname(fromFile);
  let resolved = path.normalize(path.join(dir, spec));
  // Strip .js/.ts extension and try to find the real file
  resolved = resolved.replace(/\.(js|ts|jsx|tsx)$/, "");
  for (const ext of [".ts", ".tsx", ".js", ".jsx", "/index.ts", "/index.js"]) {
    const candidate = resolved + ext;
    if (fs.existsSync(path.resolve(ROOT, candidate))) {
      return candidate;
    }
  }
  // Try the original (might already be extensionless directory)
  if (fs.existsSync(path.resolve(ROOT, resolved))) {
    return resolved;
  }
  return null;
}

// -------------------------------------------------------------------------
// Find test files for a source file
// -------------------------------------------------------------------------

function findTestFiles(filePath: string): string[] {
  const parsed = path.parse(filePath);
  const candidates = [
    path.join(parsed.dir, `${parsed.name}.test.ts`),
    path.join(parsed.dir, `${parsed.name}.test.tsx`),
    path.join(parsed.dir, `${parsed.name}.e2e.test.ts`),
  ];
  return candidates.filter((c) => fs.existsSync(path.resolve(ROOT, c)));
}

// -------------------------------------------------------------------------
// Find dependents (files that import the changed file) via grep
// -------------------------------------------------------------------------

function findDependents(filePath: string): string[] {
  const basename = path.parse(filePath).name;
  // Search for imports referencing this file's basename
  try {
    const result = execSync(
      `grep -rl --include='*.ts' --include='*.tsx' '${basename}' src/ extensions/ 2>/dev/null || true`,
      { cwd: ROOT, encoding: "utf8", timeout: 15_000 },
    );
    return result
      .split("\n")
      .map((l) => l.trim())
      .filter(
        (l) =>
          l &&
          l !== filePath &&
          !l.endsWith(".test.ts") &&
          !l.endsWith(".e2e.test.ts") &&
          !l.includes("node_modules"),
      );
  } catch {
    return [];
  }
}

// -------------------------------------------------------------------------
// Main
// -------------------------------------------------------------------------

function main() {
  const args = process.argv.slice(2);
  const changed = getChangedFiles(args);

  if (changed.length === 0) {
    console.log(
      JSON.stringify(
        { changed: [], affected: [], tests: [], summary: "No changed TS/JS files detected." },
        null,
        2,
      ),
    );
    return;
  }

  const allTests = new Set<string>();
  const allDependents = new Set<string>();
  const details: Record<string, { tests: string[]; dependents: string[]; imports: string[] }> = {};

  for (const file of changed) {
    const tests = findTestFiles(file);
    const dependents = findDependents(file);
    const imports = extractImports(file)
      .map((spec) => resolveRelativeImport(file, spec))
      .filter((r): r is string => r !== null);

    details[file] = { tests, dependents, imports };
    for (const t of tests) {
      allTests.add(t);
    }
    for (const d of dependents) {
      allDependents.add(d);
    }
  }

  // Also find tests for dependents (transitive)
  for (const dep of allDependents) {
    for (const t of findTestFiles(dep)) {
      allTests.add(t);
    }
  }

  const output = {
    changed,
    tests: [...allTests].toSorted(),
    dependents: [...allDependents].toSorted(),
    details,
    summary: `${changed.length} changed → ${allTests.size} test files, ${allDependents.size} dependents`,
    testCommand: allTests.size > 0 ? `pnpm test -- ${[...allTests].join(" ")}` : null,
  };

  console.log(JSON.stringify(output, null, 2));
}

main();
