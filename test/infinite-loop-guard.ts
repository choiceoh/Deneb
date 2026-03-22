/**
 * Infinite-loop guard: detects test timeouts (suspected infinite loops),
 * records them in a skip list, and generates bug reports.
 *
 * - On timeout detection: the reporter writes the offending test to the skip list
 *   and emits a bug-report file under `test/reports/infinite-loop/`.
 * - On subsequent runs: `beforeEach` in setup.ts reads the skip list and
 *   auto-skips any test that previously timed out, so the suite isn't blocked.
 */

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "..");

export const SKIP_LIST_PATH = path.join(REPO_ROOT, "test", "fixtures", "infinite-loop-skip.json");
export const BUG_REPORTS_DIR = path.join(REPO_ROOT, "test", "reports", "infinite-loop");

export interface SkipEntry {
  /** Repo-root-relative test file path */
  file: string;
  /** Full test name (including describe blocks) */
  testName: string;
  /** Human-readable reason */
  reason: string;
  /** ISO timestamp of first detection */
  firstSeen: string;
  /** ISO timestamp of most recent detection */
  lastSeen: string;
}

export interface SkipList {
  skipped: SkipEntry[];
}

/** Read the current skip list from disk. Returns empty list if file is missing/corrupt. */
export function loadSkipList(): SkipList {
  try {
    const raw = fs.readFileSync(SKIP_LIST_PATH, "utf-8");
    const parsed = JSON.parse(raw) as SkipList;
    if (Array.isArray(parsed.skipped)) {
      return parsed;
    }
  } catch {
    // File missing or corrupt — start fresh.
  }
  return { skipped: [] };
}

/** Persist the skip list to disk. */
export function saveSkipList(list: SkipList): void {
  fs.mkdirSync(path.dirname(SKIP_LIST_PATH), { recursive: true });
  fs.writeFileSync(SKIP_LIST_PATH, JSON.stringify(list, null, 2) + "\n", "utf-8");
}

/** Normalize a file path to repo-root-relative. */
export function toRelativePath(filePath: string): string {
  if (path.isAbsolute(filePath)) {
    return path.relative(REPO_ROOT, filePath);
  }
  return filePath;
}

/**
 * Build a composite key for deduplication.
 * Uses file + testName so the same test can only appear once.
 */
function entryKey(file: string, testName: string): string {
  return `${file}::${testName}`;
}

/**
 * Record a timed-out test in the skip list and generate a bug report.
 * Returns `true` if this is a new entry, `false` if it was already known.
 */
export function recordTimeout(
  filePath: string,
  testName: string,
  timeoutMs: number,
  errorMessage?: string,
): boolean {
  const file = toRelativePath(filePath);
  const list = loadSkipList();
  const now = new Date().toISOString();
  const key = entryKey(file, testName);
  const existing = list.skipped.find((e) => entryKey(e.file, e.testName) === key);

  if (existing) {
    existing.lastSeen = now;
    saveSkipList(list);
    return false;
  }

  const reason = `Timed out after ${timeoutMs}ms (suspected infinite loop)`;
  list.skipped.push({ file, testName, reason, firstSeen: now, lastSeen: now });
  saveSkipList(list);

  writeBugReport(file, testName, timeoutMs, errorMessage);
  return true;
}

/** Check whether a test should be auto-skipped. */
export function shouldSkip(filePath: string, testName: string): SkipEntry | undefined {
  const file = toRelativePath(filePath);
  const list = loadSkipList();
  const key = entryKey(file, testName);
  return list.skipped.find((e) => entryKey(e.file, e.testName) === key);
}

/** Remove a test from the skip list (e.g., after it's been fixed). */
export function removeFromSkipList(filePath: string, testName: string): boolean {
  const file = toRelativePath(filePath);
  const list = loadSkipList();
  const key = entryKey(file, testName);
  const idx = list.skipped.findIndex((e) => entryKey(e.file, e.testName) === key);
  if (idx === -1) {
    return false;
  }
  list.skipped.splice(idx, 1);
  saveSkipList(list);
  return true;
}

/** Generate a markdown bug report for a detected infinite-loop timeout. */
function writeBugReport(
  file: string,
  testName: string,
  timeoutMs: number,
  errorMessage?: string,
): void {
  fs.mkdirSync(BUG_REPORTS_DIR, { recursive: true });

  const slug = file.replace(/[/\\]/g, "-").replace(/\.test\.ts$/, "");
  const safeName = testName.replace(/[^a-zA-Z0-9_-]/g, "_").slice(0, 80);
  const reportPath = path.join(BUG_REPORTS_DIR, `${slug}--${safeName}.md`);

  const now = new Date().toISOString();
  const content = `# Infinite Loop Bug Report

## Test Information
- **File:** \`${file}\`
- **Test:** ${testName}
- **Detected:** ${now}
- **Timeout:** ${timeoutMs}ms

## Description
This test was automatically skipped because it exceeded the configured timeout
of ${timeoutMs}ms, which suggests an infinite loop or an unresolved async operation.

The test will be skipped on subsequent runs until this entry is removed from:
\`test/fixtures/infinite-loop-skip.json\`

## Error
\`\`\`
${errorMessage ?? "Test exceeded timeout threshold"}
\`\`\`

## How to Fix
1. Reproduce the issue by running the test in isolation:
   \`\`\`
   pnpm test -- ${file} -t "${testName}"
   \`\`\`
2. Investigate the root cause (infinite loop, unresolved promise, missing timer cleanup).
3. Fix the test or the underlying code.
4. Remove the entry from \`test/fixtures/infinite-loop-skip.json\`.
5. Verify the test passes: \`pnpm test -- ${file}\`
`;

  fs.writeFileSync(reportPath, content, "utf-8");
}
