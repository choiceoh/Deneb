import type { SessionSendPolicyConfig } from "../config/types.base.js";
import { parseAgentSessionKey } from "../sessions/session-key-utils.js";

type ParsedSessionScope = {
  channel?: string;
  chatType?: "channel" | "group" | "direct";
  normalizedKey?: string;
};

export function isScopeAllowed(
  scope: SessionSendPolicyConfig | undefined,
  sessionKey?: string,
): boolean {
  if (!scope) {
    return true;
  }
  const parsed = parseSessionScope(sessionKey);
  const channel = parsed.channel;
  const chatType = parsed.chatType;
  const normalizedKey = parsed.normalizedKey ?? "";
  const rawKey = sessionKey?.trim().toLowerCase() ?? "";
  for (const rule of scope.rules ?? []) {
    if (!rule) {
      continue;
    }
    const match = rule.match ?? {};
    if (match.channel && match.channel !== channel) {
      continue;
    }
    if (match.chatType && match.chatType !== chatType) {
      continue;
    }
    const normalizedPrefix = match.keyPrefix?.trim().toLowerCase() || undefined;
    const rawPrefix = match.rawKeyPrefix?.trim().toLowerCase() || undefined;

    if (rawPrefix && !rawKey.startsWith(rawPrefix)) {
      continue;
    }
    if (normalizedPrefix) {
      // Backward compat: older configs used `keyPrefix: "agent:<id>:..."` to match raw keys.
      const isLegacyRaw = normalizedPrefix.startsWith("agent:");
      if (isLegacyRaw) {
        if (!rawKey.startsWith(normalizedPrefix)) {
          continue;
        }
      } else if (!normalizedKey.startsWith(normalizedPrefix)) {
        continue;
      }
    }
    return rule.action === "allow";
  }
  const fallback = scope.default ?? "allow";
  return fallback === "allow";
}

export function deriveScopeChannel(key?: string): string | undefined {
  return parseSessionScope(key).channel;
}

export function deriveScopeChatType(key?: string): "channel" | "group" | "direct" | undefined {
  return parseSessionScope(key).chatType;
}

function parseSessionScope(key?: string): ParsedSessionScope {
  const normalized = normalizeSessionKey(key);
  if (!normalized) {
    return {};
  }
  const parts = normalized.split(":").filter(Boolean);
  let chatType: ParsedSessionScope["chatType"];
  if (
    parts.length >= 2 &&
    (parts[1] === "group" || parts[1] === "channel" || parts[1] === "direct" || parts[1] === "dm")
  ) {
    if (parts.includes("group")) {
      chatType = "group";
    } else if (parts.includes("channel")) {
      chatType = "channel";
    }
    return {
      normalizedKey: normalized,
      channel: parts[0]?.toLowerCase(),
      chatType: chatType ?? "direct",
    };
  }
  if (normalized.includes(":group:")) {
    return { normalizedKey: normalized, chatType: "group" };
  }
  if (normalized.includes(":channel:")) {
    return { normalizedKey: normalized, chatType: "channel" };
  }
  return { normalizedKey: normalized, chatType: "direct" };
}

function normalizeSessionKey(key?: string): string | undefined {
  if (!key) {
    return undefined;
  }
  const trimmed = key.trim();
  if (!trimmed) {
    return undefined;
  }
  const parsed = parseAgentSessionKey(trimmed);
  const normalized = (parsed?.rest ?? trimmed).toLowerCase();
  if (normalized.startsWith("subagent:")) {
    return undefined;
  }
  return normalized;
}
