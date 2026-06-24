import { describe, expect, it } from "vitest";
import { projectList, serializeList } from "./aiText";

describe("serializeList", () => {
  it("is empty for no rows (nothing to tell the AI)", () => {
    expect(serializeList("할일", [], () => "-")).toBe("");
  });

  it("builds a counted header plus one line per row", () => {
    const out = serializeList("할일", [{ t: "a" }, { t: "b" }], (r) => `- ${r.t}`);
    expect(out).toBe("[할일 2건]\n- a\n- b");
  });

  it("honors a custom unit", () => {
    expect(serializeList("연락처", [{}], () => "- x", "명")).toBe("[연락처 1명]\n- x");
  });
});

describe("projectList", () => {
  it("is empty for no rows", () => {
    expect(projectList("[검색 0건]", [], () => "-")).toBe("");
  });

  it("builds a custom header plus one line per row", () => {
    const out = projectList(`[검색 "x" — 2건]`, [{ t: "a" }, { t: "b" }], (r) => `- ${r.t}`);
    expect(out).toBe(`[검색 "x" — 2건]\n- a\n- b`);
  });
});
