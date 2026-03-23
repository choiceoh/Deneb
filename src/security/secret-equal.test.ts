import { describe, expect, it } from "vitest";
import { safeEqualSecret } from "./secret-equal.js";

describe("safeEqualSecret", () => {
  it("returns true for equal strings", () => {
    expect(safeEqualSecret("abc123", "abc123")).toBe(true);
  });

  it("returns false for different strings", () => {
    expect(safeEqualSecret("abc123", "abc124")).toBe(false);
  });

  it("returns false for different lengths", () => {
    expect(safeEqualSecret("short", "longer-string")).toBe(false);
  });

  it("returns false when provided is null/undefined", () => {
    expect(safeEqualSecret(null, "secret")).toBe(false);
    expect(safeEqualSecret(undefined, "secret")).toBe(false);
  });

  it("returns false when expected is null/undefined", () => {
    expect(safeEqualSecret("secret", null)).toBe(false);
    expect(safeEqualSecret("secret", undefined)).toBe(false);
  });

  it("returns false when both are null/undefined", () => {
    expect(safeEqualSecret(null, null)).toBe(false);
    expect(safeEqualSecret(undefined, undefined)).toBe(false);
  });

  it("handles empty strings", () => {
    expect(safeEqualSecret("", "")).toBe(true);
    expect(safeEqualSecret("", "a")).toBe(false);
  });
});
