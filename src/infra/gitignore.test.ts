import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  isIgnoredByPatterns,
  parseGitignore,
  parseGitignoreLine,
  readGitignoreFile,
  readGitignoreFromDir,
} from "./gitignore.js";

describe("parseGitignoreLine", () => {
  it("returns null for empty lines", () => {
    expect(parseGitignoreLine("")).toBeNull();
    expect(parseGitignoreLine("   ")).toBeNull();
    expect(parseGitignoreLine("\n")).toBeNull();
    expect(parseGitignoreLine("\r\n")).toBeNull();
  });

  it("returns null for comment lines", () => {
    expect(parseGitignoreLine("# this is a comment")).toBeNull();
    expect(parseGitignoreLine("#comment")).toBeNull();
  });

  it("parses a simple pattern", () => {
    const result = parseGitignoreLine("node_modules");
    expect(result).toMatchObject({ pattern: "node_modules", negated: false, directoryOnly: false });
  });

  it("parses directory-only patterns (trailing /)", () => {
    const result = parseGitignoreLine("dist/");
    expect(result).toMatchObject({ pattern: "dist", directoryOnly: true });
  });

  it("parses negation patterns", () => {
    const result = parseGitignoreLine("!important.txt");
    expect(result).toMatchObject({ pattern: "important.txt", negated: true });
  });

  it("handles escaped # at start", () => {
    const result = parseGitignoreLine("\\#not-a-comment");
    expect(result).toMatchObject({ pattern: "#not-a-comment" });
  });

  it("handles escaped ! at start", () => {
    const result = parseGitignoreLine("\\!not-negated");
    expect(result).toMatchObject({ pattern: "!not-negated", negated: false });
  });

  it("strips trailing whitespace", () => {
    const result = parseGitignoreLine("foo.txt   ");
    expect(result).toMatchObject({ pattern: "foo.txt" });
  });

  it("strips leading / for anchored patterns", () => {
    const result = parseGitignoreLine("/build");
    expect(result).toMatchObject({ pattern: "build" });
  });

  it("parses wildcard patterns", () => {
    const result = parseGitignoreLine("*.log");
    expect(result).not.toBeNull();
    expect(result?.pattern).toBe("*.log");
    expect(result?.regex.test("error.log")).toBe(true);
    expect(result?.regex.test("src/error.log")).toBe(true);
    expect(result?.regex.test("error.txt")).toBe(false);
  });

  it("parses double-star patterns", () => {
    const result = parseGitignoreLine("**/build");
    expect(result).not.toBeNull();
    expect(result?.regex.test("build")).toBe(true);
    expect(result?.regex.test("src/build")).toBe(true);
    expect(result?.regex.test("src/deep/build")).toBe(true);
  });

  it("parses ? wildcard", () => {
    const result = parseGitignoreLine("file?.txt");
    expect(result).not.toBeNull();
    expect(result?.regex.test("file1.txt")).toBe(true);
    expect(result?.regex.test("fileA.txt")).toBe(true);
    expect(result?.regex.test("file.txt")).toBe(false);
    expect(result?.regex.test("file12.txt")).toBe(false);
  });

  it("parses character class patterns", () => {
    const result = parseGitignoreLine("[abc].txt");
    expect(result).not.toBeNull();
    expect(result?.regex.test("a.txt")).toBe(true);
    expect(result?.regex.test("d.txt")).toBe(false);
  });

  it("handles unclosed character class gracefully", () => {
    const result = parseGitignoreLine("[abc");
    expect(result).toMatchObject({ pattern: "[abc" });
  });
});

describe("parseGitignore", () => {
  it("returns empty array for empty string", () => {
    expect(parseGitignore("")).toEqual([]);
  });

  it("returns empty array for null/undefined input", () => {
    expect(parseGitignore(null as unknown as string)).toEqual([]);
    expect(parseGitignore(undefined as unknown as string)).toEqual([]);
  });

  it("returns empty array for non-string input", () => {
    expect(parseGitignore(123 as unknown as string)).toEqual([]);
    expect(parseGitignore({} as unknown as string)).toEqual([]);
  });

  it("parses a typical .gitignore", () => {
    const content = [
      "# Dependencies",
      "node_modules/",
      "",
      "# Build",
      "dist/",
      "*.tsbuildinfo",
      "",
      "# Env",
      ".env",
      ".env.local",
      "",
      "# Keep this",
      "!.env.example",
    ].join("\n");

    const patterns = parseGitignore(content);
    expect(patterns).toHaveLength(6);
    expect(patterns.map((p) => p.pattern)).toEqual([
      "node_modules",
      "dist",
      "*.tsbuildinfo",
      ".env",
      ".env.local",
      ".env.example",
    ]);
    expect(patterns[0]?.directoryOnly).toBe(true);
    expect(patterns[5]?.negated).toBe(true);
  });

  it("handles Windows line endings", () => {
    const content = "node_modules/\r\ndist/\r\n*.log\r\n";
    const patterns = parseGitignore(content);
    expect(patterns).toHaveLength(3);
  });

  it("handles mixed line endings", () => {
    const content = "foo\nbar\r\nbaz\r";
    const patterns = parseGitignore(content);
    expect(patterns).toHaveLength(3);
  });

  it("handles binary-like content without crashing", () => {
    const binaryContent = "\x00\x01\x02\xff\xfe\n*.log\n\x00garbage";
    const patterns = parseGitignore(binaryContent);
    // Should parse whatever lines it can, skip the rest.
    expect(Array.isArray(patterns)).toBe(true);
  });

  it("handles extremely long lines without crashing", () => {
    const longLine = "a".repeat(100_000);
    const content = `${longLine}\n*.log`;
    const patterns = parseGitignore(content);
    expect(patterns.length).toBeGreaterThanOrEqual(1);
  });

  it("handles only comments and blank lines", () => {
    const content = "# comment 1\n# comment 2\n\n\n# comment 3\n";
    const patterns = parseGitignore(content);
    expect(patterns).toEqual([]);
  });
});

describe("isIgnoredByPatterns", () => {
  it("matches simple filename patterns", () => {
    const patterns = parseGitignore("*.log\n*.tmp");
    expect(isIgnoredByPatterns("error.log", patterns)).toBe(true);
    expect(isIgnoredByPatterns("src/error.log", patterns)).toBe(true);
    expect(isIgnoredByPatterns("error.txt", patterns)).toBe(false);
  });

  it("respects negation patterns", () => {
    const patterns = parseGitignore("*.log\n!important.log");
    expect(isIgnoredByPatterns("error.log", patterns)).toBe(true);
    expect(isIgnoredByPatterns("important.log", patterns)).toBe(false);
  });

  it("respects directory-only patterns", () => {
    const patterns = parseGitignore("build/");
    expect(isIgnoredByPatterns("build", patterns, true)).toBe(true);
    expect(isIgnoredByPatterns("build", patterns, false)).toBe(false);
  });

  it("matches nested paths", () => {
    const patterns = parseGitignore("node_modules");
    expect(isIgnoredByPatterns("node_modules", patterns)).toBe(true);
    expect(isIgnoredByPatterns("packages/foo/node_modules", patterns)).toBe(true);
  });

  it("handles double-star patterns", () => {
    const patterns = parseGitignore("**/test/*.log");
    expect(isIgnoredByPatterns("test/error.log", patterns)).toBe(true);
    expect(isIgnoredByPatterns("src/test/error.log", patterns)).toBe(true);
    expect(isIgnoredByPatterns("error.log", patterns)).toBe(false);
  });

  it("returns false for empty path", () => {
    const patterns = parseGitignore("*.log");
    expect(isIgnoredByPatterns("", patterns)).toBe(false);
  });

  it("returns false for empty patterns", () => {
    expect(isIgnoredByPatterns("anything.log", [])).toBe(false);
  });

  it("normalizes backslashes", () => {
    const patterns = parseGitignore("*.log");
    expect(isIgnoredByPatterns("src\\error.log", patterns)).toBe(true);
  });

  it("strips leading slashes from path", () => {
    const patterns = parseGitignore("*.log");
    expect(isIgnoredByPatterns("/error.log", patterns)).toBe(true);
  });

  it("last matching pattern wins", () => {
    const patterns = parseGitignore("*.log\n!error.log\nerror.log");
    // All *.log ignored, then error.log un-ignored, then re-ignored.
    expect(isIgnoredByPatterns("error.log", patterns)).toBe(true);
  });
});

describe("readGitignoreFile", () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gitignore-test-"));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it("reads and parses a valid .gitignore file", () => {
    const filePath = path.join(tmpDir, ".gitignore");
    fs.writeFileSync(filePath, "node_modules/\ndist/\n*.log\n");
    const result = readGitignoreFile(filePath);
    expect(result.error).toBeNull();
    expect(result.patterns).toHaveLength(3);
  });

  it("returns empty patterns and error for missing file", () => {
    const result = readGitignoreFile(path.join(tmpDir, "nonexistent"));
    expect(result.patterns).toEqual([]);
    expect(result.error).toBeInstanceOf(Error);
  });

  it("returns empty patterns and error for a directory path", () => {
    const result = readGitignoreFile(tmpDir);
    expect(result.patterns).toEqual([]);
    expect(result.error).toBeInstanceOf(Error);
  });

  it("handles an empty .gitignore file", () => {
    const filePath = path.join(tmpDir, ".gitignore");
    fs.writeFileSync(filePath, "");
    const result = readGitignoreFile(filePath);
    expect(result.error).toBeNull();
    expect(result.patterns).toEqual([]);
  });

  it("handles a .gitignore with only comments", () => {
    const filePath = path.join(tmpDir, ".gitignore");
    fs.writeFileSync(filePath, "# comment\n# another\n");
    const result = readGitignoreFile(filePath);
    expect(result.error).toBeNull();
    expect(result.patterns).toEqual([]);
  });
});

describe("readGitignoreFromDir", () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gitignore-dir-test-"));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it("reads .gitignore from a directory", () => {
    fs.writeFileSync(path.join(tmpDir, ".gitignore"), "*.log\n");
    const result = readGitignoreFromDir(tmpDir);
    expect(result.error).toBeNull();
    expect(result.patterns).toHaveLength(1);
  });

  it("returns error when directory has no .gitignore", () => {
    const result = readGitignoreFromDir(tmpDir);
    expect(result.patterns).toEqual([]);
    expect(result.error).toBeInstanceOf(Error);
  });
});
