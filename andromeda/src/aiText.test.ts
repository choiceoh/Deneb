import { describe, expect, it } from "vitest";
import { projectList, serializeList } from "./aiText";

describe("serializeList", () => {
  it("returns empty string when no rows to serialize", () => {
    expect(serializeList("할일", [], () => "-")).toBe("");
  });

  it("returns counted header with one line per row when serializing", () => {
    const out = serializeList("할일", [{ t: "a" }, { t: "b" }], (r) => `- ${r.t}`);
    expect(out).toBe("[할일 2건]\n- a\n- b");
  });

  it("returns header with custom unit when serializing single row", () => {
    expect(serializeList("연락처", [{}], () => "- x", "명")).toBe("[연락처 1명]\n- x");
  });
});

describe("projectList", () => {
  it("returns empty string when projecting zero rows", () => {
    expect(projectList("[검색 0건]", [], () => "-")).toBe("");
  });

  it("returns custom header with one line per row when projecting", () => {
    const out = projectList(`[검색 "x" — 2건]`, [{ t: "a" }, { t: "b" }], (r) => `- ${r.t}`);
    expect(out).toBe(`[검색 "x" — 2건]\n- a\n- b`);
  });
});
