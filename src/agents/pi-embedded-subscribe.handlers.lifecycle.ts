import { emitAgentEvent } from "../infra/agent-events.js";
import { createInlineCodeState } from "../markdown/code-spans.js";
import {
  buildApiErrorObservationFields,
  buildTextObservationFields,
  sanitizeForConsole,
} from "./pi-embedded-error-observation.js";
import { classifyFailoverReason, formatAssistantErrorText } from "./pi-embedded-helpers.js";
import type { EmbeddedPiSubscribeContext } from "./pi-embedded-subscribe.handlers.types.js";
import { isAssistantMessage } from "./pi-embedded-utils.js";

export {
  handleAutoCompactionEnd,
  handleAutoCompactionStart,
} from "./pi-embedded-subscribe.handlers.compaction.js";

// Track per-run start times for duration calculation.
const runStartTimes = new Map<string, number>();
const RUN_START_TIMES_MAX = 200;

export function handleAgentStart(ctx: EmbeddedPiSubscribeContext) {
  const now = Date.now();
  runStartTimes.set(ctx.params.runId, now);
  // Keep the map bounded.
  if (runStartTimes.size > RUN_START_TIMES_MAX) {
    const oldest = runStartTimes.keys().next().value;
    if (oldest !== undefined) {
      runStartTimes.delete(oldest);
    }
  }
  const safeRunId = sanitizeForConsole(ctx.params.runId) ?? "-";
  const safeSessionKey = sanitizeForConsole(ctx.params.sessionKey) ?? "-";
  const safeAgentId = sanitizeForConsole(ctx.params.agentId) ?? "default";
  ctx.log.debug("embedded run agent start", {
    event: "embedded_run_agent_start",
    tags: ["lifecycle", "agent_start"],
    runId: ctx.params.runId,
    sessionKey: ctx.params.sessionKey,
    agentId: ctx.params.agentId,
    startedAt: now,
    consoleMessage: `agent start: runId=${safeRunId} session=${safeSessionKey} agent=${safeAgentId}`,
  });
  emitAgentEvent({
    runId: ctx.params.runId,
    stream: "lifecycle",
    data: {
      phase: "start",
      startedAt: now,
    },
  });
  void ctx.params.onAgentEvent?.({
    stream: "lifecycle",
    data: { phase: "start" },
  });
}

type RunSummary = {
  durationMs: number | undefined;
  toolCount: number;
  compactionCount: number;
  tokensIn: number | undefined;
  tokensOut: number | undefined;
  tokensCacheRead: number | undefined;
  tokensCacheWrite: number | undefined;
  tokensTotal: number | undefined;
  lastToolError: string | undefined;
};

function buildRunSummary(ctx: EmbeddedPiSubscribeContext): RunSummary {
  const startTime = runStartTimes.get(ctx.params.runId);
  const durationMs = startTime ? Date.now() - startTime : undefined;
  const usage = ctx.getUsageTotals();
  const compactionCount = ctx.getCompactionCount();
  const toolCount = ctx.state.toolMetas.length;
  const lastToolError = ctx.state.lastToolError;
  return {
    durationMs,
    toolCount,
    compactionCount,
    tokensIn: usage?.input,
    tokensOut: usage?.output,
    tokensCacheRead: usage?.cacheRead,
    tokensCacheWrite: usage?.cacheWrite,
    tokensTotal: usage?.total,
    lastToolError: lastToolError
      ? `${lastToolError.toolName}: ${lastToolError.error ?? "unknown"}`
      : undefined,
  };
}

export function handleAgentEnd(ctx: EmbeddedPiSubscribeContext) {
  const lastAssistant = ctx.state.lastAssistant;
  const isError = isAssistantMessage(lastAssistant) && lastAssistant.stopReason === "error";
  const summary = buildRunSummary(ctx);

  if (isError && lastAssistant) {
    const friendlyError = formatAssistantErrorText(lastAssistant, {
      cfg: ctx.params.config,
      sessionKey: ctx.params.sessionKey,
      provider: lastAssistant.provider,
      model: lastAssistant.model,
    });
    const rawError = lastAssistant.errorMessage?.trim();
    const failoverReason = classifyFailoverReason(rawError ?? "");
    const errorText = (friendlyError || lastAssistant.errorMessage || "LLM request failed.").trim();
    const observedError = buildApiErrorObservationFields(rawError);
    const safeErrorText =
      buildTextObservationFields(errorText).textPreview ?? "LLM request failed.";
    const safeRunId = sanitizeForConsole(ctx.params.runId) ?? "-";
    const safeModel = sanitizeForConsole(lastAssistant.model) ?? "unknown";
    const safeProvider = sanitizeForConsole(lastAssistant.provider) ?? "unknown";
    const durationLabel = summary.durationMs ? ` duration=${summary.durationMs}ms` : "";
    ctx.log.warn("embedded run agent end", {
      event: "embedded_run_agent_end",
      tags: ["error_handling", "lifecycle", "agent_end", "assistant_error"],
      runId: ctx.params.runId,
      sessionKey: ctx.params.sessionKey,
      agentId: ctx.params.agentId,
      isError: true,
      error: safeErrorText,
      failoverReason,
      model: lastAssistant.model,
      provider: lastAssistant.provider,
      ...observedError,
      ...summary,
      consoleMessage: `agent end ERROR: runId=${safeRunId} model=${safeProvider}/${safeModel} reason=${failoverReason ?? "unknown"} error=${safeErrorText}${durationLabel}`,
    });
    emitAgentEvent({
      runId: ctx.params.runId,
      stream: "lifecycle",
      data: {
        phase: "error",
        error: safeErrorText,
        failoverReason,
        provider: lastAssistant.provider,
        model: lastAssistant.model,
        ...summary,
        endedAt: Date.now(),
      },
    });
    void ctx.params.onAgentEvent?.({
      stream: "lifecycle",
      data: {
        phase: "error",
        error: safeErrorText,
      },
    });
  } else {
    const safeRunId = sanitizeForConsole(ctx.params.runId) ?? "-";
    const durationLabel = summary.durationMs ? ` duration=${summary.durationMs}ms` : "";
    const toolsLabel = summary.toolCount ? ` tools=${summary.toolCount}` : "";
    const tokensLabel = summary.tokensTotal ? ` tokens=${summary.tokensTotal}` : "";
    ctx.log.debug("embedded run agent end", {
      event: "embedded_run_agent_end",
      tags: ["lifecycle", "agent_end"],
      runId: ctx.params.runId,
      sessionKey: ctx.params.sessionKey,
      agentId: ctx.params.agentId,
      isError: false,
      ...summary,
      consoleMessage: `agent end OK: runId=${safeRunId}${durationLabel}${toolsLabel}${tokensLabel}`,
    });
    emitAgentEvent({
      runId: ctx.params.runId,
      stream: "lifecycle",
      data: {
        phase: "end",
        ...summary,
        endedAt: Date.now(),
      },
    });
    void ctx.params.onAgentEvent?.({
      stream: "lifecycle",
      data: { phase: "end" },
    });
  }

  // Clean up start time tracking.
  runStartTimes.delete(ctx.params.runId);

  ctx.flushBlockReplyBuffer();
  // Flush the reply pipeline so the response reaches the channel before
  // compaction wait blocks the run.  This mirrors the pattern used by
  // handleToolExecutionStart and ensures delivery is not held hostage to
  // long-running compaction (#35074).
  void ctx.params.onBlockReplyFlush?.();

  ctx.state.blockState.thinking = false;
  ctx.state.blockState.final = false;
  ctx.state.blockState.inlineCode = createInlineCodeState();

  if (ctx.state.pendingCompactionRetry > 0) {
    ctx.resolveCompactionRetry();
  } else {
    ctx.maybeResolveCompactionWait();
  }
}
