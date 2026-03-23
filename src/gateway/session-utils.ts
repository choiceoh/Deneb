import fs from "node:fs";
import path from "node:path";
import { resolveAgentWorkspaceDir, resolveDefaultAgentId } from "../agents/agent-scope.js";
import { resolveContextTokensForModel } from "../agents/context.js";
import { DEFAULT_MODEL } from "../agents/defaults.js";
import {
  getSubagentRunByChildSessionKey,
  getSubagentSessionStartedAt,
  listSubagentRunsForController,
  resolveSubagentSessionStatus,
} from "../agents/subagent-registry.js";
import type { DenebConfig } from "../config/config.js";
import { resolveStateDir } from "../config/paths.js";
import {
  buildGroupDisplayName,
  resolveFreshSessionTotalTokens,
  type SessionEntry,
  type SessionScope,
} from "../config/sessions.js";
import { openBoundaryFileSync } from "../infra/boundary-file-read.js";
import {
  normalizeAgentId,
  normalizeMainKey,
  parseAgentSessionKey,
} from "../routing/session-key.js";
import { isCronRunSessionKey } from "../sessions/session-key-utils.js";
import {
  AVATAR_MAX_BYTES,
  isAvatarDataUrl,
  isAvatarHttpUrl,
  isPathWithinRoot,
  isWorkspaceRelativeAvatarPath,
  resolveAvatarMime,
} from "../shared/avatar-policy.js";
import { normalizeSessionDeliveryFields } from "../utils/delivery-context.js";
import { readSessionTitleFieldsFromTranscript } from "./session-utils.fs.js";

// Re-export from sub-modules for backward compatibility.
export {
  classifySessionKey,
  parseGroupKey,
  resolveSessionStoreKey,
  canonicalizeSpawnedByForAgent,
} from "./session-utils.keys.js";

export {
  findStoreKeysIgnoreCase,
  pruneLegacyStoreKeys,
  migrateAndPruneGatewaySessionStoreKey,
  resolveGatewaySessionStoreTarget,
  loadCombinedSessionStoreForGateway,
  loadSessionEntry,
} from "./session-utils.store.js";

export {
  getSessionDefaults,
  resolveSessionModelRef,
  resolveSessionModelIdentityRef,
} from "./session-utils.model.js";

export {
  archiveFileOnDisk,
  archiveSessionTranscripts,
  attachDenebTranscriptMeta,
  capArrayByJsonBytes,
  readFirstUserMessageFromTranscript,
  readLastMessagePreviewFromTranscript,
  readLatestSessionUsageFromTranscript,
  readSessionTitleFieldsFromTranscript,
  readSessionPreviewItemsFromTranscript,
  readSessionMessages,
  resolveSessionTranscriptCandidates,
} from "./session-utils.fs.js";
export type {
  GatewayAgentRow,
  GatewaySessionRow,
  GatewaySessionsDefaults,
  SessionsListResult,
  SessionsPatchResult,
  SessionsPreviewEntry,
  SessionsPreviewResult,
} from "./session-utils.types.js";

// Internal imports from sub-modules used by functions in this file.
import { classifySessionKey, parseGroupKey } from "./session-utils.keys.js";
import {
  getSessionDefaults,
  resolveEstimatedSessionCostUsd,
  resolveNonNegativeNumber,
  resolvePositiveNumber,
  resolveSessionModelIdentityRef,
  resolveSessionRuntimeMs,
  resolveTranscriptUsageFallback,
} from "./session-utils.model.js";
import { loadSessionEntry } from "./session-utils.store.js";
import type {
  GatewayAgentRow,
  GatewaySessionRow,
  GatewaySessionsDefaults,
  SessionsListResult,
} from "./session-utils.types.js";

const DERIVED_TITLE_MAX_LEN = 60;

function tryResolveExistingPath(value: string): string | null {
  try {
    return fs.realpathSync(value);
  } catch {
    return null;
  }
}

function resolveIdentityAvatarUrl(
  cfg: DenebConfig,
  agentId: string,
  avatar: string | undefined,
): string | undefined {
  if (!avatar) {
    return undefined;
  }
  const trimmed = avatar.trim();
  if (!trimmed) {
    return undefined;
  }
  if (isAvatarDataUrl(trimmed) || isAvatarHttpUrl(trimmed)) {
    return trimmed;
  }
  if (!isWorkspaceRelativeAvatarPath(trimmed)) {
    return undefined;
  }
  const workspaceDir = resolveAgentWorkspaceDir(cfg, agentId);
  const workspaceRoot = tryResolveExistingPath(workspaceDir) ?? path.resolve(workspaceDir);
  const resolvedCandidate = path.resolve(workspaceRoot, trimmed);
  if (!isPathWithinRoot(workspaceRoot, resolvedCandidate)) {
    return undefined;
  }
  try {
    const opened = openBoundaryFileSync({
      absolutePath: resolvedCandidate,
      rootPath: workspaceRoot,
      rootRealPath: workspaceRoot,
      boundaryLabel: "workspace root",
      maxBytes: AVATAR_MAX_BYTES,
      skipLexicalRootCheck: true,
    });
    if (!opened.ok) {
      return undefined;
    }
    try {
      const buffer = fs.readFileSync(opened.fd);
      const mime = resolveAvatarMime(resolvedCandidate);
      return `data:${mime};base64,${buffer.toString("base64")}`;
    } finally {
      fs.closeSync(opened.fd);
    }
  } catch {
    return undefined;
  }
}

function formatSessionIdPrefix(sessionId: string, updatedAt?: number | null): string {
  const prefix = sessionId.slice(0, 8);
  if (updatedAt && updatedAt > 0) {
    const d = new Date(updatedAt);
    const date = d.toISOString().slice(0, 10);
    return `${prefix} (${date})`;
  }
  return prefix;
}

function truncateTitle(text: string, maxLen: number): string {
  if (text.length <= maxLen) {
    return text;
  }
  const cut = text.slice(0, maxLen - 1);
  const lastSpace = cut.lastIndexOf(" ");
  if (lastSpace > maxLen * 0.6) {
    return cut.slice(0, lastSpace) + "…";
  }
  return cut + "…";
}

export function deriveSessionTitle(
  entry: SessionEntry | undefined,
  firstUserMessage?: string | null,
): string | undefined {
  if (!entry) {
    return undefined;
  }

  if (entry.displayName?.trim()) {
    return entry.displayName.trim();
  }

  if (entry.subject?.trim()) {
    return entry.subject.trim();
  }

  if (firstUserMessage?.trim()) {
    const normalized = firstUserMessage.replace(/\s+/g, " ").trim();
    return truncateTitle(normalized, DERIVED_TITLE_MAX_LEN);
  }

  if (entry.sessionId) {
    return formatSessionIdPrefix(entry.sessionId, entry.updatedAt);
  }

  return undefined;
}

function resolveChildSessionKeys(
  controllerSessionKey: string,
  store: Record<string, SessionEntry>,
): string[] | undefined {
  const childSessionKeys = new Set(
    listSubagentRunsForController(controllerSessionKey)
      .map((entry) => entry.childSessionKey)
      .filter((value) => typeof value === "string" && value.trim().length > 0),
  );
  for (const [key, entry] of Object.entries(store)) {
    if (!entry || key === controllerSessionKey) {
      continue;
    }
    const spawnedBy = entry.spawnedBy?.trim();
    const parentSessionKey = entry.parentSessionKey?.trim();
    if (spawnedBy === controllerSessionKey || parentSessionKey === controllerSessionKey) {
      childSessionKeys.add(key);
    }
  }
  const childSessions = Array.from(childSessionKeys);
  return childSessions.length > 0 ? childSessions : undefined;
}

function listExistingAgentIdsFromDisk(): string[] {
  const root = resolveStateDir();
  const agentsDir = path.join(root, "agents");
  try {
    const entries = fs.readdirSync(agentsDir, { withFileTypes: true });
    return entries
      .filter((entry) => entry.isDirectory())
      .map((entry) => normalizeAgentId(entry.name))
      .filter(Boolean);
  } catch {
    return [];
  }
}

function listConfiguredAgentIds(cfg: DenebConfig): string[] {
  const ids = new Set<string>();
  const defaultId = normalizeAgentId(resolveDefaultAgentId(cfg));
  ids.add(defaultId);

  for (const entry of cfg.agents?.list ?? []) {
    if (entry?.id) {
      ids.add(normalizeAgentId(entry.id));
    }
  }

  for (const id of listExistingAgentIdsFromDisk()) {
    ids.add(id);
  }

  const sorted = Array.from(ids).filter(Boolean);
  sorted.sort((a, b) => a.localeCompare(b));
  return sorted.includes(defaultId)
    ? [defaultId, ...sorted.filter((id) => id !== defaultId)]
    : sorted;
}

export function listAgentsForGateway(cfg: DenebConfig): {
  defaultId: string;
  mainKey: string;
  scope: SessionScope;
  agents: GatewayAgentRow[];
} {
  const defaultId = normalizeAgentId(resolveDefaultAgentId(cfg));
  const mainKey = normalizeMainKey(cfg.session?.mainKey);
  const scope = cfg.session?.scope ?? "per-sender";
  const configuredById = new Map<
    string,
    { name?: string; identity?: GatewayAgentRow["identity"] }
  >();
  for (const entry of cfg.agents?.list ?? []) {
    if (!entry?.id) {
      continue;
    }
    const identity = entry.identity
      ? {
          name: entry.identity.name?.trim() || undefined,
          theme: entry.identity.theme?.trim() || undefined,
          emoji: entry.identity.emoji?.trim() || undefined,
          avatar: entry.identity.avatar?.trim() || undefined,
          avatarUrl: resolveIdentityAvatarUrl(
            cfg,
            normalizeAgentId(entry.id),
            entry.identity.avatar?.trim(),
          ),
        }
      : undefined;
    configuredById.set(normalizeAgentId(entry.id), {
      name: typeof entry.name === "string" && entry.name.trim() ? entry.name.trim() : undefined,
      identity,
    });
  }
  const explicitIds = new Set(
    (cfg.agents?.list ?? [])
      .map((entry) => (entry?.id ? normalizeAgentId(entry.id) : ""))
      .filter(Boolean),
  );
  const allowedIds = explicitIds.size > 0 ? new Set([...explicitIds, defaultId]) : null;
  let agentIds = listConfiguredAgentIds(cfg).filter((id) =>
    allowedIds ? allowedIds.has(id) : true,
  );
  if (mainKey && !agentIds.includes(mainKey) && (!allowedIds || allowedIds.has(mainKey))) {
    agentIds = [...agentIds, mainKey];
  }
  const agents = agentIds.map((id) => {
    const meta = configuredById.get(id);
    return {
      id,
      name: meta?.name,
      identity: meta?.identity,
    };
  });
  return { defaultId, mainKey, scope, agents };
}

export function buildGatewaySessionRow(params: {
  cfg: DenebConfig;
  storePath: string;
  store: Record<string, SessionEntry>;
  key: string;
  entry?: SessionEntry;
  now?: number;
  includeDerivedTitles?: boolean;
  includeLastMessage?: boolean;
}): GatewaySessionRow {
  const { cfg, storePath, store, key, entry } = params;
  const now = params.now ?? Date.now();
  const updatedAt = entry?.updatedAt ?? null;
  const parsed = parseGroupKey(key);
  const channel = entry?.channel ?? parsed?.channel;
  const subject = entry?.subject;
  const groupChannel = entry?.groupChannel;
  const space = entry?.space;
  const id = parsed?.id;
  const origin = entry?.origin;
  const originLabel = origin?.label;
  const displayName =
    entry?.displayName ??
    (channel
      ? buildGroupDisplayName({
          provider: channel,
          subject,
          groupChannel,
          space,
          id,
          key,
        })
      : undefined) ??
    entry?.label ??
    originLabel;
  const deliveryFields = normalizeSessionDeliveryFields(entry);
  const parsedAgent = parseAgentSessionKey(key);
  const sessionAgentId = normalizeAgentId(parsedAgent?.agentId ?? resolveDefaultAgentId(cfg));
  const subagentRun = getSubagentRunByChildSessionKey(key);
  const subagentStatus = subagentRun ? resolveSubagentSessionStatus(subagentRun) : undefined;
  const subagentStartedAt = subagentRun ? getSubagentSessionStartedAt(subagentRun) : undefined;
  const subagentEndedAt = subagentRun ? subagentRun.endedAt : undefined;
  const subagentRuntimeMs = subagentRun ? resolveSessionRuntimeMs(subagentRun, now) : undefined;
  const resolvedModel = resolveSessionModelIdentityRef(
    cfg,
    entry,
    sessionAgentId,
    subagentRun?.model,
  );
  const modelProvider = resolvedModel.provider;
  const model = resolvedModel.model ?? DEFAULT_MODEL;
  const transcriptUsage =
    resolvePositiveNumber(resolveFreshSessionTotalTokens(entry)) === undefined ||
    resolvePositiveNumber(entry?.contextTokens) === undefined ||
    resolveEstimatedSessionCostUsd({
      cfg,
      provider: modelProvider,
      model,
      entry,
    }) === undefined
      ? resolveTranscriptUsageFallback({
          cfg,
          key,
          entry,
          storePath,
          fallbackProvider: modelProvider,
          fallbackModel: model,
        })
      : null;
  const totalTokens =
    resolvePositiveNumber(resolveFreshSessionTotalTokens(entry)) ??
    resolvePositiveNumber(transcriptUsage?.totalTokens);
  const totalTokensFresh =
    typeof totalTokens === "number" && Number.isFinite(totalTokens) && totalTokens > 0
      ? true
      : transcriptUsage?.totalTokensFresh === true;
  const childSessions = resolveChildSessionKeys(key, store);
  const estimatedCostUsd =
    resolveEstimatedSessionCostUsd({
      cfg,
      provider: modelProvider,
      model,
      entry,
    }) ?? resolveNonNegativeNumber(transcriptUsage?.estimatedCostUsd);
  const contextTokens =
    resolvePositiveNumber(entry?.contextTokens) ??
    resolvePositiveNumber(transcriptUsage?.contextTokens) ??
    resolvePositiveNumber(
      resolveContextTokensForModel({
        cfg,
        provider: modelProvider,
        model,
      }),
    );

  let derivedTitle: string | undefined;
  let lastMessagePreview: string | undefined;
  if (entry?.sessionId && (params.includeDerivedTitles || params.includeLastMessage)) {
    const fields = readSessionTitleFieldsFromTranscript(
      entry.sessionId,
      storePath,
      entry.sessionFile,
      sessionAgentId,
    );
    if (params.includeDerivedTitles) {
      derivedTitle = deriveSessionTitle(entry, fields.firstUserMessage);
    }
    if (params.includeLastMessage && fields.lastMessagePreview) {
      lastMessagePreview = fields.lastMessagePreview;
    }
  }

  return {
    key,
    spawnedBy: entry?.spawnedBy,
    kind: classifySessionKey(key, entry),
    label: entry?.label,
    displayName,
    derivedTitle,
    lastMessagePreview,
    channel,
    subject,
    groupChannel,
    space,
    chatType: entry?.chatType,
    origin,
    updatedAt,
    sessionId: entry?.sessionId,
    systemSent: entry?.systemSent,
    abortedLastRun: entry?.abortedLastRun,
    thinkingLevel: entry?.thinkingLevel,
    verboseLevel: entry?.verboseLevel,
    reasoningLevel: entry?.reasoningLevel,
    elevatedLevel: entry?.elevatedLevel,
    sendPolicy: entry?.sendPolicy,
    inputTokens: entry?.inputTokens,
    outputTokens: entry?.outputTokens,
    totalTokens,
    totalTokensFresh,
    estimatedCostUsd,
    status: subagentRun ? subagentStatus : entry?.status,
    startedAt: subagentRun ? subagentStartedAt : entry?.startedAt,
    endedAt: subagentRun ? subagentEndedAt : entry?.endedAt,
    runtimeMs: subagentRun ? subagentRuntimeMs : entry?.runtimeMs,
    parentSessionKey: entry?.parentSessionKey,
    childSessions,
    responseUsage: entry?.responseUsage,
    modelProvider,
    model,
    contextTokens,
    deliveryContext: deliveryFields.deliveryContext,
    lastChannel: deliveryFields.lastChannel ?? entry?.lastChannel,
    lastTo: deliveryFields.lastTo ?? entry?.lastTo,
    lastAccountId: deliveryFields.lastAccountId ?? entry?.lastAccountId,
  };
}

export function loadGatewaySessionRow(
  sessionKey: string,
  options?: { includeDerivedTitles?: boolean; includeLastMessage?: boolean; now?: number },
): GatewaySessionRow | null {
  const { cfg, storePath, store, entry, canonicalKey } = loadSessionEntry(sessionKey);
  if (!entry) {
    return null;
  }
  return buildGatewaySessionRow({
    cfg,
    storePath,
    store,
    key: canonicalKey,
    entry,
    now: options?.now,
    includeDerivedTitles: options?.includeDerivedTitles,
    includeLastMessage: options?.includeLastMessage,
  });
}

export function listSessionsFromStore(params: {
  cfg: DenebConfig;
  storePath: string;
  store: Record<string, SessionEntry>;
  opts: import("./protocol/index.js").SessionsListParams;
}): SessionsListResult {
  const { cfg, storePath, store, opts } = params;
  const now = Date.now();

  const includeGlobal = opts.includeGlobal === true;
  const includeUnknown = opts.includeUnknown === true;
  const includeDerivedTitles = opts.includeDerivedTitles === true;
  const includeLastMessage = opts.includeLastMessage === true;
  const spawnedBy = typeof opts.spawnedBy === "string" ? opts.spawnedBy : "";
  const label = typeof opts.label === "string" ? opts.label.trim() : "";
  const agentId = typeof opts.agentId === "string" ? normalizeAgentId(opts.agentId) : "";
  const search = typeof opts.search === "string" ? opts.search.trim().toLowerCase() : "";
  const activeMinutes =
    typeof opts.activeMinutes === "number" && Number.isFinite(opts.activeMinutes)
      ? Math.max(1, Math.floor(opts.activeMinutes))
      : undefined;

  let sessions = Object.entries(store)
    .filter(([key]) => {
      if (isCronRunSessionKey(key)) {
        return false;
      }
      if (!includeGlobal && key === "global") {
        return false;
      }
      if (!includeUnknown && key === "unknown") {
        return false;
      }
      if (agentId) {
        if (key === "global" || key === "unknown") {
          return false;
        }
        const parsed = parseAgentSessionKey(key);
        if (!parsed) {
          return false;
        }
        return normalizeAgentId(parsed.agentId) === agentId;
      }
      return true;
    })
    .filter(([key, entry]) => {
      if (!spawnedBy) {
        return true;
      }
      if (key === "unknown" || key === "global") {
        return false;
      }
      return entry?.spawnedBy === spawnedBy;
    })
    .filter(([, entry]) => {
      if (!label) {
        return true;
      }
      return entry?.label === label;
    })
    .map(([key, entry]) =>
      buildGatewaySessionRow({
        cfg,
        storePath,
        store,
        key,
        entry,
        now,
        includeDerivedTitles,
        includeLastMessage,
      }),
    )
    .toSorted((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0));

  if (search) {
    sessions = sessions.filter((s) => {
      const fields = [s.displayName, s.label, s.subject, s.sessionId, s.key];
      return fields.some((f) => typeof f === "string" && f.toLowerCase().includes(search));
    });
  }

  if (activeMinutes !== undefined) {
    const cutoff = now - activeMinutes * 60_000;
    sessions = sessions.filter((s) => (s.updatedAt ?? 0) >= cutoff);
  }

  if (typeof opts.limit === "number" && Number.isFinite(opts.limit)) {
    const limit = Math.max(1, Math.floor(opts.limit));
    sessions = sessions.slice(0, limit);
  }

  return {
    ts: now,
    path: storePath,
    count: sessions.length,
    defaults: getSessionDefaults(cfg),
    sessions,
  };
}
