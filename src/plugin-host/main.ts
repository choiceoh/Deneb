// Plugin Host entry point.
//
// The Plugin Host runs as a child process of the Go gateway, hosting the
// TypeScript plugin SDK and all channel extensions. Communication with the
// Go gateway happens over a Unix domain socket using the NDJSON frame protocol.
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

// --- Gateway method forwarding ---
// Methods that the Go gateway forwards to us are handled here.
// In Phase 3, we register stub handlers that will be replaced with
// actual gateway method wiring as the migration progresses.
//
// The architecture:
// 1. Go gateway receives RPC request
// 2. If method is not handled natively in Go, it forwards to Plugin Host
// 3. Plugin Host routes to the appropriate TypeScript handler
// 4. Response flows back through the bridge

// Register forwarded methods that map to existing TypeScript handlers.
// These are loaded lazily to avoid pulling in the entire gateway stack at startup.
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

// Register placeholder handlers for all forwarded methods.
// These return a "not yet wired" response until the full gateway context is initialized.
// In a fully integrated setup, these would call into the actual TypeScript gateway method handlers.
for (const method of FORWARDED_METHODS) {
  const m = method;
  registry.register(m, async () => ({
    ok: false,
    error: {
      code: "UNAVAILABLE",
      message: `method "${m}" is registered but not yet wired to a handler`,
    },
  }));
}

// Start the socket server.
async function main(): Promise<void> {
  const server = await startSocketServer({
    socketPath,
    handler: registry.handle,
    logger: {
      info: (...args: unknown[]) => console.log(...args),
      error: (...args: unknown[]) => console.error(...args),
    },
  });

  console.log(`[plugin-host] listening on ${socketPath} (pid: ${process.pid})`);
  console.log(`[plugin-host] registered ${registry.methods().length} methods`);

  // Graceful shutdown.
  const shutdown = () => {
    console.log("[plugin-host] shutting down...");
    server.close(() => {
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

main().catch((err) => {
  console.error("[plugin-host] fatal:", err);
  process.exit(1);
});
