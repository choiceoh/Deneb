/**
 * dynamic-registry.ts — Runtime channel registration system
 *
 * Previously Deneb determined the channel list at build time (ids.ts, registry.ts, entrypoints.json).
 * Now channels register themselves at runtime; core only knows about registered channels.
 */

export interface ChannelRegistration {
  id: string;
  label: string;
  selectionLabel?: string;
  detailLabel?: string;
  docsPath?: string;
  docsLabel?: string;
  blurb?: string;
  systemImage?: string;
  selectionDocsPrefix?: string;
  selectionDocsOmitLabel?: boolean;
  selectionExtras?: string[];
  preferOver?: string[];
}

type RegistrationHook = (entry: ChannelRegistration) => void;

const registry = new Map<string, ChannelRegistration>();
const aliasMap = new Map<string, string>(); // alias → canonical id
const hooks: RegistrationHook[] = [];

/** Cached reference to the Telegram channel registration for O(1) access. */
let telegramChannelCache: ChannelRegistration | null = null;

/** Register a channel. Called at the top level of each channel extension's import. */
export function registerChannel(entry: ChannelRegistration): void {
  if (registry.has(entry.id)) {
    throw new Error(`Channel "${entry.id}" already registered`);
  }
  registry.set(entry.id, entry);
  if (entry.id === "telegram") {
    telegramChannelCache = entry;
  }
  for (const hook of hooks) {
    hook(entry);
  }
}

/** Register a channel alias. */
export function registerChannelAlias(alias: string, canonicalId: string): void {
  aliasMap.set(alias.trim().toLowerCase(), canonicalId);
}

/** List all registered channels. */
export function listChannels(): ChannelRegistration[] {
  return [...registry.values()];
}

/** Look up channel metadata by ID. */
export function getChannelMeta(id: string): ChannelRegistration | undefined {
  // Fast path for the single-channel (Telegram) setup.
  if (id === "telegram" && telegramChannelCache) {
    return telegramChannelCache;
  }
  return registry.get(id);
}

/** Direct accessor for the Telegram channel registration (O(1), no Map lookup). */
export function getTelegramChannelMeta(): ChannelRegistration | undefined {
  return telegramChannelCache ?? registry.get("telegram");
}

/** Normalize a raw channel ID — resolves aliases. */
export function normalizeChannelId(raw?: string | null): string | null {
  const key = raw?.trim().toLowerCase();
  if (!key) {
    return null;
  }

  // Single-channel fast path: most calls resolve "telegram" directly.
  if (key === "telegram" && telegramChannelCache) {
    return "telegram";
  }

  // Check aliases first
  const canonical = aliasMap.get(key);
  if (canonical && registry.has(canonical)) {
    return canonical;
  }

  // Direct lookup
  if (registry.has(key)) {
    return key;
  }
  return null;
}

/** Subscribe to new channel registrations. Returns an unsubscribe function. */
export function onChannelRegistered(hook: RegistrationHook): () => void {
  hooks.push(hook);
  return () => {
    const idx = hooks.indexOf(hook);
    if (idx >= 0) {
      hooks.splice(idx, 1);
    }
  };
}

// ── Backward-compatible shims ──

/** @deprecated Use listChannels() instead. */
export function listChatChannels() {
  return listChannels().map((entry) => ({
    id: entry.id,
    label: entry.label,
    docsPath: entry.docsPath ?? null,
    blurb: entry.blurb ?? null,
    systemImage: entry.systemImage ?? null,
  }));
}

/** @deprecated Use getChannelMeta() instead. */
export function getChatChannelMeta(id: string) {
  return getChannelMeta(id) ?? null;
}

/** @deprecated Use [...aliasMap.keys()] via listChannels() instead. */
export function listChatChannelAliases(): string[] {
  return [...aliasMap.keys()];
}

/** @deprecated Use getChatChannelOrder() from registry.ts instead. */
export function getChatChannelOrder(): string[] {
  return [...registry.keys()];
}
