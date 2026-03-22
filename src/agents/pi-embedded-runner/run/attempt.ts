import fs from "node:fs/promises";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { streamSimple } from "@mariozechner/pi-ai";
import {
  createAgentSession,
  DefaultResourceLoader,
  SessionManager,
} from "@mariozechner/pi-coding-agent";
import { getMachineDisplayName } from "../../../infra/machine-name.js";
import {
  ensureGlobalUndiciEnvProxyDispatcher,
  ensureGlobalUndiciStreamTimeouts,
} from "../../../infra/net/undici-global-dispatcher.js";
import { MAX_IMAGE_BYTES } from "../../../media/constants.js";
import { getGlobalHookRunner } from "../../../plugins/hook-runner-global.js";
import { isSubagentSessionKey } from "../../../routing/session-key.js";
import { resolveUserPath } from "../../../utils.js";
import { resolveDenebAgentDir } from "../../agent-paths.js";
import { resolveSessionAgentIds } from "../../agent-scope.js";
import { createAnthropicPayloadLogger } from "../../anthropic-payload-log.js";
import { prependBootstrapPromptWarning } from "../../bootstrap-budget.js";
import { makeBootstrapWarn, resolveBootstrapContextForRun } from "../../bootstrap-files.js";
import { createCacheTrace } from "../../cache-trace.js";
import { DEFAULT_CONTEXT_TOKENS } from "../../defaults.js";
import { resolveDenebDocsPath } from "../../docs-path.js";
import { isTimeoutError } from "../../failover-error.js";
import { resolveImageSanitizationLimits } from "../../image-sanitization.js";
import { resolveModelAuthMode } from "../../model-auth.js";
import { supportsModelTools } from "../../model-tool-support.js";
import { createOpenAIWebSocketStreamFn, releaseWsSession } from "../../openai-ws-stream.js";
import { createBundleLspToolRuntime } from "../../pi-bundle-lsp-runtime.js";
import { createBundleMcpToolRuntime } from "../../pi-bundle-mcp-tools.js";
import {
  isCloudCodeAssistFormatError,
  validateAnthropicTurns,
  validateGeminiTurns,
} from "../../pi-embedded-helpers.js";
import { subscribeEmbeddedPiSession } from "../../pi-embedded-subscribe.js";
import { createPreparedEmbeddedPiSettingsManager } from "../../pi-project-settings.js";
import { applyPiAutoCompactionGuard } from "../../pi-settings.js";
import { toClientToolDefinitions } from "../../pi-tool-definition-adapter.js";
import { createDenebCodingTools, resolveToolLoopDetectionConfig } from "../../pi-tools.js";
import { resolveSandboxContext } from "../../sandbox.js";
import { repairSessionFileIfNeeded } from "../../session-file-repair.js";
import { guardSessionManager } from "../../session-tool-result-guard-wrapper.js";
import { sanitizeToolUseResultPairing } from "../../session-transcript-repair.js";
import {
  acquireSessionWriteLock,
  resolveSessionLockMaxHoldFromTimeout,
} from "../../session-write-lock.js";
import {
  applySkillEnvOverrides,
  applySkillEnvOverridesFromSnapshot,
  resolveSkillsPromptForRun,
} from "../../skills.js";
import { resolveTranscriptPolicy } from "../../transcript-policy.js";
import { isRunnerAbortError } from "../abort.js";
import { appendCacheTtlTimestamp, isCacheTtlEligibleProvider } from "../cache-ttl.js";
import { resolveCompactionTimeoutMs } from "../compaction-safety-timeout.js";
import { buildEmbeddedExtensionFactories } from "../extensions.js";
import { applyExtraParamsToAgent } from "../extra-params.js";
import {
  logToolSchemasForGoogle,
  sanitizeSessionHistory,
  sanitizeToolsForGoogle,
} from "../google.js";
import { getDmHistoryLimitFromSessionKey, limitHistoryTurns } from "../history.js";
import { log } from "../logger.js";
import {
  clearActiveEmbeddedRun,
  type EmbeddedPiQueueHandle,
  setActiveEmbeddedRun,
  updateActiveEmbeddedRunSnapshot,
} from "../runs.js";
import { prewarmSessionFile, trackSessionManagerAccess } from "../session-manager-cache.js";
import { prepareSessionManagerForRun } from "../session-manager-init.js";
import { resolveEmbeddedRunSkillEntries } from "../skills-runtime.js";
import { applySystemPromptOverrideToSession } from "../system-prompt.js";
import { collectAllowedToolNames } from "../tool-name-allowlist.js";
import { installToolResultContextGuard } from "../tool-result-context-guard.js";
import { truncateOversizedToolResultsInMessages } from "../tool-result-truncation.js";
import { splitSdkTools } from "../tool-split.js";
import { describeUnknownError, mapThinkingLevel } from "../utils.js";
import { flushPendingToolResultsAfterIdle } from "../wait-for-idle-before-flush.js";
import { waitForCompactionRetryWithAggregateTimeout } from "./compaction-retry-aggregate-timeout.js";
import {
  resolveRunTimeoutDuringCompaction,
  resolveRunTimeoutWithCompactionGraceMs,
  selectCompactionTimeoutSnapshot,
  shouldFlagCompactionTimeout,
} from "./compaction-timeout.js";
import { pruneProcessedHistoryImages } from "./history-image-prune.js";
import { detectAndLoadPromptImages } from "./images.js";
import {
  buildAfterTurnRuntimeContext,
  composeSystemPromptWithHookContext,
  prependSystemPromptAddition,
  resolveAttemptFsWorkspaceOnly,
  resolvePromptBuildHookResult,
  resolvePromptModeForSession,
} from "./prompt-hooks.js";
import { summarizeSessionContext } from "./session-context-summary.js";
import {
  persistSessionsYieldContextMessage,
  queueSessionsYieldInterruptMessage,
  stripSessionsYieldArtifacts,
} from "./sessions-yield.js";
import { buildStreamPipeline } from "./stream-pipeline-setup.js";
import { resolveSystemPromptContext } from "./system-prompt-context.js";
import type { EmbeddedRunAttemptParams, EmbeddedRunAttemptResult } from "./types.js";

// Re-export public API from extracted modules for backward compatibility
export {
  buildAfterTurnRuntimeContext,
  composeSystemPromptWithHookContext,
  prependSystemPromptAddition,
  resolveAttemptFsWorkspaceOnly,
  resolvePromptBuildHookResult,
  resolvePromptModeForSession,
} from "./prompt-hooks.js";
export { decodeHtmlEntitiesInObject } from "./stream-xai-decode.js";
export { wrapStreamFnRepairMalformedToolCallArguments } from "./stream-tool-call-repair.js";
export { wrapStreamFnTrimToolCallNames } from "./stream-tool-call-sanitize.js";

const MAX_BTW_SNAPSHOT_MESSAGES = 100;

export async function runEmbeddedAttempt(
  params: EmbeddedRunAttemptParams,
): Promise<EmbeddedRunAttemptResult> {
  const resolvedWorkspace = resolveUserPath(params.workspaceDir);
  const prevCwd = process.cwd();
  const runAbortController = new AbortController();
  // Proxy bootstrap must happen before timeout tuning so the timeouts wrap the
  // active EnvHttpProxyAgent instead of being replaced by a bare proxy dispatcher.
  ensureGlobalUndiciEnvProxyDispatcher();
  ensureGlobalUndiciStreamTimeouts();

  log.debug(
    `embedded run start: runId=${params.runId} sessionId=${params.sessionId} provider=${params.provider} model=${params.modelId} thinking=${params.thinkLevel} messageChannel=${params.messageChannel ?? params.messageProvider ?? "unknown"}`,
  );

  await fs.mkdir(resolvedWorkspace, { recursive: true });

  const sandboxSessionKey = params.sessionKey?.trim() || params.sessionId;
  const sandbox = await resolveSandboxContext({
    config: params.config,
    sessionKey: sandboxSessionKey,
    workspaceDir: resolvedWorkspace,
  });
  const effectiveWorkspace = sandbox?.enabled
    ? sandbox.workspaceAccess === "rw"
      ? resolvedWorkspace
      : sandbox.workspaceDir
    : resolvedWorkspace;
  await fs.mkdir(effectiveWorkspace, { recursive: true });

  // Start session file prewarm early so the OS page cache is warm by the time
  // we acquire the session lock and open the SessionManager.
  const sessionPrewarmPromise = prewarmSessionFile(params.sessionFile);

  let restoreSkillEnv: (() => void) | undefined;
  process.chdir(effectiveWorkspace);
  try {
    const { shouldLoadSkillEntries, skillEntries } = resolveEmbeddedRunSkillEntries({
      workspaceDir: effectiveWorkspace,
      config: params.config,
      skillsSnapshot: params.skillsSnapshot,
    });
    restoreSkillEnv = params.skillsSnapshot
      ? applySkillEnvOverridesFromSnapshot({
          snapshot: params.skillsSnapshot,
          config: params.config,
        })
      : applySkillEnvOverrides({
          skills: skillEntries ?? [],
          config: params.config,
        });

    const skillsPrompt = resolveSkillsPromptForRun({
      skillsSnapshot: params.skillsSnapshot,
      entries: shouldLoadSkillEntries ? skillEntries : undefined,
      config: params.config,
      workspaceDir: effectiveWorkspace,
    });

    const sessionLabel = params.sessionKey ?? params.sessionId;
    // Kick off bootstrap context file I/O early so it runs in parallel with
    // synchronous tool creation and MCP/LSP runtime initialization below.
    const bootstrapContextPromise = resolveBootstrapContextForRun({
      workspaceDir: effectiveWorkspace,
      config: params.config,
      sessionKey: params.sessionKey,
      sessionId: params.sessionId,
      warn: makeBootstrapWarn({ sessionLabel, warn: (message) => log.warn(message) }),
      contextMode: params.bootstrapContextMode,
      runKind: params.bootstrapContextRunKind,
    });

    const agentDir = params.agentDir ?? resolveDenebAgentDir();

    const { defaultAgentId, sessionAgentId } = resolveSessionAgentIds({
      sessionKey: params.sessionKey,
      config: params.config,
      agentId: params.agentId,
    });
    const effectiveFsWorkspaceOnly = resolveAttemptFsWorkspaceOnly({
      config: params.config,
      sessionAgentId,
    });
    // Track sessions_yield tool invocation (callback pattern, like clientToolCallDetected)
    let yieldDetected = false;
    let yieldMessage: string | null = null;
    // Late-binding reference so onYield can abort the session (declared after tool creation)
    let abortSessionForYield: (() => void) | null = null;
    let queueYieldInterruptForSession: (() => void) | null = null;
    let yieldAbortSettled: Promise<void> | null = null;
    // Check if the model supports native image input
    const modelHasVision = params.model.input?.includes("image") ?? false;
    const toolsRaw = params.disableTools
      ? []
      : createDenebCodingTools({
          agentId: sessionAgentId,
          exec: {
            ...params.execOverrides,
            elevated: params.bashElevated,
          },
          sandbox,
          messageProvider: params.messageChannel ?? params.messageProvider,
          agentAccountId: params.agentAccountId,
          messageTo: params.messageTo,
          messageThreadId: params.messageThreadId,
          groupId: params.groupId,
          groupChannel: params.groupChannel,
          groupSpace: params.groupSpace,
          spawnedBy: params.spawnedBy,
          senderId: params.senderId,
          senderName: params.senderName,
          senderUsername: params.senderUsername,
          senderE164: params.senderE164,
          senderIsOwner: params.senderIsOwner,
          allowGatewaySubagentBinding: params.allowGatewaySubagentBinding,
          sessionKey: sandboxSessionKey,
          sessionId: params.sessionId,
          runId: params.runId,
          agentDir,
          workspaceDir: effectiveWorkspace,
          // When sandboxing uses a copied workspace (`ro` or `none`), effectiveWorkspace points
          // at the sandbox copy. Spawned subagents should inherit the real workspace instead.
          spawnWorkspaceDir:
            sandbox?.enabled && sandbox.workspaceAccess !== "rw" ? resolvedWorkspace : undefined,
          config: params.config,
          abortSignal: runAbortController.signal,
          modelProvider: params.model.provider,
          modelId: params.modelId,
          modelCompat: params.model.compat,
          modelContextWindowTokens: params.model.contextWindow,
          modelAuthMode: resolveModelAuthMode(params.model.provider, params.config),
          currentChannelId: params.currentChannelId,
          currentThreadTs: params.currentThreadTs,
          currentMessageId: params.currentMessageId,
          replyToMode: params.replyToMode,
          hasRepliedRef: params.hasRepliedRef,
          modelHasVision,
          requireExplicitMessageTarget:
            params.requireExplicitMessageTarget ?? isSubagentSessionKey(params.sessionKey),
          disableMessageTool: params.disableMessageTool,
          onYield: (message) => {
            yieldDetected = true;
            yieldMessage = message;
            queueYieldInterruptForSession?.();
            runAbortController.abort("sessions_yield");
            abortSessionForYield?.();
          },
        });
    const toolsEnabled = supportsModelTools(params.model);
    const tools = sanitizeToolsForGoogle({
      tools: toolsEnabled ? toolsRaw : [],
      provider: params.provider,
    });
    const clientTools = toolsEnabled ? params.clientTools : undefined;
    // Kick off independent async work in parallel to reduce sequential latency.
    const machineNamePromise = getMachineDisplayName();
    const docsPathPromise = resolveDenebDocsPath({
      workspaceDir: effectiveWorkspace,
      argv1: process.argv[1],
      cwd: process.cwd(),
      moduleUrl: import.meta.url,
    });

    const baseReservedToolNames = [
      ...tools.map((tool) => tool.name),
      ...(clientTools?.map((tool) => tool.function.name) ?? []),
    ];
    // Run MCP and LSP tool runtime creation in parallel. Both receive the same
    // base reserved names; name collisions between MCP and LSP tools are
    // extremely unlikely and the latency savings outweigh the theoretical risk.
    const [bundleMcpRuntime, bundleLspRuntime] = toolsEnabled
      ? await Promise.all([
          createBundleMcpToolRuntime({
            workspaceDir: effectiveWorkspace,
            cfg: params.config,
            reservedToolNames: baseReservedToolNames,
          }),
          createBundleLspToolRuntime({
            workspaceDir: effectiveWorkspace,
            cfg: params.config,
            reservedToolNames: baseReservedToolNames,
          }),
        ])
      : [undefined, undefined];
    const effectiveTools = [
      ...tools,
      ...(bundleMcpRuntime?.tools ?? []),
      ...(bundleLspRuntime?.tools ?? []),
    ];
    const allowedToolNames = collectAllowedToolNames({
      tools: effectiveTools,
      clientTools,
    });
    logToolSchemasForGoogle({ tools: effectiveTools, provider: params.provider });

    const machineName = await machineNamePromise;
    const docsPath = await docsPathPromise;

    // Await bootstrap context that was kicked off in parallel with tool setup.
    const { bootstrapFiles: hookAdjustedBootstrapFiles, contextFiles } =
      await bootstrapContextPromise;

    // Resolve channel context, build system prompt, and generate prompt report.
    const systemPromptContext = resolveSystemPromptContext({
      config: params.config,
      provider: params.provider,
      modelId: params.modelId,
      thinkLevel: params.thinkLevel,
      reasoningLevel: params.reasoningLevel,
      extraSystemPrompt: params.extraSystemPrompt,
      ownerNumbers: params.ownerNumbers,
      sessionKey: params.sessionKey,
      sessionId: params.sessionId,
      agentAccountId: params.agentAccountId,
      messageChannel: params.messageChannel,
      messageProvider: params.messageProvider,
      currentChannelId: params.currentChannelId,
      currentThreadTs: params.currentThreadTs,
      currentMessageId: params.currentMessageId,
      senderId: params.senderId,
      bashElevated: params.bashElevated,
      bootstrapPromptWarningSignaturesSeen: params.bootstrapPromptWarningSignaturesSeen,
      bootstrapPromptWarningSignature: params.bootstrapPromptWarningSignature,
      machineName,
      sessionAgentId,
      defaultAgentId,
      effectiveWorkspace,
      sandboxSessionKey,
      sandbox,
      effectiveTools,
      skillsPrompt,
      bootstrapFiles: hookAdjustedBootstrapFiles,
      contextFiles,
      docsPath,
    });
    const { systemPromptReport, bootstrapPromptWarning, heartbeatPrompt } = systemPromptContext;
    let { systemPromptText } = systemPromptContext;

    const sessionLock = await acquireSessionWriteLock({
      sessionFile: params.sessionFile,
      maxHoldMs: resolveSessionLockMaxHoldFromTimeout({
        timeoutMs: resolveRunTimeoutWithCompactionGraceMs({
          runTimeoutMs: params.timeoutMs,
          compactionTimeoutMs: resolveCompactionTimeoutMs(params.config),
        }),
      }),
    });

    let sessionManager: ReturnType<typeof guardSessionManager> | undefined;
    let session: Awaited<ReturnType<typeof createAgentSession>>["session"] | undefined;
    let removeToolResultContextGuard: (() => void) | undefined;
    // Track queue handle for safety-net cleanup in the outer finally block.
    // If the inner try/finally (which normally clears the active run) is never
    // reached due to an exception between setActiveEmbeddedRun and the inner
    // try, the outer finally ensures the session doesn't stay stuck as "active".
    let activeRunQueueHandle: EmbeddedPiQueueHandle | undefined;
    try {
      await repairSessionFileIfNeeded({
        sessionFile: params.sessionFile,
        warn: (message) => log.warn(message),
      });
      let sessionFileBytes = 0;
      const hadSessionFile = await fs
        .stat(params.sessionFile)
        .then((stat) => {
          sessionFileBytes = stat.size;
          return true;
        })
        .catch(() => false);

      // Warn when session files grow excessively large (> 2 MB).
      // Large session files cause slow SessionManager.open() and degrade
      // response quality due to context bloat.
      const SESSION_FILE_WARN_BYTES = 2 * 1024 * 1024;
      if (hadSessionFile && sessionFileBytes > SESSION_FILE_WARN_BYTES) {
        log.warn(
          `[session-size] Large session file detected: ` +
            `bytes=${sessionFileBytes} (${(sessionFileBytes / 1024 / 1024).toFixed(1)} MB) ` +
            `sessionKey=${params.sessionKey ?? params.sessionId} ` +
            `file=${params.sessionFile}`,
        );
      }

      const transcriptPolicy = resolveTranscriptPolicy({
        modelApi: params.model?.api,
        provider: params.provider,
        modelId: params.modelId,
      });

      await sessionPrewarmPromise;
      sessionManager = guardSessionManager(SessionManager.open(params.sessionFile), {
        agentId: sessionAgentId,
        sessionKey: params.sessionKey,
        inputProvenance: params.inputProvenance,
        allowSyntheticToolResults: transcriptPolicy.allowSyntheticToolResults,
        allowedToolNames,
      });
      trackSessionManagerAccess(params.sessionFile);

      if (hadSessionFile && params.contextEngine?.bootstrap) {
        try {
          await params.contextEngine.bootstrap({
            sessionId: params.sessionId,
            sessionKey: params.sessionKey,
            sessionFile: params.sessionFile,
          });
        } catch (bootstrapErr) {
          log.warn(`context engine bootstrap failed: ${String(bootstrapErr)}`);
        }
      }

      await prepareSessionManagerForRun({
        sessionManager,
        sessionFile: params.sessionFile,
        hadSessionFile,
        sessionId: params.sessionId,
        cwd: effectiveWorkspace,
      });

      const settingsManager = createPreparedEmbeddedPiSettingsManager({
        cwd: effectiveWorkspace,
        agentDir,
        cfg: params.config,
      });
      applyPiAutoCompactionGuard({
        settingsManager,
        contextEngineInfo: params.contextEngine?.info,
      });

      // Sets compaction/pruning runtime state and returns extension factories
      // that must be passed to the resource loader for the safeguard to be active.
      const extensionFactories = buildEmbeddedExtensionFactories({
        cfg: params.config,
        sessionManager,
        provider: params.provider,
        modelId: params.modelId,
        model: params.model,
      });
      // Only create an explicit resource loader when there are extension factories
      // to register; otherwise let createAgentSession use its built-in default.
      let resourceLoader: DefaultResourceLoader | undefined;
      if (extensionFactories.length > 0) {
        resourceLoader = new DefaultResourceLoader({
          cwd: resolvedWorkspace,
          agentDir,
          settingsManager,
          extensionFactories,
        });
        await resourceLoader.reload();
      }

      // Get hook runner early so it's available when creating tools
      const hookRunner = getGlobalHookRunner();

      const { builtInTools, customTools } = splitSdkTools({
        tools: effectiveTools,
        sandboxEnabled: !!sandbox?.enabled,
      });

      // Add client tools (OpenResponses hosted tools) to customTools
      let clientToolCallDetected: { name: string; params: Record<string, unknown> } | null = null;
      const clientToolLoopDetection = resolveToolLoopDetectionConfig({
        cfg: params.config,
        agentId: sessionAgentId,
      });
      const clientToolDefs = clientTools
        ? toClientToolDefinitions(
            clientTools,
            (toolName, toolParams) => {
              clientToolCallDetected = { name: toolName, params: toolParams };
            },
            {
              agentId: sessionAgentId,
              sessionKey: sandboxSessionKey,
              sessionId: params.sessionId,
              runId: params.runId,
              loopDetection: clientToolLoopDetection,
            },
          )
        : [];

      const allCustomTools = [...customTools, ...clientToolDefs];

      ({ session } = await createAgentSession({
        cwd: resolvedWorkspace,
        agentDir,
        authStorage: params.authStorage,
        modelRegistry: params.modelRegistry,
        model: params.model,
        thinkingLevel: mapThinkingLevel(params.thinkLevel),
        tools: builtInTools,
        customTools: allCustomTools,
        sessionManager,
        settingsManager,
        resourceLoader,
      }));
      applySystemPromptOverrideToSession(session, systemPromptText);
      if (!session) {
        throw new Error("Embedded agent session missing");
      }
      const activeSession = session;
      abortSessionForYield = () => {
        yieldAbortSettled = Promise.resolve(activeSession.abort());
      };
      queueYieldInterruptForSession = () => {
        queueSessionsYieldInterruptMessage(activeSession);
      };
      removeToolResultContextGuard = installToolResultContextGuard({
        agent: activeSession.agent,
        contextWindowTokens: Math.max(
          1,
          Math.floor(
            params.model.contextWindow ?? params.model.maxTokens ?? DEFAULT_CONTEXT_TOKENS,
          ),
        ),
      });
      const cacheTrace = createCacheTrace({
        cfg: params.config,
        env: process.env,
        runId: params.runId,
        sessionId: activeSession.sessionId,
        sessionKey: params.sessionKey,
        provider: params.provider,
        modelId: params.modelId,
        modelApi: params.model.api,
        workspaceDir: params.workspaceDir,
      });
      const anthropicPayloadLogger = createAnthropicPayloadLogger({
        env: process.env,
        runId: params.runId,
        sessionId: activeSession.sessionId,
        sessionKey: params.sessionKey,
        provider: params.provider,
        modelId: params.modelId,
        modelApi: params.model.api,
        workspaceDir: params.workspaceDir,
      });

      if (params.model.api === "openai-responses" && params.provider === "openai") {
        const wsApiKey = await params.authStorage.getApiKey(params.provider);
        if (wsApiKey) {
          activeSession.agent.streamFn = createOpenAIWebSocketStreamFn(wsApiKey, params.sessionId, {
            signal: runAbortController.signal,
          });
        } else {
          log.warn(`[ws-stream] no API key for provider=${params.provider}; using HTTP transport`);
          activeSession.agent.streamFn = streamSimple;
        }
      } else {
        // Force a stable streamFn reference so vitest can reliably mock @mariozechner/pi-ai.
        activeSession.agent.streamFn = streamSimple;
      }

      // Ollama num_ctx injection removed — not available in Deneb

      applyExtraParamsToAgent(
        activeSession.agent,
        params.config,
        params.provider,
        params.modelId,
        {
          ...params.streamParams,
          fastMode: params.fastMode,
        },
        params.thinkLevel,
        sessionAgentId,
        effectiveWorkspace,
      );

      if (cacheTrace) {
        cacheTrace.recordStage("session:loaded", {
          messages: activeSession.messages,
          system: systemPromptText,
          note: "after session create",
        });
      }

      // Apply all stream function wrappers (thinking block sanitization,
      // tool call ID repair, yield interception, provider-specific fixups, etc.)
      const yieldDetectedRef = {
        get current() {
          return yieldDetected;
        },
      };
      activeSession.agent.streamFn = buildStreamPipeline(activeSession.agent.streamFn, {
        provider: params.provider,
        modelId: params.modelId,
        model: params.model,
        transcriptPolicy,
        allowedToolNames,
        runAbortController,
        yieldDetectedRef,
        cacheTrace,
        anthropicPayloadLogger,
      });

      try {
        const prior = await sanitizeSessionHistory({
          messages: activeSession.messages,
          modelApi: params.model.api,
          modelId: params.modelId,
          provider: params.provider,
          allowedToolNames,
          config: params.config,
          sessionManager,
          sessionId: params.sessionId,
          policy: transcriptPolicy,
        });
        cacheTrace?.recordStage("session:sanitized", { messages: prior });
        const validatedGemini = transcriptPolicy.validateGeminiTurns
          ? validateGeminiTurns(prior)
          : prior;
        const validated = transcriptPolicy.validateAnthropicTurns
          ? validateAnthropicTurns(validatedGemini)
          : validatedGemini;
        const truncated = limitHistoryTurns(
          validated,
          getDmHistoryLimitFromSessionKey(params.sessionKey, params.config),
        );
        // Re-run tool_use/tool_result pairing repair after truncation, since
        // limitHistoryTurns can orphan tool_result blocks by removing the
        // assistant message that contained the matching tool_use.
        const limited = transcriptPolicy.repairToolUseResultPairing
          ? sanitizeToolUseResultPairing(truncated)
          : truncated;
        cacheTrace?.recordStage("session:limited", { messages: limited });
        if (limited.length > 0) {
          activeSession.agent.replaceMessages(limited);
        }

        if (params.contextEngine) {
          try {
            const assembled = await params.contextEngine.assemble({
              sessionId: params.sessionId,
              sessionKey: params.sessionKey,
              messages: activeSession.messages,
              tokenBudget: params.contextTokenBudget,
            });
            if (assembled.messages !== activeSession.messages) {
              activeSession.agent.replaceMessages(assembled.messages);
            }
            if (assembled.systemPromptAddition) {
              systemPromptText = prependSystemPromptAddition({
                systemPrompt: systemPromptText,
                systemPromptAddition: assembled.systemPromptAddition,
              });
              applySystemPromptOverrideToSession(activeSession, systemPromptText);
              log.debug(
                `context engine: prepended system prompt addition (${assembled.systemPromptAddition.length} chars)`,
              );
            }
          } catch (assembleErr) {
            log.warn(
              `context engine assemble failed, using pipeline messages: ${String(assembleErr)}`,
            );
          }
        }

        // Pre-send guard: truncate oversized tool results before the model
        // sees them. This prevents context overflow from accumulated tool
        // output without waiting for the error-recovery path.
        const contextWindowTokens = Math.max(
          1,
          Math.floor(
            params.model.contextWindow ?? params.model.maxTokens ?? DEFAULT_CONTEXT_TOKENS,
          ),
        );
        const preSendTrunc = truncateOversizedToolResultsInMessages(
          activeSession.messages,
          contextWindowTokens,
        );
        if (preSendTrunc.truncatedCount > 0) {
          activeSession.agent.replaceMessages(preSendTrunc.messages);
          log.info(
            `[pre-send-truncation] Truncated ${preSendTrunc.truncatedCount} oversized tool result(s) ` +
              `before prompt (contextWindow=${contextWindowTokens})`,
          );
        }
      } catch (err) {
        await flushPendingToolResultsAfterIdle({
          agent: activeSession?.agent,
          sessionManager,
          clearPendingOnTimeout: true,
        });
        activeSession.dispose();
        throw err;
      }

      let aborted = Boolean(params.abortSignal?.aborted);
      let yieldAborted = false;
      let timedOut = false;
      let timedOutDuringCompaction = false;
      const getAbortReason = (signal: AbortSignal): unknown =>
        "reason" in signal ? (signal as { reason?: unknown }).reason : undefined;
      const makeTimeoutAbortReason = (): Error => {
        const err = new Error("request timed out");
        err.name = "TimeoutError";
        return err;
      };
      const makeAbortError = (signal: AbortSignal): Error => {
        const reason = getAbortReason(signal);
        const err = reason ? new Error("aborted", { cause: reason }) : new Error("aborted");
        err.name = "AbortError";
        return err;
      };
      const abortCompaction = () => {
        if (!activeSession.isCompacting) {
          return;
        }
        try {
          activeSession.abortCompaction();
        } catch (err) {
          if (!isProbeSession) {
            log.warn(
              `embedded run abortCompaction failed: runId=${params.runId} sessionId=${params.sessionId} err=${String(err)}`,
            );
          }
        }
      };
      const abortRun = (isTimeout = false, reason?: unknown) => {
        aborted = true;
        if (isTimeout) {
          timedOut = true;
        }
        if (isTimeout) {
          runAbortController.abort(reason ?? makeTimeoutAbortReason());
        } else {
          runAbortController.abort(reason);
        }
        abortCompaction();
        void activeSession.abort();
      };
      const abortable = <T>(promise: Promise<T>): Promise<T> => {
        const signal = runAbortController.signal;
        if (signal.aborted) {
          return Promise.reject(makeAbortError(signal));
        }
        return new Promise<T>((resolve, reject) => {
          const onAbort = () => {
            signal.removeEventListener("abort", onAbort);
            reject(makeAbortError(signal));
          };
          signal.addEventListener("abort", onAbort, { once: true });
          promise.then(
            (value) => {
              signal.removeEventListener("abort", onAbort);
              resolve(value);
            },
            (err) => {
              signal.removeEventListener("abort", onAbort);
              reject(err);
            },
          );
        });
      };

      const subscription = subscribeEmbeddedPiSession({
        session: activeSession,
        runId: params.runId,
        hookRunner: getGlobalHookRunner() ?? undefined,
        verboseLevel: params.verboseLevel,
        reasoningMode: params.reasoningLevel ?? "off",
        toolResultFormat: params.toolResultFormat,
        shouldEmitToolResult: params.shouldEmitToolResult,
        shouldEmitToolOutput: params.shouldEmitToolOutput,
        onToolResult: params.onToolResult,
        onReasoningStream: params.onReasoningStream,
        onReasoningEnd: params.onReasoningEnd,
        onBlockReply: params.onBlockReply,
        onBlockReplyFlush: params.onBlockReplyFlush,
        blockReplyBreak: params.blockReplyBreak,
        blockReplyChunking: params.blockReplyChunking,
        onPartialReply: params.onPartialReply,
        onAssistantMessageStart: params.onAssistantMessageStart,
        onAgentEvent: params.onAgentEvent,
        enforceFinalTag: params.enforceFinalTag,
        config: params.config,
        sessionKey: sandboxSessionKey,
        sessionId: params.sessionId,
        agentId: sessionAgentId,
      });

      const {
        assistantTexts,
        toolMetas,
        unsubscribe,
        waitForCompactionRetry,
        isCompactionInFlight,
        getMessagingToolSentTexts,
        getMessagingToolSentMediaUrls,
        getMessagingToolSentTargets,
        getSuccessfulCronAdds,
        didSendViaMessagingTool,
        getLastToolError,
        getUsageTotals,
        getCompactionCount,
      } = subscription;

      const queueHandle: EmbeddedPiQueueHandle = {
        queueMessage: async (text: string) => {
          await activeSession.steer(text);
        },
        isStreaming: () => activeSession.isStreaming,
        isCompacting: () => subscription.isCompacting(),
        abort: abortRun,
      };
      setActiveEmbeddedRun(params.sessionId, queueHandle, params.sessionKey);
      activeRunQueueHandle = queueHandle;

      let abortWarnTimer: NodeJS.Timeout | undefined;
      const isProbeSession = params.sessionId?.startsWith("probe-") ?? false;
      const compactionTimeoutMs = resolveCompactionTimeoutMs(params.config);
      let abortTimer: NodeJS.Timeout | undefined;
      let compactionGraceUsed = false;
      const scheduleAbortTimer = (delayMs: number, reason: "initial" | "compaction-grace") => {
        abortTimer = setTimeout(
          () => {
            const timeoutAction = resolveRunTimeoutDuringCompaction({
              isCompactionPendingOrRetrying: subscription.isCompacting(),
              isCompactionInFlight: activeSession.isCompacting,
              graceAlreadyUsed: compactionGraceUsed,
            });
            if (timeoutAction === "extend") {
              compactionGraceUsed = true;
              if (!isProbeSession) {
                log.warn(
                  `embedded run timeout reached during compaction; extending deadline: ` +
                    `runId=${params.runId} sessionId=${params.sessionId} extraMs=${compactionTimeoutMs}`,
                );
              }
              scheduleAbortTimer(compactionTimeoutMs, "compaction-grace");
              return;
            }

            if (!isProbeSession) {
              log.warn(
                reason === "compaction-grace"
                  ? `embedded run timeout after compaction grace: runId=${params.runId} sessionId=${params.sessionId} timeoutMs=${params.timeoutMs} compactionGraceMs=${compactionTimeoutMs}`
                  : `embedded run timeout: runId=${params.runId} sessionId=${params.sessionId} timeoutMs=${params.timeoutMs}`,
              );
            }
            if (
              shouldFlagCompactionTimeout({
                isTimeout: true,
                isCompactionPendingOrRetrying: subscription.isCompacting(),
                isCompactionInFlight: activeSession.isCompacting,
              })
            ) {
              timedOutDuringCompaction = true;
            }
            abortRun(true);
            if (!abortWarnTimer) {
              abortWarnTimer = setTimeout(() => {
                if (!activeSession.isStreaming) {
                  return;
                }
                if (!isProbeSession) {
                  log.warn(
                    `embedded run abort still streaming: runId=${params.runId} sessionId=${params.sessionId}`,
                  );
                }
              }, 10_000);
            }
          },
          Math.max(1, delayMs),
        );
      };
      scheduleAbortTimer(params.timeoutMs, "initial");

      let messagesSnapshot: AgentMessage[] = [];
      let sessionIdUsed = activeSession.sessionId;
      const onAbort = () => {
        const reason = params.abortSignal ? getAbortReason(params.abortSignal) : undefined;
        const timeout = reason ? isTimeoutError(reason) : false;
        if (
          shouldFlagCompactionTimeout({
            isTimeout: timeout,
            isCompactionPendingOrRetrying: subscription.isCompacting(),
            isCompactionInFlight: activeSession.isCompacting,
          })
        ) {
          timedOutDuringCompaction = true;
        }
        abortRun(timeout, reason);
      };
      if (params.abortSignal) {
        if (params.abortSignal.aborted) {
          onAbort();
        } else {
          params.abortSignal.addEventListener("abort", onAbort, {
            once: true,
          });
        }
      }

      // Hook runner was already obtained earlier before tool creation
      const hookAgentId = sessionAgentId;

      let promptError: unknown = null;
      let promptErrorSource: "prompt" | "compaction" | null = null;
      const prePromptMessageCount = activeSession.messages.length;
      try {
        const promptStartedAt = Date.now();

        // Run before_prompt_build hooks to allow plugins to inject prompt context.
        // Legacy compatibility: before_agent_start is also checked for context fields.
        let effectivePrompt = prependBootstrapPromptWarning(
          params.prompt,
          bootstrapPromptWarning.lines,
          {
            preserveExactPrompt: heartbeatPrompt,
          },
        );
        const hookCtx = {
          agentId: hookAgentId,
          sessionKey: params.sessionKey,
          sessionId: params.sessionId,
          workspaceDir: params.workspaceDir,
          messageProvider: params.messageProvider ?? undefined,
          trigger: params.trigger,
          channelId: params.messageChannel ?? params.messageProvider ?? undefined,
        };
        const hookResult = await resolvePromptBuildHookResult({
          prompt: params.prompt,
          messages: activeSession.messages,
          hookCtx,
          hookRunner,
          legacyBeforeAgentStartResult: params.legacyBeforeAgentStartResult,
        });
        {
          if (hookResult?.prependContext) {
            effectivePrompt = `${hookResult.prependContext}\n\n${effectivePrompt}`;
            log.debug(
              `hooks: prepended context to prompt (${hookResult.prependContext.length} chars)`,
            );
          }
          const legacySystemPrompt =
            typeof hookResult?.systemPrompt === "string" ? hookResult.systemPrompt.trim() : "";
          if (legacySystemPrompt) {
            applySystemPromptOverrideToSession(activeSession, legacySystemPrompt);
            systemPromptText = legacySystemPrompt;
            log.debug(`hooks: applied systemPrompt override (${legacySystemPrompt.length} chars)`);
          }
          const prependedOrAppendedSystemPrompt = composeSystemPromptWithHookContext({
            baseSystemPrompt: systemPromptText,
            prependSystemContext: hookResult?.prependSystemContext,
            appendSystemContext: hookResult?.appendSystemContext,
          });
          if (prependedOrAppendedSystemPrompt) {
            const prependSystemLen = hookResult?.prependSystemContext?.trim().length ?? 0;
            const appendSystemLen = hookResult?.appendSystemContext?.trim().length ?? 0;
            applySystemPromptOverrideToSession(activeSession, prependedOrAppendedSystemPrompt);
            systemPromptText = prependedOrAppendedSystemPrompt;
            log.debug(
              `hooks: applied prependSystemContext/appendSystemContext (${prependSystemLen}+${appendSystemLen} chars)`,
            );
          }
        }

        log.debug(`embedded run prompt start: runId=${params.runId} sessionId=${params.sessionId}`);
        cacheTrace?.recordStage("prompt:before", {
          prompt: effectivePrompt,
          messages: activeSession.messages,
        });

        // Repair orphaned trailing user messages so new prompts don't violate role ordering.
        const leafEntry = sessionManager.getLeafEntry();
        if (leafEntry?.type === "message" && leafEntry.message.role === "user") {
          if (leafEntry.parentId) {
            sessionManager.branch(leafEntry.parentId);
          } else {
            sessionManager.resetLeaf();
          }
          const sessionContext = sessionManager.buildSessionContext();
          activeSession.agent.replaceMessages(sessionContext.messages);
          log.warn(
            `Removed orphaned user message to prevent consecutive user turns. ` +
              `runId=${params.runId} sessionId=${params.sessionId}`,
          );
        }
        const transcriptLeafId =
          (sessionManager.getLeafEntry() as { id?: string } | null | undefined)?.id ?? null;

        try {
          // Idempotent cleanup for legacy sessions with persisted image payloads.
          // Called each run; only mutates already-answered user turns that still carry image blocks.
          const didPruneImages = pruneProcessedHistoryImages(activeSession.messages);
          if (didPruneImages) {
            activeSession.agent.replaceMessages(activeSession.messages);
          }

          // Detect and load images referenced in the prompt for vision-capable models.
          // Images are prompt-local only (pi-like behavior).
          const imageResult = await detectAndLoadPromptImages({
            prompt: effectivePrompt,
            workspaceDir: effectiveWorkspace,
            model: params.model,
            existingImages: params.images,
            maxBytes: MAX_IMAGE_BYTES,
            maxDimensionPx: resolveImageSanitizationLimits(params.config).maxDimensionPx,
            workspaceOnly: effectiveFsWorkspaceOnly,
            // Enforce sandbox path restrictions when sandbox is enabled
            sandbox:
              sandbox?.enabled && sandbox?.fsBridge
                ? { root: sandbox.workspaceDir, bridge: sandbox.fsBridge }
                : undefined,
          });

          cacheTrace?.recordStage("prompt:images", {
            prompt: effectivePrompt,
            messages: activeSession.messages,
            note: `images: prompt=${imageResult.images.length}`,
          });

          // Diagnostic: log context sizes before prompt to help debug early overflow errors.
          if (log.isEnabled("debug")) {
            const msgCount = activeSession.messages.length;
            const systemLen = systemPromptText?.length ?? 0;
            const promptLen = effectivePrompt.length;
            const sessionSummary = summarizeSessionContext(activeSession.messages);
            log.debug(
              `[context-diag] pre-prompt: sessionKey=${params.sessionKey ?? params.sessionId} ` +
                `messages=${msgCount} roleCounts=${sessionSummary.roleCounts} ` +
                `historyTextChars=${sessionSummary.totalTextChars} ` +
                `maxMessageTextChars=${sessionSummary.maxMessageTextChars} ` +
                `historyImageBlocks=${sessionSummary.totalImageBlocks} ` +
                `systemPromptChars=${systemLen} promptChars=${promptLen} ` +
                `promptImages=${imageResult.images.length} ` +
                `provider=${params.provider}/${params.modelId} sessionFile=${params.sessionFile}`,
            );
          }

          if (hookRunner?.hasHooks("llm_input")) {
            hookRunner
              .runLlmInput(
                {
                  runId: params.runId,
                  sessionId: params.sessionId,
                  provider: params.provider,
                  model: params.modelId,
                  systemPrompt: systemPromptText,
                  prompt: effectivePrompt,
                  historyMessages: activeSession.messages,
                  imagesCount: imageResult.images.length,
                },
                {
                  agentId: hookAgentId,
                  sessionKey: params.sessionKey,
                  sessionId: params.sessionId,
                  workspaceDir: params.workspaceDir,
                  messageProvider: params.messageProvider ?? undefined,
                  trigger: params.trigger,
                  channelId: params.messageChannel ?? params.messageProvider ?? undefined,
                },
              )
              .catch((err) => {
                log.warn(`llm_input hook failed: ${String(err)}`);
              });
          }

          const btwSnapshotMessages = activeSession.messages.slice(-MAX_BTW_SNAPSHOT_MESSAGES);
          updateActiveEmbeddedRunSnapshot(params.sessionId, {
            transcriptLeafId,
            messages: btwSnapshotMessages,
            inFlightPrompt: effectivePrompt,
          });

          // Only pass images option if there are actually images to pass
          // This avoids potential issues with models that don't expect the images parameter
          if (imageResult.images.length > 0) {
            await abortable(activeSession.prompt(effectivePrompt, { images: imageResult.images }));
          } else {
            await abortable(activeSession.prompt(effectivePrompt));
          }
        } catch (err) {
          // Yield-triggered abort is intentional — treat as clean stop, not error.
          // Check the abort reason to distinguish from external aborts (timeout, user cancel)
          // that may race after yieldDetected is set.
          yieldAborted =
            yieldDetected &&
            isRunnerAbortError(err) &&
            err instanceof Error &&
            err.cause === "sessions_yield";
          if (yieldAborted) {
            aborted = false;
            // Ensure the session abort has fully settled before proceeding.
            if (yieldAbortSettled) {
              // eslint-disable-next-line @typescript-eslint/await-thenable -- abort() returns Promise<void> per AgentSession.d.ts
              await yieldAbortSettled;
            }
            stripSessionsYieldArtifacts(activeSession);
            if (yieldMessage) {
              await persistSessionsYieldContextMessage(activeSession, yieldMessage);
            }
          } else {
            promptError = err;
            promptErrorSource = "prompt";
          }
        } finally {
          log.debug(
            `embedded run prompt end: runId=${params.runId} sessionId=${params.sessionId} durationMs=${Date.now() - promptStartedAt}`,
          );
        }

        // Capture snapshot before compaction wait so we have complete messages if timeout occurs
        // Check compaction state before and after to avoid race condition where compaction starts during capture
        // Use session state (not subscription) for snapshot decisions - need instantaneous compaction status
        const wasCompactingBefore = activeSession.isCompacting;
        const snapshot = activeSession.messages.slice();
        const wasCompactingAfter = activeSession.isCompacting;
        // Only trust snapshot if compaction wasn't running before or after capture
        const preCompactionSnapshot = wasCompactingBefore || wasCompactingAfter ? null : snapshot;
        const preCompactionSessionId = activeSession.sessionId;
        const COMPACTION_RETRY_AGGREGATE_TIMEOUT_MS = 60_000;

        try {
          // Flush buffered block replies before waiting for compaction so the
          // user receives the assistant response immediately.  Without this,
          // coalesced/buffered blocks stay in the pipeline until compaction
          // finishes — which can take minutes on large contexts (#35074).
          if (params.onBlockReplyFlush) {
            await params.onBlockReplyFlush();
          }

          // Skip compaction wait when yield aborted the run — the signal is
          // already tripped and abortable() would immediately reject.
          const compactionRetryWait = yieldAborted
            ? { timedOut: false }
            : await waitForCompactionRetryWithAggregateTimeout({
                waitForCompactionRetry,
                abortable,
                aggregateTimeoutMs: COMPACTION_RETRY_AGGREGATE_TIMEOUT_MS,
                isCompactionStillInFlight: isCompactionInFlight,
              });
          if (compactionRetryWait.timedOut) {
            timedOutDuringCompaction = true;
            if (!isProbeSession) {
              log.warn(
                `compaction retry aggregate timeout (${COMPACTION_RETRY_AGGREGATE_TIMEOUT_MS}ms): ` +
                  `proceeding with pre-compaction state runId=${params.runId} sessionId=${params.sessionId}`,
              );
            }
          }
        } catch (err) {
          if (isRunnerAbortError(err)) {
            if (!promptError) {
              promptError = err;
              promptErrorSource = "compaction";
            }
            if (!isProbeSession) {
              log.debug(
                `compaction wait aborted: runId=${params.runId} sessionId=${params.sessionId}`,
              );
            }
          } else {
            throw err;
          }
        }

        // Check if ANY compaction occurred during the entire attempt (prompt + retry).
        // Using a cumulative count (> 0) instead of a delta check avoids missing
        // compactions that complete during activeSession.prompt() before the delta
        // baseline is sampled.
        const compactionOccurredThisAttempt = getCompactionCount() > 0;
        // Append cache-TTL timestamp AFTER prompt + compaction retry completes.
        // Previously this was before the prompt, which caused a custom entry to be
        // inserted between compaction and the next prompt — breaking the
        // prepareCompaction() guard that checks the last entry type, leading to
        // double-compaction. See: https://github.com/deneb/deneb/issues/9282
        // Skip when timed out during compaction — session state may be inconsistent.
        // Also skip when compaction ran this attempt — appending a custom entry
        // after compaction would break the guard again. See: #28491
        if (!timedOutDuringCompaction && !compactionOccurredThisAttempt) {
          const shouldTrackCacheTtl =
            params.config?.agents?.defaults?.contextPruning?.mode === "cache-ttl" &&
            isCacheTtlEligibleProvider(params.provider, params.modelId);
          if (shouldTrackCacheTtl) {
            appendCacheTtlTimestamp(sessionManager, {
              timestamp: Date.now(),
              provider: params.provider,
              modelId: params.modelId,
            });
          }
        }

        // If timeout occurred during compaction, use pre-compaction snapshot when available
        // (compaction restructures messages but does not add user/assistant turns).
        const snapshotSelection = selectCompactionTimeoutSnapshot({
          timedOutDuringCompaction,
          preCompactionSnapshot,
          preCompactionSessionId,
          currentSnapshot: activeSession.messages.slice(),
          currentSessionId: activeSession.sessionId,
        });
        if (timedOutDuringCompaction) {
          if (!isProbeSession) {
            log.warn(
              `using ${snapshotSelection.source} snapshot: timed out during compaction runId=${params.runId} sessionId=${params.sessionId}`,
            );
          }
        }
        messagesSnapshot = snapshotSelection.messagesSnapshot;
        sessionIdUsed = snapshotSelection.sessionIdUsed;

        if (promptError && promptErrorSource === "prompt" && !compactionOccurredThisAttempt) {
          try {
            sessionManager.appendCustomEntry("deneb:prompt-error", {
              timestamp: Date.now(),
              runId: params.runId,
              sessionId: params.sessionId,
              provider: params.provider,
              model: params.modelId,
              api: params.model.api,
              error: describeUnknownError(promptError),
            });
          } catch (entryErr) {
            log.warn(`failed to persist prompt error entry: ${String(entryErr)}`);
          }
        }

        // Let the active context engine run its post-turn lifecycle.
        if (params.contextEngine) {
          const afterTurnRuntimeContext = buildAfterTurnRuntimeContext({
            attempt: params,
            workspaceDir: effectiveWorkspace,
            agentDir,
          });

          if (typeof params.contextEngine.afterTurn === "function") {
            try {
              await params.contextEngine.afterTurn({
                sessionId: sessionIdUsed,
                sessionKey: params.sessionKey,
                sessionFile: params.sessionFile,
                messages: messagesSnapshot,
                prePromptMessageCount,
                tokenBudget: params.contextTokenBudget,
                runtimeContext: afterTurnRuntimeContext,
              });
            } catch (afterTurnErr) {
              log.warn(`context engine afterTurn failed: ${String(afterTurnErr)}`);
            }
          } else {
            // Fallback: ingest new messages individually
            const newMessages = messagesSnapshot.slice(prePromptMessageCount);
            if (newMessages.length > 0) {
              if (typeof params.contextEngine.ingestBatch === "function") {
                try {
                  await params.contextEngine.ingestBatch({
                    sessionId: sessionIdUsed,
                    sessionKey: params.sessionKey,
                    messages: newMessages,
                  });
                } catch (ingestErr) {
                  log.warn(`context engine ingest failed: ${String(ingestErr)}`);
                }
              } else {
                for (const msg of newMessages) {
                  try {
                    await params.contextEngine.ingest({
                      sessionId: sessionIdUsed,
                      sessionKey: params.sessionKey,
                      message: msg,
                    });
                  } catch (ingestErr) {
                    log.warn(`context engine ingest failed: ${String(ingestErr)}`);
                  }
                }
              }
            }
          }
        }

        cacheTrace?.recordStage("session:after", {
          messages: messagesSnapshot,
          note: timedOutDuringCompaction
            ? "compaction timeout"
            : promptError
              ? "prompt error"
              : undefined,
        });
        anthropicPayloadLogger?.recordUsage(messagesSnapshot, promptError);

        // Run agent_end hooks to allow plugins to analyze the conversation
        // This is fire-and-forget, so we don't await
        // Run even on compaction timeout so plugins can log/cleanup
        if (hookRunner?.hasHooks("agent_end")) {
          hookRunner
            .runAgentEnd(
              {
                messages: messagesSnapshot,
                success: !aborted && !promptError,
                error: promptError ? describeUnknownError(promptError) : undefined,
                durationMs: Date.now() - promptStartedAt,
              },
              {
                agentId: hookAgentId,
                sessionKey: params.sessionKey,
                sessionId: params.sessionId,
                workspaceDir: params.workspaceDir,
                messageProvider: params.messageProvider ?? undefined,
                trigger: params.trigger,
                channelId: params.messageChannel ?? params.messageProvider ?? undefined,
              },
            )
            .catch((err) => {
              log.warn(`agent_end hook failed: ${err}`);
            });
        }
      } finally {
        clearTimeout(abortTimer);
        if (abortWarnTimer) {
          clearTimeout(abortWarnTimer);
        }
        if (!isProbeSession && (aborted || timedOut) && !timedOutDuringCompaction) {
          log.debug(
            `run cleanup: runId=${params.runId} sessionId=${params.sessionId} aborted=${aborted} timedOut=${timedOut}`,
          );
        }
        try {
          unsubscribe();
        } catch (err) {
          // unsubscribe() should never throw; if it does, it indicates a serious bug.
          // Log at error level to ensure visibility, but don't rethrow in finally block
          // as it would mask any exception from the try block above.
          log.error(
            `CRITICAL: unsubscribe failed, possible resource leak: runId=${params.runId} ${String(err)}`,
          );
        }
        clearActiveEmbeddedRun(params.sessionId, queueHandle, params.sessionKey);
        params.abortSignal?.removeEventListener?.("abort", onAbort);
      }

      const lastAssistant = messagesSnapshot
        .slice()
        .toReversed()
        .find((m) => m.role === "assistant");

      const toolMetasNormalized = toolMetas
        .filter(
          (entry): entry is { toolName: string; meta?: string } =>
            typeof entry.toolName === "string" && entry.toolName.trim().length > 0,
        )
        .map((entry) => ({ toolName: entry.toolName, meta: entry.meta }));

      if (hookRunner?.hasHooks("llm_output")) {
        hookRunner
          .runLlmOutput(
            {
              runId: params.runId,
              sessionId: params.sessionId,
              provider: params.provider,
              model: params.modelId,
              assistantTexts,
              lastAssistant,
              usage: getUsageTotals(),
            },
            {
              agentId: hookAgentId,
              sessionKey: params.sessionKey,
              sessionId: params.sessionId,
              workspaceDir: params.workspaceDir,
              messageProvider: params.messageProvider ?? undefined,
              trigger: params.trigger,
              channelId: params.messageChannel ?? params.messageProvider ?? undefined,
            },
          )
          .catch((err) => {
            log.warn(`llm_output hook failed: ${String(err)}`);
          });
      }

      return {
        aborted,
        timedOut,
        timedOutDuringCompaction,
        promptError,
        sessionIdUsed,
        bootstrapPromptWarningSignaturesSeen: bootstrapPromptWarning.warningSignaturesSeen,
        bootstrapPromptWarningSignature: bootstrapPromptWarning.signature,
        systemPromptReport,
        messagesSnapshot,
        assistantTexts,
        toolMetas: toolMetasNormalized,
        lastAssistant,
        lastToolError: getLastToolError?.(),
        didSendViaMessagingTool: didSendViaMessagingTool(),
        messagingToolSentTexts: getMessagingToolSentTexts(),
        messagingToolSentMediaUrls: getMessagingToolSentMediaUrls(),
        messagingToolSentTargets: getMessagingToolSentTargets(),
        successfulCronAdds: getSuccessfulCronAdds(),
        cloudCodeAssistFormatError: Boolean(
          lastAssistant?.errorMessage && isCloudCodeAssistFormatError(lastAssistant.errorMessage),
        ),
        attemptUsage: getUsageTotals(),
        compactionCount: getCompactionCount(),
        // Client tool call detected (OpenResponses hosted tools)
        clientToolCall: clientToolCallDetected ?? undefined,
        yieldDetected: yieldDetected || undefined,
      };
    } finally {
      // Always tear down the session (and release the lock) before we leave this attempt.
      //
      // BUGFIX: Wait for the agent to be truly idle before flushing pending tool results.
      // pi-agent-core's auto-retry resolves waitForRetry() on assistant message receipt,
      // *before* tool execution completes in the retried agent loop. Without this wait,
      // flushPendingToolResults() fires while tools are still executing, inserting
      // synthetic "missing tool result" errors and causing silent agent failures.
      // See: https://github.com/deneb/deneb/issues/8643
      removeToolResultContextGuard?.();
      await flushPendingToolResultsAfterIdle({
        agent: session?.agent,
        sessionManager,
        clearPendingOnTimeout: true,
      });
      session?.dispose();
      releaseWsSession(params.sessionId);
      await bundleMcpRuntime?.dispose();
      await bundleLspRuntime?.dispose();
      // Safety net: if setActiveEmbeddedRun was called but the inner
      // try/finally (which normally calls clearActiveEmbeddedRun) was never
      // reached, clear the stale active-run entry here so subsequent
      // messages aren't permanently queued. The handle identity check
      // inside clearActiveEmbeddedRun makes this idempotent — calling it
      // twice with the same handle is harmless.
      if (activeRunQueueHandle) {
        clearActiveEmbeddedRun(params.sessionId, activeRunQueueHandle, params.sessionKey);
      }
      await sessionLock.release();
    }
  } finally {
    restoreSkillEnv?.();
    process.chdir(prevCwd);
  }
}
