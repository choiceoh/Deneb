import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  formatOctal,
  formatPermissionDetail,
  formatPermissionRemediation,
  inspectPathPermissions,
  isGroupReadable,
  isGroupWritable,
  isWorldReadable,
  isWorldWritable,
  modeBits,
  safeStat,
} from "./audit-fs.js";

describe("modeBits", () => {
  it("extracts lower 9 bits from mode", () => {
    expect(modeBits(0o100755)).toBe(0o755);
    expect(modeBits(0o100644)).toBe(0o644);
    expect(modeBits(0o40700)).toBe(0o700);
  });

  it("returns null for null input", () => {
    expect(modeBits(null)).toBeNull();
  });
});

describe("formatOctal", () => {
  it("formats bits as zero-padded octal", () => {
    expect(formatOctal(0o755)).toBe("755");
    expect(formatOctal(0o644)).toBe("644");
    expect(formatOctal(0o7)).toBe("007");
  });

  it("returns 'unknown' for null", () => {
    expect(formatOctal(null)).toBe("unknown");
  });
});

describe("permission bit checks", () => {
  it("detects world writable", () => {
    expect(isWorldWritable(0o777)).toBe(true);
    expect(isWorldWritable(0o755)).toBe(false);
    expect(isWorldWritable(0o002)).toBe(true);
    expect(isWorldWritable(null)).toBe(false);
  });

  it("detects group writable", () => {
    expect(isGroupWritable(0o770)).toBe(true);
    expect(isGroupWritable(0o700)).toBe(false);
    expect(isGroupWritable(0o020)).toBe(true);
    expect(isGroupWritable(null)).toBe(false);
  });

  it("detects world readable", () => {
    expect(isWorldReadable(0o755)).toBe(true);
    expect(isWorldReadable(0o750)).toBe(false);
    expect(isWorldReadable(null)).toBe(false);
  });

  it("detects group readable", () => {
    expect(isGroupReadable(0o750)).toBe(true);
    expect(isGroupReadable(0o700)).toBe(false);
    expect(isGroupReadable(null)).toBe(false);
  });
});

describe("formatPermissionDetail", () => {
  it("formats path and mode", () => {
    const result = formatPermissionDetail("/tmp/test", {
      ok: true,
      isSymlink: false,
      isDir: false,
      mode: 0o100644,
      bits: 0o644,
      source: "posix",
      worldWritable: false,
      groupWritable: false,
      worldReadable: true,
      groupReadable: true,
    });
    expect(result).toBe("/tmp/test mode=644");
  });
});

describe("formatPermissionRemediation", () => {
  it("generates chmod command", () => {
    expect(
      formatPermissionRemediation({ targetPath: "/tmp/test", isDir: false, posixMode: 0o600 }),
    ).toBe("chmod 600 /tmp/test");
  });

  it("pads mode to 3 digits", () => {
    expect(formatPermissionRemediation({ targetPath: "/x", isDir: true, posixMode: 0o7 })).toBe(
      "chmod 007 /x",
    );
  });
});

describe("safeStat", () => {
  let tmpDir: string;

  beforeEach(async () => {
    tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "audit-fs-test-"));
  });

  afterEach(async () => {
    await fs.rm(tmpDir, { recursive: true, force: true });
  });

  it("returns stat info for existing file", async () => {
    const filePath = path.join(tmpDir, "test.txt");
    await fs.writeFile(filePath, "hello");
    const result = await safeStat(filePath);
    expect(result.ok).toBe(true);
    expect(result.isDir).toBe(false);
    expect(result.mode).toBeTypeOf("number");
  });

  it("returns stat info for directory", async () => {
    const result = await safeStat(tmpDir);
    expect(result.ok).toBe(true);
    expect(result.isDir).toBe(true);
  });

  it("returns ok=false for non-existent path", async () => {
    const result = await safeStat(path.join(tmpDir, "nonexistent"));
    expect(result.ok).toBe(false);
    expect(result.error).toBeDefined();
  });
});

describe("inspectPathPermissions", () => {
  let tmpDir: string;

  beforeEach(async () => {
    tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "audit-fs-perms-"));
  });

  afterEach(async () => {
    await fs.rm(tmpDir, { recursive: true, force: true });
  });

  it("inspects file permissions", async () => {
    const filePath = path.join(tmpDir, "test.txt");
    await fs.writeFile(filePath, "hello");
    await fs.chmod(filePath, 0o644);
    const result = await inspectPathPermissions(filePath);
    expect(result.ok).toBe(true);
    expect(result.source).toBe("posix");
    expect(result.bits).toBe(0o644);
    expect(result.worldWritable).toBe(false);
    expect(result.worldReadable).toBe(true);
  });

  it("returns error for non-existent path", async () => {
    const result = await inspectPathPermissions(path.join(tmpDir, "nope"));
    expect(result.ok).toBe(false);
    expect(result.error).toBeDefined();
  });
});
