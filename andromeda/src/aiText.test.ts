import { describe, expect, it } from "vitest";
import { projectList, serializeList } from "./aiText";

describe("serializeList", () => {
  it("is empty for no rows (nothing to tell the AI)", () => {
    expect(serializeList("할일", [], () => "-")).toBe("");
  });

  it("returns counted header with one formatted line per row when building summary", () => {
    const out = serializeList("할일", [{ t: "a" }, { t: "b" }], (r) => `- ${r.t}`);
    expect(out).toBe("[할일 2건]\n- a\n- b");
  });

  it("returns values with custom unit when unit override provided", () => {
    expect(serializeList("연락처", [{}], () => "- x", "명")).toBe("[연락처 1명]\n- x");
  });
});

describe("projectList", () => {
  it("is empty for no rows", () => {
    expect(projectList("[검색 0건]", [], () => "-")).toBe("");
  });

  it("returns custom header with one formatted line per row when header override provided", () => {
    const out = projectList(`[검색 "x" — 2건]`, [{ t: "a" }, { t: "b" }], (r) => `- ${r.t}`);
    expect(out).toBe(`[검색 "x" — 2건]\n- a\n- b`);
  });
});
