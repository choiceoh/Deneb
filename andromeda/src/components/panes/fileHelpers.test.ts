import { describe, expect, it } from "vitest";
import type { FileEntry } from "@/types";
import { entryPath, formatBytes, isFolder, joinPath, parentPath } from "./fileHelpers";

describe("entryPath", () => {
  it("prefers pathDisplay, then pathLower, then name, else empty", () => {
    expect(entryPath({ pathDisplay: "/A/b.txt", pathLower: "/a/b.txt", name: "b.txt" } as FileEntry)).toBe("/A/b.txt");
    expect(entryPath({ pathLower: "/a/b.txt", name: "b.txt" } as FileEntry)).toBe("/a/b.txt");
    expect(entryPath({ name: "b.txt" } as FileEntry)).toBe("b.txt");
    expect(entryPath({} as FileEntry)).toBe("");
  });
});

describe("isFolder", () => {
  it("treats folder/dir tags as folders, everything else not", () => {
    expect(isFolder({ tag: "folder" } as FileEntry)).toBe(true);
    expect(isFolder({ tag: "dir" } as FileEntry)).toBe(true);
    expect(isFolder({ tag: "file" } as FileEntry)).toBe(false);
    expect(isFolder({} as FileEntry)).toBe(false);
  });
});

describe("joinPath", () => {
  it("joins base + name, tolerating stray slashes", () => {
    expect(joinPath("docs", "a.txt")).toBe("docs/a.txt");
    expect(joinPath("docs/", "/a.txt")).toBe("docs/a.txt");
    expect(joinPath("", "a.txt")).toBe("a.txt");
    expect(joinPath("", "/a.txt")).toBe("a.txt");
  });
});

describe("parentPath", () => {
  it("returns the parent directory, empty at root", () => {
    expect(parentPath("a/b/c.txt")).toBe("a/b");
    expect(parentPath("/a/b/")).toBe("a");
    expect(parentPath("a.txt")).toBe("");
    expect(parentPath("")).toBe("");
    expect(parentPath("/")).toBe("");
  });
});

describe("formatBytes", () => {
  it("formats B/KB/MB and blanks unknown/zero", () => {
    expect(formatBytes()).toBe("");
    expect(formatBytes(0)).toBe("");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(2 * 1024 * 1024)).toBe("2.0 MB");
  });
});
