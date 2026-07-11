import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getJSON, getString, remove, setJSON, setString } from "./storage";

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

describe("JSON storage", () => {
  it("round-trips structured values", () => {
    setJSON("state", { count: 2, rows: ["a", "b"] });
    expect(getJSON("state")).toEqual({ count: 2, rows: ["a", "b"] });
  });

  it("returns undefined for missing, empty, or malformed data", () => {
    expect(getJSON("missing")).toBeUndefined();
    localStorage.setItem("empty", "");
    localStorage.setItem("broken", "{not-json");
    expect(getJSON("empty")).toBeUndefined();
    expect(getJSON("broken")).toBeUndefined();
  });

  it("swallows read and write failures", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    expect(getJSON("state")).toBeUndefined();
    vi.restoreAllMocks();

    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("quota", "QuotaExceededError");
    });
    expect(() => setJSON("state", { value: 1 })).not.toThrow();
  });

  it("swallows values that JSON cannot serialize", () => {
    const circular: Record<string, unknown> = {};
    circular.self = circular;
    expect(() => setJSON("circular", circular)).not.toThrow();
    expect(localStorage.getItem("circular")).toBeNull();
  });
});

describe("string storage", () => {
  it("round-trips and removes plain values", () => {
    setString("token", "abc");
    expect(getString("token")).toBe("abc");
    remove("token");
    expect(getString("token")).toBe("");
  });

  it("preserves an intentionally empty string", () => {
    setString("token", "");
    expect(localStorage.getItem("token")).toBe("");
    expect(getString("token")).toBe("");
  });

  it("swallows storage access failures", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("read blocked");
    });
    expect(getString("token")).toBe("");
    vi.restoreAllMocks();

    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("write blocked");
    });
    expect(() => setString("token", "abc")).not.toThrow();
    vi.restoreAllMocks();

    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
      throw new Error("remove blocked");
    });
    expect(() => remove("token")).not.toThrow();
  });
});
