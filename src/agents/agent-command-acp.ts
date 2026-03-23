import { getAcpSessionManager } from "../acp/control-plane/manager.js";
import type { AcpSessionResolution } from "../acp/control-plane/manager.types.js";
import { resolveAcpAgentPolicyError, resolveAcpDispatchPolicyError } from "../acp/policy.js";
import { toAcpRuntimeError } from "../acp/runtime/errors.js";
import { resolveAcpSessionCwd } from "../acp/runtime/session-identifiers.js";
import { normalizeReplyPayload } from "../auto-reply/reply/normalize-reply.js";
import { type CliDeps } from "../cli/deps.js";
import { loadConfig } from "../config/config.js";
import { resolveAgentIdFromSessionKey, type SessionEntry } from "../config/sessions.js";
import { emitAgentEvent, registerAgentRunContext } from "../infra/agent-events.js";
import { buildOutboundSessionContext } from "../infra/outbound/session-context.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { normalizeAgentId } from "../routing/session-key.js";
import { type RuntimeEnv } from "../runtime.js";
import {
  createAcpVisibleTextAccumulator,
  persistAcpTurnTranscript,
} from "./agent-command-helpers.js";
import { deliverAgentCommandResult } from "./command/delivery.js";
import type { AgentCommandOpts } from "./command/types.js";

const log = createSubsystemLogger("agents/agent-command");

export type AcpTurnParams = {
  cfg: ReturnType<typeof loadConfig>;
  deps: CliDeps;
  runtime: RuntimeEnv;
  opts: AgentCommandOpts & { senderIsOwner: boolean };
  sessionKey: string;
  sessionId: string;
  sessionAgentId: string;
  sessionEntry: SessionEntry | undefined;
  sessionStore?: Record<string, SessionEntry>;
  storePath?: string;
  workspaceDir: string;
  body: string;
  runId: string;
  acpResolution: AcpSessionResolution & { kind: "ready" };
  outboundSession: ReturnType<typeof buildOutboundSessionContext>;
};

export async function runAcpTurn(params: AcpTurnParams) {
  const {
    cfg,
    deps,
    runtime,
    opts,
    sessionKey,
    sessionId,
    sessionAgentId,
    sessionEntry: sessionEntryIn,
    sessionStore,
    storePath,
    workspaceDir,
    body,
    runId,
    acpResolution,
    outboundSession,
  } = params;

  let sessionEntry = sessionEntryIn;
  const startedAt = Date.now();
  registerAgentRunContext(runId, { sessionKey });
  emitAgentEvent({
    runId,
    stream: "lifecycle",
    data: { phase: "start", startedAt },
  });

  const visibleTextAccumulator = createAcpVisibleTextAccumulator();
  let stopReason: string | undefined;
  const acpManager = getAcpSessionManager();

  try {
    const dispatchPolicyError = resolveAcpDispatchPolicyError(cfg);
    if (dispatchPolicyError) {
      throw dispatchPolicyError;
    }
    const acpAgent = normalizeAgentId(
      acpResolution.meta.agent || resolveAgentIdFromSessionKey(sessionKey),
    );
    const agentPolicyError = resolveAcpAgentPolicyError(cfg, acpAgent);
    if (agentPolicyError) {
      throw agentPolicyError;
    }

    await acpManager.runTurn({
      cfg,
      sessionKey,
      text: body,
      mode: "prompt",
      requestId: runId,
      signal: opts.abortSignal,
      onEvent: (event) => {
        if (event.type === "done") {
          stopReason = event.stopReason;
          return;
        }
        if (event.type !== "text_delta") {
          return;
        }
        if (event.stream && event.stream !== "output") {
          return;
        }
        if (!event.text) {
          return;
        }
        const visibleUpdate = visibleTextAccumulator.consume(event.text);
        if (!visibleUpdate) {
          return;
        }
        emitAgentEvent({
          runId,
          stream: "assistant",
          data: {
            text: visibleUpdate.text,
            delta: visibleUpdate.delta,
          },
        });
      },
    });
  } catch (error) {
    const acpError = toAcpRuntimeError({
      error,
      fallbackCode: "ACP_TURN_FAILED",
      fallbackMessage: "ACP turn failed before completion.",
    });
    emitAgentEvent({
      runId,
      stream: "lifecycle",
      data: {
        phase: "error",
        error: acpError.message,
        endedAt: Date.now(),
      },
    });
    throw acpError;
  }

  emitAgentEvent({
    runId,
    stream: "lifecycle",
    data: { phase: "end", endedAt: Date.now() },
  });

  const finalTextRaw = visibleTextAccumulator.finalizeRaw();
  const finalText = visibleTextAccumulator.finalize();
  try {
    sessionEntry = await persistAcpTurnTranscript({
      body,
      finalText: finalTextRaw,
      sessionId,
      sessionKey,
      sessionEntry,
      sessionStore,
      storePath,
      sessionAgentId,
      threadId: opts.threadId,
      sessionCwd: resolveAcpSessionCwd(acpResolution.meta) ?? workspaceDir,
    });
  } catch (error) {
    log.warn(
      `ACP transcript persistence failed for ${sessionKey}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }

  const normalizedFinalPayload = normalizeReplyPayload({ text: finalText });
  const payloads = normalizedFinalPayload ? [normalizedFinalPayload] : [];
  const result = {
    payloads,
    meta: {
      durationMs: Date.now() - startedAt,
      aborted: opts.abortSignal?.aborted === true,
      stopReason,
    },
  };

  return deliverAgentCommandResult({
    cfg,
    deps,
    runtime,
    opts,
    outboundSession,
    sessionEntry,
    result,
    payloads,
  });
}
