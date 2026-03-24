// End-to-end integration test for the Plugin Host.
//
// Simulates the Go gateway's perspective: starts the Plugin Host socket server,
// connects as a bridge client, and invokes methods through the full pipeline
// (socket → method registry → gateway context → TS handlers → response).

import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import readline from "node:readline";
import { describe, it, expect, afterEach, vi } from "vitest";
import type { RequestFrame, ResponseFrame } from "../gateway/protocol/index.js";
import { createMethodRegistry } from "./method-registry.js";
import { startSocketServer } from "./socket-server.js";
import type { SocketServerHandle } from "./socket-server.js";

// Mock heavy dependencies to keep tests fast.
vi.mock("../cron/service.js", () => ({
  CronService: class {
    stop() {}
  },
}));

vi.mock("../config/config.js", () => ({
  resolveStateDir: () => "/tmp/deneb-test-e2e",
  loadConfig: () => ({}),
}));

vi.mock("../agents/agent-scope.js", () => ({
  resolveDefaultAgentId: () => "default",
  resolveAgentWorkspaceDir: () => "/tmp/deneb-test-e2e/workspace",
}));

vi.mock("../gateway/server-plugins.js", () => ({
  loadGatewayPlugins: () => ({
    pluginRegistry: { gatewayHandlers: {} },
    gatewayMethods: [],
  }),
}));

vi.mock("../commands/health.js", () => ({
  getHealthSnapshot: async () => ({
    ok: true,
    ts: Date.now(),
    channels: {},
    channelOrder: [],
    channelLabels: {},
    heartbeatSeconds: 60,
    defaultAgentId: "default",
    agents: [],
    sessions: { path: "/tmp/sessions.json", count: 0, recent: [] },
    durationMs: 1,
  }),
}));

vi.mock("../commands/status.js", () => ({
  getStatusSummary: async () => ({
    gateway: { running: true },
    version: "test",
  }),
}));

vi.mock("../gateway/server-model-catalog.js", () => ({
  loadGatewayModelCatalog: async () => [],
}));

function tmpSocketPath(): string {
  return path.join(os.tmpdir(), `deneb-e2e-${process.pid}-${Date.now()}.sock`);
}

function sendRequest(socketPath: string, req: RequestFrame): Promise<ResponseFrame> {
  return new Promise((resolve, reject) => {
    const conn = net.connect(socketPath, () => {
      conn.write(JSON.stringify(req) + "\n");
    });
    const rl = readline.createInterface({ input: conn });
    rl.on("line", (line) => {
      try {
        resolve(JSON.parse(line) as ResponseFrame);
      } catch (err) {
        reject(err);
      }
      conn.end();
    });
    conn.on("error", reject);
    setTimeout(() => {
      conn.destroy();
      reject(new Error("timeout"));
    }, 10000);
  });
}

describe("Plugin Host E2E", () => {
  const sockets: string[] = [];
  const handles: SocketServerHandle[] = [];

  afterEach(() => {
    for (const h of handles) {
      h.server.close();
    }
    handles.length = 0;
    for (const p of sockets) {
      try {
        fs.unlinkSync(p);
      } catch {
        // Already cleaned up.
      }
    }
    sockets.length = 0;
  });

  async function startTestPluginHost() {
    const socketPath = tmpSocketPath();
    sockets.push(socketPath);

    const registry = createMethodRegistry();

    // Register Plugin Host built-in methods (same as main.ts).
    registry.register("plugin-host.health", async () => ({
      ok: true,
      payload: { status: "ok", runtime: "node", pid: process.pid },
    }));

    registry.register("plugin-host.methods", async () => ({
      ok: true,
      payload: { methods: registry.methods() },
    }));

    // Wire gateway context for forwarded methods.
    const { createHeadlessGatewayContext } = await import("./gateway-context.js");
    const ctx = await createHeadlessGatewayContext();

    // Register a forwarded method that goes through the gateway context.
    registry.register("health", async (_method, params, reqId) => {
      return ctx.invoke("health", params, reqId);
    });

    registry.register("status", async (_method, params, reqId) => {
      return ctx.invoke("status", params, reqId);
    });

    // Channel sync method.
    registry.register("plugin-host.channels.sync", async (_method, params) => {
      globalThis.__pluginHostChannelSnapshot = params;
      return { ok: true, payload: { synced: true } };
    });

    const handle = await startSocketServer({
      socketPath,
      handler: registry.handle,
      logger: { info: () => {}, error: () => {} },
    });
    handles.push(handle);

    return { socketPath, handle, ctx };
  }

  it("full pipeline: socket → health method → response", async () => {
    const { socketPath } = await startTestPluginHost();

    const resp = await sendRequest(socketPath, {
      type: "req",
      id: "e2e-1",
      method: "plugin-host.health",
      params: {},
    } as RequestFrame);

    expect(resp.ok).toBe(true);
    expect(resp.id).toBe("e2e-1");
    expect((resp as { payload: { runtime: string } }).payload.runtime).toBe("node");
  });

  it("full pipeline: socket → gateway health handler → response", async () => {
    const { socketPath } = await startTestPluginHost();

    const resp = await sendRequest(socketPath, {
      type: "req",
      id: "e2e-2",
      method: "health",
      params: {},
    } as RequestFrame);

    expect(resp.ok).toBe(true);
    expect(resp.id).toBe("e2e-2");
  });

  it("full pipeline: socket → gateway status handler → response", async () => {
    const { socketPath } = await startTestPluginHost();

    const resp = await sendRequest(socketPath, {
      type: "req",
      id: "e2e-3",
      method: "status",
      params: {},
    } as RequestFrame);

    expect(resp.ok).toBe(true);
    expect(resp.id).toBe("e2e-3");
  });

  it("channel state sync → snapshot available in context", async () => {
    const { socketPath, ctx } = await startTestPluginHost();

    // Simulate Go gateway syncing channel state.
    const syncResp = await sendRequest(socketPath, {
      type: "req",
      id: "e2e-4",
      method: "plugin-host.channels.sync",
      params: {
        channels: {
          telegram: { accountId: "default", connected: true, running: true },
        },
      },
    } as unknown as RequestFrame);

    expect(syncResp.ok).toBe(true);

    // Verify the snapshot is accessible in the gateway context.
    const snapshot = ctx.context.getRuntimeSnapshot();
    expect(snapshot).toBeDefined();
    expect(
      (snapshot as { channels: { telegram: { connected: boolean } } }).channels.telegram.connected,
    ).toBe(true);
  });

  it("event emission flows from server to connected client", async () => {
    const { socketPath, handle } = await startTestPluginHost();

    // Connect and listen for events.
    const eventPromise = new Promise<{ type: string; event: string; payload: unknown }>(
      (resolve, reject) => {
        const conn = net.connect(socketPath, () => {
          setTimeout(() => handle.emitEvent("session.updated", { key: "main" }), 50);
        });
        const rl = readline.createInterface({ input: conn });
        rl.on("line", (line) => {
          const frame = JSON.parse(line);
          if (frame.type === "event") {
            resolve(frame);
            conn.end();
          }
        });
        conn.on("error", reject);
        setTimeout(() => {
          conn.destroy();
          reject(new Error("timeout"));
        }, 5000);
      },
    );

    const event = await eventPromise;
    expect(event.event).toBe("session.updated");
    expect(event.payload).toEqual({ key: "main" });
  });

  it("method list includes all registered methods", async () => {
    const { socketPath } = await startTestPluginHost();

    const resp = await sendRequest(socketPath, {
      type: "req",
      id: "e2e-5",
      method: "plugin-host.methods",
      params: {},
    } as RequestFrame);

    expect(resp.ok).toBe(true);
    const methods = (resp as { payload: { methods: string[] } }).payload.methods;
    expect(methods).toContain("plugin-host.health");
    expect(methods).toContain("plugin-host.methods");
    expect(methods).toContain("health");
    expect(methods).toContain("status");
    expect(methods).toContain("plugin-host.channels.sync");
  });
});
