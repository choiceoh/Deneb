import { PERF } from "./hardware-profile.js";

/**
 * One-time DGX SPARK environment tuning.
 * Call early in process startup (before libuv pool is created).
 *
 * - Sets UV_THREADPOOL_SIZE to saturate Grace CPU cores for async I/O
 * - Configures Node.js memory limits for 128GB unified memory
 */
export function applyDgxSparkEnvTuning(): void {
  // UV_THREADPOOL_SIZE must be set before any async I/O — libuv reads it once
  if (!process.env.UV_THREADPOOL_SIZE) {
    process.env.UV_THREADPOOL_SIZE = String(PERF.uvThreadPoolSize);
  }
}

/**
 * SQLite PRAGMA statements for DGX SPARK.
 * Apply these after opening a database connection.
 */
export const SQLITE_PERF_PRAGMAS = [
  `PRAGMA cache_size = -${PERF.sqliteCacheKb}`, // negative = KB
  `PRAGMA mmap_size = ${PERF.sqliteMmapBytes}`,
  "PRAGMA journal_mode = WAL",
  "PRAGMA synchronous = NORMAL",
  "PRAGMA temp_store = MEMORY",
  "PRAGMA wal_autocheckpoint = 10000", // Fewer checkpoints with fast storage
  "PRAGMA page_size = 8192", // Larger pages for sequential reads
] as const;

/**
 * Apply SQLite performance pragmas to a database connection.
 * Compatible with better-sqlite3 / bun:sqlite exec interface.
 */
export function applySqlitePerfPragmas(db: { exec: (sql: string) => void }): void {
  for (const pragma of SQLITE_PERF_PRAGMAS) {
    db.exec(pragma);
  }
}
