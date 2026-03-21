import { createInternalHookEvent, triggerInternalHook } from "../../hooks/internal-hooks.js";
import { getGlobalHookRunner } from "../../plugins/hook-runner-global.js";
import { log } from "./logger.js";

/** Context identifying the session for hook subscribers. */
export type CompactionHookContext = {
  sessionId: string;
  agentId?: string;
  sessionKey: string;
  workspaceDir: string;
  messageProvider?: string;
  sessionFile: string;
  missingSessionKey?: boolean;
};

/** Data passed to before-compaction hooks. */
export type BeforeCompactionHookData = {
  phase: "before";
  messageCount?: number;
  tokenCount?: number;
  messageCountOriginal?: number;
  tokenCountOriginal?: number;
};

/** Data passed to after-compaction hooks. */
export type AfterCompactionHookData = {
  phase: "after";
  messageCount?: number;
  tokenCount?: number;
  compactedCount?: number;
  summaryLength?: number;
  tokensBefore?: number;
  tokensAfter?: number;
  firstKeptEntryId?: string;
};

export type CompactionHookData = BeforeCompactionHookData | AfterCompactionHookData;

/**
 * Fire both internal hooks (triggerInternalHook) and plugin hooks
 * (hookRunner.runBeforeCompaction / runAfterCompaction) in a single call.
 *
 * This centralizes hook invocation that was previously duplicated across
 * compactEmbeddedPiSessionDirect, compactEmbeddedPiSession, and the
 * overflow recovery loop in run.ts.
 */
export async function fireCompactionHooks(
  data: CompactionHookData,
  ctx: CompactionHookContext,
): Promise<void> {
  const hookRunner = getGlobalHookRunner();

  if (data.phase === "before") {
    // Internal hook
    try {
      const hookEvent = createInternalHookEvent("session", "compact:before", ctx.sessionKey, {
        sessionId: ctx.sessionId,
        missingSessionKey: ctx.missingSessionKey,
        messageCount: data.messageCount,
        tokenCount: data.tokenCount,
        messageCountOriginal: data.messageCountOriginal,
        tokenCountOriginal: data.tokenCountOriginal,
      });
      await triggerInternalHook(hookEvent);
    } catch (err) {
      log.warn("session:compact:before hook failed", {
        errorMessage: err instanceof Error ? err.message : String(err),
        errorStack: err instanceof Error ? err.stack : undefined,
      });
    }

    // Plugin hook
    if (hookRunner?.hasHooks("before_compaction")) {
      try {
        await hookRunner.runBeforeCompaction(
          {
            messageCount: data.messageCount ?? -1,
            tokenCount: data.tokenCount,
            sessionFile: ctx.sessionFile,
          },
          {
            sessionId: ctx.sessionId,
            agentId: ctx.agentId,
            sessionKey: ctx.sessionKey,
            workspaceDir: ctx.workspaceDir,
            messageProvider: ctx.messageProvider,
          },
        );
      } catch (err) {
        log.warn("before_compaction hook failed", {
          errorMessage: err instanceof Error ? err.message : String(err),
          errorStack: err instanceof Error ? err.stack : undefined,
        });
      }
    }
  } else {
    // Internal hook
    try {
      const hookEvent = createInternalHookEvent("session", "compact:after", ctx.sessionKey, {
        sessionId: ctx.sessionId,
        missingSessionKey: ctx.missingSessionKey,
        messageCount: data.messageCount,
        tokenCount: data.tokenCount,
        compactedCount: data.compactedCount,
        summaryLength: data.summaryLength,
        tokensBefore: data.tokensBefore,
        tokensAfter: data.tokensAfter,
        firstKeptEntryId: data.firstKeptEntryId,
      });
      await triggerInternalHook(hookEvent);
    } catch (err) {
      log.warn("session:compact:after hook failed", {
        errorMessage: err instanceof Error ? err.message : String(err),
        errorStack: err instanceof Error ? err.stack : undefined,
      });
    }

    // Plugin hook
    if (hookRunner?.hasHooks("after_compaction")) {
      try {
        await hookRunner.runAfterCompaction(
          {
            messageCount: data.messageCount ?? -1,
            compactedCount: data.compactedCount ?? -1,
            tokenCount: data.tokenCount,
            sessionFile: ctx.sessionFile,
          },
          {
            sessionId: ctx.sessionId,
            agentId: ctx.agentId,
            sessionKey: ctx.sessionKey,
            workspaceDir: ctx.workspaceDir,
            messageProvider: ctx.messageProvider,
          },
        );
      } catch (err) {
        log.warn("after_compaction hook failed", {
          errorMessage: err instanceof Error ? err.message : String(err),
          errorStack: err instanceof Error ? err.stack : undefined,
        });
      }
    }
  }
}
