import type { StreamFn } from "@mariozechner/pi-agent-core";
import { describe, expect, it, vi } from "vitest";

vi.mock("../plugins/provider-runtime.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../plugins/provider-runtime.js")>();
  const {
    createOpenRouterSystemCacheWrapper,
    createOpenRouterWrapper,
    isProxyReasoningUnsupported,
  } = await import("./pi-embedded-runner/proxy-stream-wrappers.js");

  return {
    ...actual,
    prepareProviderExtraParams: () => undefined,
    wrapProviderStreamFn: (params: {
      provider: string;
      context: {
        modelId: string;
        thinkingLevel?: import("../auto-reply/thinking.js").ThinkLevel;
        extraParams?: Record<string, unknown>;
        streamFn?: StreamFn;
      };
    }) => {
      if (params.provider !== "openrouter") {
        return params.context.streamFn;
      }
      const skipReasoningInjection =
        params.context.modelId === "auto" || isProxyReasoningUnsupported(params.context.modelId);
      const thinkingLevel = skipReasoningInjection ? undefined : params.context.thinkingLevel;
      return createOpenRouterSystemCacheWrapper(
        createOpenRouterWrapper(params.context.streamFn, thinkingLevel),
      );
    },
    resolveProviderCapabilitiesWithPlugin: () => undefined,
  };
});

import { resolveExtraParams } from "./pi-embedded-runner.js";

describe("resolveExtraParams", () => {
  it("returns undefined with no model config", () => {
    const result = resolveExtraParams({
      cfg: undefined,
      provider: "zai",
      modelId: "glm-4.7",
    });

    expect(result).toBeUndefined();
  });

  it("returns params for exact provider/model key", () => {
    const result = resolveExtraParams({
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-4": {
                params: {
                  temperature: 0.7,
                  maxTokens: 2048,
                },
              },
            },
          },
        },
      },
      provider: "openai",
      modelId: "gpt-4",
    });

    expect(result).toEqual({
      temperature: 0.7,
      maxTokens: 2048,
    });
  });

  it("ignores unrelated model entries", () => {
    const result = resolveExtraParams({
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-4": {
                params: {
                  temperature: 0.7,
                },
              },
            },
          },
        },
      },
      provider: "openai",
      modelId: "gpt-4.1-mini",
    });

    expect(result).toBeUndefined();
  });

  it("returns per-agent params when agentId matches", () => {
    const result = resolveExtraParams({
      cfg: {
        agents: {
          list: [
            {
              id: "risk-reviewer",
              params: { cacheRetention: "none" },
            },
          ],
        },
      },
      provider: "anthropic",
      modelId: "claude-opus-4-6",
      agentId: "risk-reviewer",
    });

    expect(result).toEqual({ cacheRetention: "none" });
  });

  it("merges per-agent params over global model defaults", () => {
    const result = resolveExtraParams({
      cfg: {
        agents: {
          defaults: {
            models: {
              "anthropic/claude-opus-4-6": {
                params: {
                  temperature: 0.5,
                  cacheRetention: "long",
                },
              },
            },
          },
          list: [
            {
              id: "risk-reviewer",
              params: { cacheRetention: "none" },
            },
          ],
        },
      },
      provider: "anthropic",
      modelId: "claude-opus-4-6",
      agentId: "risk-reviewer",
    });

    expect(result).toEqual({
      temperature: 0.5,
      cacheRetention: "none",
    });
  });

  it("preserves higher-precedence agent parallelToolCalls override across alias styles", () => {
    const result = resolveExtraParams({
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-4.1": {
                params: {
                  parallel_tool_calls: true,
                },
              },
            },
          },
          list: [
            {
              id: "main",
              params: {
                parallelToolCalls: false,
              },
            },
          ],
        },
      },
      provider: "openai",
      modelId: "gpt-4.1",
      agentId: "main",
    });

    expect(result).toEqual({
      parallel_tool_calls: false,
    });
  });

  it("ignores per-agent params when agentId does not match", () => {
    const result = resolveExtraParams({
      cfg: {
        agents: {
          list: [
            {
              id: "risk-reviewer",
              params: { cacheRetention: "none" },
            },
          ],
        },
      },
      provider: "anthropic",
      modelId: "claude-opus-4-6",
      agentId: "main",
    });

    expect(result).toBeUndefined();
  });
});
