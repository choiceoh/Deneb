/**
 * Gateway event subscriptions: agent, heartbeat, transcript, and lifecycle events.
 *
 * Extracted from server.impl.ts to reduce god-function size.
 */
import { clearAgentRunContext, onAgentEvent } from "../infra/agent-events.js";
import { onHeartbeatEvent } from "../infra/heartbeat-events.js";
import { onSessionLifecycleEvent } from "../sessions/session-lifecycle-events.js";
import { onSessionTranscriptUpdate } from "../sessions/transcript-events.js";
import { createAgentEventHandler } from "./server-chat.js";
import { resolveSessionKeyForRun } from "./server-session-key.js";
import { resolveSessionKeyForTranscriptFile } from "./session-transcript-key.js";
import {
  attachOpenClawTranscriptMeta,
  loadGatewaySessionRow,
  loadSessionEntry,
  readSessionMessages,
} from "./session-utils.js";

type Broadcast = (event: string, payload: unknown, opts?: { dropIfSlow?: boolean }) => void;
type BroadcastToConnIds = (
  event: string,
  payload: unknown,
  connIds: Set<string>,
  opts?: { dropIfSlow?: boolean },
) => void;

export type GatewayEventSubscriptionParams = {
  minimalTestGateway: boolean;
  broadcast: Broadcast;
  broadcastToConnIds: BroadcastToConnIds;
  nodeSendToSession: (sessionKey: string, event: string, payload: unknown) => void;
  agentRunSeq: Map<string, number>;
  chatRunState: { abortedRuns: Map<string, unknown>; buffers: Map<string, string>; deltaSentAt: Map<string, number> };
  toolEventRecipients: { add: (runId: string, connId: string) => void };
  sessionEventSubscribers: {
    getAll: () => Set<string>;
    subscribe: (connId: string) => void;
    unsubscribe: (connId: string) => void;
  };
  sessionMessageSubscribers: {
    get: (sessionKey: string) => Set<string>;
    subscribe: (connId: string, sessionKey: string) => void;
    unsubscribe: (connId: string, sessionKey: string) => void;
    unsubscribeAll: (connId: string) => void;
  };
};

export type GatewayEventSubscriptions = {
  agentUnsub: (() => void) | null;
  heartbeatUnsub: (() => void) | null;
  transcriptUnsub: (() => void) | null;
  lifecycleUnsub: (() => void) | null;
};

export function createGatewayEventSubscriptions(
  params: GatewayEventSubscriptionParams,
): GatewayEventSubscriptions {
  const {
    minimalTestGateway,
    broadcast,
    broadcastToConnIds,
    nodeSendToSession,
    agentRunSeq,
    chatRunState,
    toolEventRecipients,
    sessionEventSubscribers,
    sessionMessageSubscribers,
  } = params;

  const agentUnsub = minimalTestGateway
    ? null
    : onAgentEvent(
        createAgentEventHandler({
          broadcast,
          broadcastToConnIds,
          nodeSendToSession,
          agentRunSeq,
          chatRunState,
          resolveSessionKeyForRun,
          clearAgentRunContext,
          toolEventRecipients,
          sessionEventSubscribers,
        }),
      );

  const heartbeatUnsub = minimalTestGateway
    ? null
    : onHeartbeatEvent((evt) => {
        broadcast("heartbeat", evt, { dropIfSlow: true });
      });

  const transcriptUnsub = minimalTestGateway
    ? null
    : onSessionTranscriptUpdate((update) => {
        const sessionKey =
          update.sessionKey ?? resolveSessionKeyForTranscriptFile(update.sessionFile);
        if (!sessionKey || update.message === undefined) {
          return;
        }
        const connIds = new Set<string>();
        for (const connId of sessionEventSubscribers.getAll()) {
          connIds.add(connId);
        }
        for (const connId of sessionMessageSubscribers.get(sessionKey)) {
          connIds.add(connId);
        }
        if (connIds.size === 0) {
          return;
        }
        const { entry, storePath } = loadSessionEntry(sessionKey);
        const messageSeq = entry?.sessionId
          ? readSessionMessages(entry.sessionId, storePath, entry.sessionFile).length
          : undefined;
        const sessionRow = loadGatewaySessionRow(sessionKey);
        const sessionSnapshot = sessionRow
          ? {
              session: sessionRow,
              updatedAt: sessionRow.updatedAt ?? undefined,
              sessionId: sessionRow.sessionId,
              kind: sessionRow.kind,
              channel: sessionRow.channel,
              label: sessionRow.label,
              displayName: sessionRow.displayName,
              deliveryContext: sessionRow.deliveryContext,
              parentSessionKey: sessionRow.parentSessionKey,
              childSessions: sessionRow.childSessions,
              thinkingLevel: sessionRow.thinkingLevel,
              systemSent: sessionRow.systemSent,
              abortedLastRun: sessionRow.abortedLastRun,
              lastChannel: sessionRow.lastChannel,
              lastTo: sessionRow.lastTo,
              lastAccountId: sessionRow.lastAccountId,
              totalTokens: sessionRow.totalTokens,
              totalTokensFresh: sessionRow.totalTokensFresh,
              contextTokens: sessionRow.contextTokens,
              estimatedCostUsd: sessionRow.estimatedCostUsd,
              modelProvider: sessionRow.modelProvider,
              model: sessionRow.model,
              status: sessionRow.status,
              startedAt: sessionRow.startedAt,
              endedAt: sessionRow.endedAt,
              runtimeMs: sessionRow.runtimeMs,
            }
          : {};
        const message = attachOpenClawTranscriptMeta(update.message, {
          ...(typeof update.messageId === "string" ? { id: update.messageId } : {}),
          ...(typeof messageSeq === "number" ? { seq: messageSeq } : {}),
        });
        broadcastToConnIds(
          "session.message",
          {
            sessionKey,
            message,
            ...(typeof update.messageId === "string" ? { messageId: update.messageId } : {}),
            ...(typeof messageSeq === "number" ? { messageSeq } : {}),
            ...sessionSnapshot,
          },
          connIds,
          { dropIfSlow: true },
        );

        const sessionEventConnIds = sessionEventSubscribers.getAll();
        if (sessionEventConnIds.size > 0) {
          broadcastToConnIds(
            "sessions.changed",
            {
              sessionKey,
              phase: "message",
              ts: Date.now(),
              ...(typeof update.messageId === "string" ? { messageId: update.messageId } : {}),
              ...(typeof messageSeq === "number" ? { messageSeq } : {}),
              ...sessionSnapshot,
            },
            sessionEventConnIds,
            { dropIfSlow: true },
          );
        }
      });

  const lifecycleUnsub = minimalTestGateway
    ? null
    : onSessionLifecycleEvent((event) => {
        const connIds = sessionEventSubscribers.getAll();
        if (connIds.size === 0) {
          return;
        }
        const sessionRow = loadGatewaySessionRow(event.sessionKey);
        broadcastToConnIds(
          "sessions.changed",
          {
            sessionKey: event.sessionKey,
            reason: event.reason,
            parentSessionKey: event.parentSessionKey,
            label: event.label,
            displayName: event.displayName,
            ts: Date.now(),
            ...(sessionRow
              ? {
                  updatedAt: sessionRow.updatedAt ?? undefined,
                  sessionId: sessionRow.sessionId,
                  kind: sessionRow.kind,
                  channel: sessionRow.channel,
                  label: event.label ?? sessionRow.label,
                  displayName: event.displayName ?? sessionRow.displayName,
                  deliveryContext: sessionRow.deliveryContext,
                  parentSessionKey: event.parentSessionKey ?? sessionRow.parentSessionKey,
                  childSessions: sessionRow.childSessions,
                  thinkingLevel: sessionRow.thinkingLevel,
                  systemSent: sessionRow.systemSent,
                  abortedLastRun: sessionRow.abortedLastRun,
                  lastChannel: sessionRow.lastChannel,
                  lastTo: sessionRow.lastTo,
                  lastAccountId: sessionRow.lastAccountId,
                  totalTokens: sessionRow.totalTokens,
                  totalTokensFresh: sessionRow.totalTokensFresh,
                  contextTokens: sessionRow.contextTokens,
                  estimatedCostUsd: sessionRow.estimatedCostUsd,
                  modelProvider: sessionRow.modelProvider,
                  model: sessionRow.model,
                  status: sessionRow.status,
                  startedAt: sessionRow.startedAt,
                  endedAt: sessionRow.endedAt,
                  runtimeMs: sessionRow.runtimeMs,
                }
              : {}),
          },
          connIds,
          { dropIfSlow: true },
        );
      });

  return { agentUnsub, heartbeatUnsub, transcriptUnsub, lifecycleUnsub };
}
