import { describe, expect, it } from "vitest";
import { isPathInside, extensionUsesSkippedScannerPath } from "./scan-paths.js";

describe("isPathInside", () => {
  it("returns true when candidate is inside base", () => {
    expect(isPathInside("/home/user", "/home/user/file.txt")).toBe(true);
    expect(isPathInside("/home/user", "/home/user/sub/dir/file.txt")).toBe(true);
  });

  it("returns true when candidate equals base", () => {
    expect(isPathInside("/home/user", "/home/user")).toBe(true);
  });

  it("returns false when candidate is outside base", () => {
    expect(isPathInside("/home/user", "/home/other/file.txt")).toBe(false);
    expect(isPathInside("/home/user", "/etc/passwd")).toBe(false);
  });

  it("returns false for parent traversal", () => {
    expect(isPathInside("/home/user", "/home/user/../other")).toBe(false);
  });

  it("handles base path prefix trick", () => {
    // "/home/username" should not be inside "/home/user"
    expect(isPathInside("/home/user", "/home/username")).toBe(false);
  });
});

describe("extensionUsesSkippedScannerPath", () => {
  it("returns true for node_modules paths", () => {
    expect(extensionUsesSkippedScannerPath("extensions/foo/node_modules/bar")).toBe(true);
  });

  it("returns true for hidden directories", () => {
    expect(extensionUsesSkippedScannerPath("extensions/foo/.cache/bar")).toBe(true);
  });

  it("returns false for normal paths", () => {
    expect(extensionUsesSkippedScannerPath("extensions/foo/src/bar.ts")).toBe(false);
  });

  it("returns false for . and .. segments", () => {
    expect(extensionUsesSkippedScannerPath("./extensions/foo")).toBe(false);
    expect(extensionUsesSkippedScannerPath("../extensions/foo")).toBe(false);
  });
});
