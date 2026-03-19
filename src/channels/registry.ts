/**
 * Deneb: channels/registry.ts — 동적 레지스트리 위임
 * 
 * 기존: CHAT_CHANNEL_META 정적 객체, CHAT_CHANNEL_ORDER 고정 배열
 * 변경: dynamic-registry에서 런타임에 조회
 */

import {
  registerChannel,
  registerChannelAlias,
  listChannels,
  getChannelMeta,
  normalizeChannelId,
  listChatChannels,
  getChatChannelMeta,
  listChatChannelAliases,
  onChannelRegistered,
} from "./dynamic-registry.js";

// Bootstrap: 채널 등록 (import side-effect)
// 데네브는 channel-bootstrap만 등록. 업스트림 호환이 필요하면 추가.
import "./channel-bootstrap.js";

// Bootstrap 시 CHANNEL_IDS를 populate
import { CHANNEL_IDS } from "./ids.js";
{
  const ids = getChatChannelOrder();
  CHANNEL_IDS.push(...ids);
}

// ── Re-export (기존 API 호환) ──
export {
  listChatChannels,
  getChatChannelMeta,
  normalizeChannelId,
  listChatChannelAliases,
  onChannelRegistered,
};

// ── Types (기존 API 호환) ──
import type { ChannelRegistration } from "./dynamic-registry.js";
export type ChatChannelMeta = ChannelRegistration;

// ── Aliases ──
export const CHAT_CHANNEL_ALIASES: Record<string, string> = {
  // 데네브에서는 별칭 없음. 필요하면 channel-bootstrap에서 registerChannelAlias() 사용.
};

// ── Helpers (기존 API 호환) ──
const WEBSITE_URL = "https://deneb.dev";

export function listRegisteredChannelPluginEntries(): { plugin: { id?: string; meta?: { aliases?: string[] } } }[] {
  return listChannels().map(ch => ({
    plugin: {
      id: ch.id,
      meta: { aliases: [] },
    },
  }));
}

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