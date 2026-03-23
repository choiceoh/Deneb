import type { StreamFn } from "@mariozechner/pi-agent-core";
import type { Context, Model } from "@mariozechner/pi-ai";
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

describe("applyExtraParamsToAgent — reasoning & thinking", () => {
  function runParallelToolCallsPayloadMutationCase(params: {
    applyProvider: string;
    applyModelId: string;
    model: Model<"openai-completions"> | Model<"openai-responses"> | Model<"anthropic-messages">;
    cfg?: Record<string, unknown>;
    extraParamsOverride?: Record<string, unknown>;
    payload?: Record<string, unknown>;
  }) {
    const payload = params.payload ?? {};
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
    void agent.streamFn?.(params.model, context, {});
    return payload;
  }

  it("does not inject reasoning when thinkingLevel is off (default) for OpenRouter", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { model: "deepseek/deepseek-r1" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(
      agent,
      undefined,
      "openrouter",
      "deepseek/deepseek-r1",
      undefined,
      "off",
    );

    const model = {
      api: "openai-completions",
      provider: "openrouter",
      id: "deepseek/deepseek-r1",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]).not.toHaveProperty("reasoning");
    expect(payloads[0]).not.toHaveProperty("reasoning_effort");
  });

  it("injects reasoning.effort when thinkingLevel is non-off for OpenRouter", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {};
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "openrouter", "openrouter/auto", undefined, "low");

    const model = {
      api: "openai-completions",
      provider: "openrouter",
      id: "openrouter/auto",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.reasoning).toEqual({ effort: "low" });
  });

  it("removes legacy reasoning_effort and keeps reasoning unset when thinkingLevel is off", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { reasoning_effort: "high" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "openrouter", "openrouter/auto", undefined, "off");

    const model = {
      api: "openai-completions",
      provider: "openrouter",
      id: "openrouter/auto",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]).not.toHaveProperty("reasoning_effort");
    expect(payloads[0]).not.toHaveProperty("reasoning");
  });

  it("does not inject effort when payload already has reasoning.max_tokens", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { reasoning: { max_tokens: 256 } };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "openrouter", "openrouter/auto", undefined, "low");

    const model = {
      api: "openai-completions",
      provider: "openrouter",
      id: "openrouter/auto",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]).toEqual({ reasoning: { max_tokens: 256 } });
  });

  it("does not inject reasoning.effort for x-ai/grok models on OpenRouter (#32039)", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { reasoning_effort: "medium" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(
      agent,
      undefined,
      "openrouter",
      "x-ai/grok-4.1-fast",
      undefined,
      "medium",
    );

    const model = {
      api: "openai-completions",
      provider: "openrouter",
      id: "x-ai/grok-4.1-fast",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]).not.toHaveProperty("reasoning");
    expect(payloads[0]).not.toHaveProperty("reasoning_effort");
  });

  it("injects parallel_tool_calls for openai-completions payloads when configured", () => {
    const payload = runParallelToolCallsPayloadMutationCase({
      applyProvider: "nvidia-nim",
      applyModelId: "moonshotai/kimi-k2.5",
      cfg: {
        agents: {
          defaults: {
            models: {
              "nvidia-nim/moonshotai/kimi-k2.5": {
                params: {
                  parallel_tool_calls: false,
                },
              },
            },
          },
        },
      },
      model: {
        api: "openai-completions",
        provider: "nvidia-nim",
        id: "moonshotai/kimi-k2.5",
      } as Model<"openai-completions">,
    });

    expect(payload.parallel_tool_calls).toBe(false);
  });

  it("injects parallel_tool_calls for openai-responses payloads when configured", () => {
    const payload = runParallelToolCallsPayloadMutationCase({
      applyProvider: "openai",
      applyModelId: "gpt-5",
      cfg: {
        agents: {
          defaults: {
            models: {
              "openai/gpt-5": {
                params: {
                  parallelToolCalls: true,
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

    expect(payload.parallel_tool_calls).toBe(true);
  });

  it("does not inject parallel_tool_calls for unsupported APIs", () => {
    const payload = runParallelToolCallsPayloadMutationCase({
      applyProvider: "anthropic",
      applyModelId: "claude-sonnet-4-6",
      cfg: {
        agents: {
          defaults: {
            models: {
              "anthropic/claude-sonnet-4-6": {
                params: {
                  parallel_tool_calls: false,
                },
              },
            },
          },
        },
      },
      model: {
        api: "anthropic-messages",
        provider: "anthropic",
        id: "claude-sonnet-4-6",
      } as Model<"anthropic-messages">,
    });

    expect(payload).not.toHaveProperty("parallel_tool_calls");
  });

  it("lets runtime override win across alias styles for parallel_tool_calls", () => {
    const payload = runParallelToolCallsPayloadMutationCase({
      applyProvider: "nvidia-nim",
      applyModelId: "moonshotai/kimi-k2.5",
      cfg: {
        agents: {
          defaults: {
            models: {
              "nvidia-nim/moonshotai/kimi-k2.5": {
                params: {
                  parallel_tool_calls: true,
                },
              },
            },
          },
        },
      },
      extraParamsOverride: {
        parallelToolCalls: false,
      },
      model: {
        api: "openai-completions",
        provider: "nvidia-nim",
        id: "moonshotai/kimi-k2.5",
      } as Model<"openai-completions">,
    });

    expect(payload.parallel_tool_calls).toBe(false);
  });

  it("lets null runtime override suppress inherited parallel_tool_calls injection", () => {
    const payload = runParallelToolCallsPayloadMutationCase({
      applyProvider: "nvidia-nim",
      applyModelId: "moonshotai/kimi-k2.5",
      cfg: {
        agents: {
          defaults: {
            models: {
              "nvidia-nim/moonshotai/kimi-k2.5": {
                params: {
                  parallel_tool_calls: true,
                },
              },
            },
          },
        },
      },
      extraParamsOverride: {
        parallelToolCalls: null,
      },
      model: {
        api: "openai-completions",
        provider: "nvidia-nim",
        id: "moonshotai/kimi-k2.5",
      } as Model<"openai-completions">,
    });

    expect(payload).not.toHaveProperty("parallel_tool_calls");
  });

  it("warns and skips invalid parallel_tool_calls values", () => {
    const warnSpy = vi.spyOn(log, "warn").mockImplementation(() => undefined);
    try {
      const payload = runParallelToolCallsPayloadMutationCase({
        applyProvider: "nvidia-nim",
        applyModelId: "moonshotai/kimi-k2.5",
        cfg: {
          agents: {
            defaults: {
              models: {
                "nvidia-nim/moonshotai/kimi-k2.5": {
                  params: {
                    parallelToolCalls: "false",
                  },
                },
              },
            },
          },
        },
        model: {
          api: "openai-completions",
          provider: "nvidia-nim",
          id: "moonshotai/kimi-k2.5",
        } as Model<"openai-completions">,
      });

      expect(payload).not.toHaveProperty("parallel_tool_calls");
      expect(warnSpy).toHaveBeenCalledWith("ignoring invalid parallel_tool_calls param: false");
    } finally {
      warnSpy.mockRestore();
    }
  });

  it("normalizes thinking=off to null for SiliconFlow Pro models", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { thinking: "off" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(
      agent,
      undefined,
      "siliconflow",
      "Pro/MiniMaxAI/MiniMax-M2.7",
      undefined,
      "off",
    );

    const model = {
      api: "openai-completions",
      provider: "siliconflow",
      id: "Pro/MiniMaxAI/MiniMax-M2.7",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toBeNull();
  });

  it("keeps thinking=off unchanged for non-Pro SiliconFlow model IDs", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { thinking: "off" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(
      agent,
      undefined,
      "siliconflow",
      "deepseek-ai/DeepSeek-V3.2",
      undefined,
      "off",
    );

    const model = {
      api: "openai-completions",
      provider: "siliconflow",
      id: "deepseek-ai/DeepSeek-V3.2",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toBe("off");
  });

  it("maps thinkingLevel=off to Moonshot thinking.type=disabled", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {};
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "moonshot", "kimi-k2.5", undefined, "off");

    const model = {
      api: "openai-completions",
      provider: "moonshot",
      id: "kimi-k2.5",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "disabled" });
  });

  it("maps non-off thinking levels to Moonshot thinking.type=enabled and normalizes tool_choice", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { tool_choice: "required" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "moonshot", "kimi-k2.5", undefined, "low");

    const model = {
      api: "openai-completions",
      provider: "moonshot",
      id: "kimi-k2.5",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "enabled" });
    expect(payloads[0]?.tool_choice).toBe("auto");
  });

  it("disables thinking instead of broadening pinned Moonshot tool_choice", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        tool_choice: { type: "tool", name: "read" },
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "moonshot", "kimi-k2.5", undefined, "low");

    const model = {
      api: "openai-completions",
      provider: "moonshot",
      id: "kimi-k2.5",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "disabled" });
    expect(payloads[0]?.tool_choice).toEqual({ type: "tool", name: "read" });
  });

  it("respects explicit Moonshot thinking param from model config", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {};
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };
    const cfg = {
      agents: {
        defaults: {
          models: {
            "moonshot/kimi-k2.5": {
              params: {
                thinking: { type: "disabled" },
              },
            },
          },
        },
      },
    };

    applyExtraParamsToAgent(agent, cfg, "moonshot", "kimi-k2.5", undefined, "high");

    const model = {
      api: "openai-completions",
      provider: "moonshot",
      id: "kimi-k2.5",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "disabled" });
  });

  it("applies Moonshot payload compatibility to Ollama Kimi cloud models", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = { tool_choice: "required" };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "ollama", "kimi-k2.5:cloud", undefined, "low");

    const model = {
      api: "openai-completions",
      provider: "ollama",
      id: "kimi-k2.5:cloud",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "enabled" });
    expect(payloads[0]?.tool_choice).toBe("auto");
  });

  it("maps thinkingLevel=off for Ollama Kimi cloud models through Moonshot compatibility", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {};
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "ollama", "kimi-k2.5:cloud", undefined, "off");

    const model = {
      api: "openai-completions",
      provider: "ollama",
      id: "kimi-k2.5:cloud",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "disabled" });
  });

  it("disables thinking instead of broadening pinned Ollama Kimi cloud tool_choice", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        tool_choice: { type: "function", function: { name: "read" } },
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "ollama", "kimi-k2.5:cloud", undefined, "low");

    const model = {
      api: "openai-completions",
      provider: "ollama",
      id: "kimi-k2.5:cloud",
    } as Model<"openai-completions">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.thinking).toEqual({ type: "disabled" });
    expect(payloads[0]?.tool_choice).toEqual({
      type: "function",
      function: { name: "read" },
    });
  });

  it("does not rewrite tool schema for Kimi (native Anthropic format)", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        tools: [
          {
            name: "read",
            description: "Read file",
            input_schema: {
              type: "object",
              properties: { path: { type: "string" } },
              required: ["path"],
            },
          },
        ],
        tool_choice: { type: "tool", name: "read" },
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "kimi", "kimi-code", undefined, "low");

    const model = {
      api: "anthropic-messages",
      provider: "kimi",
      id: "kimi-code",
      baseUrl: "https://api.kimi.com/coding/",
    } as Model<"anthropic-messages">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.tools).toEqual([
      {
        name: "read",
        description: "Read file",
        input_schema: {
          type: "object",
          properties: { path: { type: "string" } },
          required: ["path"],
        },
      },
    ]);
    expect(payloads[0]?.tool_choice).toEqual({ type: "tool", name: "read" });
  });

  it("does not rewrite anthropic tool schema for non-kimi endpoints", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        tools: [
          {
            name: "read",
            description: "Read file",
            input_schema: { type: "object", properties: {} },
          },
        ],
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(agent, undefined, "anthropic", "claude-sonnet-4-6", undefined, "low");

    const model = {
      api: "anthropic-messages",
      provider: "anthropic",
      id: "claude-sonnet-4-6",
      baseUrl: "https://api.anthropic.com",
    } as Model<"anthropic-messages">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.tools).toEqual([
      {
        name: "read",
        description: "Read file",
        input_schema: { type: "object", properties: {} },
      },
    ]);
  });

  it("uses explicit compat metadata for anthropic tool payload normalization", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        tools: [
          {
            name: "read",
            description: "Read file",
            input_schema: { type: "object", properties: {} },
          },
        ],
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(
      agent,
      undefined,
      "custom-anthropic-proxy",
      "proxy-model",
      undefined,
      "low",
    );

    const model = {
      api: "anthropic-messages",
      provider: "custom-anthropic-proxy",
      id: "proxy-model",
      compat: {
        requiresOpenAiAnthropicToolPayload: true,
      },
    } as unknown as Model<"anthropic-messages">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.tools).toEqual([
      {
        type: "function",
        function: {
          name: "read",
          description: "Read file",
          parameters: { type: "object", properties: {} },
        },
      },
    ]);
  });

  it("uses workspace plugin capability metadata for anthropic tool payload normalization", () => {
    const payloads: Record<string, unknown>[] = [];
    const baseStreamFn: StreamFn = (_model, _context, options) => {
      const payload: Record<string, unknown> = {
        tools: [
          {
            name: "read",
            description: "Read file",
            input_schema: { type: "object", properties: {} },
          },
        ],
        tool_choice: { type: "any" },
      };
      options?.onPayload?.(payload, _model);
      payloads.push(payload);
      return {} as ReturnType<StreamFn>;
    };
    const agent = { streamFn: baseStreamFn };

    applyExtraParamsToAgent(
      agent,
      { plugins: { enabled: true } },
      "workspace-anthropic-proxy",
      "proxy-model",
      undefined,
      "low",
      undefined,
      "/tmp/workspace-capabilities",
    );

    const model = {
      api: "anthropic-messages",
      provider: "workspace-anthropic-proxy",
      id: "proxy-model",
    } as Model<"anthropic-messages">;
    const context: Context = { messages: [] };
    void agent.streamFn?.(model, context, {});

    expect(payloads).toHaveLength(1);
    expect(payloads[0]?.tools).toEqual([
      {
        type: "function",
        function: {
          name: "read",
          description: "Read file",
          parameters: { type: "object", properties: {} },
        },
      },
    ]);
    expect(payloads[0]?.tool_choice).toBe("required");
    expect(resolveProviderCapabilitiesWithPluginMock).toHaveBeenCalledWith(
      expect.objectContaining({
        provider: "workspace-anthropic-proxy",
        workspaceDir: "/tmp/workspace-capabilities",
      }),
    );
  });
});
