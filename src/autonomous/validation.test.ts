import { describe, expect, it } from "vitest";
import {
  clampDelay,
  formatTimestamp,
  isFiniteNonNegative,
  isFinitePositive,
  safeDateParse,
  safeDivide,
  safeIsoString,
  safeMaxPerHour,
  safeTimestamp,
  sanitizeId,
  sanitizeText,
  validateStringArray,
} from "./validation.js";

describe("isFinitePositive", () => {
  it("returns true for positive finite numbers", () => {
    expect(isFinitePositive(1)).toBe(true);
    expect(isFinitePositive(0.001)).toBe(true);
    expect(isFinitePositive(Number.MAX_SAFE_INTEGER)).toBe(true);
  });

  it("returns false for zero, negative, NaN, Infinity, non-numbers", () => {
    expect(isFinitePositive(0)).toBe(false);
    expect(isFinitePositive(-1)).toBe(false);
    expect(isFinitePositive(NaN)).toBe(false);
    expect(isFinitePositive(Infinity)).toBe(false);
    expect(isFinitePositive(-Infinity)).toBe(false);
    expect(isFinitePositive("1")).toBe(false);
    expect(isFinitePositive(null)).toBe(false);
    expect(isFinitePositive(undefined)).toBe(false);
  });
});

describe("isFiniteNonNegative", () => {
  it("returns true for zero and positive", () => {
    expect(isFiniteNonNegative(0)).toBe(true);
    expect(isFiniteNonNegative(42)).toBe(true);
  });

  it("rejects negative, NaN, Infinity", () => {
    expect(isFiniteNonNegative(-1)).toBe(false);
    expect(isFiniteNonNegative(NaN)).toBe(false);
    expect(isFiniteNonNegative(Infinity)).toBe(false);
  });
});

describe("safeTimestamp", () => {
  it("returns valid timestamps", () => {
    const ts = Date.now();
    expect(safeTimestamp(ts)).toBe(ts);
  });

  it("returns fallback for invalid values", () => {
    expect(safeTimestamp(NaN)).toBe(0);
    expect(safeTimestamp(Infinity)).toBe(0);
    expect(safeTimestamp(-1)).toBe(0);
    expect(safeTimestamp("123")).toBe(0);
    expect(safeTimestamp(null)).toBe(0);
    expect(safeTimestamp(undefined)).toBe(0);
  });

  it("rejects timestamps beyond year 2100", () => {
    expect(safeTimestamp(5_000_000_000_000)).toBe(0);
  });

  it("uses custom fallback", () => {
    expect(safeTimestamp(NaN, 42)).toBe(42);
  });
});

describe("safeDateParse", () => {
  it("parses valid ISO strings", () => {
    const result = safeDateParse("2024-01-15T10:00:00Z");
    expect(result).toBeTypeOf("number");
    expect(Number.isFinite(result)).toBe(true);
  });

  it("returns undefined for invalid strings", () => {
    expect(safeDateParse("not-a-date")).toBe(undefined);
    expect(safeDateParse("")).toBe(undefined);
    expect(safeDateParse(null)).toBe(undefined);
    expect(safeDateParse(undefined)).toBe(undefined);
  });
});

describe("clampDelay", () => {
  it("clamps to range", () => {
    expect(clampDelay(500, 100, 1000)).toBe(500);
    expect(clampDelay(50, 100, 1000)).toBe(100);
    expect(clampDelay(2000, 100, 1000)).toBe(1000);
  });

  it("handles NaN and Infinity", () => {
    expect(clampDelay(NaN, 100, 1000)).toBe(100);
    expect(clampDelay(Infinity, 100, 1000)).toBe(100);
    expect(clampDelay(-Infinity, 100, 1000)).toBe(100);
  });
});

describe("safeMaxPerHour", () => {
  it("returns valid values", () => {
    expect(safeMaxPerHour(10)).toBe(10);
    expect(safeMaxPerHour(1)).toBe(1);
  });

  it("returns fallback for invalid", () => {
    expect(safeMaxPerHour(0)).toBe(12);
    expect(safeMaxPerHour(-5)).toBe(12);
    expect(safeMaxPerHour(NaN)).toBe(12);
    expect(safeMaxPerHour(null)).toBe(12);
    expect(safeMaxPerHour("10")).toBe(12);
  });

  it("caps at 1000", () => {
    expect(safeMaxPerHour(9999)).toBe(1000);
  });
});

describe("sanitizeText", () => {
  it("trims and limits length", () => {
    expect(sanitizeText("  hello  ")).toBe("hello");
    expect(sanitizeText("a".repeat(20000), 100).length).toBe(100);
  });

  it("removes null bytes", () => {
    expect(sanitizeText("hello\0world")).toBe("helloworld");
  });

  it("handles non-strings", () => {
    expect(sanitizeText(123)).toBe("");
    expect(sanitizeText(null)).toBe("");
    expect(sanitizeText(undefined)).toBe("");
  });
});

describe("sanitizeId", () => {
  it("trims and limits length", () => {
    expect(sanitizeId("  abc-123  ")).toBe("abc-123");
    expect(sanitizeId("a".repeat(300)).length).toBe(200);
  });

  it("handles non-strings", () => {
    expect(sanitizeId(42)).toBe("");
    expect(sanitizeId(null)).toBe("");
  });
});

describe("validateStringArray", () => {
  it("filters non-strings", () => {
    expect(validateStringArray(["a", 1, null, "b", {}, "c"])).toEqual(["a", "b", "c"]);
  });

  it("returns empty for non-arrays", () => {
    expect(validateStringArray("not-array")).toEqual([]);
    expect(validateStringArray(null)).toEqual([]);
    expect(validateStringArray(undefined)).toEqual([]);
  });
});

describe("safeDivide", () => {
  it("divides normally", () => {
    expect(safeDivide(10, 2)).toBe(5);
  });

  it("returns fallback for divide by zero", () => {
    expect(safeDivide(10, 0)).toBe(0);
    expect(safeDivide(10, 0, 99)).toBe(99);
  });

  it("returns fallback for NaN", () => {
    expect(safeDivide(NaN, 2)).toBe(0);
    expect(safeDivide(10, NaN)).toBe(0);
  });
});

describe("formatTimestamp", () => {
  it("formats valid timestamps", () => {
    const result = formatTimestamp(Date.now());
    expect(result).not.toBe("never");
    expect(result).not.toBe("invalid");
  });

  it("returns 'never' for zero or invalid", () => {
    expect(formatTimestamp(0)).toBe("never");
    expect(formatTimestamp(-1)).toBe("never");
    expect(formatTimestamp(NaN)).toBe("never");
  });
});

describe("safeIsoString", () => {
  it("formats valid timestamps", () => {
    const result = safeIsoString(Date.now());
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  it("returns null for invalid", () => {
    expect(safeIsoString(NaN)).toBe(null);
    expect(safeIsoString(-1)).toBe(null);
    expect(safeIsoString(Infinity)).toBe(null);
  });
});
