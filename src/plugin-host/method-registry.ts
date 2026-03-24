// Method registry for the Plugin Host.
//
// Adapts existing TypeScript RPC method handlers from src/gateway/server-methods/
// to work in the Plugin Host context (Unix socket bridge, no direct HTTP/WS).

import type { RequestFrame, ResponseFrame } from "../gateway/protocol/index.js";

export type PluginHostHandler = (
  method: string,
  params: Record<string, unknown>,
  reqId: string,
) => Promise<{ ok: boolean; payload?: unknown; error?: { code: string; message: string } }>;

export type MethodRegistry = {
  handle: (req: RequestFrame) => Promise<ResponseFrame>;
  register: (method: string, handler: PluginHostHandler) => void;
  methods: () => string[];
};

export function createMethodRegistry(): MethodRegistry {
  const handlers = new Map<string, PluginHostHandler>();

  function register(method: string, handler: PluginHostHandler): void {
    handlers.set(method, handler);
  }

  async function handle(req: RequestFrame): Promise<ResponseFrame> {
    const handler = handlers.get(req.method);
    if (!handler) {
      return {
        type: "res",
        id: req.id,
        ok: false,
        error: {
          code: "NOT_FOUND",
          message: `plugin host: unknown method "${req.method}"`,
        },
      };
    }

    try {
      const params = (req.params ?? {}) as Record<string, unknown>;
      const result = await handler(req.method, params, req.id);
      if (result.ok) {
        return {
          type: "res",
          id: req.id,
          ok: true,
          payload: result.payload,
        };
      }
      return {
        type: "res",
        id: req.id,
        ok: false,
        error: result.error ?? { code: "UNAVAILABLE", message: "handler returned not ok" },
      };
    } catch (err) {
      return {
        type: "res",
        id: req.id,
        ok: false,
        error: {
          code: "UNAVAILABLE",
          message: `handler error: ${String(err)}`,
        },
      };
    }
  }

  function methods(): string[] {
    return Array.from(handlers.keys());
  }

  return { handle, register, methods };
}
