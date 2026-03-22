import { PERF } from "./hardware-profile.js";

// Re-export SQLite pragmas so existing consumers keep working.
export { SQLITE_PERF_PRAGMAS, applySqlitePerfPragmas } from "./sqlite-perf-pragmas.js";

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
