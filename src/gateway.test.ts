import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadConfig } from "./gateway";

const KEY = "andromeda.gateway";

beforeEach(() => localStorage.clear());
afterEach(() => {
  localStorage.clear();
  vi.unstubAllEnvs();
});

describe("loadConfig", () => {
  it("returns empty when nothing is configured", () => {
    expect(loadConfig()).toEqual({ url: "", token: "" });
  });

  it("falls back to env defaults for auto-connect", () => {
    vi.stubEnv("VITE_GATEWAY_URL", "http://env:18789");
    vi.stubEnv("VITE_GATEWAY_TOKEN", "envtoken");
    expect(loadConfig()).toEqual({ url: "http://env:18789", token: "envtoken" });
  });

  it("prefers a saved config over env per field", () => {
    vi.stubEnv("VITE_GATEWAY_URL", "http://env:18789");
    vi.stubEnv("VITE_GATEWAY_TOKEN", "envtoken");
    localStorage.setItem(KEY, JSON.stringify({ url: "http://saved:18789", token: "" }));
    // saved url wins; empty saved token falls back to env token.
    expect(loadConfig()).toEqual({ url: "http://saved:18789", token: "envtoken" });
  });
});
