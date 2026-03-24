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

/** Batch-find dependents for all changed files in a single grep call. */
function findAllDependents(changedFiles: string[]): Map<string, string[]> {
  const result = new Map<string, string[]>();
  if (changedFiles.length === 0) {
    return result;
  }

  // Deduplicate basenames and map back to files
  const basenameToFiles = new Map<string, string[]>();
  for (const f of changedFiles) {
    const name = path.parse(f).name;
    const existing = basenameToFiles.get(name);
    if (existing) {
      existing.push(f);
    } else {
      basenameToFiles.set(name, [f]);
    }
    result.set(f, []);
  }

  // Single grep call with alternation pattern for all basenames
  const basenames = [...basenameToFiles.keys()];
  // Escape regex special chars in basenames
  const escaped = basenames.map((b) => b.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const pattern = escaped.length === 1 ? escaped[0] : `(${escaped.join("|")})`;

  try {
    const grepResult = execSync(
      `grep -rln --include='*.ts' --include='*.tsx' -E '${pattern}' src/ extensions/ 2>/dev/null || true`,
      { cwd: ROOT, encoding: "utf8", timeout: 30_000 },
    );

    // Parse grep -n output (file:line:content) and match basenames
    const matchedFiles = new Set<string>();
    for (const line of grepResult.split("\n")) {
      const filePath = line.split(":")[0]?.trim();
      if (
        filePath &&
        !filePath.endsWith(".test.ts") &&
        !filePath.endsWith(".e2e.test.ts") &&
        !filePath.includes("node_modules")
      ) {
        matchedFiles.add(filePath);
      }
    }

    // Assign matched files only to source files whose basename actually appears
    for (const matchedFile of matchedFiles) {
      let content: string;
      try {
        content = fs.readFileSync(path.resolve(ROOT, matchedFile), "utf8");
      } catch {
        continue;
      }
      for (const [basename, sourceFiles] of basenameToFiles) {
        if (!content.includes(basename)) {
          continue;
        }
        for (const sourceFile of sourceFiles) {
          if (matchedFile !== sourceFile) {
            result.get(sourceFile)!.push(matchedFile);
          }
        }
      }
    }
  } catch {
    // ignore
  }

  return result;
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

  // Batch-find dependents for all changed files in a single grep call
  const dependentsMap = findAllDependents(changed);

  for (const file of changed) {
    const tests = findTestFiles(file);
    const dependents = dependentsMap.get(file) ?? [];
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
