import path from "node:path";
import { describe, expect, it } from "vitest";
import { applyPathPrepend, findPathKey } from "../../infra/path-prepend.js";

const isWin = process.platform === "win32";

describe("findPathKey", () => {
  it("returns PATH when key is uppercase", () => {
    expect(findPathKey({ PATH: "/usr/bin" })).toBe("PATH");
  });

  it("returns Path when key is mixed-case (Windows style)", () => {
    expect(findPathKey({ Path: "C:\\Windows\\System32" })).toBe("Path");
  });

  it("returns PATH as default when no PATH-like key exists", () => {
    expect(findPathKey({ HOME: "/home/user" })).toBe("PATH");
  });

  it("prefers uppercase PATH when both PATH and Path exist", () => {
    expect(findPathKey({ PATH: "/usr/bin", Path: "C:\\Windows" })).toBe("PATH");
  });
});

describe("applyPathPrepend with case-insensitive PATH key", () => {
  it("prepends to Path key on Windows-style env (no uppercase PATH)", () => {
    const env: Record<string, string> = { Path: "C:\\Windows\\System32" };
    applyPathPrepend(env, ["C:\\custom\\bin"]);
    // Should write back to the same `Path` key, not create a new `PATH`
    expect(env.Path).toContain("C:\\custom\\bin");
    expect(env.Path).toContain("C:\\Windows\\System32");
    expect("PATH" in env).toBe(false);
  });

  it("preserves all existing entries when prepending via Path key", () => {
    // Use platform-appropriate paths and delimiters
    const delim = path.delimiter;
    const existing = isWin
      ? ["C:\\Windows\\System32", "C:\\Windows", "C:\\Program Files\\nodejs"]
      : ["/usr/bin", "/usr/local/bin", "/opt/node/bin"];
    const prepend = isWin ? ["C:\\custom\\bin"] : ["/custom/bin"];
    const existingPath = existing.join(delim);
    const env: Record<string, string> = { Path: existingPath };
    applyPathPrepend(env, prepend);
    const parts = env.Path.split(delim);
    expect(parts[0]).toBe(prepend[0]);
    for (const entry of existing) {
      expect(parts).toContain(entry);
    }
  });

  it("respects requireExisting option with Path key", () => {
    const env: Record<string, string> = { HOME: "/home/user" };
    applyPathPrepend(env, ["C:\\custom\\bin"], { requireExisting: true });
    // No Path/PATH key exists, so nothing should be written
    expect("PATH" in env).toBe(false);
    expect("Path" in env).toBe(false);
  });
});
