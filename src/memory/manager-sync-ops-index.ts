// Memory and session file indexing (sync) for MemoryManagerSyncOps.
import type { DatabaseSync } from "node:sqlite";
import { ResolvedMemorySearchConfig } from "../agents/memory-search.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import type { EmbeddingProvider } from "./embeddings.js";
import {
  buildFileEntry,
  listMemoryFiles,
  runWithConcurrency,
  type MemoryFileEntry,
} from "./internal.js";
import { FTS_TABLE, VECTOR_TABLE, type MemorySyncProgressState } from "./manager-sync-ops-types.js";
import { resetSessionDelta } from "./manager-sync-ops-watchers.js";
import type { SessionFileEntry } from "./session-files.js";
import {
  buildSessionEntry,
  listSessionFilesForAgent,
  sessionPathForFile,
} from "./session-files.js";
import type { MemorySource, MemorySyncProgressUpdate } from "./types.js";

const log = createSubsystemLogger("memory");

export function createSyncProgress(
  onProgress: (update: MemorySyncProgressUpdate) => void,
): MemorySyncProgressState {
  const state: MemorySyncProgressState = {
    completed: 0,
    total: 0,
    label: undefined,
    report: (update) => {
      if (update.label) {
        state.label = update.label;
      }
      const label =
        update.total > 0 && state.label
          ? `${state.label} ${update.completed}/${update.total}`
          : state.label;
      onProgress({
        completed: update.completed,
        total: update.total,
        label,
      });
    },
  };
  return state;
}

export async function syncMemoryFiles(params: {
  db: DatabaseSync;
  provider: EmbeddingProvider | null;
  settings: ResolvedMemorySearchConfig;
  workspaceDir: string;
  fts: { enabled: boolean; available: boolean };
  needsFullReindex: boolean;
  getConcurrency: () => number;
  isBatchEnabled: boolean;
  indexFile: (
    entry: MemoryFileEntry | SessionFileEntry,
    options: { source: MemorySource; content?: string },
  ) => Promise<void>;
  progress?: MemorySyncProgressState;
}): Promise<void> {
  // FTS-only mode: skip embedding sync (no provider)
  if (!params.provider) {
    log.debug("Skipping memory file sync in FTS-only mode (no embedding provider)");
    return;
  }

  const files = await listMemoryFiles(
    params.workspaceDir,
    params.settings.extraPaths,
    params.settings.multimodal,
  );
  const fileEntries = (
    await runWithConcurrency(
      files.map(
        (file) => async () =>
          await buildFileEntry(file, params.workspaceDir, params.settings.multimodal),
      ),
      params.getConcurrency(),
    )
  ).filter((entry): entry is MemoryFileEntry => entry !== null);
  log.debug("memory sync: indexing memory files", {
    files: fileEntries.length,
    needsFullReindex: params.needsFullReindex,
    batch: params.isBatchEnabled,
    concurrency: params.getConcurrency(),
  });
  const activePaths = new Set(fileEntries.map((entry) => entry.path));
  if (params.progress) {
    params.progress.total += fileEntries.length;
    params.progress.report({
      completed: params.progress.completed,
      total: params.progress.total,
      label: params.isBatchEnabled ? "Indexing memory files (batch)..." : "Indexing memory files…",
    });
  }

  const tasks = fileEntries.map((entry) => async () => {
    const record = params.db
      .prepare(`SELECT hash FROM files WHERE path = ? AND source = ?`)
      .get(entry.path, "memory") as { hash: string } | undefined;
    if (!params.needsFullReindex && record?.hash === entry.hash) {
      if (params.progress) {
        params.progress.completed += 1;
        params.progress.report({
          completed: params.progress.completed,
          total: params.progress.total,
        });
      }
      return;
    }
    await params.indexFile(entry, { source: "memory" });
    if (params.progress) {
      params.progress.completed += 1;
      params.progress.report({
        completed: params.progress.completed,
        total: params.progress.total,
      });
    }
  });
  await runWithConcurrency(tasks, params.getConcurrency());

  const staleRows = params.db
    .prepare(`SELECT path FROM files WHERE source = ?`)
    .all("memory") as Array<{ path: string }>;
  for (const stale of staleRows) {
    if (activePaths.has(stale.path)) {
      continue;
    }
    params.db.prepare(`DELETE FROM files WHERE path = ? AND source = ?`).run(stale.path, "memory");
    try {
      params.db
        .prepare(
          `DELETE FROM ${VECTOR_TABLE} WHERE id IN (SELECT id FROM chunks WHERE path = ? AND source = ?)`,
        )
        .run(stale.path, "memory");
    } catch (err) {
      log.debug?.(`failed to delete vector rows for ${stale.path}: ${String(err)}`);
    }
    params.db.prepare(`DELETE FROM chunks WHERE path = ? AND source = ?`).run(stale.path, "memory");
    if (params.fts.enabled && params.fts.available) {
      try {
        params.db
          .prepare(`DELETE FROM ${FTS_TABLE} WHERE path = ? AND source = ? AND model = ?`)
          .run(stale.path, "memory", params.provider.model);
      } catch (err) {
        log.debug?.(`failed to delete FTS rows for ${stale.path}: ${String(err)}`);
      }
    }
  }
}

export async function syncSessionFiles(params: {
  db: DatabaseSync;
  provider: EmbeddingProvider | null;
  agentId: string;
  settings: ResolvedMemorySearchConfig;
  fts: { enabled: boolean; available: boolean };
  sessionsDirtyFiles: Set<string>;
  sessionDeltas: Map<string, { lastSize: number; pendingBytes: number; pendingMessages: number }>;
  needsFullReindex: boolean;
  targetSessionFiles?: string[];
  getConcurrency: () => number;
  isBatchEnabled: boolean;
  indexFile: (
    entry: MemoryFileEntry | SessionFileEntry,
    options: { source: MemorySource; content?: string },
  ) => Promise<void>;
  progress?: MemorySyncProgressState;
}): Promise<void> {
  // FTS-only mode: skip embedding sync (no provider)
  if (!params.provider) {
    log.debug("Skipping session file sync in FTS-only mode (no embedding provider)");
    return;
  }

  const normalizedTargets = params.needsFullReindex
    ? null
    : params.targetSessionFiles
      ? new Set(params.targetSessionFiles)
      : null;
  const files = normalizedTargets
    ? Array.from(normalizedTargets)
    : await listSessionFilesForAgent(params.agentId);
  const activePaths = normalizedTargets
    ? null
    : new Set(files.map((file) => sessionPathForFile(file)));
  const indexAll =
    params.needsFullReindex || Boolean(normalizedTargets) || params.sessionsDirtyFiles.size === 0;
  log.debug("memory sync: indexing session files", {
    files: files.length,
    indexAll,
    dirtyFiles: params.sessionsDirtyFiles.size,
    targetedFiles: normalizedTargets?.size ?? 0,
    batch: params.isBatchEnabled,
    concurrency: params.getConcurrency(),
  });
  if (params.progress) {
    params.progress.total += files.length;
    params.progress.report({
      completed: params.progress.completed,
      total: params.progress.total,
      label: params.isBatchEnabled
        ? "Indexing session files (batch)..."
        : "Indexing session files…",
    });
  }

  const tasks = files.map((absPath) => async () => {
    if (!indexAll && !params.sessionsDirtyFiles.has(absPath)) {
      if (params.progress) {
        params.progress.completed += 1;
        params.progress.report({
          completed: params.progress.completed,
          total: params.progress.total,
        });
      }
      return;
    }
    const entry = await buildSessionEntry(absPath);
    if (!entry) {
      if (params.progress) {
        params.progress.completed += 1;
        params.progress.report({
          completed: params.progress.completed,
          total: params.progress.total,
        });
      }
      return;
    }
    const record = params.db
      .prepare(`SELECT hash FROM files WHERE path = ? AND source = ?`)
      .get(entry.path, "sessions") as { hash: string } | undefined;
    if (!params.needsFullReindex && record?.hash === entry.hash) {
      if (params.progress) {
        params.progress.completed += 1;
        params.progress.report({
          completed: params.progress.completed,
          total: params.progress.total,
        });
      }
      resetSessionDelta(absPath, entry.size, params.sessionDeltas);
      return;
    }
    await params.indexFile(entry, { source: "sessions", content: entry.content });
    resetSessionDelta(absPath, entry.size, params.sessionDeltas);
    if (params.progress) {
      params.progress.completed += 1;
      params.progress.report({
        completed: params.progress.completed,
        total: params.progress.total,
      });
    }
  });
  await runWithConcurrency(tasks, params.getConcurrency());

  if (activePaths === null) {
    // Targeted syncs only refresh the requested transcripts and should not
    // prune unrelated session rows without a full directory enumeration.
    return;
  }

  const staleRows = params.db
    .prepare(`SELECT path FROM files WHERE source = ?`)
    .all("sessions") as Array<{ path: string }>;
  for (const stale of staleRows) {
    if (activePaths.has(stale.path)) {
      continue;
    }
    params.db
      .prepare(`DELETE FROM files WHERE path = ? AND source = ?`)
      .run(stale.path, "sessions");
    try {
      params.db
        .prepare(
          `DELETE FROM ${VECTOR_TABLE} WHERE id IN (SELECT id FROM chunks WHERE path = ? AND source = ?)`,
        )
        .run(stale.path, "sessions");
    } catch (err) {
      log.debug?.(`failed to delete vector rows for ${stale.path}: ${String(err)}`);
    }
    params.db
      .prepare(`DELETE FROM chunks WHERE path = ? AND source = ?`)
      .run(stale.path, "sessions");
    if (params.fts.enabled && params.fts.available) {
      try {
        params.db
          .prepare(`DELETE FROM ${FTS_TABLE} WHERE path = ? AND source = ? AND model = ?`)
          .run(stale.path, "sessions", params.provider.model);
      } catch (err) {
        log.debug?.(`failed to delete FTS rows for ${stale.path}: ${String(err)}`);
      }
    }
  }
}
