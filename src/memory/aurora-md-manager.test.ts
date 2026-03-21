import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  contentId,
  formatEntry,
  AuroraMdManager,
  parseEntryLine,
  parseSections,
  type AuroraEntry,
} from "./aurora-md-manager.js";

let tmpDir: string;
let mgr: AuroraMdManager;

beforeEach(async () => {
  tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "aurora-md-mgr-"));
  mgr = new AuroraMdManager(tmpDir, "UTC");
});

afterEach(async () => {
  await fs.rm(tmpDir, { recursive: true, force: true });
});

// ---------------------------------------------------------------------------
// Entry format round-trip
// ---------------------------------------------------------------------------

describe("formatEntry / parseEntryLine", () => {
  it("round-trips a basic entry", () => {
    const entry: AuroraEntry = {
      id: "abcd1234",
      timestamp: "2026-03-21T10:00:00.000Z",
      content: "Use TypeScript for the project",
      tags: [],
      importance: "normal",
    };
    const line = formatEntry(entry);
    expect(line).toBe(
      "- [id:abcd1234] **2026-03-21T10:00:00.000Z** Use TypeScript for the project",
    );
    const parsed = parseEntryLine(line);
    expect(parsed).toEqual(entry);
  });

  it("round-trips entry with tags and importance", () => {
    const entry: AuroraEntry = {
      id: "ef567890",
      timestamp: "2026-03-21T10:00:00.000Z",
      content: "Deploy to production daily",
      tags: ["ops", "deploy"],
      importance: "high",
    };
    const line = formatEntry(entry);
    expect(line).toContain("{high}");
    expect(line).toContain("#ops #deploy");

    const parsed = parseEntryLine(line);
    expect(parsed?.importance).toBe("high");
    expect(parsed?.tags).toEqual(["ops", "deploy"]);
    expect(parsed?.content).toBe("Deploy to production daily");
  });

  it("omits importance marker for normal", () => {
    const line = formatEntry({
      id: "aa",
      timestamp: "2026-01-01T00:00:00Z",
      content: "test",
      tags: [],
      importance: "normal",
    });
    expect(line).not.toContain("{normal}");
  });

  it("returns null for non-entry lines", () => {
    expect(parseEntryLine("## Heading")).toBeNull();
    expect(parseEntryLine("random text")).toBeNull();
    expect(parseEntryLine("- plain list item")).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// contentId
// ---------------------------------------------------------------------------

describe("contentId", () => {
  it("produces stable 8-char hex IDs", () => {
    const id = contentId("hello world");
    expect(id).toHaveLength(8);
    expect(id).toMatch(/^[a-f0-9]+$/);
    expect(contentId("hello world")).toBe(id);
  });

  it("is case-insensitive", () => {
    expect(contentId("Hello World")).toBe(contentId("hello world"));
  });

  it("trims whitespace", () => {
    expect(contentId("  hello  ")).toBe(contentId("hello"));
  });
});

// ---------------------------------------------------------------------------
// Save
// ---------------------------------------------------------------------------

describe("save", () => {
  it("creates daily file and entry on first save", async () => {
    const result = await mgr.save("Remember to check tests", {
      timestamp: "2026-03-21T10:00:00Z",
    });

    expect(result.ok).toBe(true);
    expect(result.action).toBe("created");
    expect(result.id).toHaveLength(8);
    expect(result.file).toMatch(/^memory\/\d{4}-\d{2}-\d{2}\.md$/);
    expect(result.entry.content).toBe("Remember to check tests");
  });

  it("detects exact duplicates", async () => {
    await mgr.save("Same thing", { timestamp: "2026-03-21T10:00:00Z" });
    const result = await mgr.save("Same thing", { timestamp: "2026-03-21T11:00:00Z" });

    expect(result.action).toBe("duplicate");
  });

  it("detects fuzzy duplicates and upserts", async () => {
    await mgr.save("The project uses TypeScript and pnpm for building and testing all modules", {
      timestamp: "2026-03-21T10:00:00Z",
      tags: ["tooling"],
    });
    const result = await mgr.save(
      "The project uses TypeScript and pnpm for building and testing all the modules",
      {
        timestamp: "2026-03-21T11:00:00Z",
        tags: ["tech"],
      },
    );

    expect(result.action).toBe("updated");
    // Tags should be merged
    expect(result.entry.tags).toContain("tooling");
    expect(result.entry.tags).toContain("tech");
  });

  it("saves with importance and tags", async () => {
    const result = await mgr.save("Never push to main without tests", {
      importance: "critical",
      tags: ["ci", "rules"],
      timestamp: "2026-03-21T10:00:00Z",
    });

    expect(result.entry.importance).toBe("critical");
    expect(result.entry.tags).toEqual(["ci", "rules"]);
  });

  it("rejects writes to MEMORY.md", async () => {
    await expect(mgr.save("nope", { file: "MEMORY.md" })).rejects.toThrow("read-only");
  });

  it("allows explicit file target", async () => {
    const result = await mgr.save("Archived note", {
      file: "memory/archive.md",
      timestamp: "2026-03-21T10:00:00Z",
    });

    expect(result.file).toBe("memory/archive.md");
    const content = await fs.readFile(path.join(tmpDir, "memory/archive.md"), "utf-8");
    expect(content).toContain("Archived note");
  });

  it("handles empty content gracefully", async () => {
    const result = await mgr.save("  ");
    expect(result.action).toBe("duplicate");
    expect(result.id).toBe("");
  });
});

// ---------------------------------------------------------------------------
// Recall
// ---------------------------------------------------------------------------

describe("recall", () => {
  beforeEach(async () => {
    const file = mgr.dailyFilePath();
    await mgr.save("First entry about testing", {
      file,
      tags: ["testing"],
      importance: "normal",
      timestamp: "2026-03-21T09:00:00Z",
    });
    await mgr.save("Second entry about deployment", {
      file,
      tags: ["ops"],
      importance: "high",
      timestamp: "2026-03-21T10:00:00Z",
    });
    await mgr.save("Third low-priority note", {
      file,
      tags: ["misc"],
      importance: "low",
      timestamp: "2026-03-21T11:00:00Z",
    });
  });

  it("returns all entries from daily file", async () => {
    const result = await mgr.recall();
    expect(result.entries).toHaveLength(3);
    expect(result.total).toBe(3);
  });

  it("filters by tags", async () => {
    const result = await mgr.recall({ tags: ["ops"] });
    expect(result.entries).toHaveLength(1);
    expect(result.entries[0].content).toContain("deployment");
  });

  it("filters by minimum importance", async () => {
    const result = await mgr.recall({ minImportance: "high" });
    expect(result.entries).toHaveLength(1);
    expect(result.entries[0].content).toContain("deployment");
  });

  it("filters by content substring", async () => {
    const result = await mgr.recall({ contains: "testing" });
    expect(result.entries).toHaveLength(1);
    expect(result.entries[0].content).toContain("testing");
  });

  it("respects limit", async () => {
    const result = await mgr.recall({ limit: 2 });
    expect(result.entries).toHaveLength(2);
    expect(result.total).toBe(3);
  });

  it("returns empty for missing file", async () => {
    const result = await mgr.recall({ file: "memory/nonexistent.md" });
    expect(result.entries).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Recall All
// ---------------------------------------------------------------------------

describe("recallAll", () => {
  it("aggregates entries across multiple daily files", async () => {
    await mgr.save("Day 1 entry", {
      file: "memory/2026-03-19.md",
      timestamp: "2026-03-19T10:00:00Z",
    });
    await mgr.save("Day 2 entry", {
      file: "memory/2026-03-20.md",
      timestamp: "2026-03-20T10:00:00Z",
    });
    await mgr.save("Day 3 entry", {
      file: "memory/2026-03-21.md",
      timestamp: "2026-03-21T10:00:00Z",
    });

    const result = await mgr.recallAll();
    expect(result.entries).toHaveLength(3);
    expect(result.total).toBe(3);
    // Newest first
    expect(result.entries[0].content).toBe("Day 3 entry");
    expect(result.entries[2].content).toBe("Day 1 entry");
  });

  it("applies filters across all files", async () => {
    await mgr.save("Tagged ops", {
      file: "memory/2026-03-19.md",
      tags: ["ops"],
      timestamp: "2026-03-19T10:00:00Z",
    });
    await mgr.save("Tagged dev", {
      file: "memory/2026-03-20.md",
      tags: ["dev"],
      timestamp: "2026-03-20T10:00:00Z",
    });

    const result = await mgr.recallAll({ tags: ["ops"] });
    expect(result.entries).toHaveLength(1);
    expect(result.entries[0].content).toBe("Tagged ops");
  });

  it("returns empty when no memory/ directory", async () => {
    const emptyMgr = new AuroraMdManager(await fs.mkdtemp(path.join(os.tmpdir(), "empty-")));
    const result = await emptyMgr.recallAll();
    expect(result.entries).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Forget
// ---------------------------------------------------------------------------

describe("forget", () => {
  it("removes entry by ID", async () => {
    const saved = await mgr.save("Delete me", { timestamp: "2026-03-21T10:00:00Z" });
    const result = await mgr.forget({ ids: [saved.id], file: saved.file });

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.removed).toBe(1);
    }

    const remaining = await mgr.recall({ file: saved.file });
    expect(remaining.entries).toHaveLength(0);
  });

  it("removes entries by tag", async () => {
    const file = mgr.dailyFilePath();
    await mgr.save("Keep this", { file, tags: ["keep"], timestamp: "2026-03-21T09:00:00Z" });
    await mgr.save("Remove this", { file, tags: ["remove"], timestamp: "2026-03-21T10:00:00Z" });

    const result = await mgr.forget({ tags: ["remove"], file });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.removed).toBe(1);
    }

    const remaining = await mgr.recall({ file });
    expect(remaining.entries).toHaveLength(1);
    expect(remaining.entries[0].content).toBe("Keep this");
  });

  it("removes entries by content match", async () => {
    const file = mgr.dailyFilePath();
    await mgr.save("Remove keyword here", { file, timestamp: "2026-03-21T09:00:00Z" });
    await mgr.save("Keep this", { file, timestamp: "2026-03-21T10:00:00Z" });

    const result = await mgr.forget({ contains: "keyword", file });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.removed).toBe(1);
    }
  });

  it("requires at least one filter", async () => {
    const result = await mgr.forget({ file: mgr.dailyFilePath() });
    expect(result.ok).toBe(false);
  });

  it("rejects writes to MEMORY.md", async () => {
    await expect(mgr.forget({ ids: ["abc"], file: "MEMORY.md" })).rejects.toThrow("read-only");
  });

  it("returns removed=0 when no matches", async () => {
    const file = mgr.dailyFilePath();
    await mgr.save("Existing", { file, timestamp: "2026-03-21T10:00:00Z" });
    const result = await mgr.forget({ ids: ["nonexistent"], file });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.removed).toBe(0);
    }
  });
});

// ---------------------------------------------------------------------------
// Consolidate
// ---------------------------------------------------------------------------

describe("consolidate", () => {
  it("merges old daily files into archive", async () => {
    // Create files older than 7 days
    await mgr.save("Old important", {
      file: "memory/2026-03-01.md",
      importance: "high",
      timestamp: "2026-03-01T10:00:00Z",
    });
    await mgr.save("Old trivial", {
      file: "memory/2026-03-01.md",
      importance: "low",
      timestamp: "2026-03-01T11:00:00Z",
    });
    await mgr.save("Recent note", {
      file: "memory/2026-03-21.md",
      timestamp: "2026-03-21T10:00:00Z",
    });

    const result = await mgr.consolidate({ before: "2026-03-14" });

    expect(result.merged).toBe(1);
    expect(result.kept).toBe(1); // only the high-importance entry
    expect(result.removedFiles).toEqual(["memory/2026-03-01.md"]);

    // Archive should contain the kept entry
    const archive = await fs.readFile(path.join(tmpDir, "memory/archive.md"), "utf-8");
    expect(archive).toContain("Old important");
    expect(archive).not.toContain("Old trivial");

    // Recent file should be untouched
    const recent = await fs.readFile(path.join(tmpDir, "memory/2026-03-21.md"), "utf-8");
    expect(recent).toContain("Recent note");

    // Old file should be deleted
    await expect(fs.access(path.join(tmpDir, "memory/2026-03-01.md"))).rejects.toThrow();
  });

  it("does nothing when no old files exist", async () => {
    await mgr.save("Today", { file: "memory/2026-03-21.md", timestamp: "2026-03-21T10:00:00Z" });
    const result = await mgr.consolidate({ before: "2026-03-14" });
    expect(result.merged).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// Sections (for MEMORY.md reads)
// ---------------------------------------------------------------------------

describe("sections / listSections", () => {
  it("parses sections from file", async () => {
    await fs.writeFile(
      path.join(tmpDir, "MEMORY.md"),
      "# Memory\n\n## Notes\n\nSome notes\n\n## Decisions\n\nDecision content\n",
    );

    const sections = await mgr.sections("MEMORY.md");
    expect(sections).toHaveLength(3);
    expect(sections[0].title).toBe("Memory");
    expect(sections[1].title).toBe("Notes");
    expect(sections[2].title).toBe("Decisions");
  });

  it("lists section titles", async () => {
    await fs.writeFile(path.join(tmpDir, "MEMORY.md"), "## A\n\n## B\n\n### C\n");
    const list = await mgr.listSections("MEMORY.md");
    expect(list).toEqual([
      { level: 2, title: "A" },
      { level: 2, title: "B" },
      { level: 3, title: "C" },
    ]);
  });

  it("returns empty for missing file", async () => {
    const sections = await mgr.sections("missing.md");
    expect(sections).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// parseSections (pure)
// ---------------------------------------------------------------------------

describe("parseSections", () => {
  it("handles empty content", () => {
    expect(parseSections("")).toEqual([]);
  });

  it("handles content without headings", () => {
    expect(parseSections("just text\nno headings")).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// dailyFilePath
// ---------------------------------------------------------------------------

describe("dailyFilePath", () => {
  it("returns memory/YYYY-MM-DD.md format", () => {
    const p = mgr.dailyFilePath(new Date("2026-03-21T15:00:00Z").getTime());
    expect(p).toBe("memory/2026-03-21.md");
  });

  it("respects timezone", () => {
    // 2026-03-22T01:00:00Z is still March 21 in US Pacific
    const pacific = new AuroraMdManager(tmpDir, "America/Los_Angeles");
    const p = pacific.dailyFilePath(new Date("2026-03-22T06:00:00Z").getTime());
    expect(p).toBe("memory/2026-03-21.md");
  });
});

// ---------------------------------------------------------------------------
// Read-only guard
// ---------------------------------------------------------------------------

describe("read-only guard", () => {
  it.each(["MEMORY.md", "memory.md", "SOUL.md", "TOOLS.md", "AGENTS.md"])(
    "blocks writes to %s",
    async (file) => {
      await expect(mgr.save("nope", { file })).rejects.toThrow("read-only");
    },
  );
});

// ---------------------------------------------------------------------------
// Bug: content with backticks should not be confused with tags
// ---------------------------------------------------------------------------

describe("content with backticks", () => {
  it("preserves backticks in content when no trailing tags", () => {
    const entry: AuroraEntry = {
      id: "aabb1122",
      timestamp: "2026-03-21T10:00:00Z",
      content: "Use `pnpm` for builds",
      tags: [],
      importance: "normal",
    };
    const line = formatEntry(entry);
    const parsed = parseEntryLine(line);
    expect(parsed?.content).toBe("Use `pnpm` for builds");
    expect(parsed?.tags).toEqual([]);
  });

  it("distinguishes inline backticks from trailing tags", () => {
    const entry: AuroraEntry = {
      id: "cc334455",
      timestamp: "2026-03-21T10:00:00Z",
      content: "Run `vitest` before pushing",
      tags: ["ci"],
      importance: "normal",
    };
    const line = formatEntry(entry);
    const parsed = parseEntryLine(line);
    expect(parsed?.content).toBe("Run `vitest` before pushing");
    expect(parsed?.tags).toEqual(["ci"]);
  });

  it("round-trips content with multiple inline backtick words and trailing tags", () => {
    const entry: AuroraEntry = {
      id: "dd556677",
      timestamp: "2026-03-21T10:00:00Z",
      content: "Use `pnpm` and `bun` for dev",
      tags: ["tooling", "dev"],
      importance: "high",
    };
    const line = formatEntry(entry);
    const parsed = parseEntryLine(line);
    expect(parsed?.content).toBe("Use `pnpm` and `bun` for dev");
    expect(parsed?.tags).toEqual(["tooling", "dev"]);
  });

  it("handles content ending with a backtick word (no tags)", () => {
    const entry: AuroraEntry = {
      id: "ee778899",
      timestamp: "2026-03-21T10:00:00Z",
      content: "Always use `pnpm`",
      tags: [],
      importance: "normal",
    };
    const line = formatEntry(entry);
    const parsed = parseEntryLine(line);
    expect(parsed?.content).toBe("Always use `pnpm`");
    expect(parsed?.tags).toEqual([]);
  });

  it("handles content ending with backtick word alongside real tags", () => {
    const entry: AuroraEntry = {
      id: "ff001122",
      timestamp: "2026-03-21T10:00:00Z",
      content: "Prefer `bun` over `node`",
      tags: ["runtime"],
      importance: "normal",
    };
    const line = formatEntry(entry);
    const parsed = parseEntryLine(line);
    expect(parsed?.content).toBe("Prefer `bun` over `node`");
    expect(parsed?.tags).toEqual(["runtime"]);
  });

  it("save and recall preserves backtick content", async () => {
    const result = await mgr.save("Prefer `bun` over `node` for scripts", {
      tags: ["tooling"],
      timestamp: "2026-03-21T10:00:00Z",
    });
    expect(result.entry.content).toBe("Prefer `bun` over `node` for scripts");

    const recalled = await mgr.recall({ file: result.file });
    expect(recalled.entries[0].content).toBe("Prefer `bun` over `node` for scripts");
    expect(recalled.entries[0].tags).toEqual(["tooling"]);
  });
});

// ---------------------------------------------------------------------------
// Bug: consolidate should not delete the target file
// ---------------------------------------------------------------------------

describe("consolidate edge cases", () => {
  it("does not delete target file if it matches a daily file pattern", async () => {
    await mgr.save("Target entry", {
      file: "memory/2026-01-01.md",
      importance: "high",
      timestamp: "2026-01-01T10:00:00Z",
    });
    await mgr.save("Source entry", {
      file: "memory/2026-01-02.md",
      importance: "high",
      timestamp: "2026-01-02T10:00:00Z",
    });

    // Consolidate into an existing daily file as target
    await mgr.consolidate({
      before: "2026-03-01",
      targetFile: "memory/2026-01-01.md",
    });

    // The target file should still exist and contain both entries
    const content = await fs.readFile(path.join(tmpDir, "memory/2026-01-01.md"), "utf-8");
    expect(content).toContain("Target entry");
    expect(content).toContain("Source entry");
  });

  it("does not lose data when target is also a source file", async () => {
    // Both files are old enough to be consolidated
    await mgr.save("Entry in target", {
      file: "memory/2026-01-01.md",
      importance: "high",
      timestamp: "2026-01-01T10:00:00Z",
    });
    await mgr.save("Entry in source", {
      file: "memory/2026-01-02.md",
      importance: "high",
      timestamp: "2026-01-02T10:00:00Z",
    });

    // Target IS one of the old files that would be consolidated
    const consolidated = await mgr.consolidate({
      before: "2026-03-01",
      targetFile: "memory/2026-01-01.md",
    });

    expect(consolidated.merged).toBe(2); // both files consolidated
    expect(consolidated.kept).toBe(2);

    // Target should contain both entries
    const content = await fs.readFile(path.join(tmpDir, "memory/2026-01-01.md"), "utf-8");
    expect(content).toContain("Entry in target");
    expect(content).toContain("Entry in source");
  });

  it("consolidate with all low-importance entries keeps zero and still removes files", async () => {
    await mgr.save("Low entry", {
      file: "memory/2026-01-05.md",
      importance: "low",
      timestamp: "2026-01-05T10:00:00Z",
    });

    const result = await mgr.consolidate({ before: "2026-03-01", minImportance: "normal" });
    expect(result.merged).toBe(1);
    expect(result.kept).toBe(0);
    expect(result.removedFiles).toEqual(["memory/2026-01-05.md"]);

    // archive.md should NOT be created if nothing to keep
    await expect(fs.access(path.join(tmpDir, "memory/archive.md"))).rejects.toThrow();
  });
});

// ---------------------------------------------------------------------------
// Bug: forget on missing file should not create empty file
// ---------------------------------------------------------------------------

describe("forget edge cases", () => {
  it("does not create file when forgetting from nonexistent file", async () => {
    const file = "memory/2026-01-01.md";
    const result = await mgr.forget({ ids: ["abc12345"], file });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.removed).toBe(0);
    }
    // File should NOT be created
    await expect(fs.access(path.join(tmpDir, file))).rejects.toThrow();
  });

  it("does not write file when nothing was removed", async () => {
    const file = mgr.dailyFilePath();
    await mgr.save("Keep me", { file, timestamp: "2026-03-21T10:00:00Z" });

    const absPath = path.join(tmpDir, file);
    const contentBefore = await fs.readFile(absPath, "utf-8");

    const result = await mgr.forget({ ids: ["nonexistent"], file });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.removed).toBe(0);
    }

    // File content should be unchanged
    const contentAfter = await fs.readFile(absPath, "utf-8");
    expect(contentAfter).toBe(contentBefore);
  });
});

// ---------------------------------------------------------------------------
// Edge: multiple saves to same file preserve ordering
// ---------------------------------------------------------------------------

describe("ordering", () => {
  it("preserves insertion order in single file", async () => {
    const file = "memory/2026-03-21.md";
    await mgr.save("First", { file, timestamp: "2026-03-21T08:00:00Z" });
    await mgr.save("Second", { file, timestamp: "2026-03-21T09:00:00Z" });
    await mgr.save("Third", { file, timestamp: "2026-03-21T10:00:00Z" });

    const result = await mgr.recall({ file });
    expect(result.entries.map((e) => e.content)).toEqual(["First", "Second", "Third"]);
  });
});

// ---------------------------------------------------------------------------
// Edge: special characters in content
// ---------------------------------------------------------------------------

describe("special characters", () => {
  it("handles content with markdown special chars", async () => {
    const result = await mgr.save("Use **bold** and [links](url) in messages", {
      timestamp: "2026-03-21T10:00:00Z",
    });
    const recalled = await mgr.recall({ file: result.file });
    expect(recalled.entries[0].content).toBe("Use **bold** and [links](url) in messages");
  });

  it("handles content with curly braces that look like importance", async () => {
    const result = await mgr.save("Template: {name} and {value} placeholders", {
      timestamp: "2026-03-21T10:00:00Z",
    });
    const recalled = await mgr.recall({ file: result.file });
    expect(recalled.entries[0].content).toBe("Template: {name} and {value} placeholders");
    expect(recalled.entries[0].importance).toBe("normal");
  });
});
