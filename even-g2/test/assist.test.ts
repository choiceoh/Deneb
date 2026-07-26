import { describe, expect, it } from "vitest";
import {
  ASSIST_MAX_CHARS,
  ASSIST_PROMPT,
  ASSIST_SILENT,
  assistLine,
  shouldAssistNow,
} from "../src/assist";

// The bar this module has to clear: during a live negotiation, a wrong or noisy
// line costs more than a missed one. Every ambiguous case must resolve to
// silence.
describe("assistLine", () => {
  it("passes a short grounded fact through", () => {
    expect(assistLine("당진 현장 준공검사 8/14")).toBe(
      "당진 현장 준공검사 8/14",
    );
  });

  it("treats the silence verdict as nothing, however it is wrapped", () => {
    expect(assistLine(ASSIST_SILENT)).toBeNull();
    expect(assistLine("  nothing  ")).toBeNull();
    expect(assistLine("NOTHING.")).toBeNull();
    expect(assistLine("죄송합니다, NOTHING")).toBeNull();
  });

  it("is silent on empty and whitespace", () => {
    expect(assistLine(undefined)).toBeNull();
    expect(assistLine("")).toBeNull();
    expect(assistLine("   \n  ")).toBeNull();
  });

  it("drops a line too long to read mid-conversation", () => {
    expect(assistLine("가".repeat(ASSIST_MAX_CHARS + 1))).toBeNull();
    expect(assistLine("가".repeat(ASSIST_MAX_CHARS))).toHaveLength(
      ASSIST_MAX_CHARS,
    );
  });

  it("takes only the first line when the model rambles", () => {
    expect(assistLine("금호 미수금 3200만\n추가로 설명하자면…")).toBe(
      "금호 미수금 3200만",
    );
  });

  it("strips quoting and bullet furniture", () => {
    expect(assistLine('"기아 모듈 입고 3월 중순"')).toBe(
      "기아 모듈 입고 3월 중순",
    );
    expect(assistLine("- 옹진 인허가 진행 중")).toBe("옹진 인허가 진행 중");
  });

  it("is silent on a reply with no actual content", () => {
    expect(assistLine("…")).toBeNull();
    expect(assistLine("-")).toBeNull();
    expect(assistLine("???")).toBeNull();
  });
});

describe("ASSIST_PROMPT", () => {
  it("names the silence verdict and forbids invention", () => {
    // A plausible invented figure read out during a negotiation is the worst
    // outcome this feature can produce, and nobody scrolls up to check a HUD.
    expect(ASSIST_PROMPT).toContain(ASSIST_SILENT);
    expect(ASSIST_PROMPT).toContain("추측");
    expect(ASSIST_PROMPT).toContain("위키에서 확인한 사실만");
  });
});

describe("shouldAssistNow", () => {
  it("never overlaps an in-flight analysis", () => {
    // A chat turn takes seconds; stacking them queues the HUD behind a
    // conversation that has already moved on.
    expect(shouldAssistNow(100_000, 0, true, 25_000)).toBe(false);
  });

  it("runs immediately on the first window", () => {
    expect(shouldAssistNow(1_000, 0, false, 25_000)).toBe(true);
  });

  it("waits out the period", () => {
    expect(shouldAssistNow(30_000, 20_000, false, 25_000)).toBe(false);
    expect(shouldAssistNow(45_000, 20_000, false, 25_000)).toBe(true);
  });
});
