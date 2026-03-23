import type { DatabaseSync } from "node:sqlite";
import { ResolvedMemorySearchConfig } from "../agents/memory-search.js";
import { type DenebConfig } from "../config/config.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import {
  type EmbeddingProvider,
  type GeminiEmbeddingClient,
  type MistralEmbeddingClient,
  type OpenAiEmbeddingClient,
  type VoyageEmbeddingClient,
} from "./embeddings.js";
import { hashText, normalizeExtraMemoryPaths } from "./internal.js";
import type { MemorySource, MemorySyncProgressUpdate } from "./types.js";

export type MemoryIndexMeta = {
  model: string;
  provider: string;
  providerKey?: string;
  sources?: MemorySource[];
  scopeHash?: string;
  chunkTokens: number;
  chunkOverlap: number;
  vectorDims?: number;
};

export type MemorySyncProgressState = {
  completed: number;
  total: number;
  label?: string;
  report: (update: MemorySyncProgressUpdate) => void;
};

export const META_KEY = "memory_index_meta_v1";
export const VECTOR_TABLE = "chunks_vec";
export const FTS_TABLE = "chunks_fts";
export const EMBEDDING_CACHE_TABLE = "embedding_cache";
export const VECTOR_LOAD_TIMEOUT_MS = 30_000;

export const log = createSubsystemLogger("memory");

/**
 * Abstract base class providing memory index metadata read/write operations
 * and shared state declarations used throughout the sync chain.
 */
export abstract class MemoryManagerSyncMeta {
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
  protected fallbackReason?: string;
  protected closed = false;
  protected dirty = false;
  protected sessionsDirty = false;
  protected sessionsDirtyFiles = new Set<string>();
  private lastMetaSerialized: string | null = null;

  protected abstract readonly cache: { enabled: boolean; maxEntries?: number };
  protected abstract db: DatabaseSync;
  protected abstract computeProviderKey(): string;

  protected buildSourceFilter(alias?: string): { sql: string; params: MemorySource[] } {
    const sources = Array.from(this.sources);
    if (sources.length === 0) {
      return { sql: "", params: [] };
    }
    const column = alias ? `${alias}.source` : "source";
    const placeholders = sources.map(() => "?").join(", ");
    return { sql: ` AND ${column} IN (${placeholders})`, params: sources };
  }

  protected readMeta(): MemoryIndexMeta | null {
    const row = this.db.prepare(`SELECT value FROM meta WHERE key = ?`).get(META_KEY) as
      | { value: string }
      | undefined;
    if (!row?.value) {
      this.lastMetaSerialized = null;
      return null;
    }
    try {
      const parsed = JSON.parse(row.value) as MemoryIndexMeta;
      this.lastMetaSerialized = row.value;
      return parsed;
    } catch {
      this.lastMetaSerialized = null;
      return null;
    }
  }

  protected writeMeta(meta: MemoryIndexMeta) {
    const value = JSON.stringify(meta);
    if (this.lastMetaSerialized === value) {
      return;
    }
    this.db
      .prepare(
        `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
      )
      .run(META_KEY, value);
    this.lastMetaSerialized = value;
  }

  protected resolveConfiguredSourcesForMeta(): MemorySource[] {
    const normalized = Array.from(this.sources)
      .filter((source): source is MemorySource => source === "memory" || source === "sessions")
      .toSorted();
    return normalized.length > 0 ? normalized : ["memory"];
  }

  protected normalizeMetaSources(meta: MemoryIndexMeta): MemorySource[] {
    if (!Array.isArray(meta.sources)) {
      // Backward compatibility for older indexes that did not persist sources.
      return ["memory"];
    }
    const normalized = Array.from(
      new Set(
        meta.sources.filter(
          (source): source is MemorySource => source === "memory" || source === "sessions",
        ),
      ),
    ).toSorted();
    return normalized.length > 0 ? normalized : ["memory"];
  }

  protected resolveConfiguredScopeHash(): string {
    const extraPaths = normalizeExtraMemoryPaths(this.workspaceDir, this.settings.extraPaths)
      .map((value) => value.replace(/\\/g, "/"))
      .toSorted();
    return hashText(
      JSON.stringify({
        extraPaths,
        multimodal: {
          enabled: this.settings.multimodal.enabled,
          modalities: [...this.settings.multimodal.modalities].toSorted(),
          maxFileBytes: this.settings.multimodal.maxFileBytes,
        },
      }),
    );
  }

  protected metaSourcesDiffer(meta: MemoryIndexMeta, configuredSources: MemorySource[]): boolean {
    const metaSources = this.normalizeMetaSources(meta);
    if (metaSources.length !== configuredSources.length) {
      return true;
    }
    return metaSources.some((source, index) => source !== configuredSources[index]);
  }
}
