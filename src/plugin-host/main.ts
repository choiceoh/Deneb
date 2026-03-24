// Plugin Host entry point.
//
// The Plugin Host runs as a child process of the Go gateway, hosting the
// TypeScript plugin SDK and all channel extensions. Communication with the
// Go gateway happens over a Unix domain socket using the NDJSON frame protocol.
//
// Phase 3 architecture:
// 1. Go gateway receives HTTP/WS RPC request
// 2. If the method is not handled natively in Go, it forwards to Plugin Host
// 3. Plugin Host routes to the actual TypeScript gateway method handler
// 4. Response flows back through the bridge to the Go gateway
//
// Environment:
//   DENEB_PLUGIN_HOST_SOCKET — Unix socket path to listen on (required)

import fs from "node:fs";
import { createMethodRegistry } from "./method-registry.js";
import { startSocketServer } from "./socket-server.js";

const socketPathEnv = process.env.DENEB_PLUGIN_HOST_SOCKET;

if (!socketPathEnv) {
  console.error("[plugin-host] DENEB_PLUGIN_HOST_SOCKET environment variable is required");
  process.exit(1);
}

const socketPath: string = socketPathEnv;

// Remove stale socket file.
try {
  fs.unlinkSync(socketPath);
} catch {
  // Expected if no stale file exists.
}

const registry = createMethodRegistry();

// Register a health method so the Go gateway can verify the Plugin Host is alive.
registry.register("plugin-host.health", async () => ({
  ok: true,
  payload: {
    status: "ok",
    runtime: "node",
    pid: process.pid,
    uptime: process.uptime(),
  },
}));

// Register a method list method for introspection.
registry.register("plugin-host.methods", async () => ({
  ok: true,
  payload: { methods: registry.methods() },
}));

// Register a reload method so the Go gateway can reinitialize the gateway context
// (e.g., after config changes). This tears down the old context and creates a new one.
registry.register("plugin-host.reload", async () => {
  console.log("[plugin-host] reloading gateway context...");
  gatewayShutdown?.();
  gatewayInvoke = null;
  gatewayShutdown = null;
  gatewayInitPromise = null;
  try {
    await ensureGatewayContext();
    return { ok: true, payload: { reloaded: true } };
  } catch (err) {
    return {
      ok: false,
      error: { code: "UNAVAILABLE", message: `reload failed: ${String(err)}` },
    };
  }
});

// Register a channel state sync method. The Go gateway calls this to push
// channel account snapshots so TypeScript handlers (e.g., channels.status)
// return accurate runtime state from the Go-managed channel lifecycle.
registry.register("plugin-host.channels.sync", async (_method, params) => {
  // Store the snapshot on globalThis so the headless gateway context
  // can return it from getRuntimeSnapshot().
  globalThis.__pluginHostChannelSnapshot = params;
  return { ok: true, payload: { synced: true } };
});

// --- Gateway method forwarding ---
// Methods that the Go gateway forwards to us are routed to the actual
// TypeScript gateway method handlers via a headless gateway context.
//
// The headless context initializes the minimum dependencies needed to run
// TypeScript handlers without an HTTP/WS server. The Go gateway owns the
// transport, auth, and client management.

// Methods forwarded from the Go gateway to the TypeScript handler layer.
// These cover config, sessions, chat, channels, agents, skills, and system methods.
const FORWARDED_METHODS = [
  "config.get",
  "config.set",
  "config.apply",
  "config.patch",
  "config.schema",
  "config.schema.lookup",
  "models.list",
  "agents.list",
  "agents.create",
  "agents.update",
  "agents.delete",
  "agents.files.list",
  "agents.files.get",
  "agents.files.set",
  "skills.status",
  "skills.bins",
  "skills.install",
  "skills.update",
  "tools.catalog",
  "channels.status",
  "channels.logout",
  "sessions.list",
  "sessions.subscribe",
  "sessions.unsubscribe",
  "sessions.messages.subscribe",
  "sessions.messages.unsubscribe",
  "sessions.preview",
  "sessions.create",
  "sessions.send",
  "sessions.abort",
  "sessions.patch",
  "sessions.reset",
  "sessions.delete",
  "sessions.compact",
  "wizard.start",
  "wizard.next",
  "wizard.cancel",
  "wizard.status",
  "tts.status",
  "tts.providers",
  "tts.enable",
  "tts.disable",
  "tts.convert",
  "tts.setProvider",
  "usage.status",
  "usage.cost",
  "talk.config",
  "talk.mode",
  "update.run",
  "voicewake.get",
  "voicewake.set",
  "secrets.reload",
  "secrets.resolve",
  "logs.tail",
  "doctor.memory.status",
  "status",
  "health",
  "last-heartbeat",
  "set-heartbeats",
  "wake",
  "node.pair.request",
  "node.pair.list",
  "node.pair.approve",
  "node.pair.reject",
  "node.pair.verify",
  "device.pair.list",
  "device.pair.approve",
  "device.pair.reject",
  "device.pair.remove",
  "device.token.rotate",
  "device.token.revoke",
  "node.rename",
  "node.list",
  "node.describe",
  "node.pending.drain",
  "node.pending.enqueue",
  "node.invoke",
  "node.pending.pull",
  "node.pending.ack",
  "node.invoke.result",
  "node.event",
  "node.canvas.capability.refresh",
  "exec.approvals.get",
  "exec.approvals.set",
  "exec.approvals.node.get",
  "exec.approvals.node.set",
  "exec.approval.request",
  "exec.approval.waitDecision",
  "exec.approval.resolve",
  "cron.list",
  "cron.status",
  "cron.add",
  "cron.update",
  "cron.remove",
  "cron.run",
  "cron.runs",
  "gateway.identity.get",
  "system-presence",
  "system-event",
  "send",
  "agent",
  "agent.identity.get",
  "agent.wait",
  "browser.request",
  "chat.history",
  "chat.abort",
  "chat.send",
];

// Gateway context is initialized lazily on first forwarded request.
// This avoids pulling in the full gateway stack during startup.
let gatewayInvoke:
  | ((
      method: string,
      params: Record<string, unknown>,
      reqId: string,
    ) => Promise<{ ok: boolean; payload?: unknown; error?: { code: string; message: string } }>)
  | null = null;
let gatewayShutdown: (() => void) | null = null;
let gatewayInitPromise: Promise<void> | null = null;

async function ensureGatewayContext(): Promise<typeof gatewayInvoke> {
  if (gatewayInvoke) {
    return gatewayInvoke;
  }
  if (!gatewayInitPromise) {
    gatewayInitPromise = (async () => {
      try {
        const { createHeadlessGatewayContext } = await import("./gateway-context.js");
        const ctx = await createHeadlessGatewayContext();
        gatewayInvoke = ctx.invoke;
        gatewayShutdown = ctx.shutdown;
        console.log("[plugin-host] gateway context initialized");
      } catch (err) {
        console.error("[plugin-host] failed to initialize gateway context:", err);
        // Fall through — methods will return UNAVAILABLE on next call.
      }
    })();
  }
  await gatewayInitPromise;
  return gatewayInvoke;
}

// Register forwarded methods that route to the TypeScript gateway handlers.
for (const method of FORWARDED_METHODS) {
  const m = method;
  registry.register(m, async (_method, params, reqId) => {
    const invoke = await ensureGatewayContext();
    if (!invoke) {
      return {
        ok: false,
        error: {
          code: "UNAVAILABLE",
          message: `gateway context not initialized; method "${m}" unavailable`,
        },
      };
    }
    return invoke(m, params, reqId);
  });
}

// Start the socket server.
async function main(): Promise<void> {
  const handle = await startSocketServer({
    socketPath,
    handler: registry.handle,
    logger: {
      info: (...args: unknown[]) => console.log(...args),
      error: (...args: unknown[]) => console.error(...args),
    },
  });

  console.log(`[plugin-host] listening on ${socketPath} (pid: ${process.pid})`);
  console.log(`[plugin-host] registered ${registry.methods().length} methods`);

  // Export the event emitter so the gateway context can broadcast events
  // back to the Go gateway (which then relays them to WS clients).
  globalThis.__pluginHostEmitEvent = handle.emitEvent;

  // Pre-warm the gateway context in the background so the first RPC
  // doesn't pay the full initialization cost.
  ensureGatewayContext().catch(() => {
    // Logged inside ensureGatewayContext; swallow here.
  });

  // Graceful shutdown.
  const shutdown = () => {
    console.log("[plugin-host] shutting down...");
    gatewayShutdown?.();
    handle.server.close(() => {
      // Clean up socket file.
      try {
        fs.unlinkSync(socketPath);
      } catch {
        // Ignore cleanup errors.
      }
      process.exit(0);
    });

    // Force exit after 5 seconds.
    setTimeout(() => process.exit(1), 5000).unref();
  };

  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

// Extend globalThis type for the event emitter bridge and channel state sync.
declare global {
  // eslint-disable-next-line no-var
  var __pluginHostEmitEvent: ((event: string, payload?: unknown) => void) | undefined;
  // eslint-disable-next-line no-var
  var __pluginHostChannelSnapshot: Record<string, unknown> | undefined;
}

main().catch((err) => {
  console.error("[plugin-host] fatal:", err);
  process.exit(1);
});
