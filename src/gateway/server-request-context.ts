/**
 * Gateway request context assembly.
 *
 * Extracted from server.impl.ts to reduce god-function size.
 * Pure wiring — takes dependencies and assembles a GatewayRequestContext object.
 */
import type { createDefaultDeps } from "../cli/deps.js";
import type { CronService } from "../cron/service.js";
import type { ChannelId } from "../channels/plugins/index.js";
import type { createSubsystemLogger } from "../logging/subsystem.js";
import type { ExecApprovalManager } from "./exec-approval-manager.js";
import type { NodeRegistry } from "./node-registry.js";
import type { GatewayBroadcastFn, GatewayBroadcastToConnIdsFn } from "./server-broadcast.js";
import type { ChannelRuntimeSnapshot } from "./server-channels.js";
import type { DedupeEntry } from "./server-shared.js";
import type { loadGatewayModelCatalog as LoadGatewayModelCatalogFn } from "./server-model-catalog.js";
import type { GatewayRequestContext, GatewayClient } from "./server-methods/types.js";
import type { WizardSession } from "../wizard/session.js";
import type { ConnectParams } from "./protocol/index.js";
import type { ChatAbortControllerEntry } from "./chat-abort.js";

export type BuildGatewayRequestContextParams = {
  deps: ReturnType<typeof createDefaultDeps>;
  cron: CronService;
  cronStorePath: string;
  execApprovalManager: ExecApprovalManager;
  loadGatewayModelCatalog: typeof LoadGatewayModelCatalogFn;
  getHealthCache: () => import("../commands/health.js").HealthSummary | null;
  refreshHealthSnapshot: (opts?: { probe?: boolean }) => Promise<import("../commands/health.js").HealthSummary>;
  logHealth: { error: (msg: string) => void };
  logGateway: ReturnType<typeof createSubsystemLogger>;
  incrementPresenceVersion: () => number;
  getHealthVersion: () => number;
  broadcast: GatewayBroadcastFn;
  broadcastToConnIds: GatewayBroadcastToConnIdsFn;
  nodeSendToSession: (sessionKey: string, event: string, payload: unknown) => void;
  nodeSendToAllSubscribed: (event: string, payload: unknown) => void;
  nodeSubscribe: (nodeId: string, sessionKey: string) => void;
  nodeUnsubscribe: (nodeId: string, sessionKey: string) => void;
  nodeUnsubscribeAll: (nodeId: string) => void;
  hasConnectedMobileNode: () => boolean;
  clients: Set<GatewayClient>;
  nodeRegistry: NodeRegistry;
  agentRunSeq: Map<string, number>;
  chatAbortControllers: Map<string, ChatAbortControllerEntry>;
  chatRunState: {
    abortedRuns: Map<string, number>;
    buffers: Map<string, string>;
    deltaSentAt: Map<string, number>;
  };
  addChatRun: (sessionId: string, entry: { sessionKey: string; clientRunId: string }) => void;
  removeChatRun: (
    sessionId: string,
    clientRunId: string,
    sessionKey?: string,
  ) => { sessionKey: string; clientRunId: string } | undefined;
  sessionEventSubscribers: {
    subscribe: (connId: string) => void;
    unsubscribe: (connId: string) => void;
    getAll: () => Set<string>;
  };
  sessionMessageSubscribers: {
    subscribe: (connId: string, sessionKey: string) => void;
    unsubscribe: (connId: string, sessionKey: string) => void;
    unsubscribeAll: (connId: string) => void;
  };
  toolEventRecipients: { add: (runId: string, connId: string) => void };
  dedupe: Map<string, DedupeEntry>;
  wizardSessions: Map<string, WizardSession>;
  findRunningWizard: () => string | null;
  purgeWizardSession: (id: string) => void;
  getRuntimeSnapshot: () => ChannelRuntimeSnapshot;
  startChannel: (channel: ChannelId, accountId?: string) => Promise<void>;
  stopChannel: (channel: ChannelId, accountId?: string) => Promise<void>;
  markChannelLoggedOut: (channelId: ChannelId, cleared: boolean, accountId?: string) => void;
  wizardRunner: GatewayRequestContext["wizardRunner"];
  broadcastVoiceWakeChanged: (triggers: string[]) => void;
};

export function buildGatewayRequestContext(
  params: BuildGatewayRequestContextParams,
): GatewayRequestContext {
  const {
    deps,
    cron,
    cronStorePath,
    execApprovalManager,
    loadGatewayModelCatalog,
    getHealthCache,
    refreshHealthSnapshot,
    logHealth,
    logGateway,
    incrementPresenceVersion,
    getHealthVersion,
    broadcast,
    broadcastToConnIds,
    nodeSendToSession,
    nodeSendToAllSubscribed,
    nodeSubscribe,
    nodeUnsubscribe,
    nodeUnsubscribeAll,
    hasConnectedMobileNode,
    clients,
    nodeRegistry,
    agentRunSeq,
    chatAbortControllers,
    chatRunState,
    addChatRun,
    removeChatRun,
    sessionEventSubscribers,
    sessionMessageSubscribers,
    toolEventRecipients,
    dedupe,
    wizardSessions,
    findRunningWizard,
    purgeWizardSession,
    getRuntimeSnapshot,
    startChannel,
    stopChannel,
    markChannelLoggedOut,
    wizardRunner,
    broadcastVoiceWakeChanged,
  } = params;

  return {
    deps,
    cron,
    cronStorePath,
    execApprovalManager,
    loadGatewayModelCatalog,
    getHealthCache,
    refreshHealthSnapshot: refreshHealthSnapshot as GatewayRequestContext["refreshHealthSnapshot"],
    logHealth,
    logGateway,
    incrementPresenceVersion,
    getHealthVersion,
    broadcast,
    broadcastToConnIds,
    nodeSendToSession,
    nodeSendToAllSubscribed,
    nodeSubscribe,
    nodeUnsubscribe,
    nodeUnsubscribeAll,
    hasConnectedMobileNode,
    hasExecApprovalClients: () => {
      for (const gatewayClient of clients) {
        const scopes = Array.isArray(gatewayClient.connect.scopes)
          ? gatewayClient.connect.scopes
          : [];
        if (scopes.includes("operator.admin") || scopes.includes("operator.approvals")) {
          return true;
        }
      }
      return false;
    },
    nodeRegistry,
    agentRunSeq,
    chatAbortControllers,
    chatAbortedRuns: chatRunState.abortedRuns,
    chatRunBuffers: chatRunState.buffers,
    chatDeltaSentAt: chatRunState.deltaSentAt,
    addChatRun,
    removeChatRun,
    subscribeSessionEvents: sessionEventSubscribers.subscribe,
    unsubscribeSessionEvents: sessionEventSubscribers.unsubscribe,
    subscribeSessionMessageEvents: sessionMessageSubscribers.subscribe,
    unsubscribeSessionMessageEvents: sessionMessageSubscribers.unsubscribe,
    unsubscribeAllSessionEvents: (connId: string) => {
      sessionEventSubscribers.unsubscribe(connId);
      sessionMessageSubscribers.unsubscribeAll(connId);
    },
    getSessionEventSubscriberConnIds: sessionEventSubscribers.getAll,
    registerToolEventRecipient: toolEventRecipients.add,
    dedupe,
    wizardSessions,
    findRunningWizard,
    purgeWizardSession,
    getRuntimeSnapshot,
    startChannel,
    stopChannel,
    markChannelLoggedOut,
    wizardRunner,
    broadcastVoiceWakeChanged,
  };
}
