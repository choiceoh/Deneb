/**
 * Native bridge — replaces createLcmDependencies from the lossless-claw plugin.
 *
 * Instead of going through the plugin API (api.config, api.runtime.subagent, etc.),
 * this module imports core modules directly, eliminating ~450 lines of glue code.
 */

import { readFileSync } from "node:fs";
import {
  completeSimple,
  getEnvApiKey,
  getModel,
  type Api,
  type AssistantMessage,
  type KnownProvider,
  type Message,
  type Model,
  type ThinkingLevel,
} from "@mariozechner/pi-ai";
import { resolveApiKeyForProvider, getCustomProviderApiKey } from "../../agents/model-auth.js";
import { parseModelRef } from "../../agents/model-selection.js";
import type { OpenClawConfig } from "../../config/config.js";
import { loadConfig } from "../../config/io.js";
import { resolveDefaultSessionStorePath, resolveStorePath } from "../../config/sessions/paths.js";
import { callGateway } from "../../gateway/call.js";
import { createSubsystemLogger } from "../../logging/subsystem.js";
import { normalizeAgentId } from "../../routing/session-key.js";
import { parseAgentSessionKey } from "../../sessions/session-key-utils.js";
import { resolveLcmConfig } from "./src/db/config.js";
import type { LcmDependencies } from "./src/types.js";

const log = createSubsystemLogger("lcm");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function inferApiFromProvider(provider: string): string {
  const map: Record<string, string> = {
    openai: "openai",
    anthropic: "anthropic",
    google: "google-genai",
    openrouter: "openrouter",
    ollama: "ollama",
    deepseek: "openai",
    zai: "openai",
  };
  return map[provider] ?? "openai";
}

/** Find provider-level config (baseUrl, headers, apiKey) from runtime config. */
function findProviderConfig(cfg: OpenClawConfig, provider: string): Record<string, unknown> {
  if (!cfg?.models?.providers) {
    return {};
  }
  const entry = (cfg.models.providers as Record<string, Record<string, unknown>>)[provider];
  return entry && typeof entry === "object" ? entry : {};
}

// ---------------------------------------------------------------------------
// Model resolution
// ---------------------------------------------------------------------------

function nativeResolveModel(
  modelRef?: string,
  providerHint?: string,
): { provider: string; model: string } {
  const cfg = loadConfig();
  const raw = (
    modelRef?.trim() ||
    ((cfg.models as Record<string, unknown> | undefined)?.default as string | undefined)
  )?.trim();
  if (!raw) {
    throw new Error("No model configured for LCM summarization.");
  }

  const parsed = parseModelRef(raw, providerHint?.trim() || "openai");
  if (parsed) {
    return { provider: parsed.provider, model: parsed.model };
  }

  // Fallback: provider/model format
  if (raw.includes("/")) {
    const [provider, ...rest] = raw.split("/");
    const model = rest.join("/").trim();
    if (provider && model) {
      return { provider: provider.trim(), model };
    }
  }

  return { provider: providerHint?.trim() || "openai", model: raw };
}

// ---------------------------------------------------------------------------
// API key resolution
// ---------------------------------------------------------------------------

async function nativeGetApiKey(
  provider: string,
  _model: string,
  _options?: { profileId?: string; agentDir?: string },
): Promise<string | undefined> {
  const cfg = loadConfig();

  // 1. Core model auth (auth profiles, env vars, custom providers)
  const auth = await resolveApiKeyForProvider({ provider, cfg });
  if (auth?.apiKey) {
    return auth.apiKey;
  }

  // 2. Custom provider apiKey from config
  const customKey = getCustomProviderApiKey(cfg, provider);
  if (customKey) {
    return customKey;
  }

  // 3. pi-ai env-based lookup
  const envKey = getEnvApiKey(provider as Api);
  if (envKey) {
    return envKey;
  }

  return undefined;
}

async function nativeRequireApiKey(
  provider: string,
  model: string,
  options?: { profileId?: string; agentDir?: string },
): Promise<string> {
  const key = await nativeGetApiKey(provider, model, options);
  if (!key) {
    throw new Error(`Missing API key for provider '${provider}' (model '${model}').`);
  }
  return key;
}

// ---------------------------------------------------------------------------
// LLM completion (summarization)
// ---------------------------------------------------------------------------

async function nativeComplete(params: {
  provider?: string;
  model: string;
  apiKey?: string;
  providerApi?: string;
  authProfileId?: string;
  agentDir?: string;
  runtimeConfig?: unknown;
  messages: Array<{ role: string; content: unknown }>;
  system?: string;
  maxTokens: number;
  temperature?: number;
  reasoning?: string;
}): Promise<{
  content: Array<{ type: string; text?: string; [key: string]: unknown }>;
  [key: string]: unknown;
}> {
  try {
    const providerId = (params.provider ?? "").trim();
    const modelId = params.model.trim();
    if (!providerId || !modelId) {
      return { content: [] };
    }

    const cfg = loadConfig();
    const providerCfg = findProviderConfig(cfg, providerId);

    // getModel's second param is `keyof MODELS[TProvider]`; when TProvider is the
    // full KnownProvider union this collapses to `never`. We pass runtime strings
    // (wrapped in try/catch for unknown provider×model combos) and cast modelId
    // through `never` — no narrower static type is expressible here.
    let knownModel: Model<Api> | undefined;
    try {
      knownModel = getModel(providerId as KnownProvider, modelId as never);
    } catch {
      knownModel = undefined;
    }
    const fallbackApi = params.providerApi?.trim() || inferApiFromProvider(providerId);

    const resolvedModel: Model<Api> = knownModel
      ? {
          ...knownModel,
          baseUrl: knownModel.baseUrl || (providerCfg.baseUrl as string) || "",
          ...(providerCfg.headers
            ? { headers: providerCfg.headers as Record<string, string> }
            : {}),
        }
      : {
          id: modelId,
          name: modelId,
          provider: providerId,
          api: fallbackApi as Api,
          reasoning: false,
          input: ["text"] as ("text" | "image")[],
          cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
          contextWindow: 200_000,
          maxTokens: 8_000,
          baseUrl: (providerCfg.baseUrl as string) || "",
          ...(providerCfg.headers
            ? { headers: providerCfg.headers as Record<string, string> }
            : {}),
        };

    let resolvedApiKey = params.apiKey?.trim();
    if (!resolvedApiKey) {
      resolvedApiKey = await nativeGetApiKey(providerId, modelId, {
        profileId: params.authProfileId,
        agentDir: params.agentDir,
      });
    }

    const validRoles = new Set<Message["role"]>(["user", "assistant", "toolResult"]);
    const result: AssistantMessage = await completeSimple(
      resolvedModel,
      {
        ...(params.system?.trim() ? { systemPrompt: params.system.trim() } : {}),
        messages: params.messages
          .filter((m) => validRoles.has(m.role as Message["role"]))
          .map((m) => ({
            role: m.role as Message["role"],
            content: m.content,
            timestamp: Date.now(),
          })) as Message[],
      },
      {
        apiKey: resolvedApiKey,
        maxTokens: params.maxTokens,
        temperature: params.temperature,
        reasoning: params.reasoning as ThinkingLevel | undefined,
      },
    );

    if (!result || typeof result !== "object") {
      return { content: [] };
    }
    return {
      ...result,
      content: Array.isArray(result.content)
        ? (result.content as Array<{ type: string; text?: string; [key: string]: unknown }>)
        : [],
    };
  } catch (err) {
    log.error(`completeSimple error: ${err instanceof Error ? err.message : String(err)}`);
    return { content: [] };
  }
}

// ---------------------------------------------------------------------------
// Gateway RPC (subagent operations)
// ---------------------------------------------------------------------------

async function nativeCallGateway(params: {
  method: string;
  params?: Record<string, unknown>;
  timeoutMs?: number;
}): Promise<unknown> {
  switch (params.method) {
    case "agent":
      return callGateway({
        method: "agent.run",
        params: params.params,
        timeoutMs: params.timeoutMs,
      });
    case "agent.wait":
      return callGateway({
        method: "agent.waitForRun",
        params: params.params,
        timeoutMs: params.timeoutMs,
      });
    case "sessions.get":
      return callGateway({
        method: "sessions.get",
        params: params.params,
      });
    case "sessions.delete":
      return callGateway({
        method: "sessions.delete",
        params: params.params,
      });
    default:
      throw new Error(`Unsupported gateway method in LCM native bridge: ${params.method}`);
  }
}

// ---------------------------------------------------------------------------
// Session key utilities
// ---------------------------------------------------------------------------

function adaptParsedSessionKey(sessionKey: string): { agentId: string; suffix: string } | null {
  const parsed = parseAgentSessionKey(sessionKey);
  return parsed ? { agentId: parsed.agentId, suffix: parsed.rest } : null;
}

function nativeIsSubagentSessionKey(sessionKey: string): boolean {
  const parsed = adaptParsedSessionKey(sessionKey);
  return !!parsed && parsed.suffix.startsWith("subagent:");
}

async function nativeResolveSessionIdFromSessionKey(
  sessionKey: string,
): Promise<string | undefined> {
  const key = sessionKey.trim();
  if (!key) {
    return undefined;
  }

  try {
    const cfg = loadConfig();
    const parsed = parseAgentSessionKey(key);
    const agentId = normalizeAgentId(parsed?.agentId);
    const storePath = resolveStorePath(cfg.session?.store, { agentId });
    const raw = readFileSync(storePath, "utf8");
    const store = JSON.parse(raw) as Record<string, { sessionId?: string } | undefined>;
    const sessionId = store[key]?.sessionId;
    return typeof sessionId === "string" && sessionId.trim() ? sessionId.trim() : undefined;
  } catch {
    return undefined;
  }
}

// ---------------------------------------------------------------------------
// Public factory
// ---------------------------------------------------------------------------

export function createNativeLcmDependencies(): LcmDependencies {
  const cfg = loadConfig();
  const pluginConfig = cfg.plugins?.entries?.["lossless-claw"] as
    | Record<string, unknown>
    | undefined;
  const config = resolveLcmConfig(process.env, pluginConfig ?? {});

  // Apply model overrides from plugin config
  if (pluginConfig) {
    if (typeof pluginConfig.summaryModel === "string") {
      (config as Record<string, unknown>).summaryModel = pluginConfig.summaryModel.trim();
    }
    if (typeof pluginConfig.summaryProvider === "string") {
      (config as Record<string, unknown>).summaryProvider = pluginConfig.summaryProvider.trim();
    }
  }

  return {
    config,
    complete: nativeComplete,
    callGateway: nativeCallGateway,
    resolveModel: nativeResolveModel,
    getApiKey: nativeGetApiKey,
    requireApiKey: nativeRequireApiKey,
    parseAgentSessionKey: adaptParsedSessionKey,
    isSubagentSessionKey: nativeIsSubagentSessionKey,
    normalizeAgentId,
    // buildSubagentSystemPrompt — not critical, provide stub
    buildSubagentSystemPrompt: (params: {
      depth: number;
      maxDepth: number;
      taskSummary?: string;
    }) => {
      const parts = ["You are a sub-agent performing a focused research task."];
      if (params.depth > 0) {
        parts.push(`Depth: ${params.depth}/${params.maxDepth}`);
      }
      if (params.taskSummary) {
        parts.push(`Task: ${params.taskSummary}`);
      }
      return parts.join("\n");
    },
    readLatestAssistantReply: (messages: unknown[]) => {
      if (!Array.isArray(messages)) {
        return undefined;
      }
      for (let i = messages.length - 1; i >= 0; i--) {
        const msg = messages[i] as Record<string, unknown>;
        if (msg?.role === "assistant" && typeof msg?.content === "string" && msg.content.trim()) {
          return msg.content;
        }
      }
      return undefined;
    },
    resolveAgentDir: () => {
      // Use default session store path to derive agent dir
      return resolveDefaultSessionStorePath();
    },
    resolveSessionIdFromSessionKey: nativeResolveSessionIdFromSessionKey,
    agentLaneSubagent: "subagent",
    log: {
      info: (msg) => log.info(msg),
      warn: (msg) => log.warn(msg),
      error: (msg) => log.error(msg),
      debug: (msg) => log.debug?.(msg) ?? (() => {}),
    },
  };
}
