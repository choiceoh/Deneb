import { detectGpu } from "./gpu-detect.js";
import { PERF } from "./hardware-profile.js";

// Re-export SQLite pragmas so existing consumers keep working.
export { SQLITE_PERF_PRAGMAS, applySqlitePerfPragmas } from "./sqlite-perf-pragmas.js";

/**
 * One-time environment tuning based on detected hardware.
 * Call early in process startup (before libuv pool is created).
 *
 * - Sets UV_THREADPOOL_SIZE to match hardware profile
 * - Configures V8 heap size for unified memory systems
 * - Logs GPU detection results for diagnostics
 */
export function applyDgxSparkEnvTuning(): void {
  // UV_THREADPOOL_SIZE must be set before any async I/O — libuv reads it once
  if (!process.env.UV_THREADPOOL_SIZE) {
    process.env.UV_THREADPOOL_SIZE = String(PERF.uvThreadPoolSize);
  }

  // V8 heap tuning: on DGX Spark GB10 the 128GB unified memory allows a large heap.
  // This reduces GC pauses during heavy embedding/context work.
  if (PERF.v8MaxOldSpaceMb > 0 && !process.env.DENEB_V8_MAX_OLD_SPACE_CONFIGURED) {
    process.env.DENEB_V8_MAX_OLD_SPACE_CONFIGURED = "1";
    // Note: --max-old-space-size must be set via NODE_OPTIONS before process start,
    // or via process respawn. We set the env hint for the respawn path in entry.ts.
    const current = process.env.NODE_OPTIONS ?? "";
    if (!current.includes("--max-old-space-size")) {
      process.env.NODE_OPTIONS = `${current} --max-old-space-size=${PERF.v8MaxOldSpaceMb}`.trim();
    }
  }

  // Warm the GPU detection cache (synchronous, runs nvidia-smi once)
  const gpu = detectGpu();
  if (gpu.available && !process.env.VITEST) {
    const parts = [`GPU: ${gpu.gpuName ?? "detected"}`];
    if (gpu.cudaVersion) {
      parts.push(`CUDA ${gpu.cudaVersion}`);
    }
    if (gpu.memoryMb) {
      parts.push(`${gpu.memoryMb}MB VRAM`);
    }
    parts.push(`profile=${PERF.name}`);
    // Low-level log; subsystem logger may not be initialized yet
    process.stderr.write(`[deneb] ${parts.join(", ")}\n`);
  }
}
