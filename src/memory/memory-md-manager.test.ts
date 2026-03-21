import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  addEntry,
  findSection,
  formatEntry,
  initMemoryMd,
  listEntries,
  listSections,
  parseEntry,
  parseSections,
  readMemoryMd,
  removeEntry,
  removeSection,
  resolveMemoryMdPath,
  updateSection,
  writeMemoryMd,
} from "./memory-md-manager.js";

let tmpDir: string;

beforeEach(async () => {
  tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "memory-md-manager-test-"));
});

afterEach(async () => {
  await fs.rm(tmpDir, { recursive: true, force: true });
});

describe("resolveMemoryMdPath", () => {
  it("prefers MEMORY.md when it exists", async () => {
    await fs.writeFile(path.join(tmpDir, "MEMORY.md"), "# Memory\n");
    const resolved = await resolveMemoryMdPath(tmpDir);
    expect(resolved).toBe(path.join(tmpDir, "MEMORY.md"));
  });

  it("falls back to memory.md", async () => {
    const resolved = await resolveMemoryMdPath(tmpDir);
    expect(resolved).toBe(path.join(tmpDir, "memory.md"));
  });
});

describe("readMemoryMd / writeMemoryMd", () => {
  it("returns empty string for missing file", async () => {
    const content = await readMemoryMd(path.join(tmpDir, "missing.md"));
    expect(content).toBe("");
  });

  it("round-trips content", async () => {
    const filePath = path.join(tmpDir, "test.md");
    await writeMemoryMd(filePath, "# Hello\n\nWorld\n");
    const content = await readMemoryMd(filePath);
    expect(content).toBe("# Hello\n\nWorld\n");
  });

  it("creates parent directories", async () => {
    const filePath = path.join(tmpDir, "sub", "dir", "test.md");
    await writeMemoryMd(filePath, "test\n");
    const content = await readMemoryMd(filePath);
    expect(content).toBe("test\n");
  });
});

describe("parseSections", () => {
  it("parses sections from markdown", () => {
    const content =
      "# Title\n\nIntro text\n\n## Notes\n\nSome notes\n\n## Decisions\n\nA decision\n";
    const sections = parseSections(content);
    expect(sections).toHaveLength(3);
    expect(sections[0].title).toBe("Title");
    expect(sections[0].level).toBe(1);
    expect(sections[1].title).toBe("Notes");
    expect(sections[1].level).toBe(2);
    expect(sections[2].title).toBe("Decisions");
    expect(sections[2].level).toBe(2);
  });

  it("returns empty array for empty content", () => {
    expect(parseSections("")).toEqual([]);
  });

  it("handles content without headings", () => {
    expect(parseSections("just some text\nno headings")).toEqual([]);
  });

  it("preserves section content", () => {
    const content = "## Notes\n\n- Item 1\n- Item 2\n\n## Other\n\nText";
    const sections = parseSections(content);
    expect(sections[0].content).toContain("- Item 1");
    expect(sections[0].content).toContain("- Item 2");
  });
});

describe("findSection", () => {
  it("finds section case-insensitively", () => {
    const sections = parseSections("## Notes\n\nContent\n\n## Decisions\n\nMore");
    const found = findSection(sections, "notes");
    expect(found).toBeDefined();
    expect(found!.title).toBe("Notes");
  });

  it("returns undefined for missing section", () => {
    const sections = parseSections("## Notes\n\nContent");
    expect(findSection(sections, "missing")).toBeUndefined();
  });
});

describe("listSections", () => {
  it("lists section titles and levels", () => {
    const content = "# Memory\n\n## Notes\n\n### Sub\n\n## Tasks\n";
    const result = listSections(content);
    expect(result).toEqual([
      { level: 1, title: "Memory" },
      { level: 2, title: "Notes" },
      { level: 3, title: "Sub" },
      { level: 2, title: "Tasks" },
    ]);
  });
});

describe("formatEntry / parseEntry", () => {
  it("formats and parses a basic entry", () => {
    const entry = { timestamp: "2026-03-21T10:00:00.000Z", text: "Decided to use TypeScript" };
    const formatted = formatEntry(entry);
    expect(formatted).toBe("- **2026-03-21T10:00:00.000Z** Decided to use TypeScript");

    const parsed = parseEntry(formatted);
    expect(parsed).toEqual(entry);
  });

  it("formats and parses entry with tags", () => {
    const entry = {
      timestamp: "2026-03-21T10:00:00.000Z",
      text: "Use pnpm for deps",
      tags: ["tooling", "deps"],
    };
    const formatted = formatEntry(entry);
    expect(formatted).toContain("`tooling` `deps`");

    const parsed = parseEntry(formatted);
    expect(parsed?.tags).toEqual(["tooling", "deps"]);
    expect(parsed?.text).toBe("Use pnpm for deps");
  });

  it("returns null for non-entry lines", () => {
    expect(parseEntry("just some text")).toBeNull();
    expect(parseEntry("## Heading")).toBeNull();
    expect(parseEntry("- Regular list item")).toBeNull();
  });
});

describe("addEntry", () => {
  it("creates file and adds entry when file is missing", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await addEntry(filePath, "First note", {
      timestamp: "2026-03-21T10:00:00.000Z",
    });

    const content = await readMemoryMd(filePath);
    expect(content).toContain("**2026-03-21T10:00:00.000Z** First note");
  });

  it("adds entry to existing section", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "# Memory\n\n## Notes\n\n- existing\n\n## Tasks\n");

    await addEntry(filePath, "New note", {
      section: "Notes",
      timestamp: "2026-03-21T12:00:00.000Z",
    });

    const content = await readMemoryMd(filePath);
    expect(content).toContain("- existing");
    expect(content).toContain("**2026-03-21T12:00:00.000Z** New note");
  });

  it("creates section if missing", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "# Memory\n\n## Notes\n\nSome notes\n");

    await addEntry(filePath, "A decision", {
      section: "Decisions",
      timestamp: "2026-03-21T12:00:00.000Z",
    });

    const content = await readMemoryMd(filePath);
    expect(content).toContain("## Decisions");
    expect(content).toContain("**2026-03-21T12:00:00.000Z** A decision");
  });

  it("adds entry with tags", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await addEntry(filePath, "Use vitest", {
      tags: ["testing"],
      timestamp: "2026-03-21T12:00:00.000Z",
    });

    const content = await readMemoryMd(filePath);
    expect(content).toContain("`testing`");
  });
});

describe("updateSection", () => {
  it("replaces section body", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "## Notes\n\nOld content\n\n## Tasks\n\nTask list\n");

    const result = await updateSection(filePath, "Notes", { content: "New content here" });
    expect(result).toBe(true);

    const content = await readMemoryMd(filePath);
    expect(content).toContain("## Notes");
    expect(content).toContain("New content here");
    expect(content).not.toContain("Old content");
    expect(content).toContain("## Tasks");
  });

  it("appends to section when append is true", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "## Notes\n\nExisting\n");

    const result = await updateSection(filePath, "Notes", {
      content: "Appended text",
      append: true,
    });
    expect(result).toBe(true);

    const content = await readMemoryMd(filePath);
    expect(content).toContain("Existing");
    expect(content).toContain("Appended text");
  });

  it("returns false for missing section", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "## Notes\n\nContent\n");

    const result = await updateSection(filePath, "Missing", { content: "new" });
    expect(result).toBe(false);
  });
});

describe("removeSection", () => {
  it("removes a section", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "## Notes\n\nKeep\n\n## Remove Me\n\nGone\n\n## Tasks\n\nStay\n");

    const result = await removeSection(filePath, "Remove Me");
    expect(result).toBe(true);

    const content = await readMemoryMd(filePath);
    expect(content).toContain("## Notes");
    expect(content).toContain("Keep");
    expect(content).not.toContain("Remove Me");
    expect(content).not.toContain("Gone");
    expect(content).toContain("## Tasks");
    expect(content).toContain("Stay");
  });

  it("returns false for missing section", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "## Notes\n\nContent\n");

    const result = await removeSection(filePath, "Nope");
    expect(result).toBe(false);
  });
});

describe("removeEntry", () => {
  it("removes entry by timestamp", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(
      filePath,
      [
        "## Notes",
        "",
        "- **2026-03-20T10:00:00.000Z** Keep this",
        "- **2026-03-21T10:00:00.000Z** Remove this",
        "- **2026-03-22T10:00:00.000Z** Also keep",
        "",
      ].join("\n"),
    );

    const result = await removeEntry(filePath, "2026-03-21T10:00:00.000Z");
    expect(result).toBe(true);

    const content = await readMemoryMd(filePath);
    expect(content).toContain("Keep this");
    expect(content).not.toContain("Remove this");
    expect(content).toContain("Also keep");
  });

  it("returns false for missing timestamp", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "- **2026-03-21T10:00:00.000Z** Entry\n");

    const result = await removeEntry(filePath, "2026-01-01T00:00:00.000Z");
    expect(result).toBe(false);
  });
});

describe("listEntries", () => {
  it("lists all entries from file", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(
      filePath,
      [
        "## Notes",
        "",
        "- **2026-03-20T10:00:00.000Z** First",
        "- **2026-03-21T10:00:00.000Z** Second `tag1`",
        "Some non-entry text",
        "",
      ].join("\n"),
    );

    const entries = await listEntries(filePath);
    expect(entries).toHaveLength(2);
    expect(entries[0].text).toBe("First");
    expect(entries[1].text).toBe("Second");
    expect(entries[1].tags).toEqual(["tag1"]);
  });

  it("filters by section", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(
      filePath,
      [
        "## Notes",
        "",
        "- **2026-03-20T10:00:00.000Z** In notes",
        "",
        "## Tasks",
        "",
        "- **2026-03-21T10:00:00.000Z** In tasks",
        "",
      ].join("\n"),
    );

    const entries = await listEntries(filePath, { section: "Tasks" });
    expect(entries).toHaveLength(1);
    expect(entries[0].text).toBe("In tasks");
  });

  it("filters by tags", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(
      filePath,
      [
        "- **2026-03-20T10:00:00.000Z** Tagged `important`",
        "- **2026-03-21T10:00:00.000Z** Not tagged",
        "- **2026-03-22T10:00:00.000Z** Also tagged `important` `urgent`",
        "",
      ].join("\n"),
    );

    const entries = await listEntries(filePath, { tags: ["important"] });
    expect(entries).toHaveLength(2);
  });

  it("returns empty for missing file", async () => {
    const entries = await listEntries(path.join(tmpDir, "nope.md"));
    expect(entries).toEqual([]);
  });
});

describe("initMemoryMd", () => {
  it("creates a new memory file with template", async () => {
    const result = await initMemoryMd(tmpDir);
    expect(result.created).toBe(true);
    expect(result.filePath).toBe(path.join(tmpDir, "MEMORY.md"));

    const content = await readMemoryMd(result.filePath);
    expect(content).toContain("# Memory");
    expect(content).toContain("## Notes");
    expect(content).toContain("## Decisions");
    expect(content).toContain("## Tasks");
  });

  it("does not overwrite existing file", async () => {
    const filePath = path.join(tmpDir, "MEMORY.md");
    await writeMemoryMd(filePath, "# Existing\n");

    const result = await initMemoryMd(tmpDir);
    expect(result.created).toBe(false);

    const content = await readMemoryMd(filePath);
    expect(content).toBe("# Existing\n");
  });

  it("supports custom filename and title", async () => {
    const result = await initMemoryMd(tmpDir, { filename: "memory.md", title: "Project Memory" });
    expect(result.created).toBe(true);

    const content = await readMemoryMd(result.filePath);
    expect(content).toContain("# Project Memory");
  });
});
