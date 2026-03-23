import type { StreamFn } from "@mariozechner/pi-agent-core";
import type { Context, Model, SimpleStreamOptions } from "@mariozechner/pi-ai";
import { describe, expect, it, vi } from "vitest";

const resolveProviderCapabilitiesWithPluginMock = vi.fn(
  (params: { provider: string; workspaceDir?: string }) => {
    if (
      params.provider === "workspace-anthropic-proxy" &&
      params.workspaceDir === "/tmp/workspace-capabilities"
    ) {
      return {
        anthropicToolSchemaMode: "openai-functions",
        anthropicToolChoiceMode: "openai-string-modes",
      };
    }
    return undefined;
  },
);

vi.mock("../plugins/provider-runtime.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../plugins/provider-runtime.js")>();
  const {
    createOpenRouterSystemCacheWrapper,
    createOpenRouterWrapper,
    isProxyReasoningUnsupported,
  } = await import("./pi-embedded-runner/proxy-stream-wrappers.js");

  return {
    ...actual,
    prepareProviderExtraParams: (params: {
      provider: string;
      context: { extraParams?: Record<string, unknown> };
    }) => {
      if (params.provider !== "openai-codex") {
        return undefined;
      }
      const transport = params.context.extraParams?.transport;
      if (transport === "auto" || transport === "sse" || transport === "websocket") {
        return params.context.extraParams;
      }
      return {
        ...params.context.extraParams,
        transport: "auto",
      };
    },
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

      const providerRouting =
        params.context.extraParams?.provider != null &&
        typeof params.context.extraParams.provider === "object"
          ? (params.context.extraParams.provider as Record<string, unknown>)
          : undefined;
      let streamFn = params.context.streamFn;
      if (providerRouting) {
        const underlying = streamFn;
        streamFn = (model, context, options) =>
          (underlying as StreamFn)(
            {
              ...model,
              compat: { ...model.compat, openRouterRouting: providerRouting },
            },
            context,
            options,
          );
      }

      const skipReasoningInjection =
        params.context.modelId === "auto" || isProxyReasoningUnsupported(params.context.modelId);
      const thinkingLevel = skipReasoningInjection ? undefined : params.context.thinkingLevel;
      return createOpenRouterSystemCacheWrapper(createOpenRouterWrapper(streamFn, thinkingLevel));
    },
    resolveProviderCapabilitiesWithPlugin: (params: { provider: string; workspaceDir?: string }) =>
      resolveProviderCapabilitiesWithPluginMock(params),
  };
});

import { applyExtraParamsToAgent } from "./pi-embedded-runner.js";
import { log } from "./pi-embedded-runner/logger.js";

describe("applyExtraParamsToAgent — transport, headers & payload", () => {
  function createOptionsCaptureAgent() {
    const calls: Array<(SimpleStreamOptions & { openaiWsWarmup?: boolean }) | undefined> = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      calls.push(options as (SimpleStreamOptions & { openaiWsWarmup?: boolean }) | undefined);
      return {} as ReturnType<StreamFn>;
    };
    return {
      calls,
      agent: { streamFn: baseStreamFn },
    };
  }

  function buildAnthropicModelConfig(modelKey: string, params: Record<string, unknown>) {
    return {
      agents: {
        defaults: {
          models: {
            [modelKey]: { params },
          },
        },
      },
    };
  }

  function runResponsesPayloadMutationCase(params: {
    applyProvider: string;
    applyModelId: string;
    model:
      | Model<"openai-responses">
      | Model<"openai-codex-responses">
      | Model<"openai-completions">
      | Model<"anthropic-messages">;
    options?: SimpleStreamOptions;
    cfg?: Record<string, unknown>;
    extraParamsOverride?: Record<string, unknown>;
    payload?: Record<string, unknown>;
  }) {
    const payload = params.payload ?? { store: false };
    const baseStreamFn: StreamFn = (model, _context, options) => {
      options?.onPayload?.(payload, model);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };
    applyExtraParamsToAgent(
      agent,
      params.cfg as Parameters<typeof applyExtraParamsToAgent>[1],
      params.applyProvider,
      params.applyModelId,
      params.extraParamsOverride,
    );
    const context: Context = { messages: [] };
    void agent.streamFn?.(params.model, context, params.options ?? {});
    return payload;
  }

  function runAnthropicHeaderCase(params: {
    cfg: Record<string, unknown>;
    modelId: string;
    options?: SimpleStreamOptions;
  }) {
    const { calls, agent } = createOptionsCaptureAgent();
    applyExtraParamsToAgent(agent, params.cfg, "anthropic", params.modelId);

    const model = {
      api: "anthropic-messages",
      provider: "anthropic",
      id: params.modelId,
    } as Model<"anthropic-messages">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, params.options ?? {});

    expect(calls).toHaveLength(1);
    return calls[0]?.headers;
  }

  it("removes invalid negative Google thinkingBudget and maps Gemini 3.1 to thinkingLevel", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        contents: [
          {
            role: "user",
            parts: [
              { text: "describe image" },
              {
                inlineData: {
                  mimeType: "image/png",
                  data: "ZmFrZQ==",
                },
              },
            ],
          },
        ],
        config: {
          thinkingConfig: {
            includeThoughts: true,
            thinkingBudget: -1,
          },
        },
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "atproxy", "gemini-3.1-pro-high", undefined, "high");

    const model = {
      api: "google-generative-ai",
      provider: "atproxy",
      id: "gemini-3.1-pro-high",
    } as Model<"google-generative-ai">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    const thinkingConfig = (
      payloads[0]?.config as { thinkingConfig?: Record<string, unknown> } | undefined
    )?.thinkingConfig;
    expect(thinkingConfig).toEqual({
      includeThoughts: true,
      thinkingLevel: "HIGH",
    });
    expect(
      (
        payloads[0]?.contents as
          | Array<{ parts?: Array<{ inlineData?: { mimeType?: string; data?: string } }> }>
          | undefined
      )?.[0]?.parts?.[1]?.inlineData,
    ).toEqual({
      mimeType: "image/png",
      data: "ZmFrZQ==",
    });
  });

  it("keeps valid Google thinkingBudget unchanged", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        config: {
          thinkingConfig: {
            includeThoughts: true,
            thinkingBudget: 2048,
          },
        },
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "atproxy", "gemini-3.1-pro-high", undefined, "high");

    const model = {
      api: "google-generative-ai",
      provider: "atproxy",
      id: "gemini-3.1-pro-high",
    } as Model<"google-generative-ai">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.config).toEqual({
      thinkingConfig: {
        includeThoughts: true,
        thinkingBudget: 2048,
      },
    });
  });
  it("adds OpenRouter attribution headers to stream options", () => {
    const { calls, agent } = createOptionsCaptureAgent();

    applyExtraParamsToAgent(agent, undefined, "openrouter", "openrouter/auto");

    const model = {
      api: "openai-completions",
      provider: "openrouter",
      id: "openrouter/auto",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };

    void agent.streamFn?.(model, context, { headers: { "X-Custom": "1" } });

    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers).toEqual({
      "HTTP-Referer": "https://deneb.ai",
      "X-OpenRouter-Title": "Deneb",
      "X-OpenRouter-Categories": "cli-agent",
      "X-Custom": "1",
    });
  });

  it("passes configured websocket transport through stream options", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai-codex/gpt-5.3-codex": {
              params: {
                transport: "websocket",
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai-codex", "gpt-5.3-codex");

    const model = {
      api: "openai-codex-responses",
      provider: "openai-codex",
      id: "gpt-5.3-codex",
    } as Model<"openai-codex-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("websocket");
  });

  it("passes configured websocket transport through stream options for openai-codex gpt-5.4", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai-codex/gpt-5.4": {
              params: {
                transport: "websocket",
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai-codex", "gpt-5.4");

    const model = {
      api: "openai-codex-responses",
      provider: "openai-codex",
      id: "gpt-5.4",
    } as Model<"openai-codex-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("websocket");
  });

  it("defaults Codex transport to auto (WebSocket-first)", () => {
    const { calls, agent } = createOptionsCaptureAgent();

    applyExtraParamsToAgent(agent, undefined, "openai-codex", "gpt-5.3-codex");

    const model = {
      api: "openai-codex-responses",
      provider: "openai-codex",
      id: "gpt-5.3-codex",
    } as Model<"openai-codex-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("auto");
  });

  it("defaults OpenAI transport to auto (WebSocket-first)", () => {
    const { calls, agent } = createOptionsCaptureAgent();

    applyExtraParamsToAgent(agent, undefined, "openai", "gpt-5");

    const model = {
      api: "openai-responses",
      provider: "openai",
      id: "gpt-5",
    } as Model<"openai-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("auto");
    expect(calls[0]?.openaiWsWarmup).toBe(false);
  });

  it("lets runtime options override OpenAI default transport", () => {
    const { calls, agent } = createOptionsCaptureAgent();

    applyExtraParamsToAgent(agent, undefined, "openai", "gpt-5");

    const model = {
      api: "openai-responses",
      provider: "openai",
      id: "gpt-5",
    } as Model<"openai-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, { transport: "sse" });

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("sse");
  });

  it("allows disabling OpenAI websocket warm-up via model params", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai/gpt-5": {
              params: {
                openaiWsWarmup: false,
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai", "gpt-5");

    const model = {
      api: "openai-responses",
      provider: "openai",
      id: "gpt-5",
    } as Model<"openai-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.openaiWsWarmup).toBe(false);
  });

  it("lets runtime options override configured OpenAI websocket warm-up", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai/gpt-5": {
              params: {
                openaiWsWarmup: false,
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai", "gpt-5");

    const model = {
      api: "openai-responses",
      provider: "openai",
      id: "gpt-5",
    } as Model<"openai-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {
      openaiWsWarmup: true,
    } as unknown as SimpleStreamOptions);

    expect(calls).toHaveLength(1);
    expect(calls[0]?.openaiWsWarmup).toBe(true);
  });

  it("allows forcing Codex transport to SSE", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai-codex/gpt-5.3-codex": {
              params: {
                transport: "sse",
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai-codex", "gpt-5.3-codex");

    const model = {
      api: "openai-codex-responses",
      provider: "openai-codex",
      id: "gpt-5.3-codex",
    } as Model<"openai-codex-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("sse");
  });

  it("lets runtime options override configured transport", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai-codex/gpt-5.3-codex": {
              params: {
                transport: "websocket",
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai-codex", "gpt-5.3-codex");

    const model = {
      api: "openai-codex-responses",
      provider: "openai-codex",
      id: "gpt-5.3-codex",
    } as Model<"openai-codex-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, { transport: "sse" });

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("sse");
  });

  it("falls back to Codex default transport when configured value is invalid", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "openai-codex/gpt-5.3-codex": {
              params: {
                transport: "udp",
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "openai-codex", "gpt-5.3-codex");

    const model = {
      api: "openai-codex-responses",
      provider: "openai-codex",
      id: "gpt-5.3-codex",
    } as Model<"openai-codex-responses">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.transport).toBe("auto");
  });

  it("disables prompt caching for non-Anthropic Bedrock models", () => {
    const { calls, agent } = createOptionsCaptureAgent();

    applyExtraParamsToAgent(agent, undefined, "amazon-bedrock", "amazon.nova-micro-v1");

    const model = {
      api: "openai-completions",
      provider: "amazon-bedrock",
      id: "amazon.nova-micro-v1",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };

    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.cacheRetention).toBe("none");
  });

  it("keeps Anthropic Bedrock models eligible for provider-side caching", () => {
    const { calls, agent } = createOptionsCaptureAgent();

    applyExtraParamsToAgent(agent, undefined, "amazon-bedrock", "us.anthropic.claude-sonnet-4-5");

    const model = {
      api: "openai-completions",
      provider: "amazon-bedrock",
      id: "us.anthropic.claude-sonnet-4-5",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };

    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.cacheRetention).toBeUndefined();
  });

  it("passes through explicit cacheRetention for Anthropic Bedrock models", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = {
      agents: {
        defaults: {
          models: {
            "amazon-bedrock/us.anthropic.claude-opus-4-6-v1": {
              params: {
                cacheRetention: "long",
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "amazon-bedrock", "us.anthropic.claude-opus-4-6-v1");

    const model = {
      api: "openai-completions",
      provider: "amazon-bedrock",
      id: "us.anthropic.claude-opus-4-6-v1",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };

    void agent.streamFn?.(model, context, {});

    expect(calls).toHaveLength(1);
    expect(calls[0]?.cacheRetention).toBe("long");
  });

  it("adds Anthropic 1M beta header when context1m is enabled for Opus/Sonnet", () => {
    const { calls, agent } = createOptionsCaptureAgent();
    const cfg = buildAnthropicModelConfig("anthropic/claude-opus-4-6", { context1m: true });

    applyExtraParamsToAgent(agent, cfg, "anthropic", "claude-opus-4-6");

    const model = {
      api: "anthropic-messages",
      provider: "anthropic",
      id: "claude-opus-4-6",
    } as Model<"anthropic-messages">;
    const context: Context = { messages: [] };

    // Simulate pi-agent-core passing apiKey in options (API key, not OAuth token)
    void agent.streamFn?.(model, context, {
      apiKey: "sk-ant-api03-test", // pragma: allowlist secret
      headers: { "X-Custom": "1" },
    });

    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers).toEqual({
      "X-Custom": "1",
      // Includes pi-ai default betas (preserved to avoid overwrite) + context1m
      "anthropic-beta":
        "fine-grained-tool-streaming-2025-05-14,interleaved-thinking-2025-05-14,context-1m-2025-08-07",
    });
  });

  it("does not add Anthropic 1M beta header when context1m is not enabled", () => {
    const cfg = buildAnthropicModelConfig("anthropic/claude-opus-4-6", {
      temperature: 0.2,
    });
    const headers = runAnthropicHeaderCase({
      cfg,
      modelId: "claude-opus-4-6",
      options: { headers: { "X-Custom": "1" } },
    });

    expect(headers).toEqual({ "X-Custom": "1" });
  });

  it("skips context1m beta for OAuth tokens but preserves OAuth-required betas", () => {
    const calls: Array<SimpleStreamOptions | undefined> = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      calls.push(options);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };
    const cfg = {
      agents: {
        defaults: {
          models: {
            "anthropic/claude-sonnet-4-6": {
              params: {
                context1m: true,
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "anthropic", "claude-sonnet-4-6");

    const model = {
      api: "anthropic-messages",
      provider: "anthropic",
      id: "claude-sonnet-4-6",
    } as Model<"anthropic-messages">;
    const context: Context = { messages: [] };

    // Simulate pi-agent-core passing an OAuth token (sk-ant-oat-*) as apiKey
    void agent.streamFn?.(model, context, {
      apiKey: "sk-ant-oat01-test-oauth-token", // pragma: allowlist secret
      headers: { "X-Custom": "1" },
    });

    expect(calls).toHaveLength(1);
    const betaHeader = calls[0]?.headers?.["anthropic-beta"] as string;
    // Must include the OAuth-required betas so they aren't stripped by pi-ai's mergeHeaders
    expect(betaHeader).toContain("oauth-2025-04-20");
    expect(betaHeader).toContain("claude-code-20250219");
    expect(betaHeader).not.toContain("context-1m-2025-08-07");
  });

  it("merges existing anthropic-beta headers with configured betas", () => {
    const cfg = buildAnthropicModelConfig("anthropic/claude-sonnet-4-5", {
      context1m: true,
      anthropicBeta: ["files-api-2025-04-14"],
    });
    const headers = runAnthropicHeaderCase({
      cfg,
      modelId: "claude-sonnet-4-5",
      options: {
        apiKey: "sk-ant-api03-test", // pragma: allowlist secret
        headers: { "anthropic-beta": "prompt-caching-2024-07-31" },
      },
    });

    expect(headers).toEqual({
      "anthropic-beta":
        "prompt-caching-2024-07-31,fine-grained-tool-streaming-2025-05-14,interleaved-thinking-2025-05-14,files-api-2025-04-14,context-1m-2025-08-07",
    });
  });

  it("ignores context1m for non-Opus/Sonnet Anthropic models", () => {
    const cfg = buildAnthropicModelConfig("anthropic/claude-haiku-3-5", { context1m: true });
    const headers = runAnthropicHeaderCase({
      cfg,
      modelId: "claude-haiku-3-5",
      options: { headers: { "X-Custom": "1" } },
    });
    expect(headers).toEqual({ "X-Custom": "1" });
  });

  it("forces store=true for direct OpenAI Responses payloads", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.store).toBe(true);
  });

  it("forces store=true for azure-openai provider with openai-responses API (#42800)", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "azure-openai",
      applyModelId: "gpt-5-mini",
      model: {
        api: "openai-responses",
        provider: "azure-openai",
        id: "gpt-5-mini",
        baseUrl: "https://myresource.openai.azure.com/openai/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.store).toBe(true);
  });

  it("injects configured OpenAI service_tier into Responses payloads", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5.4",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5.4": {
                params: {
                  serviceTier: "priority",
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5.4",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.service_tier).toBe("priority");
  });

  it("preserves caller-provided service_tier values", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5.4",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5.4": {
                params: {
                  serviceTier: "priority",
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5.4",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        service_tier: "default",
      },
    });
    expect(payload.service_tier).toBe("default");
  });

  it("injects fast-mode payload defaults for direct OpenAI Responses", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5.4",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5.4": {
                params: {
                  fastMode: true,
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5.4",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
      },
    });
    expect(payload.reasoning).toEqual({ effort: "low" });
    expect(payload.text).toEqual({ verbosity: "low" });
    expect(payload.service_tier).toBe("priority");
  });

  it("preserves caller-provided OpenAI payload fields when fast mode is enabled", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5.4",
      extraParamsOverride: { fastMode: true },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5.4",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        reasoning: { effort: "medium" },
        text: { verbosity: "high" },
        service_tier: "default",
      },
    });
    expect(payload.reasoning).toEqual({ effort: "medium" });
    expect(payload.text).toEqual({ verbosity: "high" });
    expect(payload.service_tier).toBe("default");
  });

  it("injects service_tier=auto for Anthropic fast mode on direct API-key models", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "anthropic",
      applyModelId: "claude-sonnet-4-5",
      extraParamsOverride: { fastMode: true },
      model: {
        api: "anthropic-messages",
        provider: "anthropic",
        id: "claude-sonnet-4-5",
        baseUrl: "https://api.anthropic.com",
      } as unknown as Model<"anthropic-messages">,
      payload: {},
    });
    expect(payload.service_tier).toBe("auto");
  });

  it("injects service_tier=standard_only for Anthropic fast mode off", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "anthropic",
      applyModelId: "claude-sonnet-4-5",
      extraParamsOverride: { fastMode: false },
      model: {
        api: "anthropic-messages",
        provider: "anthropic",
        id: "claude-sonnet-4-5",
        baseUrl: "https://api.anthropic.com",
      } as unknown as Model<"anthropic-messages">,
      payload: {},
    });
    expect(payload.service_tier).toBe("standard_only");
  });

  it("preserves caller-provided Anthropic service_tier values", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "anthropic",
      applyModelId: "claude-sonnet-4-5",
      extraParamsOverride: { fastMode: true },
      model: {
        api: "anthropic-messages",
        provider: "anthropic",
        id: "claude-sonnet-4-5",
        baseUrl: "https://api.anthropic.com",
      } as unknown as Model<"anthropic-messages">,
      payload: {
        service_tier: "standard_only",
      },
    });
    expect(payload.service_tier).toBe("standard_only");
  });

  it("does not inject Anthropic fast mode service_tier for OAuth auth", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "anthropic",
      applyModelId: "claude-sonnet-4-5",
      extraParamsOverride: { fastMode: true },
      model: {
        api: "anthropic-messages",
        provider: "anthropic",
        id: "claude-sonnet-4-5",
        baseUrl: "https://api.anthropic.com",
      } as unknown as Model<"anthropic-messages">,
      options: {
        apiKey: "sk-ant-oat-test-token",
      },
      payload: {},
    });
    expect(payload).not.toHaveProperty("service_tier");
  });

  it("does not inject Anthropic fast mode service_tier for proxied base URLs", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "anthropic",
      applyModelId: "claude-sonnet-4-5",
      extraParamsOverride: { fastMode: true },
      model: {
        api: "anthropic-messages",
        provider: "anthropic",
        id: "claude-sonnet-4-5",
        baseUrl: "https://proxy.example.com/anthropic",
      } as unknown as Model<"anthropic-messages">,
      payload: {},
    });
    expect(payload).not.toHaveProperty("service_tier");
  });

  it("applies fast-mode defaults for openai-codex responses without service_tier", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai-codex",
      applyModelId: "gpt-5.4",
      extraParamsOverride: { fastMode: true },
      model: {
        api: "openai-codex-responses",
        provider: "openai-codex",
        id: "gpt-5.4",
        baseUrl: "https://chatgpt.com/backend-api",
      } as unknown as Model<"openai-codex-responses">,
      payload: {
        store: false,
      },
    });
    expect(payload.reasoning).toEqual({ effort: "low" });
    expect(payload.text).toEqual({ verbosity: "low" });
    expect(payload).not.toHaveProperty("service_tier");
  });

  it("does not inject service_tier for non-openai providers", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "azure-openai-responses",
      applyModelId: "gpt-5.4",
      cfg: {
        agents: {
          defaults: {
            models: {
              "azure-openai-responses/gpt-5.4": {
                params: {
                  serviceTier: "priority",
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "azure-openai-responses",
        id: "gpt-5.4",
        baseUrl: "https://example.openai.azure.com/openai/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("service_tier");
  });

  it("does not inject service_tier for proxied openai base URLs", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5.4",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5.4": {
                params: {
                  serviceTier: "priority",
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5.4",
        baseUrl: "https://proxy.example.com/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("service_tier");
  });

  it("does not inject service_tier for openai provider routed to Azure base URLs", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5.4",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5.4": {
                params: {
                  serviceTier: "priority",
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5.4",
        baseUrl: "https://example.openai.azure.com/openai/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("service_tier");
  });

  it("warns and skips service_tier injection for invalid serviceTier values", () => {
    const warnSpy = vi.spyOn(log, "warn").mockImplementation(() => undefined);
    try {
      const payload = runResponsesPayloadMutationCase({
        applyProvider: "openai",
        applyModelId: "gpt-5.4",
        cfg: {
          agents: {
            defaults: {
              models: {
                "openai/gpt-5.4": {
                  params: {
                    serviceTier: "invalid",
                  },
                },
              },
            },
          },
        },
        model: {
          api: "openai-responses",
          provider: "openai",
          id: "gpt-5.4",
          baseUrl: "https://api.openai.com/v1",
        } as unknown as Model<"openai-responses">,
      });

      expect(payload).not.toHaveProperty("service_tier");
      expect(warnSpy).toHaveBeenCalledWith("ignoring invalid OpenAI service tier param: invalid");
    } finally {
      warnSpy.mockRestore();
    }
  });

  it("does not force store for OpenAI Responses routed through non-OpenAI base URLs", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "https://proxy.example.com/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.store).toBe(false);
  });

  it("does not force store for OpenAI Responses when baseUrl is empty", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.store).toBe(false);
  });

  it("strips store from payload for models that declare supportsStore=false", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "azure-openai-responses",
      applyModelId: "gpt-4o",
      model: {
        api: "openai-responses",
        provider: "azure-openai-responses",
        id: "gpt-4o",
        name: "gpt-4o",
        baseUrl: "https://example.openai.azure.com/openai/v1",
        reasoning: false,
        input: ["text"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 128_000,
        maxTokens: 16_384,
        compat: { supportsStore: false },
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("store");
  });

  it("strips store from payload for non-OpenAI responses providers with supportsStore=false", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "custom-openai-responses",
      applyModelId: "gemini-2.5-pro",
      model: {
        api: "openai-responses",
        provider: "custom-openai-responses",
        id: "gemini-2.5-pro",
        name: "gemini-2.5-pro",
        baseUrl: "https://gateway.ai.cloudflare.com/v1/account/gateway/openai",
        reasoning: false,
        input: ["text"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 1_000_000,
        maxTokens: 65_536,
        compat: { supportsStore: false },
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("store");
  });

  it("keeps existing context_management when stripping store for supportsStore=false models", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "custom-openai-responses",
      applyModelId: "gemini-2.5-pro",
      model: {
        api: "openai-responses",
        provider: "custom-openai-responses",
        id: "gemini-2.5-pro",
        name: "gemini-2.5-pro",
        baseUrl: "https://gateway.ai.cloudflare.com/v1/account/gateway/openai",
        reasoning: false,
        input: ["text"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 1_000_000,
        maxTokens: 65_536,
        compat: { supportsStore: false },
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        context_management: [{ type: "compaction", compact_threshold: 12_345 }],
      },
    });
    expect(payload).not.toHaveProperty("store");
    expect(payload.context_management).toEqual([{ type: "compaction", compact_threshold: 12_345 }]);
  });

  it("auto-injects OpenAI Responses context_management compaction for direct OpenAI models", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "https://api.openai.com/v1",
        contextWindow: 200_000,
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.context_management).toEqual([
      {
        type: "compaction",
        compact_threshold: 140_000,
      },
    ]);
  });

  it("does not auto-inject OpenAI Responses context_management for Azure by default", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "azure-openai-responses",
      applyModelId: "gpt-4o",
      model: {
        api: "openai-responses",
        provider: "azure-openai-responses",
        id: "gpt-4o",
        baseUrl: "https://example.openai.azure.com/openai/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("context_management");
  });

  it("allows explicitly enabling OpenAI Responses context_management compaction", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "azure-openai-responses",
      applyModelId: "gpt-4o",
      cfg: {
        agents: {
          defaults: {
            models: {
              "azure-openai-responses/gpt-4o": {
                params: {
                  responsesServerCompaction: true,
                  responsesCompactThreshold: 42_000,
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "azure-openai-responses",
        id: "gpt-4o",
        baseUrl: "https://example.openai.azure.com/openai/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload.context_management).toEqual([
      {
        type: "compaction",
        compact_threshold: 42_000,
      },
    ]);
  });

  it("preserves existing context_management payload values", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        context_management: [{ type: "compaction", compact_threshold: 12_345 }],
      },
    });
    expect(payload.context_management).toEqual([{ type: "compaction", compact_threshold: 12_345 }]);
  });

  it("allows disabling OpenAI Responses context_management compaction via model params", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5": {
                params: {
                  responsesServerCompaction: false,
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
    });
    expect(payload).not.toHaveProperty("context_management");
  });

  it.each([
    {
      name: "with openai-codex provider config",
      run: () =>
        runResponsesPayloadMutationCase({
          applyProvider: "openai-codex",
          applyModelId: "codex-mini-latest",
          model: {
            api: "openai-codex-responses",
            provider: "openai-codex",
            id: "codex-mini-latest",
            baseUrl: "https://chatgpt.com/backend-api/codex/responses",
          } as Model<"openai-codex-responses">,
        }),
    },
    {
      name: "without config via provider/model hints",
      run: () =>
        runResponsesPayloadMutationCase({
          applyProvider: "openai-codex",
          applyModelId: "codex-mini-latest",
          model: {
            api: "openai-codex-responses",
            provider: "openai-codex",
            id: "codex-mini-latest",
            baseUrl: "https://chatgpt.com/backend-api/codex/responses",
          } as Model<"openai-codex-responses">,
          options: {},
        }),
    },
  ])(
    "does not force store=true for Codex responses (Codex requires store=false) ($name)",
    ({ run }) => {
      expect(run().store).toBe(false);
    },
  );

  it("strips prompt cache fields for non-OpenAI openai-responses endpoints", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "custom-proxy",
      applyModelId: "some-model",
      model: {
        api: "openai-responses",
        provider: "custom-proxy",
        id: "some-model",
        baseUrl: "https://my-proxy.example.com/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        prompt_cache_key: "session-xyz",
        prompt_cache_retention: "24h",
      },
    });
    expect(payload).not.toHaveProperty("prompt_cache_key");
    expect(payload).not.toHaveProperty("prompt_cache_retention");
  });

  it("keeps prompt cache fields for direct OpenAI openai-responses endpoints", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
        baseUrl: "https://api.openai.com/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        prompt_cache_key: "session-123",
        prompt_cache_retention: "24h",
      },
    });
    expect(payload.prompt_cache_key).toBe("session-123");
    expect(payload.prompt_cache_retention).toBe("24h");
  });

  it("keeps prompt cache fields for direct Azure OpenAI openai-responses endpoints", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "azure-openai-responses",
      applyModelId: "gpt-4o",
      model: {
        api: "openai-responses",
        provider: "azure-openai-responses",
        id: "gpt-4o",
        baseUrl: "https://example.openai.azure.com/openai/v1",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        prompt_cache_key: "session-azure",
        prompt_cache_retention: "24h",
      },
    });
    expect(payload.prompt_cache_key).toBe("session-azure");
    expect(payload.prompt_cache_retention).toBe("24h");
  });

  it("keeps prompt cache fields when openai-responses baseUrl is omitted", () => {
    const payload = runResponsesPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      model: {
        api: "openai-responses",
        provider: "openai",
        id: "gpt-5",
      } as unknown as Model<"openai-responses">,
      payload: {
        store: false,
        prompt_cache_key: "session-default",
        prompt_cache_retention: "24h",
      },
    });
    expect(payload.prompt_cache_key).toBe("session-default");
    expect(payload.prompt_cache_retention).toBe("24h");
  });
});
