import { execFileSync } from "node:child_process";
import path from "node:path";
import { describe, expect, it } from "vitest";

const repoRoot = process.cwd();
const scriptPath = path.join(repoRoot, "scripts", "check-plugin-sdk-subpath-exports.mjs");

describe("check-plugin-sdk-subpath-exports", () => {
  it("passes on the current codebase with no violations", () => {
    const result = execFileSync(process.execPath, [scriptPath], {
      cwd: repoRoot,
      encoding: "utf8",
      timeout: 30_000,
    });

    expect(result).toContain("OK");
  });
});
