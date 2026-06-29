import { describe, expect, it } from "vitest";

import { codeStatusColor, codeStatusGlance, codeStatusLabel } from "./codeStatus";

describe("codeStatus", () => {
  it("maps statuses to the three dot colors", () => {
    expect(codeStatusColor("working")).toBe("var(--online)"); // 진행중 초록
    expect(codeStatusColor("passed")).toBe("var(--ink)"); // 멈춤 검정
    expect(codeStatusColor("failed")).toBe("var(--danger)"); // 문제 빨강
    expect(codeStatusColor("missing")).toBe("var(--danger)"); // 문제 빨강
    expect(codeStatusColor(undefined)).toBe("var(--ink)"); // 기본 멈춤
  });

  it("gives a glance label aligned with the dot color", () => {
    expect(codeStatusGlance("working")).toBe("진행중");
    expect(codeStatusGlance("passed")).toBe("멈춤");
    expect(codeStatusGlance("failed")).toBe("문제");
    expect(codeStatusGlance("missing")).toBe("문제 (워크트리 없음)");
  });

  it("gives a precise detail label", () => {
    expect(codeStatusLabel("working")).toBe("작업중");
    expect(codeStatusLabel("passed")).toBe("검증 통과");
    expect(codeStatusLabel("failed")).toBe("검증 실패");
    expect(codeStatusLabel("missing")).toBe("워크트리 없음");
  });
});
