import { describe, expect, it } from "vitest";

import { bumpCargoVersion, bumpJsonVersion } from "./bump-version.mjs";

describe("bump-version", () => {
  it("sets the version key and keeps 2-space + trailing-newline JSON", () => {
    const out = bumpJsonVersion('{\n  "name": "andromeda",\n  "version": "0.0.1"\n}\n', "0.1.0");
    expect(JSON.parse(out).version).toBe("0.1.0");
    expect(out.endsWith("\n")).toBe(true);
    expect(out).toContain('  "version": "0.1.0"');
  });

  it("replaces only the [package] version in Cargo.toml, not inline dep specs", () => {
    const cargo = [
      "[package]",
      'name = "andromeda"',
      'version = "0.0.1"',
      "",
      "[dependencies]",
      'tauri = { version = "2" }',
      "",
    ].join("\n");
    const out = bumpCargoVersion(cargo, "0.2.3");
    expect(out).toContain('version = "0.2.3"');
    expect(out).toContain('tauri = { version = "2" }'); // dep spec untouched
    expect(out).not.toContain('version = "0.0.1"');
  });

  it("throws if no [package] version line is present", () => {
    expect(() => bumpCargoVersion('[dependencies]\ntauri = { version = "2" }\n', "1.0.0")).toThrow();
  });
});
