/**
 * channels/registry.ts — Dynamic registry delegation
 *
 * Previously: CHAT_CHANNEL_META static object, CHAT_CHANNEL_ORDER fixed array.
 * Now: looked up at runtime from dynamic-registry.
 */

import {
  normalizeChannelId,
  listChatChannels,
  getChatChannelMeta,
  listChatChannelAliases,
  onChannelRegistered,
  getChatChannelOrder,
} from "./dynamic-registry.js";
// Bootstrap: register channels (import side-effect)
import "./channel-bootstrap.js";
// Populate CHANNEL_IDS at bootstrap time
import { CHANNEL_IDS } from "./ids.js";
{
  const ids = getChatChannelOrder();
  CHANNEL_IDS.push(...ids);
}

// ── Re-exports (existing API surface) ──
export {
  listChatChannels,
  getChatChannelMeta,
  normalizeChannelId,
  listChatChannelAliases,
  onChannelRegistered,
};

// ── Types ──
import type { ChannelRegistration } from "./dynamic-registry.js";
export type ChatChannelMeta = ChannelRegistration;

// ── Helpers ──

export function normalizeAnyChannelId(raw?: string | null): string | null {
  return normalizeChannelId(raw);
}

export function formatChannelPrimerLine(meta: ChatChannelMeta): string {
  return `${meta.label}: ${meta.blurb}`;
}

export function formatChannelSelectionLine(
  meta: ChatChannelMeta,
  docsLink: (path: string, label?: string) => string,
): string {
  const docsPrefix = meta.selectionDocsPrefix ?? "Docs:";
  const docsLabel = meta.docsLabel ?? meta.id;
  const docs = meta.selectionDocsOmitLabel
    ? docsLink(meta.docsPath ?? "")
    : docsLink(meta.docsPath ?? "", docsLabel);
  const extras = (meta.selectionExtras ?? []).filter(Boolean).join(" ");
  return `${meta.label} — ${meta.blurb} ${docsPrefix ? `${docsPrefix} ` : ""}${docs}${extras ? ` ${extras}` : ""}`;
}

export { getChatChannelOrder } from "./dynamic-registry.js";

// ── Legacy aliases ──
export type { ChatChannelId } from "./ids.js";
export { CHANNEL_IDS } from "./ids.js";

/** @deprecated Use normalizeChannelId instead. */
export const normalizeChatChannelId = normalizeChannelId;

/** @deprecated Use getChatChannelOrder() instead. */
export const CHAT_CHANNEL_ORDER = getChatChannelOrder();
