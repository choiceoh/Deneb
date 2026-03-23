import { resolveDefaultAgentId } from "../../agents/agent-scope.js";
import type { DenebConfig } from "../../config/config.js";
import {
  canonicalizeMainSessionAlias,
  resolveAgentMainSessionKey,
  resolveMainSessionKey,
  type SessionEntry,
} from "../../config/sessions.js";
import {
  normalizeAgentId,
  normalizeMainKey,
  parseAgentSessionKey,
} from "../../routing/session-key.js";

export function classifySessionKey(
  key: string,
  entry?: SessionEntry,
): "global" | "unknown" | "group" | "direct" {
  if (key === "global") {
    return "global";
  }
  if (key === "unknown") {
    return "unknown";
  }
  if (entry?.chatType === "group" || entry?.chatType === "channel") {
    return "group";
  }
  if (key.includes(":group:") || key.includes(":channel:")) {
    return "group";
  }
  return "direct";
}

export function parseGroupKey(
  key: string,
): { channel?: string; kind?: "group" | "channel"; id?: string } | null {
  const agentParsed = parseAgentSessionKey(key);
  const rawKey = agentParsed?.rest ?? key;
  const parts = rawKey.split(":").filter(Boolean);
  if (parts.length >= 3) {
    const [channel, kind, ...rest] = parts;
    if (kind === "group" || kind === "channel") {
      const id = rest.join(":");
      return { channel, kind, id };
    }
  }
  return null;
}

export function canonicalizeSessionKeyForAgent(agentId: string, key: string): string {
  const lowered = key.toLowerCase();
  if (lowered === "global" || lowered === "unknown") {
    return lowered;
  }
  if (lowered.startsWith("agent:")) {
    return lowered;
  }
  return `agent:${normalizeAgentId(agentId)}:${lowered}`;
}

export function resolveDefaultStoreAgentId(cfg: DenebConfig): string {
  return normalizeAgentId(resolveDefaultAgentId(cfg));
}

export function resolveSessionStoreKey(params: { cfg: DenebConfig; sessionKey: string }): string {
  const raw = (params.sessionKey ?? "").trim();
  if (!raw) {
    return raw;
  }
  const rawLower = raw.toLowerCase();
  if (rawLower === "global" || rawLower === "unknown") {
    return rawLower;
  }

  const parsed = parseAgentSessionKey(raw);
  if (parsed) {
    const agentId = normalizeAgentId(parsed.agentId);
    const lowered = raw.toLowerCase();
    const canonical = canonicalizeMainSessionAlias({
      cfg: params.cfg,
      agentId,
      sessionKey: lowered,
    });
    if (canonical !== lowered) {
      return canonical;
    }
    return lowered;
  }

  const lowered = raw.toLowerCase();
  const rawMainKey = normalizeMainKey(params.cfg.session?.mainKey);
  if (lowered === "main" || lowered === rawMainKey) {
    return resolveMainSessionKey(params.cfg);
  }
  const agentId = resolveDefaultStoreAgentId(params.cfg);
  return canonicalizeSessionKeyForAgent(agentId, lowered);
}

export function resolveSessionStoreAgentId(cfg: DenebConfig, canonicalKey: string): string {
  if (canonicalKey === "global" || canonicalKey === "unknown") {
    return resolveDefaultStoreAgentId(cfg);
  }
  const parsed = parseAgentSessionKey(canonicalKey);
  if (parsed?.agentId) {
    return normalizeAgentId(parsed.agentId);
  }
  return resolveDefaultStoreAgentId(cfg);
}

export function canonicalizeSpawnedByForAgent(
  cfg: DenebConfig,
  agentId: string,
  spawnedBy?: string,
): string | undefined {
  const raw = spawnedBy?.trim();
  if (!raw) {
    return undefined;
  }
  const lower = raw.toLowerCase();
  if (lower === "global" || lower === "unknown") {
    return lower;
  }
  let result: string;
  if (raw.toLowerCase().startsWith("agent:")) {
    result = raw.toLowerCase();
  } else {
    result = `agent:${normalizeAgentId(agentId)}:${lower}`;
  }
  // Resolve main-alias references (e.g. agent:ops:main → configured main key).
  const parsed = parseAgentSessionKey(result);
  const resolvedAgent = parsed?.agentId ? normalizeAgentId(parsed.agentId) : agentId;
  return canonicalizeMainSessionAlias({ cfg, agentId: resolvedAgent, sessionKey: result });
}

export function buildGatewaySessionStoreScanTargets(params: {
  cfg: DenebConfig;
  key: string;
  canonicalKey: string;
  agentId: string;
}): string[] {
  const targets = new Set<string>();
  if (params.canonicalKey) {
    targets.add(params.canonicalKey);
  }
  if (params.key && params.key !== params.canonicalKey) {
    targets.add(params.key);
  }
  if (params.canonicalKey === "global" || params.canonicalKey === "unknown") {
    return [...targets];
  }
  const agentMainKey = resolveAgentMainSessionKey({ cfg: params.cfg, agentId: params.agentId });
  if (params.canonicalKey === agentMainKey) {
    targets.add(`agent:${params.agentId}:main`);
  }
  return [...targets];
}
