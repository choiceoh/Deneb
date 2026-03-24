import os from "node:os";
import { detectGpu, type GpuTier } from "./gpu-detect.js";

/**
 * Performance profile for hardware-aware tuning.
 *
 * Three tiers:
 * - DGX Spark GB10: Grace Blackwell, 128GB unified memory, single-user desktop
 * - Desktop GPU: generic NVIDIA GPU with discrete VRAM
 * - CPU: conservative defaults for systems without GPU acceleration
 */

export type PerformanceProfile = {
  /** Human-readable profile name for diagnostics. */
  name: string;

  // Agent concurrency
  agentMaxConcurrent: number;
  subagentMaxConcurrent: number;

  // Media processing
  ffmpegMaxBufferBytes: number;
  ffmpegTimeoutMs: number;
  ffprobeTimeoutMs: number;
  imageWorkerCount: number;
  /** Max audio duration allowed for processing (seconds). */
  ffmpegMaxAudioDurationSecs: number;

  // Memory/embedding
  embeddingBatchConcurrency: number;
  /** Max tokens per embedding batch. Larger batches are more GPU-efficient. */
  embeddingBatchMaxTokens: number;

  // Model scanning
  modelScanConcurrency: number;

  // SQLite
  sqliteCacheKb: number;
  sqliteMmapBytes: number;

  // FFmpeg GPU acceleration
  ffmpegHwAccel: "cuda" | "none";
  ffmpegVideoEncoder: string;
  ffmpegVideoDecoder: string;
  /** Extra FFmpeg output flags for quality/speed tuning. */
  ffmpegExtraOutputFlags: string[];

  // Node.js tuning
  uvThreadPoolSize: number;
  /** V8 max old generation size in MB (--max-old-space-size). 0 = use default. */
  v8MaxOldSpaceMb: number;
};

const cpuCores = os.cpus().length;
const totalMemoryMb = Math.floor(os.totalmem() / (1024 * 1024));

/**
 * DGX Spark GB10 profile — optimized for Grace Blackwell desktop supercomputer.
 *
 * Hardware: 10-core Grace CPU + Blackwell GPU, 128GB unified LPDDR5x, NVMe SSD.
 * Single-user, no contention. Unified memory means GPU and CPU share the same pool.
 *
 * Tuning rationale:
 * - Agent concurrency: 10 agents, 20 subagents — matches 10 Grace cores, GPU
 *   inference handles the heavy lifting so CPU cores are mostly I/O-bound
 * - FFmpeg: NVENC with Blackwell presets (p4 = quality/speed sweet spot).
 *   AV1 NVENC available but H.264 used as default for broadest compatibility.
 * - FFmpeg buffer: 64MB — unified memory is abundant, avoids re-reads
 * - Embedding: 8 concurrent batches, 16K tokens — Blackwell tensor cores are fast
 * - SQLite: 256MB cache + 1GB mmap — unified memory allows generous caching
 * - UV threadpool: 20 (2x 10 cores) — saturates async I/O on Grace ARM cores
 * - V8 heap: 8GB — plenty of headroom from 128GB unified memory
 * - Audio duration: 60 min — NVENC is fast enough for long media
 */
const DGX_SPARK_PROFILE: PerformanceProfile = {
  name: "dgx-spark-gb10",

  agentMaxConcurrent: 10,
  subagentMaxConcurrent: 20,

  ffmpegMaxBufferBytes: 64 * 1024 * 1024, // 64MB — unified memory is plentiful
  ffmpegTimeoutMs: 60_000, // Blackwell NVENC is very fast
  ffprobeTimeoutMs: 8_000,
  imageWorkerCount: 5, // ceil(10 cores / 2)
  ffmpegMaxAudioDurationSecs: 60 * 60, // 60 min (up from 30 min)

  embeddingBatchConcurrency: 8, // Blackwell tensor cores handle parallel well
  embeddingBatchMaxTokens: 16_000, // Larger batches for GPU efficiency

  modelScanConcurrency: 8,

  sqliteCacheKb: 256_000, // 256MB — unified memory allows generous caching
  sqliteMmapBytes: 1024 * 1024 * 1024, // 1GB mmap — NVMe + unified memory

  // Blackwell NVENC with quality preset p4 (balanced speed/quality)
  ffmpegHwAccel: "cuda",
  ffmpegVideoEncoder: "h264_nvenc",
  ffmpegVideoDecoder: "h264_cuvid",
  ffmpegExtraOutputFlags: ["-preset", "p4", "-tune", "hq", "-rc", "vbr"],

  uvThreadPoolSize: 20, // 2x 10 Grace cores
  v8MaxOldSpaceMb: 8_192, // 8GB from 128GB unified memory
};

/**
 * Generic desktop NVIDIA GPU profile.
 *
 * For consumer/workstation GPUs with discrete VRAM (e.g., RTX 3090, 4090).
 * More conservative than DGX Spark since VRAM and system memory are separate.
 */
const DESKTOP_GPU_PROFILE: PerformanceProfile = {
  name: "desktop-gpu",

  agentMaxConcurrent: Math.min(cpuCores, 8),
  subagentMaxConcurrent: Math.min(cpuCores, 16),

  ffmpegMaxBufferBytes: 32 * 1024 * 1024, // 32MB
  ffmpegTimeoutMs: 90_000,
  ffprobeTimeoutMs: 10_000,
  imageWorkerCount: Math.min(Math.ceil(cpuCores / 2), 6),
  ffmpegMaxAudioDurationSecs: 30 * 60, // 30 min

  embeddingBatchConcurrency: 6,
  embeddingBatchMaxTokens: 12_000,

  modelScanConcurrency: 6,

  sqliteCacheKb: 128_000, // 128MB cache
  sqliteMmapBytes: 512 * 1024 * 1024, // 512MB mmap

  ffmpegHwAccel: "cuda",
  ffmpegVideoEncoder: "h264_nvenc",
  ffmpegVideoDecoder: "h264_cuvid",
  ffmpegExtraOutputFlags: [],

  uvThreadPoolSize: Math.min(cpuCores * 2, 32),
  v8MaxOldSpaceMb: Math.min(Math.floor(totalMemoryMb * 0.25), 4_096),
};

/**
 * CPU-only profile — conservative defaults for systems without GPU.
 */
const CPU_PROFILE: PerformanceProfile = {
  name: "cpu",

  agentMaxConcurrent: Math.min(cpuCores, 4),
  subagentMaxConcurrent: Math.min(cpuCores, 8),

  ffmpegMaxBufferBytes: 10 * 1024 * 1024, // 10MB
  ffmpegTimeoutMs: 120_000, // CPU encoding needs more time
  ffprobeTimeoutMs: 10_000,
  imageWorkerCount: Math.min(Math.ceil(cpuCores / 4), 4),
  ffmpegMaxAudioDurationSecs: 20 * 60, // 20 min

  embeddingBatchConcurrency: 2,
  embeddingBatchMaxTokens: 8_000,

  modelScanConcurrency: 4,

  sqliteCacheKb: 64_000, // 64MB cache
  sqliteMmapBytes: 256 * 1024 * 1024, // 256MB mmap

  ffmpegHwAccel: "none",
  ffmpegVideoEncoder: "libx264",
  ffmpegVideoDecoder: "", // Use FFmpeg default
  ffmpegExtraOutputFlags: [],

  uvThreadPoolSize: Math.min(cpuCores, 16),
  v8MaxOldSpaceMb: 0, // Use Node.js default
};

const PROFILES_BY_TIER: Record<GpuTier, PerformanceProfile> = {
  "dgx-spark": DGX_SPARK_PROFILE,
  desktop: DESKTOP_GPU_PROFILE,
  none: CPU_PROFILE,
};

let cachedProfile: PerformanceProfile | null = null;

function resolveProfile(): PerformanceProfile {
  const gpu = detectGpu();
  return PROFILES_BY_TIER[gpu.tier];
}

/**
 * Active performance profile, auto-detected based on GPU availability and tier.
 *
 * Override with `DENEB_GPU_ACCEL` env var:
 * - `"dgx-spark"` — force DGX Spark GB10 profile
 * - `"cuda"` / `"1"` — force desktop GPU profile
 * - `"none"` / `"0"` — force CPU-only profile
 */
export const PERF: PerformanceProfile = resolveProfile();

export function getCachedPerformanceProfile(): PerformanceProfile {
  if (!cachedProfile) {
    cachedProfile = resolveProfile();
  }
  return cachedProfile;
}

/**
 * Reset cached profile (for testing).
 * @internal
 */
export function _resetProfileCache(): void {
  cachedProfile = null;
}
