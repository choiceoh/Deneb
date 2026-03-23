import { resolveDefaultAgentId } from "../../agents/agent-scope.js";
import { lookupContextTokens, resolveContextTokensForModel } from "../../agents/context.js";
import { DEFAULT_CONTEXT_TOKENS, DEFAULT_MODEL, DEFAULT_PROVIDER } from "../../agents/defaults.js";
import {
  inferUniqueProviderFromConfiguredModels,
  parseModelRef,
  resolveConfiguredModelRef,
  resolveDefaultModelForAgent,
} from "../../agents/model-selection.js";
import { getSubagentSessionRuntimeMs } from "../../agents/subagent-registry.js";
import type { DenebConfig } from "../../config/config.js";
import type { SessionEntry } from "../../config/sessions.js";
import { normalizeAgentId, parseAgentSessionKey } from "../../routing/session-key.js";
import { estimateUsageCost, resolveModelCostConfig } from "../../utils/usage-format.js";
import { readLatestSessionUsageFromTranscript } from "./session-utils.fs.js";
import type { GatewaySessionsDefaults } from "./session-utils.types.js";

export function resolveSessionRuntimeMs(
  run: { startedAt?: number; endedAt?: number; accumulatedRuntimeMs?: number } | null,
  now: number,
) {
  return getSubagentSessionRuntimeMs(run, now);
}

export function resolvePositiveNumber(value: number | null | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
}

export function resolveNonNegativeNumber(value: number | null | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : undefined;
}

export function resolveEstimatedSessionCostUsd(params: {
  cfg: DenebConfig;
  provider?: string;
  model?: string;
  entry?: Pick<
    SessionEntry,
    "estimatedCostUsd" | "inputTokens" | "outputTokens" | "cacheRead" | "cacheWrite"
  >;
  explicitCostUsd?: number;
}): number | undefined {
  const explicitCostUsd = resolveNonNegativeNumber(
    params.explicitCostUsd ?? params.entry?.estimatedCostUsd,
  );
  if (explicitCostUsd !== undefined) {
    return explicitCostUsd;
  }
  const input = resolvePositiveNumber(params.entry?.inputTokens);
  const output = resolvePositiveNumber(params.entry?.outputTokens);
  const cacheRead = resolvePositiveNumber(params.entry?.cacheRead);
  const cacheWrite = resolvePositiveNumber(params.entry?.cacheWrite);
  if (
    input === undefined &&
    output === undefined &&
    cacheRead === undefined &&
    cacheWrite === undefined
  ) {
    return undefined;
  }
  const cost = resolveModelCostConfig({
    provider: params.provider,
    model: params.model,
    config: params.cfg,
  });
  if (!cost) {
    return undefined;
  }
  const estimated = estimateUsageCost({
    usage: {
      ...(input !== undefined ? { input } : {}),
      ...(output !== undefined ? { output } : {}),
      ...(cacheRead !== undefined ? { cacheRead } : {}),
      ...(cacheWrite !== undefined ? { cacheWrite } : {}),
    },
    cost,
  });
  return resolveNonNegativeNumber(estimated);
}

export function resolveTranscriptUsageFallback(params: {
  cfg: DenebConfig;
  key: string;
  entry?: SessionEntry;
  storePath: string;
  fallbackProvider?: string;
  fallbackModel?: string;
}): {
  estimatedCostUsd?: number;
  totalTokens?: number;
  totalTokensFresh?: boolean;
  contextTokens?: number;
} | null {
  const entry = params.entry;
  if (!entry?.sessionId) {
    return null;
  }
  const parsed = parseAgentSessionKey(params.key);
  const agentId = parsed?.agentId
    ? normalizeAgentId(parsed.agentId)
    : resolveDefaultAgentId(params.cfg);
  const snapshot = readLatestSessionUsageFromTranscript(
    entry.sessionId,
    params.storePath,
    entry.sessionFile,
    agentId,
  );
  if (!snapshot) {
    return null;
  }
  const modelProvider = snapshot.modelProvider ?? params.fallbackProvider;
  const model = snapshot.model ?? params.fallbackModel;
  const contextTokens = resolveContextTokensForModel({
    cfg: params.cfg,
    provider: modelProvider,
    model,
  });
  const estimatedCostUsd = resolveEstimatedSessionCostUsd({
    cfg: params.cfg,
    provider: modelProvider,
    model,
    explicitCostUsd: snapshot.costUsd,
    entry: {
      inputTokens: snapshot.inputTokens,
      outputTokens: snapshot.outputTokens,
      cacheRead: snapshot.cacheRead,
      cacheWrite: snapshot.cacheWrite,
    },
  });
  return {
    totalTokens: resolvePositiveNumber(snapshot.totalTokens),
    totalTokensFresh: snapshot.totalTokensFresh === true,
    contextTokens: resolvePositiveNumber(contextTokens),
    estimatedCostUsd,
  };
}

export function getSessionDefaults(cfg: DenebConfig): GatewaySessionsDefaults {
  const resolved = resolveConfiguredModelRef({
    cfg,
    defaultProvider: DEFAULT_PROVIDER,
    defaultModel: DEFAULT_MODEL,
  });
  const contextTokens =
    cfg.agents?.defaults?.contextTokens ??
    lookupContextTokens(resolved.model) ??
    DEFAULT_CONTEXT_TOKENS;
  return {
    modelProvider: resolved.provider ?? null,
    model: resolved.model ?? null,
    contextTokens: contextTokens ?? null,
  };
}

export function resolveSessionModelRef(
  cfg: DenebConfig,
  entry?:
    | SessionEntry
    | Pick<SessionEntry, "model" | "modelProvider" | "modelOverride" | "providerOverride">,
  agentId?: string,
): { provider: string; model: string } {
  const resolved = agentId
    ? resolveDefaultModelForAgent({ cfg, agentId })
    : resolveConfiguredModelRef({
        cfg,
        defaultProvider: DEFAULT_PROVIDER,
        defaultModel: DEFAULT_MODEL,
      });

  // Prefer the last runtime model recorded on the session entry.
  // This is the actual model used by the latest run and must win over defaults.
  let provider = resolved.provider;
  let model = resolved.model;
  const runtimeModel = entry?.model?.trim();
  const runtimeProvider = entry?.modelProvider?.trim();
  if (runtimeModel) {
    if (runtimeProvider) {
      // Provider is explicitly recorded — use it directly. Re-parsing the
      // model string through parseModelRef would incorrectly split OpenRouter
      // vendor-prefixed model names (e.g. model="anthropic/claude-haiku-4.5"
      // with provider="openrouter") into { provider: "anthropic" }, discarding
      // the stored OpenRouter provider and causing direct API calls to a
      // provider the user has no credentials for.
      return { provider: runtimeProvider, model: runtimeModel };
    }
    const parsedRuntime = parseModelRef(runtimeModel, provider || DEFAULT_PROVIDER);
    if (parsedRuntime) {
      provider = parsedRuntime.provider;
      model = parsedRuntime.model;
    } else {
      model = runtimeModel;
    }
    return { provider, model };
  }

  // Fall back to explicit per-session override (set at spawn/model-patch time),
  // then finally to configured defaults.
  const storedModelOverride = entry?.modelOverride?.trim();
  if (storedModelOverride) {
    const overrideProvider = entry?.providerOverride?.trim() || provider || DEFAULT_PROVIDER;
    const parsedOverride = parseModelRef(storedModelOverride, overrideProvider);
    if (parsedOverride) {
      provider = parsedOverride.provider;
      model = parsedOverride.model;
    } else {
      provider = overrideProvider;
      model = storedModelOverride;
    }
  }
  return { provider, model };
}

export function resolveSessionModelIdentityRef(
  cfg: DenebConfig,
  entry?:
    | SessionEntry
    | Pick<SessionEntry, "model" | "modelProvider" | "modelOverride" | "providerOverride">,
  agentId?: string,
  fallbackModelRef?: string,
): { provider?: string; model: string } {
  const runtimeModel = entry?.model?.trim();
  const runtimeProvider = entry?.modelProvider?.trim();
  if (runtimeModel) {
    if (runtimeProvider) {
      return { provider: runtimeProvider, model: runtimeModel };
    }
    const inferredProvider = inferUniqueProviderFromConfiguredModels({
      cfg,
      model: runtimeModel,
    });
    if (inferredProvider) {
      return { provider: inferredProvider, model: runtimeModel };
    }
    if (runtimeModel.includes("/")) {
      const parsedRuntime = parseModelRef(runtimeModel, DEFAULT_PROVIDER);
      if (parsedRuntime) {
        return { provider: parsedRuntime.provider, model: parsedRuntime.model };
      }
      return { model: runtimeModel };
    }
    return { model: runtimeModel };
  }
  const fallbackRef = fallbackModelRef?.trim();
  if (fallbackRef) {
    const parsedFallback = parseModelRef(fallbackRef, DEFAULT_PROVIDER);
    if (parsedFallback) {
      return { provider: parsedFallback.provider, model: parsedFallback.model };
    }
    const inferredProvider = inferUniqueProviderFromConfiguredModels({
      cfg,
      model: fallbackRef,
    });
    if (inferredProvider) {
      return { provider: inferredProvider, model: fallbackRef };
    }
    return { model: fallbackRef };
  }
  const resolved = resolveSessionModelRef(cfg, entry, agentId);
  return { provider: resolved.provider, model: resolved.model };
}
