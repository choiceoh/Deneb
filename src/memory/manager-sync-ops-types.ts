// Shared types and constants for MemoryManagerSyncOps split modules.

export type MemoryIndexMeta = {
  model: string;
  provider: string;
  providerKey?: string;
  sources?: import("./types.js").MemorySource[];
  scopeHash?: string;
  chunkTokens: number;
  chunkOverlap: number;
  vectorDims?: number;
};

export type MemorySyncProgressState = {
  completed: number;
  total: number;
  label?: string;
  report: (update: import("./types.js").MemorySyncProgressUpdate) => void;
};

export const META_KEY = "memory_index_meta_v1";
export const VECTOR_TABLE = "chunks_vec";
export const FTS_TABLE = "chunks_fts";
export const EMBEDDING_CACHE_TABLE = "embedding_cache";
export const SESSION_DIRTY_DEBOUNCE_MS = 2000; // Faster dirty detection (down from 5s)
export const SESSION_DELTA_READ_CHUNK_BYTES = 256 * 1024; // Larger read chunks (up from 64KB)
export const VECTOR_LOAD_TIMEOUT_MS = 30_000;
export const IGNORED_MEMORY_WATCH_DIR_NAMES = new Set([
  ".git",
  "node_modules",
  ".pnpm-store",
  ".venv",
  "venv",
  ".tox",
  "__pycache__",
]);
