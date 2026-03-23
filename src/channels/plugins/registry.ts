import {
  getActivePluginRegistryVersion,
  requireActivePluginRegistry,
} from "../../plugins/runtime.js";
import { CHAT_CHANNEL_ORDER, type ChatChannelId, normalizeAnyChannelId } from "../registry.js";
import type { ChannelId, ChannelPlugin } from "./types.js";

function dedupeChannels(channels: ChannelPlugin[]): ChannelPlugin[] {
  const seen = new Set<string>();
  const resolved: ChannelPlugin[] = [];
  for (const plugin of channels) {
    const id = String(plugin.id).trim();
    if (!id || seen.has(id)) {
      continue;
    }
    seen.add(id);
    resolved.push(plugin);
  }
  return resolved;
}

type CachedChannelPlugins = {
  registryVersion: number;
  sorted: ChannelPlugin[];
  byId: Map<string, ChannelPlugin>;
  /** Fast-path: cached reference when exactly one channel is registered. */
  singletonPlugin: ChannelPlugin | null;
};

const EMPTY_CHANNEL_PLUGIN_CACHE: CachedChannelPlugins = {
  registryVersion: -1,
  sorted: [],
  byId: new Map(),
  singletonPlugin: null,
};

let cachedChannelPlugins = EMPTY_CHANNEL_PLUGIN_CACHE;

function resolveCachedChannelPlugins(): CachedChannelPlugins {
  const registry = requireActivePluginRegistry();
  const registryVersion = getActivePluginRegistryVersion();
  const cached = cachedChannelPlugins;
  if (cached.registryVersion === registryVersion) {
    return cached;
  }

  const deduped = dedupeChannels(registry.channels.map((entry) => entry.plugin));

  // Optimization: skip the full sort when only one channel is registered
  // (common case — Telegram is the only bundled channel).
  const sorted =
    deduped.length <= 1
      ? deduped
      : deduped.toSorted((a, b) => {
          const indexA = CHAT_CHANNEL_ORDER.indexOf(a.id as ChatChannelId);
          const indexB = CHAT_CHANNEL_ORDER.indexOf(b.id as ChatChannelId);
          const orderA = a.meta.order ?? (indexA === -1 ? 999 : indexA);
          const orderB = b.meta.order ?? (indexB === -1 ? 999 : indexB);
          if (orderA !== orderB) {
            return orderA - orderB;
          }
          return a.id.localeCompare(b.id);
        });
  const byId = new Map<string, ChannelPlugin>();
  for (const plugin of sorted) {
    byId.set(plugin.id, plugin);
  }

  const next: CachedChannelPlugins = {
    registryVersion,
    sorted,
    byId,
    // Single-channel fast path: cache the lone plugin for O(1) access.
    singletonPlugin: sorted.length === 1 ? sorted[0] : null,
  };
  cachedChannelPlugins = next;
  return next;
}

export function listChannelPlugins(): ChannelPlugin[] {
  const cache = resolveCachedChannelPlugins();
  // Single-channel fast path: return a fresh single-element array directly,
  // avoiding the generic .slice() copy.
  if (cache.singletonPlugin) {
    return [cache.singletonPlugin];
  }
  return cache.sorted.slice();
}

export function getChannelPlugin(id: ChannelId): ChannelPlugin | undefined {
  const resolvedId = String(id).trim();
  if (!resolvedId) {
    return undefined;
  }
  const cache = resolveCachedChannelPlugins();
  // Single-channel fast path: direct identity check avoids Map hash lookup.
  if (cache.singletonPlugin && cache.singletonPlugin.id === resolvedId) {
    return cache.singletonPlugin;
  }
  return cache.byId.get(resolvedId);
}

export function normalizeChannelId(raw?: string | null): ChannelId | null {
  return normalizeAnyChannelId(raw);
}
