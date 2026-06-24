import { describe, expect, it } from "vitest";
import { parseDayKey, toLocalInput, visibleRangeForMonth } from "./calendarHelpers";

describe("toLocalInput", () => {
  it("is empty for missing or unparseable input", () => {
    expect(toLocalInput()).toBe("");
    expect(toLocalInput("not a date")).toBe("");
  });
  it("formats to a minute-precision datetime-local value (local wall-clock)", () => {
    // Round-trip a locally-built instant so the assertion is timezone-independent.
    const d = new Date(2026, 5, 7, 9, 4); // 2026-06-07 09:04 local
    expect(toLocalInput(d.toISOString())).toBe("2026-06-07T09:04");
  });
});

describe("parseDayKey", () => {
  it("parses a YYYY-M-D key to a local date", () => {
    const d = parseDayKey("2026-6-7");
    expect(d?.getFullYear()).toBe(2026);
    expect(d?.getMonth()).toBe(5); // 0-based June
    expect(d?.getDate()).toBe(7);
  });
  it("returns null for empty or malformed keys", () => {
    expect(parseDayKey()).toBeNull();
    expect(parseDayKey("2026-06")).toBeNull();
    expect(parseDayKey("garbage")).toBeNull();
  });
});

describe("visibleRangeForMonth", () => {
  it("spans whole weeks and carries a matching cache key + label", () => {
    const r = visibleRangeForMonth(2026, 5); // June 2026
    expect(new Date(r.from).getTime()).toBeLessThan(new Date(r.to).getTime());
    expect(r.cacheKey).toBe(`calendar-range.${r.from}.${r.to}`);
    expect(r.label).toContain("2026");
  });
});
