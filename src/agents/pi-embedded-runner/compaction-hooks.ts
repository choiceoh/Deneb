import { createInternalHookEvent, triggerInternalHook } from "../../hooks/internal-hooks.js";
import { getGlobalHookRunner } from "../../plugins/hook-runner-global.js";
import { log } from "./logger.js";

// ── Types ────────────────────────────────────────────────────────────────────

/** Session context for hook subscribers. */
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

// ── Context builder ──────────────────────────────────────────────────────────

/**
 * Build a CompactionHookContext from common session parameters.
 * Eliminates the need to manually construct hookCtx objects at each call site.
 */
export function buildCompactionHookContext(params: {
  sessionId: string;
  sessionKey?: string | null;
  agentId?: string;
  workspaceDir: string;
  messageProvider?: string;
  sessionFile: string;
}): CompactionHookContext {
  const sessionKey = params.sessionKey?.trim() || params.sessionId;
  return {
    sessionId: params.sessionId,
    agentId: params.agentId,
    sessionKey,
    workspaceDir: params.workspaceDir,
    messageProvider: params.messageProvider,
    sessionFile: params.sessionFile,
    missingSessionKey: !params.sessionKey || !params.sessionKey.trim(),
  };
}

// ── Hook firing ──────────────────────────────────────────────────────────────

/**
 * Fire both internal hooks and plugin hooks in a single call.
 *
 * Centralizes hook invocation so callers only need:
 *   const ctx = buildCompactionHookContext({ ... });
 *   await fireCompactionHooks({ phase: "before" }, ctx);
 */
export async function fireCompactionHooks(
  data: CompactionHookData,
  ctx: CompactionHookContext,
): Promise<void> {
  const hookRunner = getGlobalHookRunner();

  if (data.phase === "before") {
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
