import { getGlobalHookRunner } from "../../../plugins/hook-runner-global.js";
import type { PluginHookBeforeAgentStartResult } from "../../../plugins/types.js";
import { isMarkdownCapableMessageChannel } from "../../../utils/message-channel.js";
import { resolveDenebAgentDir } from "../../agent-paths.js";
import { hasConfiguredModelFallbacks } from "../../agent-scope.js";
import {
  CONTEXT_WINDOW_HARD_MIN_TOKENS,
  CONTEXT_WINDOW_WARN_BELOW_TOKENS,
  evaluateContextWindowGuard,
  resolveContextWindowInfo,
  type ContextWindowInfo,
} from "../../context-window-guard.js";
import { DEFAULT_CONTEXT_TOKENS, DEFAULT_MODEL, DEFAULT_PROVIDER } from "../../defaults.js";
import { FailoverError } from "../../failover-error.js";
import { ensureAuthProfileStore, resolveAuthProfileOrder } from "../../model-auth.js";
import { normalizeProviderId } from "../../model-selection.js";
import { ensureDenebModelsJson } from "../../models-config.js";
import { ensureRuntimePluginsLoaded } from "../../runtime-plugins.js";
import { redactRunIdentifier, resolveRunWorkspaceDir } from "../../workspace-run.js";
import { log } from "../logger.js";
import { resolveModelAsync } from "../model.js";
import type { RunEmbeddedPiAgentParams } from "./params.js";

export type RunSetupResult = {
  provider: string;
  modelId: string;
  runtimeModel: NonNullable<Awaited<ReturnType<typeof resolveModelAsync>>["model"]>;
  effectiveModel: NonNullable<Awaited<ReturnType<typeof resolveModelAsync>>["model"]>;
  authStorage: NonNullable<Awaited<ReturnType<typeof resolveModelAsync>>["authStorage"]>;
  modelRegistry: NonNullable<Awaited<ReturnType<typeof resolveModelAsync>>["modelRegistry"]>;
  ctxInfo: ContextWindowInfo;
  agentDir: string;
  resolvedWorkspace: string;
  workspaceAgentId: string | undefined;
  fallbackConfigured: boolean;
  resolvedToolResultFormat: "markdown" | "plain";
  isProbeSession: boolean;
  profileCandidates: Array<string | undefined>;
  lockedProfileId: string | undefined;
  preferredProfileId: string | undefined;
  authStore: ReturnType<typeof ensureAuthProfileStore>;
  legacyBeforeAgentStartResult: PluginHookBeforeAgentStartResult | undefined;
  hookCtx: {
    agentId: string | undefined;
    sessionKey: string | undefined;
    sessionId: string;
    workspaceDir: string;
    messageProvider: string | undefined;
    trigger: string | undefined;
    channelId: string | undefined;
  };
};

/**
 * Perform all one-time initialization for a run: workspace resolution,
 * plugin loading, model resolution, context window checks, auth profile ordering,
 * and hook-based model overrides.
 */
export async function setupRun(params: RunEmbeddedPiAgentParams): Promise<RunSetupResult> {
  const channelHint = params.messageChannel ?? params.messageProvider;
  const resolvedToolResultFormat =
    params.toolResultFormat ??
    (channelHint
      ? isMarkdownCapableMessageChannel(channelHint)
        ? "markdown"
        : "plain"
      : "markdown");
  const isProbeSession = params.sessionId?.startsWith("probe-") ?? false;

  const workspaceResolution = resolveRunWorkspaceDir({
    workspaceDir: params.workspaceDir,
    sessionKey: params.sessionKey,
    agentId: params.agentId,
    config: params.config,
  });
  const resolvedWorkspace = workspaceResolution.workspaceDir;
  const redactedSessionId = redactRunIdentifier(params.sessionId);
  const redactedSessionKey = redactRunIdentifier(params.sessionKey);
  const redactedWorkspace = redactRunIdentifier(resolvedWorkspace);
  if (workspaceResolution.usedFallback) {
    log.warn(
      `[workspace-fallback] caller=runEmbeddedPiAgent reason=${workspaceResolution.fallbackReason} run=${params.runId} session=${redactedSessionId} sessionKey=${redactedSessionKey} agent=${workspaceResolution.agentId} workspace=${redactedWorkspace}`,
    );
  }
  ensureRuntimePluginsLoaded({
    config: params.config,
    workspaceDir: resolvedWorkspace,
    allowGatewaySubagentBinding: params.allowGatewaySubagentBinding,
  });

  let provider = (params.provider ?? DEFAULT_PROVIDER).trim() || DEFAULT_PROVIDER;
  let modelId = (params.model ?? DEFAULT_MODEL).trim() || DEFAULT_MODEL;
  const agentDir = params.agentDir ?? resolveDenebAgentDir();
  const fallbackConfigured = hasConfiguredModelFallbacks({
    cfg: params.config,
    agentId: params.agentId,
    sessionKey: params.sessionKey,
  });
  await ensureDenebModelsJson(params.config, agentDir);

  // Run before_model_resolve and before_agent_start hooks for provider/model overrides.
  let modelResolveOverride: { providerOverride?: string; modelOverride?: string } | undefined;
  let legacyBeforeAgentStartResult: PluginHookBeforeAgentStartResult | undefined;
  const hookRunner = getGlobalHookRunner();
  const hookCtx = {
    agentId: workspaceResolution.agentId,
    sessionKey: params.sessionKey,
    sessionId: params.sessionId,
    workspaceDir: resolvedWorkspace,
    messageProvider: params.messageProvider ?? undefined,
    trigger: params.trigger,
    channelId: params.messageChannel ?? params.messageProvider ?? undefined,
  };

  if (hookRunner?.hasHooks("before_model_resolve")) {
    try {
      modelResolveOverride = await hookRunner.runBeforeModelResolve(
        { prompt: params.prompt },
        hookCtx,
      );
    } catch (hookErr) {
      log.warn(`before_model_resolve hook failed: ${String(hookErr)}`);
    }
  }
  if (hookRunner?.hasHooks("before_agent_start")) {
    try {
      legacyBeforeAgentStartResult = await hookRunner.runBeforeAgentStart(
        { prompt: params.prompt },
        hookCtx,
      );
      modelResolveOverride = {
        providerOverride:
          modelResolveOverride?.providerOverride ?? legacyBeforeAgentStartResult?.providerOverride,
        modelOverride:
          modelResolveOverride?.modelOverride ?? legacyBeforeAgentStartResult?.modelOverride,
      };
    } catch (hookErr) {
      log.warn(`before_agent_start hook (legacy model resolve path) failed: ${String(hookErr)}`);
    }
  }
  if (modelResolveOverride?.providerOverride) {
    provider = modelResolveOverride.providerOverride;
    log.info(`[hooks] provider overridden to ${provider}`);
  }
  if (modelResolveOverride?.modelOverride) {
    modelId = modelResolveOverride.modelOverride;
    log.info(`[hooks] model overridden to ${modelId}`);
  }

  // Resolve model
  const { model, error, authStorage, modelRegistry } = await resolveModelAsync(
    provider,
    modelId,
    agentDir,
    params.config,
  );
  if (!model) {
    throw new FailoverError(error ?? `Unknown model: ${provider}/${modelId}`, {
      reason: "model_not_found",
      provider,
      model: modelId,
    });
  }
  const runtimeModel = model;

  // Context window resolution
  const ctxInfo = resolveContextWindowInfo({
    cfg: params.config,
    provider,
    modelId,
    modelContextWindow: runtimeModel.contextWindow,
    defaultTokens: DEFAULT_CONTEXT_TOKENS,
  });
  const effectiveModel =
    ctxInfo.tokens < (runtimeModel.contextWindow ?? Infinity)
      ? { ...runtimeModel, contextWindow: ctxInfo.tokens }
      : runtimeModel;
  const ctxGuard = evaluateContextWindowGuard({
    info: ctxInfo,
    warnBelowTokens: CONTEXT_WINDOW_WARN_BELOW_TOKENS,
    hardMinTokens: CONTEXT_WINDOW_HARD_MIN_TOKENS,
  });
  if (ctxGuard.shouldWarn) {
    log.warn(
      `low context window: ${provider}/${modelId} ctx=${ctxGuard.tokens} (warn<${CONTEXT_WINDOW_WARN_BELOW_TOKENS}) source=${ctxGuard.source}`,
    );
  }
  if (ctxGuard.shouldBlock) {
    log.error(
      `blocked model (context window too small): ${provider}/${modelId} ctx=${ctxGuard.tokens} (min=${CONTEXT_WINDOW_HARD_MIN_TOKENS}) source=${ctxGuard.source}`,
    );
    throw new FailoverError(
      `Model context window too small (${ctxGuard.tokens} tokens). Minimum is ${CONTEXT_WINDOW_HARD_MIN_TOKENS}.`,
      { reason: "unknown", provider, model: modelId },
    );
  }

  // Auth profile ordering
  const authStore = ensureAuthProfileStore(agentDir, { allowKeychainPrompt: false });
  const preferredProfileId = params.authProfileId?.trim();
  let lockedProfileId = params.authProfileIdSource === "user" ? preferredProfileId : undefined;
  if (lockedProfileId) {
    const lockedProfile = authStore.profiles[lockedProfileId];
    if (
      !lockedProfile ||
      normalizeProviderId(lockedProfile.provider) !== normalizeProviderId(provider)
    ) {
      lockedProfileId = undefined;
    }
  }
  const profileOrder = resolveAuthProfileOrder({
    cfg: params.config,
    store: authStore,
    provider,
    preferredProfile: preferredProfileId,
  });
  if (lockedProfileId && !profileOrder.includes(lockedProfileId)) {
    throw new Error(`Auth profile "${lockedProfileId}" is not configured for ${provider}.`);
  }
  const profileCandidates = lockedProfileId
    ? [lockedProfileId]
    : profileOrder.length > 0
      ? profileOrder
      : [undefined];

  return {
    provider,
    modelId,
    runtimeModel,
    effectiveModel,
    authStorage,
    modelRegistry,
    ctxInfo,
    agentDir,
    resolvedWorkspace,
    workspaceAgentId: workspaceResolution.agentId,
    fallbackConfigured,
    resolvedToolResultFormat,
    isProbeSession,
    profileCandidates,
    lockedProfileId,
    preferredProfileId,
    authStore,
    legacyBeforeAgentStartResult,
    hookCtx,
  };
}
