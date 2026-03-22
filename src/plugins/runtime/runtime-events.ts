import { onAgentEvent } from "../../infra/agent-events.js";
import { onSessionTranscriptUpdate } from "../../sessions/transcript-events.js";
import type { PluginRuntime } from "./types.js";

// ── Shared cross-plugin event bus ───────────────────────────────────
// Allows plugins to communicate via typed pub/sub without direct imports.
// The bus is process-scoped so all plugins share the same instance.

type PluginEventHandler = (payload: unknown) => void;

const PLUGIN_EVENT_BUS_KEY: unique symbol = Symbol.for(
  "deneb.pluginEventBus",
) as unknown as typeof PLUGIN_EVENT_BUS_KEY;

type PluginEventBusState = {
  listeners: Map<string, Set<PluginEventHandler>>;
};

const pluginEventBus: PluginEventBusState = (() => {
  const g = globalThis as typeof globalThis & {
    [PLUGIN_EVENT_BUS_KEY]?: PluginEventBusState;
  };
  const existing = g[PLUGIN_EVENT_BUS_KEY];
  if (existing) {
    return existing;
  }
  const created: PluginEventBusState = { listeners: new Map() };
  g[PLUGIN_EVENT_BUS_KEY] = created;
  return created;
})();

export function createRuntimeEvents(): PluginRuntime["events"] {
  return {
    onAgentEvent,
    onSessionTranscriptUpdate,

    publish(event: string, payload: unknown): void {
      const handlers = pluginEventBus.listeners.get(event);
      if (!handlers) {
        return;
      }
      for (const handler of handlers) {
        try {
          handler(payload);
        } catch {
          // Swallow handler errors to prevent one plugin from breaking others.
        }
      }
    },

    subscribe(event: string, handler: PluginEventHandler): () => void {
      let handlers = pluginEventBus.listeners.get(event);
      if (!handlers) {
        handlers = new Set();
        pluginEventBus.listeners.set(event, handlers);
      }
      handlers.add(handler);
      return () => {
        handlers.delete(handler);
        if (handlers.size === 0) {
          pluginEventBus.listeners.delete(event);
        }
      };
    },
  };
}
