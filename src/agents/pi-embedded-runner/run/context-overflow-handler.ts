import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { ThinkLevel, ReasoningLevel } from "../../../auto-reply/thinking.js";
import type { DenebConfig } from "../../../config/config.js";
import type { ContextEngine } from "../../../context-engine/types.js";
import type { ExecElevatedDefaults } from "../../bash-tools/bash-tools.js";
import type { ContextWindowInfo } from "../../context-window-guard.js";
import {
  extractObservedOverflowTokenCount,
  isCompactionFailureError,
  isLikelyContextOverflowError,
} from "../../pi-embedded-helpers.js";
import type { SkillSnapshot } from "../../skills.js";
import { buildCompactionHookContext, fireCompactionHooks } from "../compaction-hooks.js";
import { buildEmbeddedCompactionRuntimeContext } from "../compaction-runtime-context.js";
import { log } from "../logger.js";
import { createCompactionDiagId } from "../run-usage.js";
import {
  sessionLikelyHasOversizedToolResults,
  truncateOversizedToolResultsInSession,
} from "../tool-result-truncation.js";
import { describeUnknownError } from "../utils.js";

export type CompactionTracker = {
  overflowAttempts: number;
  successCount: number;
  recordOverflowAttempt(): void;
  recordSuccess(count?: number): void;
  canRetryOverflow(): boolean;
};

export const MAX_OVERFLOW_COMPACTION_ATTEMPTS = 3;

export function createCompactionTracker(): CompactionTracker {
  const tracker: CompactionTracker = {
    overflowAttempts: 0,
    successCount: 0,
    recordOverflowAttempt() {
      this.overflowAttempts++;
    },
    recordSuccess(count = 1) {
      this.successCount += count;
    },
    canRetryOverflow() {
      return this.overflowAttempts < MAX_OVERFLOW_COMPACTION_ATTEMPTS;
    },
  };
  return tracker;
}

export type ContextOverflowInfo = {
  text: string;
  source: "promptError" | "assistantError";
} | null;

/**
 * Detect whether the current attempt hit a context overflow error.
 */
export function detectContextOverflow(params: {
  aborted: boolean;
  promptError: unknown;
  assistantErrorText?: string;
}): ContextOverflowInfo {
  if (params.aborted) {
    return null;
  }
  if (params.promptError) {
    const errorText = describeUnknownError(params.promptError);
    if (isLikelyContextOverflowError(errorText)) {
      return { text: errorText, source: "promptError" };
    }
    // Prompt submission failed with a non-overflow error.
    return null;
  }
  if (params.assistantErrorText && isLikelyContextOverflowError(params.assistantErrorText)) {
    return { text: params.assistantErrorText, source: "assistantError" };
  }
  return null;
}

export type OverflowHandlerParams = {
  contextOverflowError: NonNullable<ContextOverflowInfo>;
  compactionTracker: CompactionTracker;
  toolResultTruncationAttempted: boolean;
  attemptCompactionCount: number;
  messagesSnapshot: AgentMessage[] | undefined;
  ctxInfo: ContextWindowInfo;
  contextEngine: ContextEngine;
  // Session/run identifiers
  sessionId: string;
  sessionKey?: string;
  sessionFile: string;
  runId?: string;
  provider: string;
  modelId: string;
  // Hook context fields
  agentId?: string;
  workspaceDir: string;
  messageProvider?: string;
  // Compaction runtime context fields
  messageChannel?: string;
  agentAccountId?: string;
  currentChannelId?: string;
  currentThreadTs?: string | null;
  currentMessageId?: string | number | null;
  authProfileId?: string;
  agentDir: string;
  config?: DenebConfig;
  skillsSnapshot?: SkillSnapshot;
  senderIsOwner?: boolean;
  senderId?: string | null;
  thinkLevel?: ThinkLevel;
  reasoningLevel?: ReasoningLevel;
  bashElevated?: ExecElevatedDefaults;
  extraSystemPrompt?: string;
  ownerNumbers?: string[];
};

export type OverflowHandlerResult =
  | { action: "continue"; toolResultTruncationAttempted?: boolean }
  | { action: "give_up"; kind: "compaction_failure" | "context_overflow"; errorText: string };

/**
 * Attempt to recover from a context overflow via compaction and/or
 * tool result truncation. Returns "continue" if the caller should retry
 * the prompt, or "give_up" with error details.
 */
export async function handleContextOverflow(
  params: OverflowHandlerParams,
): Promise<OverflowHandlerResult> {
  const { contextOverflowError, compactionTracker } = params;
  const overflowDiagId = createCompactionDiagId();
  const errorText = contextOverflowError.text;
  const msgCount = params.messagesSnapshot?.length ?? 0;
  const observedOverflowTokens = extractObservedOverflowTokenCount(errorText);

  log.warn(
    `[context-overflow-diag] sessionKey=${params.sessionKey ?? params.sessionId} ` +
      `provider=${params.provider}/${params.modelId} source=${contextOverflowError.source} ` +
      `messages=${msgCount} sessionFile=${params.sessionFile} ` +
      `diagId=${overflowDiagId} compactionAttempts=${compactionTracker.overflowAttempts} ` +
      `observedTokens=${observedOverflowTokens ?? "unknown"} ` +
      `error=${errorText.slice(0, 200)}`,
  );

  const isCompactionFailure = isCompactionFailureError(errorText);
  const hadAttemptLevelCompaction = params.attemptCompactionCount > 0;

  // If this attempt already compacted (SDK auto-compaction), avoid immediately
  // running another explicit compaction for the same overflow trigger.
  if (!isCompactionFailure && hadAttemptLevelCompaction && compactionTracker.canRetryOverflow()) {
    compactionTracker.recordOverflowAttempt();
    log.warn(
      `context overflow persisted after in-attempt compaction (attempt ${compactionTracker.overflowAttempts}/${MAX_OVERFLOW_COMPACTION_ATTEMPTS}); retrying prompt without additional compaction for ${params.provider}/${params.modelId}`,
    );
    return { action: "continue" };
  }

  // Attempt explicit overflow compaction only when this attempt did not
  // already auto-compact.
  if (!isCompactionFailure && !hadAttemptLevelCompaction && compactionTracker.canRetryOverflow()) {
    if (log.isEnabled("debug")) {
      log.debug(
        `[compaction-diag] decision diagId=${overflowDiagId} branch=compact ` +
          `isCompactionFailure=${isCompactionFailure} hasOversizedToolResults=unknown ` +
          `attempt=${compactionTracker.overflowAttempts + 1} maxAttempts=${MAX_OVERFLOW_COMPACTION_ATTEMPTS}`,
      );
    }
    compactionTracker.recordOverflowAttempt();
    log.warn(
      `context overflow detected (attempt ${compactionTracker.overflowAttempts}/${MAX_OVERFLOW_COMPACTION_ATTEMPTS}); attempting auto-compaction for ${params.provider}/${params.modelId}`,
    );

    const overflowHookCtx = buildCompactionHookContext({
      sessionId: params.sessionId,
      sessionKey: params.sessionKey,
      agentId: params.agentId,
      workspaceDir: params.workspaceDir,
      messageProvider: params.messageProvider,
      sessionFile: params.sessionFile,
    });
    await fireCompactionHooks({ phase: "before" }, overflowHookCtx);

    let compactResult: Awaited<ReturnType<typeof params.contextEngine.compact>>;
    try {
      compactResult = await params.contextEngine.compact({
        sessionId: params.sessionId,
        sessionKey: params.sessionKey,
        sessionFile: params.sessionFile,
        tokenBudget: params.ctxInfo.tokens,
        ...(observedOverflowTokens !== undefined
          ? { currentTokenCount: observedOverflowTokens }
          : {}),
        force: true,
        compactionTarget: "budget",
        runtimeContext: {
          ...buildEmbeddedCompactionRuntimeContext({
            sessionKey: params.sessionKey,
            messageChannel: params.messageChannel,
            messageProvider: params.messageProvider,
            agentAccountId: params.agentAccountId,
            currentChannelId: params.currentChannelId,
            currentThreadTs: params.currentThreadTs,
            currentMessageId: params.currentMessageId,
            authProfileId: params.authProfileId,
            workspaceDir: params.workspaceDir,
            agentDir: params.agentDir,
            config: params.config,
            skillsSnapshot: params.skillsSnapshot,
            senderIsOwner: params.senderIsOwner,
            senderId: params.senderId,
            provider: params.provider,
            modelId: params.modelId,
            thinkLevel: params.thinkLevel,
            reasoningLevel: params.reasoningLevel,
            bashElevated: params.bashElevated,
            extraSystemPrompt: params.extraSystemPrompt,
            ownerNumbers: params.ownerNumbers,
          }),
          runId: params.runId,
          trigger: "overflow",
          ...(observedOverflowTokens !== undefined
            ? { currentTokenCount: observedOverflowTokens }
            : {}),
          diagId: overflowDiagId,
          attempt: compactionTracker.overflowAttempts,
          maxAttempts: MAX_OVERFLOW_COMPACTION_ATTEMPTS,
        },
      });
    } catch (compactErr) {
      log.warn(
        `contextEngine.compact() threw during overflow recovery for ${params.provider}/${params.modelId}: ${String(compactErr)}`,
      );
      compactResult = { ok: false, compacted: false, reason: String(compactErr) };
    }

    if (compactResult.ok && compactResult.compacted) {
      await fireCompactionHooks(
        {
          phase: "after",
          tokenCount: compactResult.result?.tokensAfter,
          tokensBefore: compactResult.result?.tokensBefore,
          tokensAfter: compactResult.result?.tokensAfter,
        },
        overflowHookCtx,
      );
    }
    if (compactResult.compacted) {
      compactionTracker.recordSuccess();
      log.info(
        `auto-compaction succeeded for ${params.provider}/${params.modelId}; retrying prompt`,
      );
      return { action: "continue" };
    }
    log.warn(
      `auto-compaction failed for ${params.provider}/${params.modelId}: ${compactResult.reason ?? "nothing to compact"}`,
    );
  }

  // Fallback: try truncating oversized tool results in the session.
  if (!params.toolResultTruncationAttempted) {
    const contextWindowTokens = params.ctxInfo.tokens;
    const hasOversized = params.messagesSnapshot
      ? sessionLikelyHasOversizedToolResults({
          messages: params.messagesSnapshot,
          contextWindowTokens,
        })
      : false;

    if (hasOversized) {
      if (log.isEnabled("debug")) {
        log.debug(
          `[compaction-diag] decision diagId=${overflowDiagId} branch=truncate_tool_results ` +
            `isCompactionFailure=${isCompactionFailure} hasOversizedToolResults=${hasOversized} ` +
            `attempt=${compactionTracker.overflowAttempts} maxAttempts=${MAX_OVERFLOW_COMPACTION_ATTEMPTS}`,
        );
      }
      log.warn(
        `[context-overflow-recovery] Attempting tool result truncation for ${params.provider}/${params.modelId} ` +
          `(contextWindow=${contextWindowTokens} tokens)`,
      );
      const truncResult = await truncateOversizedToolResultsInSession({
        sessionFile: params.sessionFile,
        contextWindowTokens,
        sessionId: params.sessionId,
        sessionKey: params.sessionKey,
      });
      if (truncResult.truncated) {
        log.info(
          `[context-overflow-recovery] Truncated ${truncResult.truncatedCount} tool result(s); retrying prompt`,
        );
        return { action: "continue", toolResultTruncationAttempted: true };
      }
      log.warn(
        `[context-overflow-recovery] Tool result truncation did not help: ${truncResult.reason ?? "unknown"}`,
      );
    } else if (log.isEnabled("debug")) {
      log.debug(
        `[compaction-diag] decision diagId=${overflowDiagId} branch=give_up ` +
          `isCompactionFailure=${isCompactionFailure} hasOversizedToolResults=${hasOversized} ` +
          `attempt=${compactionTracker.overflowAttempts} maxAttempts=${MAX_OVERFLOW_COMPACTION_ATTEMPTS}`,
      );
    }
  }

  if (
    (isCompactionFailure ||
      !compactionTracker.canRetryOverflow() ||
      params.toolResultTruncationAttempted) &&
    log.isEnabled("debug")
  ) {
    log.debug(
      `[compaction-diag] decision diagId=${overflowDiagId} branch=give_up ` +
        `isCompactionFailure=${isCompactionFailure} hasOversizedToolResults=unknown ` +
        `attempt=${compactionTracker.overflowAttempts} maxAttempts=${MAX_OVERFLOW_COMPACTION_ATTEMPTS}`,
    );
  }

  const kind = isCompactionFailure ? "compaction_failure" : "context_overflow";
  return { action: "give_up", kind, errorText };
}
