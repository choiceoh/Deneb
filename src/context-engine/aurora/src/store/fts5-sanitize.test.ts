import { describe, expect, it } from "vitest";
import { sanitizeFts5Query } from "./fts5-sanitize.js";

describe("sanitizeFts5Query", () => {
  it("wraps plain tokens in double quotes", () => {
    expect(sanitizeFts5Query("hello world")).toBe('"hello" "world"');
  });

  it("handles single token", () => {
    expect(sanitizeFts5Query("hello")).toBe('"hello"');
  });

  it("returns empty quoted string for empty input", () => {
    expect(sanitizeFts5Query("")).toBe('""');
    expect(sanitizeFts5Query("   ")).toBe('""');
  });

  it("neutralizes FTS5 operators", () => {
    expect(sanitizeFts5Query("aurora_expand OR crash")).toBe('"aurora_expand" "OR" "crash"');
    expect(sanitizeFts5Query("-negative")).toBe('"-negative"');
    expect(sanitizeFts5Query("+required")).toBe('"+required"');
    expect(sanitizeFts5Query("prefix*")).toBe('"prefix*"');
  });

  it("handles hyphenated words", () => {
    expect(sanitizeFts5Query("sub-agent restrict")).toBe('"sub-agent" "restrict"');
  });

  it("strips internal double quotes", () => {
    expect(sanitizeFts5Query('hello "world"')).toBe('"hello" "world"');
  });

  it("handles column filter syntax", () => {
    expect(sanitizeFts5Query("agent:foo")).toBe('"agent:foo"');
  });

  it("handles multiple spaces between tokens", () => {
    expect(sanitizeFts5Query("hello   world")).toBe('"hello" "world"');
  });
});
