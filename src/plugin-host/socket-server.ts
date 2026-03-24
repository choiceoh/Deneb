// Unix domain socket NDJSON server for the Plugin Host.
//
// Implements the same frame protocol as gateway-go/internal/bridge/protocol.go:
// single-line JSON frames delimited by newlines (NDJSON).

import net from "node:net";
import readline from "node:readline";
import type { RequestFrame, ResponseFrame } from "../gateway/protocol/index.js";

export type FrameHandler = (req: RequestFrame) => Promise<ResponseFrame>;

export type SocketServerOptions = {
  socketPath: string;
  handler: FrameHandler;
  logger?: { info: (...args: unknown[]) => void; error: (...args: unknown[]) => void };
};

export function createSocketServer(opts: SocketServerOptions): net.Server {
  const { handler, logger = console } = opts;

  const server = net.createServer((conn) => {
    logger.info("[plugin-host] bridge client connected");

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
    });

    conn.on("error", (err) => {
      logger.error("[plugin-host] connection error:", err);
    });
  });

  return server;
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

export function startSocketServer(opts: SocketServerOptions): Promise<net.Server> {
  const server = createSocketServer(opts);

  return new Promise((resolve, reject) => {
    server.on("error", reject);
    server.listen(opts.socketPath, () => {
      resolve(server);
    });
  });
}
