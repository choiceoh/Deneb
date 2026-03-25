import { describe, expect, it } from "vitest";
import {
  DEFAULT_FAST_MODEL,
  DEFAULT_MODEL,
  DEFAULT_REASONING_MODEL,
  DEFAULT_THINKING_MODEL,
  resolveAgentModel,
} from "./defaults.js";

describe("resolveAgentModel", () => {
  it("returns default model for default mode", () => {
    expect(resolveAgentModel("default")).toBe(DEFAULT_MODEL);
  });

  it("returns thinking model for thinking mode", () => {
    expect(resolveAgentModel("thinking")).toBe(DEFAULT_THINKING_MODEL);
  });

  it("returns fast model for fast mode", () => {
    expect(resolveAgentModel("fast")).toBe(DEFAULT_FAST_MODEL);
  });

  it("returns reasoning model for reasoning mode", () => {
    expect(resolveAgentModel("reasoning")).toBe(DEFAULT_REASONING_MODEL);
  });

  it("uses agent-level override when provided", () => {
    const agentDefaults = { fastModel: "custom-fast-model" };
    expect(resolveAgentModel("fast", agentDefaults)).toBe("custom-fast-model");
  });

  it("falls back to default when agent override model is unavailable", () => {
    const agentDefaults = { fastModel: "unavailable-model" };
    const opts = { isModelAvailable: (id: string) => id !== "unavailable-model" };
    expect(resolveAgentModel("fast", agentDefaults, opts)).toBe(DEFAULT_MODEL);
  });

  it("uses agent model override for default mode", () => {
    const agentDefaults = { model: "custom-default" };
    expect(resolveAgentModel("default", agentDefaults)).toBe("custom-default");
  });
});
