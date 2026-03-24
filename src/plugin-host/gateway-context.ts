// Headless gateway context factory for the Plugin Host.
//
// Creates a GatewayRequestContext without an HTTP/WS server, suitable for
// handling forwarded RPC requests from the Go gateway over the bridge.
//
// The Go gateway owns the transport (HTTP, WebSocket, auth). The Plugin Host
// only needs the request context to invoke TypeScript method handlers.

import { createDefaultDeps } from "../cli/deps.js";
import type { ConnectParams, RequestFrame, ErrorShape } from "../gateway/protocol/index.js";
import { handleGatewayRequest, coreGatewayHandlers } from "../gateway/server-methods.js";
import type {
  GatewayRequestContext,
  GatewayRequestHandlers,
  GatewayClient,
} from "../gateway/server-methods/types.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import type { PluginHostHandler } from "./method-registry.js";

const log = createSubsystemLogger("plugin-host");

// Minimal no-op implementations for dependencies that require the WS transport
// layer (broadcast, node registry, etc.). The Go gateway handles these natively;
// the Plugin Host only needs to execute method handler logic.
const NOOP = () => {};
const NOOP_ASYNC = async () => {};
const NOOP_NUMBER = () => 0;
const NOOP_BOOL = () => false;
const NOOP_NULL = () => null;

type BroadcastResult = { sent: number; errors: Error[] };

function noopBroadcast(): BroadcastResult {
  return { sent: 0, errors: [] };
}

/** Operator client stub used when no client context is provided by the Go gateway. */
function createOperatorClient(): GatewayClient {
  return {
    connect: {
      role: "operator",
      scopes: ["operator.admin"],
    } as ConnectParams,
    connId: "plugin-host",
  };
}

export type HeadlessGatewayContext = {
  context: GatewayRequestContext;
  /** Invoke a gateway method handler and return the result. */
  invoke: PluginHostHandler;
  /** Shut down the context and release resources. */
  shutdown: () => void;
};

/**
 * Create a headless gateway context for the Plugin Host.
 *
 * This initializes the minimum set of dependencies needed to run TypeScript
 * RPC method handlers without an HTTP/WS server. The Go gateway is responsible
 * for transport, auth, and client management.
 */
export async function createHeadlessGatewayContext(): Promise<HeadlessGatewayContext> {
  const deps = createDefaultDeps();

  // Lazy-load heavy dependencies to keep startup fast.
  const { CronService } = await import("../cron/service.js");
  const { resolveStateDir } = await import("../config/config.js");

  const stateDir = resolveStateDir(process.env);
  const cronStorePath = `${stateDir}/cron`;

  // CronService expects a specific Logger shape; adapt SubsystemLogger.
  const cronLogger = {
    debug: (obj: unknown, msg?: string) => log.debug(msg ?? String(obj)),
    info: (obj: unknown, msg?: string) => log.info(msg ?? String(obj)),
    warn: (obj: unknown, msg?: string) => log.warn(msg ?? String(obj)),
    error: (obj: unknown, msg?: string) => log.error(msg ?? String(obj)),
  };

  const cron = new CronService({
    storePath: cronStorePath,
    log: cronLogger,
    cronEnabled: false,
    enqueueSystemEvent: NOOP,
    requestHeartbeatNow: NOOP,
    runIsolatedAgentJob: async () => ({
      summary: undefined,
      outputText: undefined,
      delivered: false,
      deliveryAttempted: false,
      status: "ok" as const,
    }),
  });

  // Load plugins to get extra gateway handlers (e.g., from channel extensions).
  // This makes plugin-registered RPC methods available through the bridge.
  let extraHandlers: GatewayRequestHandlers = {};
  try {
    const { loadConfig } = await import("../config/config.js");
    const { resolveDefaultAgentId } = await import("../agents/agent-scope.js");
    const { resolveAgentWorkspaceDir } = await import("../agents/agent-scope.js");
    const { loadGatewayPlugins } = await import("../gateway/server-plugins.js");

    const cfg = loadConfig();
    const agentId = resolveDefaultAgentId(cfg);
    const workspaceDir = resolveAgentWorkspaceDir(cfg, agentId);
    const baseMethods = Object.keys(coreGatewayHandlers);

    const { pluginRegistry } = loadGatewayPlugins({
      cfg,
      workspaceDir,
      log: {
        info: (msg) => log.info(msg),
        warn: (msg) => log.warn(msg),
        error: (msg) => log.error(msg),
        debug: (msg) => log.debug(msg),
      },
      coreGatewayHandlers,
      baseMethods,
      logDiagnostics: true,
    });

    extraHandlers = pluginRegistry.gatewayHandlers;
    const pluginMethodCount = Object.keys(extraHandlers).length;
    if (pluginMethodCount > 0) {
      log.info(`loaded ${pluginMethodCount} plugin gateway handler(s)`);
    }
  } catch (err) {
    log.warn(`plugin loading skipped: ${String(err)}`);
  }

  // Shared mutable state for session/chat tracking.
  const agentRunSeq = new Map<string, number>();
  const chatAbortControllers = new Map<
    string,
    { controller: AbortController; sessionKey: string; clientRunId: string }
  >();
  const chatAbortedRuns = new Map<string, number>();
  const chatRunBuffers = new Map<string, string>();
  const chatDeltaSentAt = new Map<string, number>();
  const dedupe = new Map<string, { expiresAt: number }>();
  const wizardSessions = new Map<string, { id: string; status: string }>();

  // Session event subscriber tracking (connIds that want session lifecycle events).
  const sessionEventSubs = new Set<string>();
  const sessionMessageSubs = new Map<string, Set<string>>();

  // Health cache — lazily populated on first request.
  let healthCache: import("../commands/health.js").HealthSummary | null = null;

  const context: GatewayRequestContext = {
    deps,
    cron,
    cronStorePath,
    execApprovalManager: undefined,

    loadGatewayModelCatalog: async () => {
      const { loadGatewayModelCatalog: load } = await import("../gateway/server-model-catalog.js");
      return load();
    },

    getHealthCache: () => healthCache,
    refreshHealthSnapshot: async (opts) => {
      const { getHealthSnapshot } = await import("../commands/health.js");
      healthCache = await getHealthSnapshot({ probe: opts?.probe });
      return healthCache;
    },
    logHealth: { error: (msg: string) => log.error(msg) },
    logGateway: log,

    incrementPresenceVersion: (() => {
      let version = 0;
      return () => ++version;
    })(),
    getHealthVersion: NOOP_NUMBER,

    // Broadcast: relay events back to the Go gateway via the bridge socket.
    // The Go gateway then broadcasts to connected WS clients.
    broadcast: ((event: string, payload: unknown) => {
      const emitter = globalThis.__pluginHostEmitEvent;
      if (emitter) {
        emitter(event, payload);
        return { sent: 1, errors: [] };
      }
      return noopBroadcast();
    }) as unknown as GatewayRequestContext["broadcast"],
    broadcastToConnIds: (() =>
      noopBroadcast()) as unknown as GatewayRequestContext["broadcastToConnIds"],

    // Node management: no-op. Go gateway manages connected nodes.
    nodeSendToSession: NOOP,
    nodeSendToAllSubscribed: NOOP,
    nodeSubscribe: NOOP,
    nodeUnsubscribe: NOOP,
    nodeUnsubscribeAll: NOOP,
    hasConnectedMobileNode: NOOP_BOOL,
    hasExecApprovalClients: NOOP_BOOL,
    nodeRegistry: {
      get: NOOP_NULL,
      list: () => [],
      remove: NOOP,
      add: NOOP,
    } as unknown as GatewayRequestContext["nodeRegistry"],

    agentRunSeq,
    chatAbortControllers:
      chatAbortControllers as unknown as GatewayRequestContext["chatAbortControllers"],
    chatAbortedRuns,
    chatRunBuffers,
    chatDeltaSentAt,

    addChatRun: NOOP as unknown as GatewayRequestContext["addChatRun"],
    removeChatRun: (() => undefined) as unknown as GatewayRequestContext["removeChatRun"],

    subscribeSessionEvents: (connId) => sessionEventSubs.add(connId),
    unsubscribeSessionEvents: (connId) => sessionEventSubs.delete(connId),
    subscribeSessionMessageEvents: (connId, sessionKey) => {
      let set = sessionMessageSubs.get(connId);
      if (!set) {
        set = new Set();
        sessionMessageSubs.set(connId, set);
      }
      set.add(sessionKey);
    },
    unsubscribeSessionMessageEvents: (connId, sessionKey) => {
      sessionMessageSubs.get(connId)?.delete(sessionKey);
    },
    unsubscribeAllSessionEvents: (connId) => {
      sessionEventSubs.delete(connId);
      sessionMessageSubs.delete(connId);
    },
    getSessionEventSubscriberConnIds: () => sessionEventSubs as ReadonlySet<string>,
    registerToolEventRecipient:
      NOOP as unknown as GatewayRequestContext["registerToolEventRecipient"],

    dedupe: dedupe as unknown as GatewayRequestContext["dedupe"],
    wizardSessions: wizardSessions as unknown as GatewayRequestContext["wizardSessions"],
    findRunningWizard: NOOP_NULL as unknown as GatewayRequestContext["findRunningWizard"],
    purgeWizardSession: NOOP,

    // Channel runtime: returns the snapshot synced from the Go gateway via
    // the plugin-host.channels.sync bridge method. Falls back to empty if
    // the Go gateway hasn't synced yet.
    getRuntimeSnapshot: (() => {
      const snap = globalThis.__pluginHostChannelSnapshot;
      if (snap) {
        return snap;
      }
      return { channels: {}, channelAccounts: {} };
    }) as unknown as GatewayRequestContext["getRuntimeSnapshot"],
    startChannel: NOOP_ASYNC as unknown as GatewayRequestContext["startChannel"],
    stopChannel: NOOP_ASYNC as unknown as GatewayRequestContext["stopChannel"],
    markChannelLoggedOut: NOOP as unknown as GatewayRequestContext["markChannelLoggedOut"],

    wizardRunner: NOOP_ASYNC as unknown as GatewayRequestContext["wizardRunner"],
    broadcastVoiceWakeChanged: NOOP,
    autoMaintenance: undefined,
  };

  const operatorClient = createOperatorClient();

  const invoke: PluginHostHandler = async (method, params, reqId) => {
    return new Promise((resolve) => {
      const req: RequestFrame = {
        type: "req",
        id: reqId,
        method,
        params,
      } as RequestFrame;

      const respond = (ok: boolean, payload?: unknown, error?: ErrorShape) => {
        if (ok) {
          resolve({ ok: true, payload });
        } else {
          resolve({
            ok: false,
            error: error
              ? {
                  code: (error as { code?: string }).code ?? "UNAVAILABLE",
                  message: (error as { message?: string }).message ?? "unknown error",
                }
              : { code: "UNAVAILABLE", message: "handler returned error" },
          });
        }
      };

      handleGatewayRequest({
        req,
        client: operatorClient,
        isWebchatConnect: () => false,
        respond,
        context,
        extraHandlers,
      }).catch((err) => {
        resolve({
          ok: false,
          error: {
            code: "UNAVAILABLE",
            message: `handler threw: ${String(err)}`,
          },
        });
      });
    });
  };

  const shutdown = () => {
    cron.stop();
  };

  const totalHandlers = Object.keys(coreGatewayHandlers).length + Object.keys(extraHandlers).length;
  log.info(`headless gateway context created (${totalHandlers} handlers available)`);

  return { context, invoke, shutdown };
}
