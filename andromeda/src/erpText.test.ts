import { describe, expect, it } from "vitest";
import { erpTextToMarkdown } from "@/erpText";

describe("erpTextToMarkdown", () => {
  it("promotes the first line to a heading", () => {
    const md = erpTextToMarkdown("현재고 요약\n\n1. 품목 A · 재고 10\n2. 품목 B · 재고 3");
    expect(md.startsWith("## 현재고 요약")).toBe(true);
    expect(md).toContain("1. 품목 A");
  });

  it("returns empty for blank input", () => {
    expect(erpTextToMarkdown("  \n  ")).toBe("");
  });
});
