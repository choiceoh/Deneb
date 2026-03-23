import { describe, expect, it } from "vitest";
import { compileConfigRegex, compileConfigRegexes } from "./config-regex.js";

describe("compileConfigRegex", () => {
  it("compiles valid regex pattern", () => {
    const result = compileConfigRegex("hello.*world");
    expect(result).not.toBeNull();
    expect(result!.regex).toBeInstanceOf(RegExp);
    expect(result!.reason).toBeNull();
  });

  it("returns null for empty pattern", () => {
    expect(compileConfigRegex("")).toBeNull();
  });

  it("returns rejection for invalid regex", () => {
    const result = compileConfigRegex("[invalid");
    expect(result).not.toBeNull();
    expect(result!.regex).toBeNull();
    expect(result!.reason).toBeDefined();
  });

  it("applies flags", () => {
    const result = compileConfigRegex("test", "i");
    expect(result).not.toBeNull();
    expect(result!.regex!.flags).toContain("i");
  });
});

describe("compileConfigRegexes", () => {
  it("compiles multiple patterns", () => {
    const result = compileConfigRegexes(["foo", "bar", "baz"]);
    expect(result.regexes).toHaveLength(3);
    expect(result.rejected).toHaveLength(0);
  });

  it("filters empty patterns", () => {
    const result = compileConfigRegexes(["foo", "", "bar"]);
    expect(result.regexes).toHaveLength(2);
    expect(result.rejected).toHaveLength(0);
  });

  it("collects rejected patterns", () => {
    const result = compileConfigRegexes(["foo", "[invalid"]);
    expect(result.regexes).toHaveLength(1);
    expect(result.rejected).toHaveLength(1);
    expect(result.rejected[0].pattern).toBe("[invalid");
  });

  it("applies flags to all patterns", () => {
    const result = compileConfigRegexes(["foo", "bar"], "i");
    for (const regex of result.regexes) {
      expect(regex.flags).toContain("i");
    }
  });
});
