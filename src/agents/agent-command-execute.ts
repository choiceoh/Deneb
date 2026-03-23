import {
  formatXHighModelHint,
  supportsXHighThinking,
  type VerboseLevel,
} from "../auto-reply/thinking.js";
import { type CliDeps, createDefaultDeps } from "../cli/deps.js";
import { type SessionEntry } from "../config/sessions.js";
import { resolveSessionTranscriptFile } from "../config/sessions/transcript.js";
import {
  clearAgentRunContext,
  emitAgentEvent,
  registerAgentRunContext,
} from "../infra/agent-events.js";
import { getRemoteSkillEligibility } from "../infra/skills-remote.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { defaultRuntime, type RuntimeEnv } from "../runtime.js";
import { applyVerboseOverride } from "../sessions/level-overrides.js";
import { applyModelOverrideToSessionEntry } from "../sessions/model-overrides.js";
import { resolveSendPolicy } from "../sessions/send-policy.js";
import { sanitizeForLog } from "../terminal/ansi.js";
import { resolveMessageChannel } from "../utils/message-channel.js";
import { runAcpTurn } from "./agent-command-acp.js";
import { persistSessionEntry, normalizeExplicitOverrideInput } from "./agent-command-helpers.js";
import { prepareAgentCommandExecution } from "./agent-command-prepare.js";
import { runAgentAttempt } from "./agent-command-run.js";
import { resolveAgentSkillsFilter, resolveEffectiveModelFallbacks } from "./agent-scope.js";
import { ensureAuthProfileStore } from "./auth-profiles.js";
import { clearSessionAuthProfileOverride } from "./auth-profiles/session-override.js";
import { deliverAgentCommandResult } from "./command/delivery.js";
import { resolveAgentRunContext } from "./command/run-context.js";
import { updateSessionStoreAfterAgentRun } from "./command/session-store.js";
import type { AgentCommandOpts, AgentCommandIngressOpts } from "./command/types.js";
import { FailoverError } from "./failover-error.js";
import { loadModelCatalog } from "./models/model-catalog.js";
import { runWithModelFallback } from "./models/model-fallback.js";
import {
  buildAllowedModelSet,
  isCliProvider,
  modelKey,
  normalizeModelRef,
  parseModelRef,
  resolveDefaultModelForAgent,
  resolveThinkingDefault,
} from "./models/model-selection.js";
import { buildWorkspaceSkillSnapshot } from "./skills.js";
import { getSkillsSnapshotVersion } from "./skills/refresh.js";

const log = createSubsystemLogger("agents/agent-command");

async function agentCommandInternal(
  opts: AgentCommandOpts & { senderIsOwner: boolean },
  runtime: RuntimeEnv = defaultRuntime,
  deps: CliDeps = createDefaultDeps(),
) {
  const prepared = await prepareAgentCommandExecution(opts, runtime);
  const {
    body,
    cfg,
    agentCfg,
    thinkOverride,
    thinkOnce,
    verboseOverride,
    timeoutMs,
    sessionId,
    sessionKey,
    sessionStore,
    storePath,
    isNewSession,
    persistedThinking,
    persistedVerbose,
    sessionAgentId,
    outboundSession,
    workspaceDir,
    agentDir,
    runId,
    acpResolution,
  } = prepared;
  let sessionEntry = prepared.sessionEntry;

  try {
    if (opts.deliver === true) {
      const sendPolicy = resolveSendPolicy({
        cfg,
        entry: sessionEntry,
        sessionKey,
        channel: sessionEntry?.channel,
        chatType: sessionEntry?.chatType,
      });
      if (sendPolicy === "deny") {
        throw new Error("send blocked by session policy");
      }
    }

    if (acpResolution?.kind === "stale") {
      throw acpResolution.error;
    }

    if (acpResolution?.kind === "ready" && sessionKey) {
      return await runAcpTurn({
        cfg,
        deps,
        runtime,
        opts,
        sessionKey,
        sessionId,
        sessionAgentId,
        sessionEntry,
        sessionStore,
        storePath,
        workspaceDir,
        body,
        runId,
        acpResolution,
        outboundSession,
      });
    }

    let resolvedThinkLevel = thinkOnce ?? thinkOverride ?? persistedThinking;
    const resolvedVerboseLevel =
      verboseOverride ?? persistedVerbose ?? (agentCfg?.verboseDefault as VerboseLevel | undefined);

    if (sessionKey) {
      registerAgentRunContext(runId, {
        sessionKey,
        verboseLevel: resolvedVerboseLevel,
      });
    }

    const needsSkillsSnapshot = isNewSession || !sessionEntry?.skillsSnapshot;
    const skillsSnapshotVersion = getSkillsSnapshotVersion(workspaceDir);
    const skillFilter = resolveAgentSkillsFilter(cfg, sessionAgentId);
    const skillsSnapshot = needsSkillsSnapshot
      ? buildWorkspaceSkillSnapshot(workspaceDir, {
          config: cfg,
          eligibility: { remote: getRemoteSkillEligibility() },
          snapshotVersion: skillsSnapshotVersion,
          skillFilter,
        })
      : sessionEntry?.skillsSnapshot;

    if (skillsSnapshot && sessionStore && sessionKey && needsSkillsSnapshot) {
      const current = sessionEntry ?? {
        sessionId,
        updatedAt: Date.now(),
      };
      const next: SessionEntry = {
        ...current,
        sessionId,
        updatedAt: Date.now(),
        skillsSnapshot,
      };
      await persistSessionEntry({
        sessionStore,
        sessionKey,
        storePath,
        entry: next,
      });
      sessionEntry = next;
    }

    // Persist explicit /command overrides to the session store when we have a key.
    if (sessionStore && sessionKey) {
      const entry = sessionStore[sessionKey] ??
        sessionEntry ?? { sessionId, updatedAt: Date.now() };
      const next: SessionEntry = { ...entry, sessionId, updatedAt: Date.now() };
      if (thinkOverride) {
        next.thinkingLevel = thinkOverride;
      }
      applyVerboseOverride(next, verboseOverride);
      await persistSessionEntry({
        sessionStore,
        sessionKey,
        storePath,
        entry: next,
      });
      sessionEntry = next;
    }

    const configuredDefaultRef = resolveDefaultModelForAgent({
      cfg,
      agentId: sessionAgentId,
    });
    const { provider: defaultProvider, model: defaultModel } = normalizeModelRef(
      configuredDefaultRef.provider,
      configuredDefaultRef.model,
    );
    let provider = defaultProvider;
    let model = defaultModel;
    const hasAllowlist = agentCfg?.models && Object.keys(agentCfg.models).length > 0;
    const hasStoredOverride = Boolean(
      sessionEntry?.modelOverride || sessionEntry?.providerOverride,
    );
    const explicitProviderOverride =
      typeof opts.provider === "string"
        ? normalizeExplicitOverrideInput(opts.provider, "provider")
        : undefined;
    const explicitModelOverride =
      typeof opts.model === "string"
        ? normalizeExplicitOverrideInput(opts.model, "model")
        : undefined;
    const hasExplicitRunOverride = Boolean(explicitProviderOverride || explicitModelOverride);
    if (hasExplicitRunOverride && opts.allowModelOverride !== true) {
      throw new Error("Model override is not authorized for this caller.");
    }
    const needsModelCatalog = hasAllowlist || hasStoredOverride || hasExplicitRunOverride;
    let allowedModelKeys = new Set<string>();
    let allowedModelCatalog: Awaited<ReturnType<typeof loadModelCatalog>> = [];
    let modelCatalog: Awaited<ReturnType<typeof loadModelCatalog>> | null = null;
    let allowAnyModel = false;

    if (needsModelCatalog) {
      modelCatalog = await loadModelCatalog({ config: cfg });
      const allowed = buildAllowedModelSet({
        cfg,
        catalog: modelCatalog,
        defaultProvider,
        defaultModel,
        agentId: sessionAgentId,
      });
      allowedModelKeys = allowed.allowedKeys;
      allowedModelCatalog = allowed.allowedCatalog;
      allowAnyModel = allowed.allowAny ?? false;
    }

    if (sessionEntry && sessionStore && sessionKey && hasStoredOverride) {
      const entry = sessionEntry;
      const overrideProvider = sessionEntry.providerOverride?.trim() || defaultProvider;
      const overrideModel = sessionEntry.modelOverride?.trim();
      if (overrideModel) {
        const normalizedOverride = normalizeModelRef(overrideProvider, overrideModel);
        const key = modelKey(normalizedOverride.provider, normalizedOverride.model);
        if (
          !isCliProvider(normalizedOverride.provider, cfg) &&
          !allowAnyModel &&
          !allowedModelKeys.has(key)
        ) {
          const { updated } = applyModelOverrideToSessionEntry({
            entry,
            selection: { provider: defaultProvider, model: defaultModel, isDefault: true },
          });
          if (updated) {
            await persistSessionEntry({
              sessionStore,
              sessionKey,
              storePath,
              entry,
            });
          }
        }
      }
    }

    const storedProviderOverride = sessionEntry?.providerOverride?.trim();
    const storedModelOverride = sessionEntry?.modelOverride?.trim();
    if (storedModelOverride) {
      const candidateProvider = storedProviderOverride || defaultProvider;
      const normalizedStored = normalizeModelRef(candidateProvider, storedModelOverride);
      const key = modelKey(normalizedStored.provider, normalizedStored.model);
      if (
        isCliProvider(normalizedStored.provider, cfg) ||
        allowAnyModel ||
        allowedModelKeys.has(key)
      ) {
        provider = normalizedStored.provider;
        model = normalizedStored.model;
      }
    }
    const providerForAuthProfileValidation = provider;
    if (hasExplicitRunOverride) {
      const explicitRef = explicitModelOverride
        ? explicitProviderOverride
          ? normalizeModelRef(explicitProviderOverride, explicitModelOverride)
          : parseModelRef(explicitModelOverride, provider)
        : explicitProviderOverride
          ? normalizeModelRef(explicitProviderOverride, model)
          : null;
      if (!explicitRef) {
        throw new Error("Invalid model override.");
      }
      const explicitKey = modelKey(explicitRef.provider, explicitRef.model);
      if (
        !isCliProvider(explicitRef.provider, cfg) &&
        !allowAnyModel &&
        !allowedModelKeys.has(explicitKey)
      ) {
        throw new Error(
          `Model override "${sanitizeForLog(explicitRef.provider)}/${sanitizeForLog(explicitRef.model)}" is not allowed for agent "${sessionAgentId}".`,
        );
      }
      provider = explicitRef.provider;
      model = explicitRef.model;
    }
    if (sessionEntry) {
      const authProfileId = sessionEntry.authProfileOverride;
      if (authProfileId) {
        const entry = sessionEntry;
        const store = ensureAuthProfileStore();
        const profile = store.profiles[authProfileId];
        if (!profile || profile.provider !== providerForAuthProfileValidation) {
          if (sessionStore && sessionKey) {
            await clearSessionAuthProfileOverride({
              sessionEntry: entry,
              sessionStore,
              sessionKey,
              storePath,
            });
          }
        }
      }
    }

    if (!resolvedThinkLevel) {
      let catalogForThinking = modelCatalog ?? allowedModelCatalog;
      if (!catalogForThinking || catalogForThinking.length === 0) {
        modelCatalog = await loadModelCatalog({ config: cfg });
        catalogForThinking = modelCatalog;
      }
      resolvedThinkLevel = resolveThinkingDefault({
        cfg,
        provider,
        model,
        catalog: catalogForThinking,
      });
    }
    if (resolvedThinkLevel === "xhigh" && !supportsXHighThinking(provider, model)) {
      const explicitThink = Boolean(thinkOnce || thinkOverride);
      if (explicitThink) {
        throw new Error(`Thinking level "xhigh" is only supported for ${formatXHighModelHint()}.`);
      }
      resolvedThinkLevel = "high";
      if (sessionEntry && sessionStore && sessionKey && sessionEntry.thinkingLevel === "xhigh") {
        const entry = sessionEntry;
        entry.thinkingLevel = "high";
        entry.updatedAt = Date.now();
        await persistSessionEntry({
          sessionStore,
          sessionKey,
          storePath,
          entry,
        });
      }
    }
    let sessionFile: string | undefined;
    if (sessionStore && sessionKey) {
      const resolvedSessionFile = await resolveSessionTranscriptFile({
        sessionId,
        sessionKey,
        sessionStore,
        storePath,
        sessionEntry,
        agentId: sessionAgentId,
        threadId: opts.threadId,
      });
      sessionFile = resolvedSessionFile.sessionFile;
      sessionEntry = resolvedSessionFile.sessionEntry;
    }
    if (!sessionFile) {
      const resolvedSessionFile = await resolveSessionTranscriptFile({
        sessionId,
        sessionKey: sessionKey ?? sessionId,
        storePath,
        sessionEntry,
        agentId: sessionAgentId,
        threadId: opts.threadId,
      });
      sessionFile = resolvedSessionFile.sessionFile;
      sessionEntry = resolvedSessionFile.sessionEntry;
    }

    const startedAt = Date.now();
    let lifecycleEnded = false;

    let result: Awaited<ReturnType<typeof runAgentAttempt>>;
    let fallbackProvider = provider;
    let fallbackModel = model;
    try {
      const runContext = resolveAgentRunContext(opts);
      const messageChannel = resolveMessageChannel(
        runContext.messageChannel,
        opts.replyChannel ?? opts.channel,
      );
      const spawnedBy = prepared.normalizedSpawned.spawnedBy ?? sessionEntry?.spawnedBy;
      // Keep fallback candidate resolution centralized so session model overrides,
      // per-agent overrides, and default fallbacks stay consistent across callers.
      const effectiveFallbacksOverride = resolveEffectiveModelFallbacks({
        cfg,
        agentId: sessionAgentId,
        hasSessionModelOverride: Boolean(storedModelOverride),
      });

      // Track model fallback attempts so retries on an existing session don't
      // re-inject the original prompt as a duplicate user message.
      let fallbackAttemptIndex = 0;
      const fallbackResult = await runWithModelFallback({
        cfg,
        provider,
        model,
        runId,
        agentDir,
        fallbacksOverride: effectiveFallbacksOverride,
        run: (providerOverride, modelOverride, runOptions) => {
          const isFallbackRetry = fallbackAttemptIndex > 0;
          fallbackAttemptIndex += 1;
          return runAgentAttempt({
            providerOverride,
            modelOverride,
            cfg,
            sessionEntry,
            sessionId,
            sessionKey,
            sessionAgentId,
            sessionFile,
            workspaceDir,
            body,
            isFallbackRetry,
            resolvedThinkLevel,
            timeoutMs,
            runId,
            opts,
            runContext,
            spawnedBy,
            messageChannel,
            skillsSnapshot,
            resolvedVerboseLevel,
            agentDir,
            authProfileProvider: providerForAuthProfileValidation,
            sessionStore,
            storePath,
            allowTransientCooldownProbe: runOptions?.allowTransientCooldownProbe,
            onAgentEvent: (evt) => {
              // Track lifecycle end for fallback emission below.
              if (
                evt.stream === "lifecycle" &&
                typeof evt.data?.phase === "string" &&
                (evt.data.phase === "end" || evt.data.phase === "error")
              ) {
                lifecycleEnded = true;
              }
            },
          });
        },
      });
      result = fallbackResult.result;
      fallbackProvider = fallbackResult.provider;
      fallbackModel = fallbackResult.model;
      if (!lifecycleEnded) {
        const stopReason = result.meta.stopReason;
        if (stopReason && stopReason !== "end_turn") {
          console.error(`[agent] run ${runId} ended with stopReason=${stopReason}`);
        }
        emitAgentEvent({
          runId,
          stream: "lifecycle",
          data: {
            phase: "end",
            startedAt,
            endedAt: Date.now(),
            aborted: result.meta.aborted ?? false,
            stopReason,
          },
        });
      }
    } catch (err) {
      const errorEndedAt = Date.now();
      const errorDurationMs = errorEndedAt - startedAt;
      if (!lifecycleEnded) {
        emitAgentEvent({
          runId,
          stream: "lifecycle",
          data: {
            phase: "error",
            startedAt,
            endedAt: errorEndedAt,
            error: String(err),
          },
        });
      }
      const isFailover = err instanceof FailoverError;
      const failoverReason = isFailover ? err.reason : undefined;
      log.error("agent run failed", {
        event: "agent_run_failed",
        runId,
        sessionKey,
        agentId: sessionAgentId,
        provider,
        model,
        durationMs: errorDurationMs,
        failoverReason,
        error: err instanceof Error ? err.message : String(err),
        consoleMessage: `agent run FAILED: runId=${runId} provider=${provider}/${model} duration=${errorDurationMs}ms error=${err instanceof Error ? err.message : String(err)}`,
      });
      // Send critical alert for severe failures (billing, auth, repeated errors).
      if (
        isFailover &&
        (failoverReason === "billing" ||
          failoverReason === "auth_permanent" ||
          failoverReason === "auth")
      ) {
        void import("../infra/agent-critical-alert.js").then(({ deliverCriticalAlert }) =>
          deliverCriticalAlert({
            cfg,
            sessionKey: sessionKey ?? runId,
            entry: sessionEntry,
            severity: failoverReason === "billing" ? "fatal" : "error",
            title: failoverReason === "billing" ? "LLM Billing Error" : "LLM Authentication Error",
            details: err instanceof Error ? err.message : String(err),
            runId,
            agentId: sessionAgentId,
            provider,
            model,
          }).catch(() => {
            // best-effort alert delivery
          }),
        );
      }
      throw err;
    }

    // Update token+model fields in the session store.
    if (sessionStore && sessionKey) {
      await updateSessionStoreAfterAgentRun({
        cfg,
        contextTokensOverride: agentCfg?.contextTokens,
        sessionId,
        sessionKey,
        storePath,
        sessionStore,
        defaultProvider: provider,
        defaultModel: model,
        fallbackProvider,
        fallbackModel,
        result,
      });
    }

    const payloads = result.payloads ?? [];
    const totalDurationMs = Date.now() - startedAt;
    log.info("agent run completed", {
      event: "agent_run_completed",
      runId,
      sessionKey,
      agentId: sessionAgentId,
      provider: fallbackProvider,
      model: fallbackModel,
      durationMs: totalDurationMs,
      aborted: result.meta.aborted ?? false,
      stopReason: result.meta.stopReason,
      payloadCount: payloads.length,
      consoleMessage: `agent run OK: runId=${runId} provider=${fallbackProvider}/${fallbackModel} duration=${totalDurationMs}ms payloads=${payloads.length}`,
    });
    return await deliverAgentCommandResult({
      cfg,
      deps,
      runtime,
      opts,
      outboundSession,
      sessionEntry,
      result,
      payloads,
    });
  } finally {
    clearAgentRunContext(runId);
  }
}

export async function agentCommand(
  opts: AgentCommandOpts,
  runtime: RuntimeEnv = defaultRuntime,
  deps: CliDeps = createDefaultDeps(),
) {
  return await agentCommandInternal(
    {
      ...opts,
      // agentCommand is the trusted-operator entrypoint used by CLI/local flows.
      // Ingress callers must opt into owner semantics explicitly via
      // agentCommandFromIngress so network-facing paths cannot inherit this default by accident.
      senderIsOwner: opts.senderIsOwner ?? true,
      // Local/CLI callers are trusted by default for per-run model overrides.
      allowModelOverride: opts.allowModelOverride ?? true,
    },
    runtime,
    deps,
  );
}

export async function agentCommandFromIngress(
  opts: AgentCommandIngressOpts,
  runtime: RuntimeEnv = defaultRuntime,
  deps: CliDeps = createDefaultDeps(),
) {
  if (typeof opts.senderIsOwner !== "boolean") {
    // HTTP/WS ingress must declare the trust level explicitly at the boundary.
    // This keeps network-facing callers from silently picking up the local trusted default.
    throw new Error("senderIsOwner must be explicitly set for ingress agent runs.");
  }
  if (typeof opts.allowModelOverride !== "boolean") {
    throw new Error("allowModelOverride must be explicitly set for ingress agent runs.");
  }
  return await agentCommandInternal(
    {
      ...opts,
      senderIsOwner: opts.senderIsOwner,
      allowModelOverride: opts.allowModelOverride,
    },
    runtime,
    deps,
  );
}
