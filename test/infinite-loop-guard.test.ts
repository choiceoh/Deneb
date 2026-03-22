import fs from "node:fs";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  BUG_REPORTS_DIR,
  loadSkipList,
  recordTimeout,
  removeFromSkipList,
  saveSkipList,
  shouldSkip,
  SKIP_LIST_PATH,
  toRelativePath,
} from "./infinite-loop-guard.js";

describe("infinite-loop-guard", () => {
  let originalContent: string | null = null;

  beforeEach(() => {
    // Preserve existing skip list if present.
    try {
      originalContent = fs.readFileSync(SKIP_LIST_PATH, "utf-8");
    } catch {
      originalContent = null;
    }
    // Start with a clean slate.
    saveSkipList({ skipped: [] });
  });

  afterEach(() => {
    // Restore original skip list.
    if (originalContent !== null) {
      fs.writeFileSync(SKIP_LIST_PATH, originalContent, "utf-8");
    } else {
      try {
        fs.unlinkSync(SKIP_LIST_PATH);
      } catch {
        // Ignore.
      }
    }
    // Clean up any generated bug reports.
    try {
      const reports = fs.readdirSync(BUG_REPORTS_DIR);
      for (const report of reports) {
        if (report.includes("fake-test")) {
          fs.unlinkSync(path.join(BUG_REPORTS_DIR, report));
        }
      }
    } catch {
      // Directory may not exist.
    }
  });

  describe("loadSkipList", () => {
    it("returns empty list when file is missing", () => {
      try {
        fs.unlinkSync(SKIP_LIST_PATH);
      } catch {
        // Already missing.
      }
      const list = loadSkipList();
      expect(list.skipped).toEqual([]);
    });

    it("returns empty list when file is corrupt", () => {
      fs.writeFileSync(SKIP_LIST_PATH, "not json!!", "utf-8");
      const list = loadSkipList();
      expect(list.skipped).toEqual([]);
    });

    it("loads saved entries", () => {
      saveSkipList({
        skipped: [
          {
            file: "src/foo.test.ts",
            testName: "bar",
            reason: "timeout",
            firstSeen: "2026-01-01T00:00:00.000Z",
            lastSeen: "2026-01-01T00:00:00.000Z",
          },
        ],
      });
      const list = loadSkipList();
      expect(list.skipped).toHaveLength(1);
      expect(list.skipped[0].file).toBe("src/foo.test.ts");
    });
  });

  describe("toRelativePath", () => {
    it("converts absolute path to repo-relative", () => {
      const abs = path.resolve("src/foo.test.ts");
      expect(toRelativePath(abs)).toBe("src/foo.test.ts");
    });

    it("passes through relative paths unchanged", () => {
      expect(toRelativePath("src/foo.test.ts")).toBe("src/foo.test.ts");
    });
  });

  describe("recordTimeout", () => {
    it("adds a new entry and returns true", () => {
      const isNew = recordTimeout("src/fake-test.test.ts", "suite > hangs forever", 30_000);
      expect(isNew).toBe(true);

      const list = loadSkipList();
      expect(list.skipped).toHaveLength(1);
      expect(list.skipped[0].file).toBe("src/fake-test.test.ts");
      expect(list.skipped[0].testName).toBe("suite > hangs forever");
      expect(list.skipped[0].reason).toContain("30000ms");
    });

    it("updates lastSeen for duplicate and returns false", () => {
      recordTimeout("src/fake-test.test.ts", "suite > hangs forever", 30_000);
      const isNew = recordTimeout("src/fake-test.test.ts", "suite > hangs forever", 30_000);
      expect(isNew).toBe(false);

      const list = loadSkipList();
      expect(list.skipped).toHaveLength(1);
    });

    it("generates a bug report file", () => {
      recordTimeout("src/fake-test.test.ts", "it loops", 30_000, "Test timed out in 30000ms.");
      const reports = fs.readdirSync(BUG_REPORTS_DIR);
      const match = reports.find((r) => r.includes("fake-test"));
      expect(match).toBeDefined();

      const content = fs.readFileSync(path.join(BUG_REPORTS_DIR, match!), "utf-8");
      expect(content).toContain("src/fake-test.test.ts");
      expect(content).toContain("it loops");
      expect(content).toContain("30000ms");
    });
  });

  describe("shouldSkip", () => {
    it("returns undefined for unknown tests", () => {
      expect(shouldSkip("src/foo.test.ts", "never recorded")).toBeUndefined();
    });

    it("returns the entry for a recorded test", () => {
      recordTimeout("src/fake-test.test.ts", "bad test", 30_000);
      const entry = shouldSkip("src/fake-test.test.ts", "bad test");
      expect(entry).toBeDefined();
      expect(entry!.testName).toBe("bad test");
    });
  });

  describe("removeFromSkipList", () => {
    it("removes an existing entry and returns true", () => {
      recordTimeout("src/fake-test.test.ts", "bad test", 30_000);
      const removed = removeFromSkipList("src/fake-test.test.ts", "bad test");
      expect(removed).toBe(true);
      expect(shouldSkip("src/fake-test.test.ts", "bad test")).toBeUndefined();
    });

    it("returns false when entry does not exist", () => {
      expect(removeFromSkipList("src/nope.test.ts", "nope")).toBe(false);
    });
  });
});
