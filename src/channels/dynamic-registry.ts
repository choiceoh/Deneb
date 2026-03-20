/**
 * dynamic-registry.ts — Deneb: 런타임 채널 등록 시스템
 *
 * 기존 OpenClaw은 빌드 시점에 채널 목록을 결정(ids.ts, registry.ts, entrypoints.json).
 * 데네브는 채널이 런타임에 자신을 등록하는 구조로 변경.
 * 코어는 등록된 채널만 알고, 나머지는 모름.
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
}

type RegistrationHook = (entry: ChannelRegistration) => void;

const registry = new Map<string, ChannelRegistration>();
const aliasMap = new Map<string, string>(); // alias → canonical id
const hooks: RegistrationHook[] = [];

/**
 * 채널이 자신을 등록.
 * 각 채널 확장의 최상단(top-level import)에서 호출.
 */
export function registerChannel(entry: ChannelRegistration): void {
  if (registry.has(entry.id)) {
    throw new Error(`Channel "${entry.id}" already registered`);
  }
  registry.set(entry.id, entry);
  for (const hook of hooks) {
    hook(entry);
  }
}

/** 채널 별칭 등록 */
export function registerChannelAlias(alias: string, canonicalId: string): void {
  aliasMap.set(alias.trim().toLowerCase(), canonicalId);
}

/** 등록된 모든 채널 */
export function listChannels(): ChannelRegistration[] {
  return [...registry.values()];
}

/** 채널 메타 조회 */
export function getChannelMeta(id: string): ChannelRegistration | undefined {
  return registry.get(id);
}

/** ID 정규화 — alias 해결 포함 */
export function normalizeChannelId(raw?: string | null): string | null {
  const key = raw?.trim().toLowerCase();
  if (!key) {
    return null;
  }

  // 별칭 먼저 확인
  const canonical = aliasMap.get(key);
  if (canonical && registry.has(canonical)) {
    return canonical;
  }

  // 직접 확인
  if (registry.has(key)) {
    return key;
  }
  return null;
}

/** 새 채널 등록 시 알림 */
export function onChannelRegistered(hook: RegistrationHook): () => void {
  hooks.push(hook);
  return () => {
    const idx = hooks.indexOf(hook);
    if (idx >= 0) {
      hooks.splice(idx, 1);
    }
  };
}

// ── 하위 호환 shim ──

/** listChatChannels — 기존 API 호환 */
export function listChatChannels() {
  return listChannels().map((entry) => ({
    id: entry.id,
    label: entry.label,
    docsPath: entry.docsPath ?? null,
    blurb: entry.blurb ?? null,
    systemImage: entry.systemImage ?? null,
  }));
}

/** getChatChannelMeta — 기존 API 호환 */
export function getChatChannelMeta(id: string) {
  return getChannelMeta(id) ?? null;
}

/** listChatChannelAliases — 기존 API 호환 */
export function listChatChannelAliases(): string[] {
  return [...aliasMap.keys()];
}

/** CHAT_CHANNEL_ORDER — 기존 API 호환 (동적) */
export function getChatChannelOrder(): string[] {
  return [...registry.keys()];
}
