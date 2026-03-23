import { randomUUID } from "node:crypto";
import type { DatabaseSync } from "node:sqlite";
import type { FSWatcher } from "chokidar";
import { resolveAgentDir } from "../agents/agent-scope.js";
import { ResolvedMemorySearchConfig } from "../agents/memory-search.js";
import { type DenebConfig } from "../config/config.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { resolveUserPath } from "../utils.js";
import { DEFAULT_GEMINI_EMBEDDING_MODEL } from "./embeddings-gemini.js";
import { DEFAULT_MISTRAL_EMBEDDING_MODEL } from "./embeddings-mistral.js";
import { DEFAULT_OPENAI_EMBEDDING_MODEL } from "./embeddings-openai.js";
import { DEFAULT_VOYAGE_EMBEDDING_MODEL } from "./embeddings-voyage.js";
import {
  createEmbeddingProvider,
  type EmbeddingProvider,
  type GeminiEmbeddingClient,
  type MistralEmbeddingClient,
  type OpenAiEmbeddingClient,
  type VoyageEmbeddingClient,
} from "./embeddings.js";
import type { MemoryFileEntry } from "./internal.js";
import {
  buildSourceFilter,
  openDatabase,
  openDatabaseAtPath,
  removeIndexFiles,
  runEnsureSchema,
  seedEmbeddingCache,
  swapIndexFiles,
} from "./manager-sync-ops-db.js";
import { syncMemoryFiles, syncSessionFiles, createSyncProgress } from "./manager-sync-ops-index.js";
import {
  metaSourcesDiffer,
  readMeta as readMetaHelper,
  resolveConfiguredScopeHash as resolveConfiguredScopeHashHelper,
  resolveConfiguredSourcesForMeta as resolveConfiguredSourcesForMetaHelper,
  writeMeta as writeMetaHelper,
} from "./manager-sync-ops-meta.js";
import {
  FTS_TABLE,
  VECTOR_LOAD_TIMEOUT_MS,
  type MemoryIndexMeta,
  type MemorySyncProgressState,
} from "./manager-sync-ops-types.js";
import {
  dropVectorTable,
  ensureVectorTable,
  loadVectorExtension,
} from "./manager-sync-ops-vector.js";
import {
  clearSyncedSessionFiles,
  createIntervalSyncTimer,
  createMemoryWatcher,
  createSessionListener,
  normalizeTargetSessionFiles,
  scheduleSessionDirty,
  scheduleWatchSync,
  shouldSyncSessions,
  updateSessionDelta,
} from "./manager-sync-ops-watchers.js";
import type { SessionFileEntry } from "./session-files.js";
import type { MemorySource, MemorySyncProgressUpdate } from "./types.js";

export type { MemoryIndexMeta };

const log = createSubsystemLogger("memory");

export abstract class MemoryManagerSyncOps {
  protected abstract readonly cfg: DenebConfig;
  protected abstract readonly agentId: string;
  protected abstract readonly workspaceDir: string;
  protected abstract readonly settings: ResolvedMemorySearchConfig;
  protected provider: EmbeddingProvider | null = null;
  protected fallbackFrom?: "openai" | "local" | "gemini" | "voyage" | "mistral";
  protected openAi?: OpenAiEmbeddingClient;
  protected gemini?: GeminiEmbeddingClient;
  protected voyage?: VoyageEmbeddingClient;
  protected mistral?: MistralEmbeddingClient;
  protected abstract batch: {
    enabled: boolean;
    wait: boolean;
    concurrency: number;
    pollIntervalMs: number;
    timeoutMs: number;
  };
  protected readonly sources: Set<MemorySource> = new Set();
  protected providerKey: string | null = null;
  protected abstract readonly vector: {
    enabled: boolean;
    available: boolean | null;
    extensionPath?: string;
    loadError?: string;
    dims?: number;
  };
  protected readonly fts: {
    enabled: boolean;
    available: boolean;
    loadError?: string;
  } = { enabled: false, available: false };
  protected vectorReady: Promise<boolean> | null = null;
  protected watcher: FSWatcher | null = null;
  protected watchTimer: NodeJS.Timeout | null = null;
  protected sessionWatchTimer: NodeJS.Timeout | null = null;
  protected sessionUnsubscribe: (() => void) | null = null;
  protected fallbackReason?: string;
  protected intervalTimer: NodeJS.Timeout | null = null;
  protected closed = false;
  protected dirty = false;
  protected sessionsDirty = false;
  protected sessionsDirtyFiles = new Set<string>();
  protected sessionPendingFiles = new Set<string>();
  protected sessionDeltas = new Map<
    string,
    { lastSize: number; pendingBytes: number; pendingMessages: number }
  >();
  private readonly lastMetaSerializedRef: { value: string | null } = { value: null };

  protected abstract readonly cache: { enabled: boolean; maxEntries?: number };
  protected abstract db: DatabaseSync;
  protected abstract computeProviderKey(): string;
  protected abstract sync(params?: {
    reason?: string;
    force?: boolean;
    forceSessions?: boolean;
    sessionFile?: string;
    progress?: (update: MemorySyncProgressUpdate) => void;
  }): Promise<void>;
  protected abstract withTimeout<T>(
    promise: Promise<T>,
    timeoutMs: number,
    message: string,
  ): Promise<T>;
  protected abstract getIndexConcurrency(): number;
  protected abstract pruneEmbeddingCacheIfNeeded(): void;
  protected abstract indexFile(
    entry: MemoryFileEntry | SessionFileEntry,
    options: { source: MemorySource; content?: string },
  ): Promise<void>;

  protected async ensureVectorReady(dimensions?: number): Promise<boolean> {
    if (!this.vector.enabled) {
      return false;
    }
    if (!this.vectorReady) {
      this.vectorReady = this.withTimeout(
        loadVectorExtension(this.vector, this.db),
        VECTOR_LOAD_TIMEOUT_MS,
        `sqlite-vec load timed out after ${Math.round(VECTOR_LOAD_TIMEOUT_MS / 1000)}s`,
      );
    }
    let ready = false;
    try {
      ready = (await this.vectorReady) || false;
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.vector.available = false;
      this.vector.loadError = message;
      this.vectorReady = null;
      log.warn(`sqlite-vec unavailable: ${message}`);
      return false;
    }
    if (ready && typeof dimensions === "number" && dimensions > 0) {
      ensureVectorTable(this.vector, this.db, dimensions);
    }
    return ready;
  }

  protected buildSourceFilter(alias?: string): { sql: string; params: MemorySource[] } {
    return buildSourceFilter(this.sources, alias);
  }

  protected openDatabase(): DatabaseSync {
    return openDatabase(this.settings);
  }

  private openDatabaseAtPath(dbPath: string): DatabaseSync {
    return openDatabaseAtPath(this.settings, dbPath);
  }

  private seedEmbeddingCache(sourceDb: DatabaseSync): void {
    seedEmbeddingCache(this.cache, this.db, sourceDb);
  }

  private async swapIndexFiles(targetPath: string, tempPath: string): Promise<void> {
    await swapIndexFiles(targetPath, tempPath);
  }

  private async removeIndexFiles(basePath: string): Promise<void> {
    await removeIndexFiles(basePath);
  }

  protected ensureSchema() {
    runEnsureSchema(this.db, this.fts);
    // Only warn when hybrid search is enabled; otherwise this is expected noise.
    if (this.fts.loadError && this.fts.enabled) {
      log.warn(`fts unavailable: ${this.fts.loadError}`);
    }
  }

  protected ensureWatcher() {
    const newWatcher = createMemoryWatcher(
      this.sources,
      this.settings,
      this.workspaceDir,
      this.watcher,
      () => {
        this.dirty = true;
        this.scheduleWatchSync();
      },
    );
    if (newWatcher) {
      this.watcher = newWatcher;
    }
  }

  protected ensureSessionListener() {
    const unsubscribe = createSessionListener(
      this.sources,
      this.agentId,
      this.sessionUnsubscribe,
      () => this.closed,
      (sessionFile) => this.scheduleSessionDirty(sessionFile),
    );
    if (unsubscribe) {
      this.sessionUnsubscribe = unsubscribe;
    }
  }

  private scheduleSessionDirty(sessionFile: string) {
    const newTimer = scheduleSessionDirty(
      sessionFile,
      this.sessionPendingFiles,
      this.sessionWatchTimer,
      () => {
        this.sessionWatchTimer = null;
        void this.processSessionDeltaBatch().catch((err) => {
          log.warn(`memory session delta failed: ${String(err)}`);
        });
      },
    );
    this.sessionWatchTimer = newTimer;
  }

  private async processSessionDeltaBatch(): Promise<void> {
    if (this.sessionPendingFiles.size === 0) {
      return;
    }
    const pending = Array.from(this.sessionPendingFiles);
    this.sessionPendingFiles.clear();
    let shouldSync = false;
    for (const sessionFile of pending) {
      const delta = await updateSessionDelta(sessionFile, this.settings, this.sessionDeltas);
      if (!delta) {
        continue;
      }
      const bytesThreshold = delta.deltaBytes;
      const messagesThreshold = delta.deltaMessages;
      const bytesHit =
        bytesThreshold <= 0 ? delta.pendingBytes > 0 : delta.pendingBytes >= bytesThreshold;
      const messagesHit =
        messagesThreshold <= 0
          ? delta.pendingMessages > 0
          : delta.pendingMessages >= messagesThreshold;
      if (!bytesHit && !messagesHit) {
        continue;
      }
      this.sessionsDirtyFiles.add(sessionFile);
      this.sessionsDirty = true;
      delta.pendingBytes =
        bytesThreshold > 0 ? Math.max(0, delta.pendingBytes - bytesThreshold) : 0;
      delta.pendingMessages =
        messagesThreshold > 0 ? Math.max(0, delta.pendingMessages - messagesThreshold) : 0;
      shouldSync = true;
    }
    if (shouldSync) {
      void this.sync({ reason: "session-delta" }).catch((err) => {
        log.warn(`memory sync failed (session-delta): ${String(err)}`);
      });
    }
  }

  protected ensureIntervalSync() {
    const newTimer = createIntervalSyncTimer(this.settings, this.intervalTimer, () => {
      void this.sync({ reason: "interval" }).catch((err) => {
        log.warn(`memory sync failed (interval): ${String(err)}`);
      });
    });
    if (newTimer) {
      this.intervalTimer = newTimer;
    }
  }

  private scheduleWatchSync() {
    this.watchTimer = scheduleWatchSync(this.sources, this.settings, this.watchTimer, () => {
      this.watchTimer = null;
      void this.sync({ reason: "watch" }).catch((err) => {
        log.warn(`memory sync failed (watch): ${String(err)}`);
      });
    });
  }

  private shouldSyncSessions(
    params?: { reason?: string; force?: boolean; sessionFiles?: string[] },
    needsFullReindex = false,
  ) {
    return shouldSyncSessions(
      this.sources,
      this.sessionsDirty,
      this.sessionsDirtyFiles,
      params,
      needsFullReindex,
    );
  }

  private async syncMemoryFiles(params: {
    needsFullReindex: boolean;
    progress?: MemorySyncProgressState;
  }) {
    await syncMemoryFiles({
      db: this.db,
      provider: this.provider,
      settings: this.settings,
      workspaceDir: this.workspaceDir,
      fts: this.fts,
      needsFullReindex: params.needsFullReindex,
      getConcurrency: () => this.getIndexConcurrency(),
      isBatchEnabled: this.batch.enabled,
      indexFile: (entry, options) => this.indexFile(entry, options),
      progress: params.progress,
    });
  }

  private async syncSessionFiles(params: {
    needsFullReindex: boolean;
    targetSessionFiles?: string[];
    progress?: MemorySyncProgressState;
  }) {
    await syncSessionFiles({
      db: this.db,
      provider: this.provider,
      agentId: this.agentId,
      settings: this.settings,
      fts: this.fts,
      sessionsDirtyFiles: this.sessionsDirtyFiles,
      sessionDeltas: this.sessionDeltas,
      needsFullReindex: params.needsFullReindex,
      targetSessionFiles: params.targetSessionFiles,
      getConcurrency: () => this.getIndexConcurrency(),
      isBatchEnabled: this.batch.enabled,
      indexFile: (entry, options) => this.indexFile(entry, options),
      progress: params.progress,
    });
  }

  private createSyncProgress(
    onProgress: (update: MemorySyncProgressUpdate) => void,
  ): MemorySyncProgressState {
    return createSyncProgress(onProgress);
  }

  protected async runSync(params?: {
    reason?: string;
    force?: boolean;
    sessionFiles?: string[];
    progress?: (update: MemorySyncProgressUpdate) => void;
  }) {
    const progress = params?.progress ? this.createSyncProgress(params.progress) : undefined;
    if (progress) {
      progress.report({
        completed: progress.completed,
        total: progress.total,
        label: "Loading vector extension…",
      });
    }
    const vectorReady = await this.ensureVectorReady();
    const meta = this.readMeta();
    const configuredSources = this.resolveConfiguredSourcesForMeta();
    const configuredScopeHash = this.resolveConfiguredScopeHash();
    const targetSessionFiles = normalizeTargetSessionFiles(this.agentId, params?.sessionFiles);
    const hasTargetSessionFiles = targetSessionFiles !== null;
    if (hasTargetSessionFiles && targetSessionFiles && this.sources.has("sessions")) {
      // Post-compaction refreshes should only update the explicit transcript files and
      // leave broader reindex/dirty-work decisions to the regular sync path.
      try {
        await this.syncSessionFiles({
          needsFullReindex: false,
          targetSessionFiles: Array.from(targetSessionFiles),
          progress: progress ?? undefined,
        });
        this.sessionsDirty = clearSyncedSessionFiles(this.sessionsDirtyFiles, targetSessionFiles);
      } catch (err) {
        const reason = err instanceof Error ? err.message : String(err);
        const activated =
          this.shouldFallbackOnError(reason) && (await this.activateFallbackProvider(reason));
        if (activated) {
          if (
            process.env.DENEB_TEST_FAST === "1" &&
            process.env.DENEB_TEST_MEMORY_UNSAFE_REINDEX === "1"
          ) {
            await this.runUnsafeReindex({
              reason: params?.reason,
              force: true,
              progress: progress ?? undefined,
            });
          } else {
            await this.runSafeReindex({
              reason: params?.reason,
              force: true,
              progress: progress ?? undefined,
            });
          }
          return;
        }
        throw err;
      }
      return;
    }
    const needsFullReindex =
      (params?.force && !hasTargetSessionFiles) ||
      !meta ||
      (this.provider && meta.model !== this.provider.model) ||
      (this.provider && meta.provider !== this.provider.id) ||
      meta.providerKey !== this.providerKey ||
      this.metaSourcesDiffer(meta, configuredSources) ||
      meta.scopeHash !== configuredScopeHash ||
      meta.chunkTokens !== this.settings.chunking.tokens ||
      meta.chunkOverlap !== this.settings.chunking.overlap ||
      (vectorReady && !meta?.vectorDims);
    try {
      if (needsFullReindex) {
        if (
          process.env.DENEB_TEST_FAST === "1" &&
          process.env.DENEB_TEST_MEMORY_UNSAFE_REINDEX === "1"
        ) {
          await this.runUnsafeReindex({
            reason: params?.reason,
            force: params?.force,
            progress: progress ?? undefined,
          });
        } else {
          await this.runSafeReindex({
            reason: params?.reason,
            force: params?.force,
            progress: progress ?? undefined,
          });
        }
        return;
      }

      const shouldSyncMemory =
        this.sources.has("memory") &&
        ((!hasTargetSessionFiles && params?.force) || needsFullReindex || this.dirty);
      const shouldSyncSessionsFlag = this.shouldSyncSessions(params, needsFullReindex);

      if (shouldSyncMemory) {
        await this.syncMemoryFiles({ needsFullReindex, progress: progress ?? undefined });
        this.dirty = false;
      }

      if (shouldSyncSessionsFlag) {
        await this.syncSessionFiles({
          needsFullReindex,
          targetSessionFiles: targetSessionFiles ? Array.from(targetSessionFiles) : undefined,
          progress: progress ?? undefined,
        });
        this.sessionsDirty = false;
        this.sessionsDirtyFiles.clear();
      } else if (this.sessionsDirtyFiles.size > 0) {
        this.sessionsDirty = true;
      } else {
        this.sessionsDirty = false;
      }
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      const activated =
        this.shouldFallbackOnError(reason) && (await this.activateFallbackProvider(reason));
      if (activated) {
        await this.runSafeReindex({
          reason: params?.reason ?? "fallback",
          force: true,
          progress: progress ?? undefined,
        });
        return;
      }
      throw err;
    }
  }

  private shouldFallbackOnError(message: string): boolean {
    return /embedding|embeddings|batch/i.test(message);
  }

  protected resolveBatchConfig(): {
    enabled: boolean;
    wait: boolean;
    concurrency: number;
    pollIntervalMs: number;
    timeoutMs: number;
  } {
    const batch = this.settings.remote?.batch;
    const enabled = Boolean(
      batch?.enabled &&
      this.provider &&
      ((this.openAi && this.provider.id === "openai") ||
        (this.gemini && this.provider.id === "gemini") ||
        (this.voyage && this.provider.id === "voyage")),
    );
    return {
      enabled,
      wait: batch?.wait ?? true,
      concurrency: Math.max(1, batch?.concurrency ?? 2),
      pollIntervalMs: batch?.pollIntervalMs ?? 2000,
      timeoutMs: (batch?.timeoutMinutes ?? 60) * 60 * 1000,
    };
  }

  private async activateFallbackProvider(reason: string): Promise<boolean> {
    const fallback = this.settings.fallback;
    if (!fallback || fallback === "none" || !this.provider || fallback === this.provider.id) {
      return false;
    }
    if (this.fallbackFrom) {
      return false;
    }
    const fallbackFrom = this.provider.id as "openai" | "gemini" | "local" | "voyage" | "mistral";

    const fallbackModel =
      fallback === "gemini"
        ? DEFAULT_GEMINI_EMBEDDING_MODEL
        : fallback === "openai"
          ? DEFAULT_OPENAI_EMBEDDING_MODEL
          : fallback === "voyage"
            ? DEFAULT_VOYAGE_EMBEDDING_MODEL
            : fallback === "mistral"
              ? DEFAULT_MISTRAL_EMBEDDING_MODEL
              : this.settings.model;

    const fallbackResult = await createEmbeddingProvider({
      config: this.cfg,
      agentDir: resolveAgentDir(this.cfg, this.agentId),
      provider: fallback,
      remote: this.settings.remote,
      model: fallbackModel,
      outputDimensionality: this.settings.outputDimensionality,
      fallback: "none",
      local: this.settings.local,
    });

    this.fallbackFrom = fallbackFrom;
    this.fallbackReason = reason;
    this.provider = fallbackResult.provider;
    this.openAi = fallbackResult.openAi;
    this.gemini = fallbackResult.gemini;
    this.voyage = fallbackResult.voyage;
    this.mistral = fallbackResult.mistral;
    this.providerKey = this.computeProviderKey();
    this.batch = this.resolveBatchConfig();
    log.warn(`memory embeddings: switched to fallback provider (${fallback})`, { reason });
    return true;
  }

  private async runSafeReindex(params: {
    reason?: string;
    force?: boolean;
    progress?: MemorySyncProgressState;
  }): Promise<void> {
    const dbPath = resolveUserPath(this.settings.store.path);
    const tempDbPath = `${dbPath}.tmp-${randomUUID()}`;
    const tempDb = this.openDatabaseAtPath(tempDbPath);

    const originalDb = this.db;
    let originalDbClosed = false;
    const originalState = {
      ftsAvailable: this.fts.available,
      ftsError: this.fts.loadError,
      vectorAvailable: this.vector.available,
      vectorLoadError: this.vector.loadError,
      vectorDims: this.vector.dims,
      vectorReady: this.vectorReady,
    };

    const restoreOriginalState = () => {
      if (originalDbClosed) {
        this.db = this.openDatabaseAtPath(dbPath);
      } else {
        this.db = originalDb;
      }
      this.fts.available = originalState.ftsAvailable;
      this.fts.loadError = originalState.ftsError;
      this.vector.available = originalDbClosed ? null : originalState.vectorAvailable;
      this.vector.loadError = originalState.vectorLoadError;
      this.vector.dims = originalState.vectorDims;
      this.vectorReady = originalDbClosed ? null : originalState.vectorReady;
    };

    this.db = tempDb;
    this.vectorReady = null;
    this.vector.available = null;
    this.vector.loadError = undefined;
    this.vector.dims = undefined;
    this.fts.available = false;
    this.fts.loadError = undefined;
    this.ensureSchema();

    let nextMeta: MemoryIndexMeta | null = null;

    try {
      this.seedEmbeddingCache(originalDb);
      const shouldSyncMemory = this.sources.has("memory");
      const shouldSyncSessionsFlag = this.shouldSyncSessions(
        { reason: params.reason, force: params.force },
        true,
      );

      if (shouldSyncMemory) {
        await this.syncMemoryFiles({ needsFullReindex: true, progress: params.progress });
        this.dirty = false;
      }

      if (shouldSyncSessionsFlag) {
        await this.syncSessionFiles({ needsFullReindex: true, progress: params.progress });
        this.sessionsDirty = false;
        this.sessionsDirtyFiles.clear();
      } else if (this.sessionsDirtyFiles.size > 0) {
        this.sessionsDirty = true;
      } else {
        this.sessionsDirty = false;
      }

      nextMeta = {
        model: this.provider?.model ?? "fts-only",
        provider: this.provider?.id ?? "none",
        providerKey: this.providerKey!,
        sources: this.resolveConfiguredSourcesForMeta(),
        scopeHash: this.resolveConfiguredScopeHash(),
        chunkTokens: this.settings.chunking.tokens,
        chunkOverlap: this.settings.chunking.overlap,
      };
      if (!nextMeta) {
        throw new Error("Failed to compute memory index metadata for reindexing.");
      }

      if (this.vector.available && this.vector.dims) {
        nextMeta.vectorDims = this.vector.dims;
      }

      this.writeMeta(nextMeta);
      this.pruneEmbeddingCacheIfNeeded?.();

      this.db.close();
      originalDb.close();
      originalDbClosed = true;

      await this.swapIndexFiles(dbPath, tempDbPath);

      this.db = this.openDatabaseAtPath(dbPath);
      this.vectorReady = null;
      this.vector.available = null;
      this.vector.loadError = undefined;
      this.ensureSchema();
      this.vector.dims = nextMeta?.vectorDims;
    } catch (err) {
      try {
        this.db.close();
      } catch {
        // Best-effort close before restoring the original DB; the primary error is rethrown.
      }
      await this.removeIndexFiles(tempDbPath);
      restoreOriginalState();
      throw err;
    }
  }

  private async runUnsafeReindex(params: {
    reason?: string;
    force?: boolean;
    progress?: MemorySyncProgressState;
  }): Promise<void> {
    // Perf: for test runs, skip atomic temp-db swapping. The index is isolated
    // under the per-test HOME anyway, and this cuts substantial fs+sqlite churn.
    this.resetIndex();

    const shouldSyncMemory = this.sources.has("memory");
    const shouldSyncSessionsFlag = this.shouldSyncSessions(
      { reason: params.reason, force: params.force },
      true,
    );

    if (shouldSyncMemory) {
      await this.syncMemoryFiles({ needsFullReindex: true, progress: params.progress });
      this.dirty = false;
    }

    if (shouldSyncSessionsFlag) {
      await this.syncSessionFiles({ needsFullReindex: true, progress: params.progress });
      this.sessionsDirty = false;
      this.sessionsDirtyFiles.clear();
    } else if (this.sessionsDirtyFiles.size > 0) {
      this.sessionsDirty = true;
    } else {
      this.sessionsDirty = false;
    }

    const nextMeta: MemoryIndexMeta = {
      model: this.provider?.model ?? "fts-only",
      provider: this.provider?.id ?? "none",
      providerKey: this.providerKey!,
      sources: this.resolveConfiguredSourcesForMeta(),
      scopeHash: this.resolveConfiguredScopeHash(),
      chunkTokens: this.settings.chunking.tokens,
      chunkOverlap: this.settings.chunking.overlap,
    };
    if (this.vector.available && this.vector.dims) {
      nextMeta.vectorDims = this.vector.dims;
    }

    this.writeMeta(nextMeta);
    this.pruneEmbeddingCacheIfNeeded?.();
  }

  private resetIndex() {
    this.db.exec(`DELETE FROM files`);
    this.db.exec(`DELETE FROM chunks`);
    if (this.fts.enabled && this.fts.available) {
      try {
        this.db.exec(`DELETE FROM ${FTS_TABLE}`);
      } catch (err) {
        log.debug?.(`failed to reset FTS table: ${String(err)}`);
      }
    }
    dropVectorTable(this.vector, this.db);
    this.vector.dims = undefined;
    this.sessionsDirtyFiles.clear();
  }

  protected readMeta(): MemoryIndexMeta | null {
    return readMetaHelper(this.db, this.lastMetaSerializedRef);
  }

  protected writeMeta(meta: MemoryIndexMeta) {
    writeMetaHelper(this.db, meta, this.lastMetaSerializedRef);
  }

  private resolveConfiguredSourcesForMeta(): MemorySource[] {
    return resolveConfiguredSourcesForMetaHelper(this.sources);
  }

  private resolveConfiguredScopeHash(): string {
    return resolveConfiguredScopeHashHelper(this.workspaceDir, this.settings);
  }

  private metaSourcesDiffer(meta: MemoryIndexMeta, configuredSources: MemorySource[]): boolean {
    return metaSourcesDiffer(meta, configuredSources);
  }
}
