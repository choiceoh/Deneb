import { PERF } from "./hardware-profile.js";

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
 * Compatible with better-sqlite3 / bun:sqlite / node:sqlite exec interface.
 */
export function applySqlitePerfPragmas(db: { exec: (sql: string) => void }): void {
  for (const pragma of SQLITE_PERF_PRAGMAS) {
    db.exec(pragma);
  }
}
