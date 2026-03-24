// Unix domain socket NDJSON server for the Plugin Host.
//
// Implements the same frame protocol as gateway-go/internal/bridge/protocol.go:
// single-line JSON frames delimited by newlines (NDJSON).
//
// Supports three frame types:
// - "req" (inbound): requests from Go gateway → Plugin Host handler
// - "res" (outbound): responses from Plugin Host → Go gateway
// - "event" (outbound): async events from Plugin Host → Go gateway broadcaster

import net from "node:net";
import readline from "node:readline";
import type { RequestFrame, ResponseFrame } from "../gateway/protocol/index.js";

/** Event frame sent from Plugin Host to Go gateway for broadcasting to WS clients. */
export type EventFrame = {
  type: "event";
  event: string;
  payload?: unknown;
  seq?: number;
};

export type FrameHandler = (req: RequestFrame) => Promise<ResponseFrame>;

export type SocketServerOptions = {
  socketPath: string;
  handler: FrameHandler;
  logger?: { info: (...args: unknown[]) => void; error: (...args: unknown[]) => void };
};

/** Handle to the running socket server with event emission capabilities. */
export type SocketServerHandle = {
  server: net.Server;
  /** Emit an event frame to the Go gateway for broadcasting to WS clients. */
  emitEvent: (event: string, payload?: unknown) => void;
  /** Number of currently connected bridge clients. */
  clientCount: () => number;
};

export function createSocketServer(opts: SocketServerOptions): SocketServerHandle {
  const { handler, logger = console } = opts;
  const clients = new Set<net.Socket>();
  let eventSeq = 0;

  const server = net.createServer((conn) => {
    logger.info("[plugin-host] bridge client connected");
    clients.add(conn);

    const rl = readline.createInterface({ input: conn, crlfDelay: Infinity });

    rl.on("line", (line) => {
      if (!line.trim()) {
        return;
      }

      let req: RequestFrame;
      try {
        req = JSON.parse(line) as RequestFrame;
      } catch {
        logger.error("[plugin-host] invalid JSON frame:", line.slice(0, 200));
        return;
      }

      // Only handle request frames.
      if (req.type !== "req") {
        return;
      }

      handler(req)
        .then((resp) => {
          writeFrame(conn, resp);
        })
        .catch((err) => {
          const errorResp: ResponseFrame = {
            type: "res",
            id: req.id,
            ok: false,
            error: {
              code: "UNAVAILABLE",
              message: `plugin host error: ${String(err)}`,
            },
          };
          writeFrame(conn, errorResp);
        });
    });

    rl.on("close", () => {
      logger.info("[plugin-host] bridge client disconnected");
      clients.delete(conn);
    });

    conn.on("error", (err) => {
      logger.error("[plugin-host] connection error:", err);
      clients.delete(conn);
    });
  });

  const emitEvent = (event: string, payload?: unknown) => {
    const frame: EventFrame = {
      type: "event",
      event,
      payload,
      seq: ++eventSeq,
    };
    const data = JSON.stringify(frame) + "\n";
    for (const conn of clients) {
      if (!conn.destroyed) {
        try {
          conn.write(data);
        } catch {
          // Connection may have closed.
        }
      }
    }
  };

  return {
    server,
    emitEvent,
    clientCount: () => clients.size,
  };
}

function writeFrame(conn: net.Socket, frame: ResponseFrame): void {
  if (conn.destroyed) {
    return;
  }
  try {
    conn.write(JSON.stringify(frame) + "\n");
  } catch {
    // Connection may have closed.
  }
}

export function startSocketServer(opts: SocketServerOptions): Promise<SocketServerHandle> {
  const handle = createSocketServer(opts);

  return new Promise((resolve, reject) => {
    handle.server.on("error", reject);
    handle.server.listen(opts.socketPath, () => {
      resolve(handle);
    });
  });
}
