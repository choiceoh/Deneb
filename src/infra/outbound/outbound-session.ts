import {
  buildTelegramGroupPeerId,
  parseTelegramTarget,
  parseTelegramThreadId,
  resolveTelegramTargetChatType,
} from "../../../extensions/telegram/api.js";
import type { MsgContext } from "../../auto-reply/templating.js";
import type { ChatType } from "../../channels/chat-type.js";
import { getChannelPlugin } from "../../channels/plugins/index.js";
import type { ChannelId } from "../../channels/plugins/types.js";
import type { DenebConfig } from "../../config/config.js";
import { recordSessionMetaFromInbound, resolveStorePath } from "../../config/sessions.js";
import { buildAgentSessionKey, type RoutePeer } from "../../routing/resolve-route.js";
import { resolveThreadSessionKeys } from "../../routing/session-key.js";
// WhatsApp stub (module removed)
function normalizeWhatsAppTarget(target: string): string {
  return target;
}
function isWhatsAppGroupJid(_jid: string): boolean {
  return false;
}

import type { ResolvedMessagingTarget } from "./target-resolver.js";

export type OutboundSessionRoute = {
  sessionKey: string;
  baseSessionKey: string;
  peer: RoutePeer;
  chatType: "direct" | "group" | "channel";
  from: string;
  to: string;
  threadId?: string | number;
};

export type ResolveOutboundSessionRouteParams = {
  cfg: DenebConfig;
  channel: ChannelId;
  agentId: string;
  accountId?: string | null;
  target: string;
  resolvedTarget?: ResolvedMessagingTarget;
  replyToId?: string | null;
  threadId?: string | number | null;
};

function normalizeThreadId(value?: string | number | null): string | undefined {
  if (value == null) {
    return undefined;
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      return undefined;
    }
    return String(Math.trunc(value));
  }
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function stripProviderPrefix(raw: string, channel: string): string {
  const trimmed = raw.trim();
  const lower = trimmed.toLowerCase();
  const prefix = `${channel.toLowerCase()}:`;
  if (lower.startsWith(prefix)) {
    return trimmed.slice(prefix.length).trim();
  }
  return trimmed;
}

function stripKindPrefix(raw: string): string {
  return raw.replace(/^(user|channel|group|conversation|room|dm):/i, "").trim();
}

function inferPeerKind(params: {
  channel: ChannelId;
  resolvedTarget?: ResolvedMessagingTarget;
}): ChatType {
  const resolvedKind = params.resolvedTarget?.kind;
  if (resolvedKind === "user") {
    return "direct";
  }
  if (resolvedKind === "channel") {
    return "channel";
  }
  if (resolvedKind === "group") {
    const plugin = getChannelPlugin(params.channel);
    const chatTypes = plugin?.capabilities?.chatTypes ?? [];
    const supportsChannel = chatTypes.includes("channel");
    const supportsGroup = chatTypes.includes("group");
    if (supportsChannel && !supportsGroup) {
      return "channel";
    }
    return "group";
  }
  return "direct";
}

function buildBaseSessionKey(params: {
  cfg: DenebConfig;
  agentId: string;
  channel: ChannelId;
  accountId?: string | null;
  peer: RoutePeer;
}): string {
  return buildAgentSessionKey({
    agentId: params.agentId,
    channel: params.channel,
    accountId: params.accountId,
    peer: params.peer,
    dmScope: params.cfg.session?.dmScope ?? "main",
    identityLinks: params.cfg.session?.identityLinks,
  });
}

function _resolveTelegramSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  const parsed = parseTelegramTarget(params.target);
  const chatId = parsed.chatId.trim();
  if (!chatId) {
    return null;
  }
  const parsedThreadId = parsed.messageThreadId;
  const fallbackThreadId = normalizeThreadId(params.threadId);
  const resolvedThreadId = parsedThreadId ?? parseTelegramThreadId(fallbackThreadId);
  // Telegram topics are encoded in the peer id (chatId:topic:<id>).
  const chatType = resolveTelegramTargetChatType(params.target);
  // If the target is a username and we lack a resolvedTarget, default to DM to avoid group keys.
  const isGroup =
    chatType === "group" ||
    (chatType === "unknown" &&
      params.resolvedTarget?.kind &&
      params.resolvedTarget.kind !== "user");
  // For groups: include thread ID in peerId. For DMs: use simple chatId (thread handled via suffix).
  const peerId =
    isGroup && resolvedThreadId ? buildTelegramGroupPeerId(chatId, resolvedThreadId) : chatId;
  const peer: RoutePeer = {
    kind: isGroup ? "group" : "direct",
    id: peerId,
  };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "telegram",
    accountId: params.accountId,
    peer,
  });
  // Use thread suffix for DM topics to match inbound session key format
  const threadKeys =
    resolvedThreadId && !isGroup
      ? { sessionKey: `${baseSessionKey}:thread:${resolvedThreadId}` }
      : null;
  return {
    sessionKey: threadKeys?.sessionKey ?? baseSessionKey,
    baseSessionKey,
    peer,
    chatType: isGroup ? "group" : "direct",
    from: isGroup
      ? `telegram:group:${peerId}`
      : resolvedThreadId
        ? `telegram:${chatId}:topic:${resolvedThreadId}`
        : `telegram:${chatId}`,
    to: `telegram:${chatId}`,
    threadId: resolvedThreadId,
  };
}

function resolveWhatsAppSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  const normalized = normalizeWhatsAppTarget(params.target);
  if (!normalized) {
    return null;
  }
  const isGroup = isWhatsAppGroupJid(normalized);
  const peer: RoutePeer = {
    kind: isGroup ? "group" : "direct",
    id: normalized,
  };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "whatsapp",
    accountId: params.accountId,
    peer,
  });
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType: isGroup ? "group" : "direct",
    from: normalized,
    to: normalized,
  };
}

function resolveMatrixSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  const stripped = stripProviderPrefix(params.target, "matrix");
  const isUser =
    params.resolvedTarget?.kind === "user" || stripped.startsWith("@") || /^user:/i.test(stripped);
  const rawId = stripKindPrefix(stripped);
  if (!rawId) {
    return null;
  }
  const peer: RoutePeer = { kind: isUser ? "direct" : "channel", id: rawId };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "matrix",
    accountId: params.accountId,
    peer,
  });
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType: isUser ? "direct" : "channel",
    from: isUser ? `matrix:${rawId}` : `matrix:channel:${rawId}`,
    to: `room:${rawId}`,
  };
}

function resolveMattermostSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  let trimmed = params.target.trim();
  if (!trimmed) {
    return null;
  }
  trimmed = trimmed.replace(/^mattermost:/i, "").trim();
  const lower = trimmed.toLowerCase();
  const resolvedKind = params.resolvedTarget?.kind;
  const isUser =
    resolvedKind === "user" ||
    (resolvedKind !== "channel" &&
      resolvedKind !== "group" &&
      (lower.startsWith("user:") || trimmed.startsWith("@")));
  if (trimmed.startsWith("@")) {
    trimmed = trimmed.slice(1).trim();
  }
  const rawId = stripKindPrefix(trimmed);
  if (!rawId) {
    return null;
  }
  const peer: RoutePeer = { kind: isUser ? "direct" : "channel", id: rawId };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "mattermost",
    accountId: params.accountId,
    peer,
  });
  const threadId = normalizeThreadId(params.replyToId ?? params.threadId);
  const threadKeys = resolveThreadSessionKeys({
    baseSessionKey,
    threadId,
  });
  return {
    sessionKey: threadKeys.sessionKey,
    baseSessionKey,
    peer,
    chatType: isUser ? "direct" : "channel",
    from: isUser ? `mattermost:${rawId}` : `mattermost:channel:${rawId}`,
    to: isUser ? `user:${rawId}` : `channel:${rawId}`,
    threadId,
  };
}

function resolveNextcloudTalkSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  let trimmed = params.target.trim();
  if (!trimmed) {
    return null;
  }
  trimmed = trimmed.replace(/^(nextcloud-talk|nc-talk|nc):/i, "").trim();
  trimmed = trimmed.replace(/^room:/i, "").trim();
  if (!trimmed) {
    return null;
  }
  const peer: RoutePeer = { kind: "group", id: trimmed };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "nextcloud-talk",
    accountId: params.accountId,
    peer,
  });
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType: "group",
    from: `nextcloud-talk:room:${trimmed}`,
    to: `nextcloud-talk:${trimmed}`,
  };
}

function resolveNostrSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  const trimmed = stripProviderPrefix(params.target, "nostr").trim();
  if (!trimmed) {
    return null;
  }
  const peer: RoutePeer = { kind: "direct", id: trimmed };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "nostr",
    accountId: params.accountId,
    peer,
  });
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType: "direct",
    from: `nostr:${trimmed}`,
    to: `nostr:${trimmed}`,
  };
}

function normalizeTlonShip(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) {
    return trimmed;
  }
  return trimmed.startsWith("~") ? trimmed : `~${trimmed}`;
}

function resolveTlonSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  let trimmed = stripProviderPrefix(params.target, "tlon");
  trimmed = trimmed.trim();
  if (!trimmed) {
    return null;
  }
  const lower = trimmed.toLowerCase();
  let isGroup =
    lower.startsWith("group:") || lower.startsWith("room:") || lower.startsWith("chat/");
  let peerId = trimmed;
  if (lower.startsWith("group:") || lower.startsWith("room:")) {
    peerId = trimmed.replace(/^(group|room):/i, "").trim();
    if (!peerId.startsWith("chat/")) {
      const parts = peerId.split("/").filter(Boolean);
      if (parts.length === 2) {
        peerId = `chat/${normalizeTlonShip(parts[0])}/${parts[1]}`;
      }
    }
    isGroup = true;
  } else if (lower.startsWith("dm:")) {
    peerId = normalizeTlonShip(trimmed.slice("dm:".length));
    isGroup = false;
  } else if (lower.startsWith("chat/")) {
    peerId = trimmed;
    isGroup = true;
  } else if (trimmed.includes("/")) {
    const parts = trimmed.split("/").filter(Boolean);
    if (parts.length === 2) {
      peerId = `chat/${normalizeTlonShip(parts[0])}/${parts[1]}`;
      isGroup = true;
    }
  } else {
    peerId = normalizeTlonShip(trimmed);
  }

  const peer: RoutePeer = { kind: isGroup ? "group" : "direct", id: peerId };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "tlon",
    accountId: params.accountId,
    peer,
  });
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType: isGroup ? "group" : "direct",
    from: isGroup ? `tlon:group:${peerId}` : `tlon:${peerId}`,
    to: `tlon:${peerId}`,
  };
}

/**
 * Feishu ID formats:
 * - oc_xxx: chat_id (can be group or DM, use chat_mode to distinguish or explicit dm:/group: prefix)
 * - ou_xxx: user open_id (DM)
 * - on_xxx: user union_id (DM)
 * - cli_xxx: app_id (not a valid send target)
 */
function resolveFeishuSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  let trimmed = stripProviderPrefix(params.target, "feishu");
  trimmed = stripProviderPrefix(trimmed, "lark").trim();
  if (!trimmed) {
    return null;
  }

  const lower = trimmed.toLowerCase();
  let isGroup = false;
  let typeExplicit = false;

  if (lower.startsWith("group:") || lower.startsWith("chat:")) {
    trimmed = trimmed.replace(/^(group|chat):/i, "").trim();
    isGroup = true;
    typeExplicit = true;
  } else if (lower.startsWith("user:") || lower.startsWith("dm:")) {
    trimmed = trimmed.replace(/^(user|dm):/i, "").trim();
    isGroup = false;
    typeExplicit = true;
  }

  const idLower = trimmed.toLowerCase();
  // Only infer type from ID prefix if not explicitly specified
  // Note: oc_ is a chat_id and can be either group or DM (must check chat_mode from API)
  // Only ou_/on_ can be reliably identified as user IDs (always DM)
  if (!typeExplicit) {
    if (idLower.startsWith("ou_") || idLower.startsWith("on_")) {
      isGroup = false;
    }
    // oc_ requires explicit prefix: dm:oc_xxx or group:oc_xxx
  }

  const peer: RoutePeer = {
    kind: isGroup ? "group" : "direct",
    id: trimmed,
  };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: "feishu",
    accountId: params.accountId,
    peer,
  });
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType: isGroup ? "group" : "direct",
    from: isGroup ? `feishu:group:${trimmed}` : `feishu:${trimmed}`,
    to: trimmed,
  };
}

function resolveFallbackSession(
  params: ResolveOutboundSessionRouteParams,
): OutboundSessionRoute | null {
  const trimmed = stripProviderPrefix(params.target, params.channel).trim();
  if (!trimmed) {
    return null;
  }
  const peerKind = inferPeerKind({
    channel: params.channel,
    resolvedTarget: params.resolvedTarget,
  });
  const peerId = stripKindPrefix(trimmed);
  if (!peerId) {
    return null;
  }
  const peer: RoutePeer = { kind: peerKind, id: peerId };
  const baseSessionKey = buildBaseSessionKey({
    cfg: params.cfg,
    agentId: params.agentId,
    channel: params.channel,
    peer,
  });
  const chatType = peerKind === "direct" ? "direct" : peerKind === "channel" ? "channel" : "group";
  const from =
    peerKind === "direct"
      ? `${params.channel}:${peerId}`
      : `${params.channel}:${peerKind}:${peerId}`;
  const toPrefix = peerKind === "direct" ? "user" : "channel";
  return {
    sessionKey: baseSessionKey,
    baseSessionKey,
    peer,
    chatType,
    from,
    to: `${toPrefix}:${peerId}`,
  };
}

type OutboundSessionResolver = (
  params: ResolveOutboundSessionRouteParams,
) => OutboundSessionRoute | null | Promise<OutboundSessionRoute | null>;

const OUTBOUND_SESSION_RESOLVERS: Partial<Record<ChannelId, OutboundSessionResolver>> = {
  whatsapp: resolveWhatsAppSession,
  matrix: resolveMatrixSession,
  mattermost: resolveMattermostSession,
  "nextcloud-talk": resolveNextcloudTalkSession,
  nostr: resolveNostrSession,
  tlon: resolveTlonSession,
  feishu: resolveFeishuSession,
};

export async function resolveOutboundSessionRoute(
  params: ResolveOutboundSessionRouteParams,
): Promise<OutboundSessionRoute | null> {
  const target = params.target.trim();
  if (!target) {
    return null;
  }
  const nextParams = { ...params, target };
  const resolver = OUTBOUND_SESSION_RESOLVERS[params.channel];
  if (!resolver) {
    return resolveFallbackSession(nextParams);
  }
  return await resolver(nextParams);
}

export async function ensureOutboundSessionEntry(params: {
  cfg: DenebConfig;
  agentId: string;
  channel: ChannelId;
  accountId?: string | null;
  route: OutboundSessionRoute;
}): Promise<void> {
  const storePath = resolveStorePath(params.cfg.session?.store, {
    agentId: params.agentId,
  });
  const ctx: MsgContext = {
    From: params.route.from,
    To: params.route.to,
    SessionKey: params.route.sessionKey,
    AccountId: params.accountId ?? undefined,
    ChatType: params.route.chatType,
    Provider: params.channel,
    Surface: params.channel,
    MessageThreadId: params.route.threadId,
    OriginatingChannel: params.channel,
    OriginatingTo: params.route.to,
  };
  try {
    await recordSessionMetaFromInbound({
      storePath,
      sessionKey: params.route.sessionKey,
      ctx,
    });
  } catch {
    // Do not block outbound sends on session meta writes.
  }
}
