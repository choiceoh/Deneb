import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import readline from "node:readline";
import { describe, it, expect, afterEach } from "vitest";
import type { RequestFrame, ResponseFrame } from "../gateway/protocol/index.js";
import { createMethodRegistry } from "./method-registry.js";
import { startSocketServer } from "./socket-server.js";

function tmpSocketPath(): string {
  return path.join(os.tmpdir(), `deneb-test-plugin-host-${process.pid}-${Date.now()}.sock`);
}

describe("createMethodRegistry", () => {
  it("returns NOT_FOUND for unknown methods", async () => {
    const registry = createMethodRegistry();
    const resp = await registry.handle({
      type: "req",
      id: "1",
      method: "unknown.method",
      params: {},
    } as RequestFrame);
    expect(resp.ok).toBe(false);
    expect(resp.error?.code).toBe("NOT_FOUND");
  });

  it("invokes registered handler", async () => {
    const registry = createMethodRegistry();
    registry.register("test.echo", async (_method, params) => ({
      ok: true,
      payload: { echo: params },
    }));

    const resp = await registry.handle({
      type: "req",
      id: "2",
      method: "test.echo",
      params: { hello: "world" },
    } as unknown as RequestFrame);
    expect(resp.ok).toBe(true);
    expect((resp as { payload: { echo: { hello: string } } }).payload.echo.hello).toBe("world");
  });

  it("catches handler errors", async () => {
    const registry = createMethodRegistry();
    registry.register("test.fail", async () => {
      throw new Error("boom");
    });

    const resp = await registry.handle({
      type: "req",
      id: "3",
      method: "test.fail",
      params: {},
    } as RequestFrame);
    expect(resp.ok).toBe(false);
    expect(resp.error?.code).toBe("UNAVAILABLE");
    expect(resp.error?.message).toContain("boom");
  });

  it("lists registered methods", () => {
    const registry = createMethodRegistry();
    registry.register("a.b", async () => ({ ok: true }));
    registry.register("c.d", async () => ({ ok: true }));
    expect(registry.methods()).toEqual(["a.b", "c.d"]);
  });
});

describe("startSocketServer", () => {
  const sockets: string[] = [];
  const servers: net.Server[] = [];

  afterEach(() => {
    for (const s of servers) {
      s.close();
    }
    servers.length = 0;
    for (const p of sockets) {
      try {
        fs.unlinkSync(p);
      } catch {
        // Already cleaned up.
      }
    }
    sockets.length = 0;
  });

  it("accepts connections and routes requests", async () => {
    const socketPath = tmpSocketPath();
    sockets.push(socketPath);

    const registry = createMethodRegistry();
    registry.register("plugin-host.health", async () => ({
      ok: true,
      payload: { status: "ok" },
    }));

    const handle = await startSocketServer({
      socketPath,
      handler: registry.handle,
      logger: { info: () => {}, error: () => {} },
    });
    servers.push(handle.server);

    // Connect as a client (simulating Go bridge).
    const response = await sendRequest(socketPath, {
      type: "req",
      id: "test-1",
      method: "plugin-host.health",
      params: {},
    } as RequestFrame);

    expect(response.ok).toBe(true);
    expect(response.id).toBe("test-1");
  });

  it("returns error for unknown methods", async () => {
    const socketPath = tmpSocketPath();
    sockets.push(socketPath);

    const registry = createMethodRegistry();
    const handle = await startSocketServer({
      socketPath,
      handler: registry.handle,
      logger: { info: () => {}, error: () => {} },
    });
    servers.push(handle.server);

    const response = await sendRequest(socketPath, {
      type: "req",
      id: "test-2",
      method: "nonexistent",
      params: {},
    } as RequestFrame);

    expect(response.ok).toBe(false);
    expect(response.error?.code).toBe("NOT_FOUND");
  });

  it("emits event frames to connected clients", async () => {
    const socketPath = tmpSocketPath();
    sockets.push(socketPath);

    const registry = createMethodRegistry();
    const handle = await startSocketServer({
      socketPath,
      handler: registry.handle,
      logger: { info: () => {}, error: () => {} },
    });
    servers.push(handle.server);

    // Connect a client and listen for event frames.
    const eventPromise = new Promise<{ type: string; event: string; payload: unknown }>(
      (resolve, reject) => {
        const conn = net.connect(socketPath, () => {
          // Trigger event emission after connection is established.
          setTimeout(() => handle.emitEvent("test.event", { hello: "world" }), 50);
        });

        const rl = readline.createInterface({ input: conn });
        rl.on("line", (line) => {
          try {
            const frame = JSON.parse(line);
            if (frame.type === "event") {
              resolve(frame);
              conn.end();
            }
          } catch (err) {
            reject(err);
          }
        });

        conn.on("error", reject);
        setTimeout(() => {
          conn.destroy();
          reject(new Error("timeout waiting for event"));
        }, 5000);
      },
    );

    const event = await eventPromise;
    expect(event.type).toBe("event");
    expect(event.event).toBe("test.event");
    expect(event.payload).toEqual({ hello: "world" });
  });
});

// Helper: send a request over Unix socket and read the response.
function sendRequest(socketPath: string, req: RequestFrame): Promise<ResponseFrame> {
  return new Promise((resolve, reject) => {
    const conn = net.connect(socketPath, () => {
      conn.write(JSON.stringify(req) + "\n");
    });

    const rl = readline.createInterface({ input: conn });
    rl.on("line", (line) => {
      try {
        const resp = JSON.parse(line) as ResponseFrame;
        resolve(resp);
      } catch (err) {
        reject(err);
      }
      conn.end();
    });

    conn.on("error", reject);
    setTimeout(() => {
      conn.destroy();
      reject(new Error("timeout waiting for response"));
    }, 5000);
  });
}
