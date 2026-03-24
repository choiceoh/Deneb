import { detectGpu } from "./gpu-detect.js";
import { PERF } from "./hardware-profile.js";

// Re-export SQLite pragmas so existing consumers keep working.
export { SQLITE_PERF_PRAGMAS, applySqlitePerfPragmas } from "./sqlite-perf-pragmas.js";

/**
 * One-time environment tuning based on detected hardware.
 * Call early in process startup (before libuv pool is created).
 *
 * - Sets UV_THREADPOOL_SIZE to match hardware profile
 * - Logs GPU detection results for diagnostics
 */
export function applyDgxSparkEnvTuning(): void {
  // UV_THREADPOOL_SIZE must be set before any async I/O — libuv reads it once
  if (!process.env.UV_THREADPOOL_SIZE) {
    process.env.UV_THREADPOOL_SIZE = String(PERF.uvThreadPoolSize);
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
    // Low-level log; subsystem logger may not be initialized yet
    process.stderr.write(`[deneb] ${parts.join(", ")}\n`);
  }
}
