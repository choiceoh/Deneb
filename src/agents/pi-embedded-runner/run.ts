import { randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import {
  ensureContextEnginesInitialized,
  resolveContextEngine,
} from "../../context-engine/index.js";
import { enqueueCommandInLane } from "../../process/command-queue.js";
import { markAuthProfileGood, markAuthProfileUsed } from "../auth-profiles.js";
import { applyLocalNoAuthHeaderOverride } from "../models/model-auth.js";
import { formatAssistantErrorText } from "../pi-embedded-helpers.js";
import { normalizeUsage, derivePromptTokens, type UsageLike } from "../usage.js";
import { resolveSessionLane, resolveGlobalLane } from "./lanes.js";
import { log } from "./logger.js";
import {
  createUsageAccumulator,
  mergeUsageIntoAccumulator,
  resolveActiveErrorContext,
  resolveMaxRunRetryIterations,
  scrubAnthropicRefusalMagic,
  buildErrorAgentMeta,
  toNormalizedUsage,
} from "./run-usage.js";
import { runEmbeddedAttempt } from "./run/attempt.js";
import { AuthProfileOrchestrator } from "./run/auth-profile-orchestration.js";
import {
  createCompactionTracker,
  detectContextOverflow,
  handleContextOverflow,
} from "./run/context-overflow-handler.js";
import {
  handlePromptError,
  handleAssistantError,
  type ErrorHandlerContext,
} from "./run/error-handlers.js";
import type { RunEmbeddedPiAgentParams } from "./run/params.js";
import { buildEmbeddedRunPayloads } from "./run/payloads.js";
import { RuntimeAuthManager } from "./run/runtime-auth.js";
import { setupRun } from "./run/setup.js";
import type { EmbeddedPiAgentMeta, EmbeddedPiRunResult } from "./types.js";

export async function runEmbeddedPiAgent(
  params: RunEmbeddedPiAgentParams,
): Promise<EmbeddedPiRunResult> {
  const sessionLane = resolveSessionLane(params.sessionKey?.trim() || params.sessionId);
  const globalLane = resolveGlobalLane(params.lane);
  const enqueueGlobal =
    params.enqueue ?? ((task, opts) => enqueueCommandInLane(globalLane, task, opts));
  const enqueueSession =
    params.enqueue ?? ((task, opts) => enqueueCommandInLane(sessionLane, task, opts));

  return enqueueSession(() =>
    enqueueGlobal(async () => {
      const started = Date.now();
      const prevCwd = process.cwd();

      // ── Phase 1: Setup ──────────────────────────────────────────
      const setup = await setupRun(params);
      const {
        provider,
        modelId,
        authStorage,
        modelRegistry,
        ctxInfo,
        agentDir,
        resolvedWorkspace,
        fallbackConfigured,
        resolvedToolResultFormat,
        isProbeSession,
        profileCandidates,
        lockedProfileId,
        authStore,
        legacyBeforeAgentStartResult,
        hookCtx,
      } = setup;
      const model = setup.runtimeModel;

      // ── Phase 2: Runtime auth + auth profile orchestration ──────
      const runtimeAuthManager = new RuntimeAuthManager(setup.runtimeModel, setup.effectiveModel, {
        config: params.config,
        workspaceDir: resolvedWorkspace,
        agentDir,
        modelId,
        authStorage,
      });
      const authOrchestrator = new AuthProfileOrchestrator(
        {
          config: params.config,
          provider,
          modelId,
          agentDir,
          workspaceDir: resolvedWorkspace,
          runId: params.runId,
          fallbackConfigured,
          authStore,
          runtimeAuthManager,
        },
        profileCandidates,
        lockedProfileId,
        params.thinkLevel ?? "off",
      );

      await authOrchestrator.selectInitialProfile({
        allowTransientCooldownProbe: params.allowTransientCooldownProbe,
      });

      // ── Phase 3: Run loop ───────────────────────────────────────
      const compactionTracker = createCompactionTracker();
      let toolResultTruncationAttempted = false;
      let bootstrapPromptWarningSignaturesSeen =
        params.bootstrapPromptWarningSignaturesSeen ??
        (params.bootstrapPromptWarningSignature ? [params.bootstrapPromptWarningSignature] : []);
      const usageAccumulator = createUsageAccumulator();
      let lastRunPromptUsage: ReturnType<typeof normalizeUsage> | undefined;
      let runLoopIterations = 0;
      const overloadFailoverAttempts = { value: 0 };
      const MAX_RUN_LOOP_ITERATIONS = resolveMaxRunRetryIterations(profileCandidates.length);

      ensureContextEnginesInitialized();
      const contextEngine = await resolveContextEngine(params.config);

      const errorCtx: ErrorHandlerContext = {
        params: {
          sessionId: params.sessionId,
          sessionKey: params.sessionKey,
          runId: params.runId,
          abortSignal: params.abortSignal,
        },
        provider,
        modelId,
        modelOriginalId: model.id,
        started,
        fallbackConfigured,
        usageAccumulator,
        lastRunPromptUsage: undefined,
        config: params.config,
        authOrchestrator,
        runtimeAuthManager,
        isProbeSession,
      };

      try {
        let authRetryPending = false;
        let lastTurnTotal: number | undefined;

        while (true) {
          if (runLoopIterations >= MAX_RUN_LOOP_ITERATIONS) {
            log.error(
              `[run-retry-limit] sessionKey=${params.sessionKey ?? params.sessionId} ` +
                `provider=${provider}/${modelId} attempts=${runLoopIterations} ` +
                `maxAttempts=${MAX_RUN_LOOP_ITERATIONS}`,
            );
            return {
              payloads: [
                {
                  text:
                    "Request failed after repeated internal retries. " +
                    "Please try again, or use /new to start a fresh session.",
                  isError: true,
                },
              ],
              meta: {
                durationMs: Date.now() - started,
                agentMeta: buildErrorAgentMeta({
                  sessionId: params.sessionId,
                  provider,
                  model: model.id,
                  usageAccumulator,
                  lastRunPromptUsage,
                  lastTurnTotal,
                }),
                error: {
                  kind: "retry_limit",
                  message: `Exceeded retry limit after ${runLoopIterations} attempts (max=${MAX_RUN_LOOP_ITERATIONS}).`,
                },
              },
            };
          }
          runLoopIterations += 1;
          const runtimeAuthRetry = authRetryPending;
          authRetryPending = false;
          authOrchestrator.attemptedThinking.add(authOrchestrator.thinkLevel);
          await fs.mkdir(resolvedWorkspace, { recursive: true });

          const prompt =
            provider === "anthropic" ? scrubAnthropicRefusalMagic(params.prompt) : params.prompt;

          const effectiveModel = runtimeAuthManager.getEffectiveModel();

          // ── Run attempt ───────────────────────────────────────
          const attempt = await runEmbeddedAttempt({
            sessionId: params.sessionId,
            sessionKey: params.sessionKey,
            trigger: params.trigger,
            memoryFlushWritePath: params.memoryFlushWritePath,
            messageChannel: params.messageChannel,
            messageProvider: params.messageProvider,
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
            currentChannelId: params.currentChannelId,
            currentThreadTs: params.currentThreadTs,
            currentMessageId: params.currentMessageId,
            replyToMode: params.replyToMode,
            hasRepliedRef: params.hasRepliedRef,
            sessionFile: params.sessionFile,
            workspaceDir: resolvedWorkspace,
            agentDir,
            config: params.config,
            allowGatewaySubagentBinding: params.allowGatewaySubagentBinding,
            contextEngine,
            contextTokenBudget: ctxInfo.tokens,
            skillsSnapshot: params.skillsSnapshot,
            prompt,
            images: params.images,
            disableTools: params.disableTools,
            provider,
            modelId,
            model: applyLocalNoAuthHeaderOverride(effectiveModel, authOrchestrator.apiKeyInfo),
            authProfileId: authOrchestrator.lastProfileId,
            authProfileIdSource: lockedProfileId ? "user" : "auto",
            authStorage,
            modelRegistry,
            agentId: setup.workspaceAgentId,
            legacyBeforeAgentStartResult,
            thinkLevel: authOrchestrator.thinkLevel,
            fastMode: params.fastMode,
            verboseLevel: params.verboseLevel,
            reasoningLevel: params.reasoningLevel,
            toolResultFormat: resolvedToolResultFormat,
            execOverrides: params.execOverrides,
            bashElevated: params.bashElevated,
            timeoutMs: params.timeoutMs,
            runId: params.runId,
            abortSignal: params.abortSignal,
            shouldEmitToolResult: params.shouldEmitToolResult,
            shouldEmitToolOutput: params.shouldEmitToolOutput,
            onPartialReply: params.onPartialReply,
            onAssistantMessageStart: params.onAssistantMessageStart,
            onBlockReply: params.onBlockReply,
            onBlockReplyFlush: params.onBlockReplyFlush,
            blockReplyBreak: params.blockReplyBreak,
            blockReplyChunking: params.blockReplyChunking,
            onReasoningStream: params.onReasoningStream,
            onReasoningEnd: params.onReasoningEnd,
            onToolResult: params.onToolResult,
            onAgentEvent: params.onAgentEvent,
            extraSystemPrompt: params.extraSystemPrompt,
            inputProvenance: params.inputProvenance,
            streamParams: params.streamParams,
            ownerNumbers: params.ownerNumbers,
            enforceFinalTag: params.enforceFinalTag,
            bootstrapPromptWarningSignaturesSeen,
            bootstrapPromptWarningSignature:
              bootstrapPromptWarningSignaturesSeen[bootstrapPromptWarningSignaturesSeen.length - 1],
          });

          // ── Accumulate usage ──────────────────────────────────
          const {
            aborted,
            promptError,
            timedOut,
            timedOutDuringCompaction,
            sessionIdUsed,
            lastAssistant,
          } = attempt;
          bootstrapPromptWarningSignaturesSeen =
            attempt.bootstrapPromptWarningSignaturesSeen ??
            (attempt.bootstrapPromptWarningSignature
              ? Array.from(
                  new Set([
                    ...bootstrapPromptWarningSignaturesSeen,
                    attempt.bootstrapPromptWarningSignature,
                  ]),
                )
              : bootstrapPromptWarningSignaturesSeen);
          const lastAssistantUsage = normalizeUsage(lastAssistant?.usage as UsageLike);
          const attemptUsage = attempt.attemptUsage ?? lastAssistantUsage;
          mergeUsageIntoAccumulator(usageAccumulator, attemptUsage);
          lastRunPromptUsage = lastAssistantUsage ?? attemptUsage;
          errorCtx.lastRunPromptUsage = lastRunPromptUsage;
          lastTurnTotal = lastAssistantUsage?.total ?? attemptUsage?.total;
          const attemptCompactionCount = Math.max(0, attempt.compactionCount ?? 0);
          compactionTracker.recordSuccess(attemptCompactionCount);

          const activeErrorContext = resolveActiveErrorContext({
            lastAssistant,
            provider,
            model: modelId,
          });
          const formattedAssistantErrorText = lastAssistant
            ? formatAssistantErrorText(lastAssistant, {
                cfg: params.config,
                sessionKey: params.sessionKey ?? params.sessionId,
                provider: activeErrorContext.provider,
                model: activeErrorContext.model,
              })
            : undefined;
          const assistantErrorText =
            lastAssistant?.stopReason === "error"
              ? lastAssistant.errorMessage?.trim() || formattedAssistantErrorText
              : undefined;

          // ── Context overflow recovery ─────────────────────────
          const contextOverflowError = detectContextOverflow({
            aborted,
            promptError,
            assistantErrorText,
          });

          if (contextOverflowError) {
            const overflowResult = await handleContextOverflow({
              contextOverflowError,
              compactionTracker,
              toolResultTruncationAttempted,
              attemptCompactionCount,
              messagesSnapshot: attempt.messagesSnapshot,
              ctxInfo,
              contextEngine,
              sessionId: params.sessionId,
              sessionKey: params.sessionKey,
              sessionFile: params.sessionFile,
              runId: params.runId,
              provider,
              modelId,
              agentId: hookCtx.agentId,
              workspaceDir: resolvedWorkspace,
              messageProvider: params.messageProvider,
              messageChannel: params.messageChannel,
              agentAccountId: params.agentAccountId,
              currentChannelId: params.currentChannelId,
              currentThreadTs: params.currentThreadTs,
              currentMessageId: params.currentMessageId,
              authProfileId: authOrchestrator.lastProfileId,
              agentDir,
              config: params.config,
              skillsSnapshot: params.skillsSnapshot,
              senderIsOwner: params.senderIsOwner,
              senderId: params.senderId,
              thinkLevel: authOrchestrator.thinkLevel,
              reasoningLevel: params.reasoningLevel,
              bashElevated: params.bashElevated,
              extraSystemPrompt: params.extraSystemPrompt,
              ownerNumbers: params.ownerNumbers,
            });

            if (overflowResult.action === "continue") {
              if (overflowResult.toolResultTruncationAttempted) {
                toolResultTruncationAttempted = true;
              }
              continue;
            }
            // give_up
            return {
              payloads: [
                {
                  text:
                    "Context overflow: prompt too large for the model. " +
                    "Try /reset (or /new) to start a fresh session, or use a larger-context model.",
                  isError: true,
                },
              ],
              meta: {
                durationMs: Date.now() - started,
                agentMeta: buildErrorAgentMeta({
                  sessionId: sessionIdUsed,
                  provider,
                  model: model.id,
                  usageAccumulator,
                  lastRunPromptUsage,
                  lastAssistant,
                  lastTurnTotal,
                }),
                systemPromptReport: attempt.systemPromptReport,
                error: { kind: overflowResult.kind, message: overflowResult.errorText },
              },
            };
          }

          // ── Prompt error handling ─────────────────────────────
          if (promptError && !aborted) {
            const promptResult = await handlePromptError(
              errorCtx,
              attempt,
              overloadFailoverAttempts,
              runtimeAuthRetry,
            );
            if (promptResult.action === "continue") {
              if (promptResult.authRetryPending) {
                authRetryPending = true;
              }
              continue;
            }
            if (promptResult.action === "return") {
              return promptResult.result;
            }
            throw promptResult.error;
          }

          // ── Assistant error handling ──────────────────────────
          const assistantResult = await handleAssistantError(
            errorCtx,
            attempt,
            overloadFailoverAttempts,
          );
          if (assistantResult.action === "continue") {
            if (assistantResult.authRetryPending) {
              authRetryPending = true;
            }
            continue;
          }
          if (assistantResult.action === "throw") {
            throw assistantResult.error;
          }

          // ── Success path ──────────────────────────────────────
          const usage = toNormalizedUsage(usageAccumulator);
          if (usage && lastTurnTotal && lastTurnTotal > 0) {
            usage.total = lastTurnTotal;
          }
          const lastCallUsage = normalizeUsage(lastAssistant?.usage as UsageLike);
          const promptTokens = derivePromptTokens(lastRunPromptUsage);
          const agentMeta: EmbeddedPiAgentMeta = {
            sessionId: sessionIdUsed,
            provider: lastAssistant?.provider ?? provider,
            model: lastAssistant?.model ?? model.id,
            usage,
            lastCallUsage: lastCallUsage ?? undefined,
            promptTokens,
            compactionCount:
              compactionTracker.successCount > 0 ? compactionTracker.successCount : undefined,
          };

          const payloads = buildEmbeddedRunPayloads({
            assistantTexts: attempt.assistantTexts,
            toolMetas: attempt.toolMetas,
            lastAssistant: attempt.lastAssistant,
            lastToolError: attempt.lastToolError,
            config: params.config,
            sessionKey: params.sessionKey ?? params.sessionId,
            provider: activeErrorContext.provider,
            model: activeErrorContext.model,
            verboseLevel: params.verboseLevel,
            reasoningLevel: params.reasoningLevel,
            toolResultFormat: resolvedToolResultFormat,
            suppressToolErrorWarnings: params.suppressToolErrorWarnings,
            inlineToolResultsAllowed: false,
            didSendViaMessagingTool: attempt.didSendViaMessagingTool,
            didSendDeterministicApprovalPrompt: attempt.didSendDeterministicApprovalPrompt,
          });

          // Timeout aborts can leave the run without any assistant payloads.
          if (timedOut && !timedOutDuringCompaction && payloads.length === 0) {
            return {
              payloads: [
                {
                  text:
                    "Request timed out before a response was generated. " +
                    "Please try again, or increase `agents.defaults.timeoutSeconds` in your config.",
                  isError: true,
                },
              ],
              meta: {
                durationMs: Date.now() - started,
                agentMeta,
                aborted,
                systemPromptReport: attempt.systemPromptReport,
              },
              didSendViaMessagingTool: attempt.didSendViaMessagingTool,
              didSendDeterministicApprovalPrompt: attempt.didSendDeterministicApprovalPrompt,
              messagingToolSentTexts: attempt.messagingToolSentTexts,
              messagingToolSentMediaUrls: attempt.messagingToolSentMediaUrls,
              messagingToolSentTargets: attempt.messagingToolSentTargets,
              successfulCronAdds: attempt.successfulCronAdds,
            };
          }

          log.debug(
            `embedded run done: runId=${params.runId} sessionId=${params.sessionId} durationMs=${Date.now() - started} aborted=${aborted}`,
          );
          if (authOrchestrator.lastProfileId) {
            await markAuthProfileGood({
              store: authStore,
              provider,
              profileId: authOrchestrator.lastProfileId,
              agentDir: params.agentDir,
            });
            await markAuthProfileUsed({
              store: authStore,
              profileId: authOrchestrator.lastProfileId,
              agentDir: params.agentDir,
            });
          }
          return {
            payloads: payloads.length ? payloads : undefined,
            meta: {
              durationMs: Date.now() - started,
              agentMeta,
              aborted,
              systemPromptReport: attempt.systemPromptReport,
              stopReason: attempt.clientToolCall
                ? "tool_calls"
                : attempt.yieldDetected
                  ? "end_turn"
                  : (lastAssistant?.stopReason as string | undefined),
              pendingToolCalls: attempt.clientToolCall
                ? [
                    {
                      id: randomBytes(5).toString("hex").slice(0, 9),
                      name: attempt.clientToolCall.name,
                      arguments: JSON.stringify(attempt.clientToolCall.params),
                    },
                  ]
                : undefined,
            },
            didSendViaMessagingTool: attempt.didSendViaMessagingTool,
            didSendDeterministicApprovalPrompt: attempt.didSendDeterministicApprovalPrompt,
            messagingToolSentTexts: attempt.messagingToolSentTexts,
            messagingToolSentMediaUrls: attempt.messagingToolSentMediaUrls,
            messagingToolSentTargets: attempt.messagingToolSentTargets,
            successfulCronAdds: attempt.successfulCronAdds,
          };
        }
      } finally {
        await contextEngine.dispose?.();
        runtimeAuthManager.stop();
        process.chdir(prevCwd);
      }
    }),
  );
}
