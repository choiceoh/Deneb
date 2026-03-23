// File watcher, session listener, and sync scheduling for MemoryManagerSyncOps.
import fsSync from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import chokidar, { FSWatcher } from "chokidar";
import { ResolvedMemorySearchConfig } from "../agents/memory-search.js";
import { resolveSessionTranscriptsDirForAgent } from "../config/sessions/paths.js";
import { onSessionTranscriptUpdate } from "../sessions/transcript-events.js";
import { isFileMissingError } from "./fs-utils.js";
import { normalizeExtraMemoryPaths } from "./internal.js";
import {
  IGNORED_MEMORY_WATCH_DIR_NAMES,
  SESSION_DELTA_READ_CHUNK_BYTES,
  SESSION_DIRTY_DEBOUNCE_MS,
} from "./manager-sync-ops-types.js";
import {
  buildCaseInsensitiveExtensionGlob,
  classifyMemoryMultimodalPath,
  getMemoryMultimodalExtensions,
} from "./multimodal.js";
import type { MemorySource } from "./types.js";

export function shouldIgnoreMemoryWatchPath(watchPath: string): boolean {
  const normalized = path.normalize(watchPath);
  const parts = normalized.split(path.sep).map((segment) => segment.trim().toLowerCase());
  return parts.some((segment) => IGNORED_MEMORY_WATCH_DIR_NAMES.has(segment));
}

/**
 * Creates a chokidar watcher for memory files if watch mode is enabled and one
 * does not already exist. Returns the new watcher, or null if not created.
 */
export function createMemoryWatcher(
  sources: Set<MemorySource>,
  settings: ResolvedMemorySearchConfig,
  workspaceDir: string,
  existingWatcher: FSWatcher | null,
  onDirty: () => void,
): FSWatcher | null {
  if (!sources.has("memory") || !settings.sync.watch || existingWatcher) {
    return null;
  }
  const watchPaths = new Set<string>([
    path.join(workspaceDir, "MEMORY.md"),
    path.join(workspaceDir, "memory.md"),
    path.join(workspaceDir, "memory", "**", "*.md"),
  ]);
  const additionalPaths = normalizeExtraMemoryPaths(workspaceDir, settings.extraPaths);
  for (const entry of additionalPaths) {
    try {
      const stat = fsSync.lstatSync(entry);
      if (stat.isSymbolicLink()) {
        continue;
      }
      if (stat.isDirectory()) {
        watchPaths.add(path.join(entry, "**", "*.md"));
        if (settings.multimodal.enabled) {
          for (const modality of settings.multimodal.modalities) {
            for (const extension of getMemoryMultimodalExtensions(modality)) {
              watchPaths.add(path.join(entry, "**", buildCaseInsensitiveExtensionGlob(extension)));
            }
          }
        }
        continue;
      }
      if (
        stat.isFile() &&
        (entry.toLowerCase().endsWith(".md") ||
          classifyMemoryMultimodalPath(entry, settings.multimodal) !== null)
      ) {
        watchPaths.add(entry);
      }
    } catch {
      // Skip missing/unreadable additional paths.
    }
  }
  const watcher = chokidar.watch(Array.from(watchPaths), {
    ignoreInitial: true,
    ignored: (watchPath) => shouldIgnoreMemoryWatchPath(String(watchPath)),
    awaitWriteFinish: {
      stabilityThreshold: settings.sync.watchDebounceMs,
      pollInterval: 100,
    },
  });
  watcher.on("add", onDirty);
  watcher.on("change", onDirty);
  watcher.on("unlink", onDirty);
  return watcher;
}

/**
 * Sets up a recurring sync interval if not already active. Returns the timer,
 * or null if intervals are disabled or one already exists.
 */
export function createIntervalSyncTimer(
  settings: ResolvedMemorySearchConfig,
  existingTimer: NodeJS.Timeout | null,
  onInterval: () => void,
): NodeJS.Timeout | null {
  const minutes = settings.sync.intervalMinutes;
  if (!minutes || minutes <= 0 || existingTimer) {
    return null;
  }
  return setInterval(onInterval, minutes * 60 * 1000);
}

/**
 * (Re-)schedules a debounced watch sync. Returns the new timer handle.
 * Caller must cancel the old timer before calling if one exists.
 */
export function scheduleWatchSync(
  sources: Set<MemorySource>,
  settings: ResolvedMemorySearchConfig,
  existingTimer: NodeJS.Timeout | null,
  onSync: () => void,
): NodeJS.Timeout | null {
  if (!sources.has("memory") || !settings.sync.watch) {
    return existingTimer;
  }
  if (existingTimer) {
    clearTimeout(existingTimer);
  }
  return setTimeout(onSync, settings.sync.watchDebounceMs);
}

/**
 * Subscribes to session transcript update events if not already subscribed.
 * Returns the unsubscribe function, or null if already subscribed or not needed.
 */
export function createSessionListener(
  sources: Set<MemorySource>,
  agentId: string,
  existingUnsubscribe: (() => void) | null,
  isClosedFn: () => boolean,
  scheduleSessionDirtyFn: (sessionFile: string) => void,
): (() => void) | null {
  if (!sources.has("sessions") || existingUnsubscribe) {
    return null;
  }
  return onSessionTranscriptUpdate((update) => {
    if (isClosedFn()) {
      return;
    }
    const sessionFile = update.sessionFile;
    if (!isSessionFileForAgent(agentId, sessionFile)) {
      return;
    }
    scheduleSessionDirtyFn(sessionFile);
  });
}

export function isSessionFileForAgent(agentId: string, sessionFile: string): boolean {
  if (!sessionFile) {
    return false;
  }
  const sessionsDir = resolveSessionTranscriptsDirForAgent(agentId);
  const resolvedFile = path.resolve(sessionFile);
  const resolvedDir = path.resolve(sessionsDir);
  return resolvedFile.startsWith(`${resolvedDir}${path.sep}`);
}

/**
 * Schedules a deferred session delta batch. Returns the new timer handle.
 * No-op (returns the existing timer) if one is already pending.
 */
export function scheduleSessionDirty(
  sessionFile: string,
  pendingFiles: Set<string>,
  existingTimer: NodeJS.Timeout | null,
  onBatch: () => void,
): NodeJS.Timeout {
  pendingFiles.add(sessionFile);
  if (existingTimer) {
    return existingTimer;
  }
  return setTimeout(onBatch, SESSION_DIRTY_DEBOUNCE_MS);
}

export async function updateSessionDelta(
  sessionFile: string,
  settings: ResolvedMemorySearchConfig,
  sessionDeltas: Map<string, { lastSize: number; pendingBytes: number; pendingMessages: number }>,
): Promise<{
  deltaBytes: number;
  deltaMessages: number;
  pendingBytes: number;
  pendingMessages: number;
} | null> {
  const thresholds = settings.sync.sessions;
  if (!thresholds) {
    return null;
  }
  let stat: { size: number };
  try {
    stat = await fs.stat(sessionFile);
  } catch {
    return null;
  }
  const size = stat.size;
  let state = sessionDeltas.get(sessionFile);
  if (!state) {
    state = { lastSize: 0, pendingBytes: 0, pendingMessages: 0 };
    sessionDeltas.set(sessionFile, state);
  }
  const deltaBytes = Math.max(0, size - state.lastSize);
  if (deltaBytes === 0 && size === state.lastSize) {
    return {
      deltaBytes: thresholds.deltaBytes,
      deltaMessages: thresholds.deltaMessages,
      pendingBytes: state.pendingBytes,
      pendingMessages: state.pendingMessages,
    };
  }
  if (size < state.lastSize) {
    state.lastSize = size;
    state.pendingBytes += size;
    const shouldCountMessages =
      thresholds.deltaMessages > 0 &&
      (thresholds.deltaBytes <= 0 || state.pendingBytes < thresholds.deltaBytes);
    if (shouldCountMessages) {
      state.pendingMessages += await countNewlines(sessionFile, 0, size);
    }
  } else {
    state.pendingBytes += deltaBytes;
    const shouldCountMessages =
      thresholds.deltaMessages > 0 &&
      (thresholds.deltaBytes <= 0 || state.pendingBytes < thresholds.deltaBytes);
    if (shouldCountMessages) {
      state.pendingMessages += await countNewlines(sessionFile, state.lastSize, size);
    }
    state.lastSize = size;
  }
  sessionDeltas.set(sessionFile, state);
  return {
    deltaBytes: thresholds.deltaBytes,
    deltaMessages: thresholds.deltaMessages,
    pendingBytes: state.pendingBytes,
    pendingMessages: state.pendingMessages,
  };
}

async function countNewlines(absPath: string, start: number, end: number): Promise<number> {
  if (end <= start) {
    return 0;
  }
  let handle;
  try {
    handle = await fs.open(absPath, "r");
  } catch (err) {
    if (isFileMissingError(err)) {
      return 0;
    }
    throw err;
  }
  try {
    let offset = start;
    let count = 0;
    const buffer = Buffer.alloc(SESSION_DELTA_READ_CHUNK_BYTES);
    while (offset < end) {
      const toRead = Math.min(buffer.length, end - offset);
      const { bytesRead } = await handle.read(buffer, 0, toRead, offset);
      if (bytesRead <= 0) {
        break;
      }
      for (let i = 0; i < bytesRead; i += 1) {
        if (buffer[i] === 10) {
          count += 1;
        }
      }
      offset += bytesRead;
    }
    return count;
  } finally {
    await handle.close();
  }
}

export function resetSessionDelta(
  absPath: string,
  size: number,
  sessionDeltas: Map<string, { lastSize: number; pendingBytes: number; pendingMessages: number }>,
): void {
  const state = sessionDeltas.get(absPath);
  if (!state) {
    return;
  }
  state.lastSize = size;
  state.pendingBytes = 0;
  state.pendingMessages = 0;
}

export function normalizeTargetSessionFiles(
  agentId: string,
  sessionFiles?: string[],
): Set<string> | null {
  if (!sessionFiles || sessionFiles.length === 0) {
    return null;
  }
  const normalized = new Set<string>();
  for (const sessionFile of sessionFiles) {
    const trimmed = sessionFile.trim();
    if (!trimmed) {
      continue;
    }
    const resolved = path.resolve(trimmed);
    if (isSessionFileForAgent(agentId, resolved)) {
      normalized.add(resolved);
    }
  }
  return normalized.size > 0 ? normalized : null;
}

/**
 * Clears synced session files from the dirty set. Returns whether any dirty
 * files remain.
 */
export function clearSyncedSessionFiles(
  sessionsDirtyFiles: Set<string>,
  targetSessionFiles?: Iterable<string> | null,
): boolean {
  if (!targetSessionFiles) {
    sessionsDirtyFiles.clear();
  } else {
    for (const targetSessionFile of targetSessionFiles) {
      sessionsDirtyFiles.delete(targetSessionFile);
    }
  }
  return sessionsDirtyFiles.size > 0;
}

export function shouldSyncSessions(
  sources: Set<MemorySource>,
  sessionsDirty: boolean,
  sessionsDirtyFiles: Set<string>,
  params?: { reason?: string; force?: boolean; sessionFiles?: string[] },
  needsFullReindex = false,
): boolean {
  if (!sources.has("sessions")) {
    return false;
  }
  if (params?.sessionFiles?.some((sessionFile) => sessionFile.trim().length > 0)) {
    return true;
  }
  if (params?.force) {
    return true;
  }
  const reason = params?.reason;
  if (reason === "session-start" || reason === "watch") {
    return false;
  }
  if (needsFullReindex) {
    return true;
  }
  return sessionsDirty && sessionsDirtyFiles.size > 0;
}
