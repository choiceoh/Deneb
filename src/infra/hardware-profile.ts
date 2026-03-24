import os from "node:os";
import { isCudaAvailable } from "./gpu-detect.js";

/**
 * Performance profile for hardware-aware tuning.
 *
 * Two profiles:
 * - GPU (CUDA): optimized for NVIDIA GPU systems (DGX SPARK, etc.)
 * - CPU: conservative defaults for systems without GPU acceleration
 */

export type PerformanceProfile = {
  // Agent concurrency
  agentMaxConcurrent: number;
  subagentMaxConcurrent: number;

  // Media processing
  ffmpegMaxBufferBytes: number;
  ffmpegTimeoutMs: number;
  ffprobeTimeoutMs: number;
  imageWorkerCount: number;

  // Memory/embedding
  embeddingBatchConcurrency: number;

  // Model scanning
  modelScanConcurrency: number;

  // SQLite
  sqliteCacheKb: number;
  sqliteMmapBytes: number;

  // FFmpeg GPU acceleration
  ffmpegHwAccel: "cuda" | "none";
  ffmpegVideoEncoder: string;
  ffmpegVideoDecoder: string;

  // Node.js tuning
  uvThreadPoolSize: number;
};

const cpuCores = os.cpus().length;

/**
 * CUDA GPU profile — optimized for NVIDIA GPU systems.
 *
 * What changed from CPU defaults (and why):
 * - Concurrency: 2x agents/subagents — GPU handles parallel well
 * - FFmpeg: CUDA NVENC/NVDEC — hardware encode/decode is 5-10x faster
 * - FFmpeg buffer: 32MB (up from 10MB) — avoids re-reads on larger media
 * - Embedding batch: 6 concurrent (up from 2) — SGLang handles parallel well
 * - SQLite: 128MB cache + 512MB mmap — keeps hot data in memory
 * - UV threadpool: 2x cores — saturates async I/O on many-core systems
 */
const GPU_PROFILE: PerformanceProfile = {
  agentMaxConcurrent: Math.min(cpuCores, 8),
  subagentMaxConcurrent: Math.min(cpuCores, 16),

  ffmpegMaxBufferBytes: 32 * 1024 * 1024, // 32MB
  ffmpegTimeoutMs: 90_000, // GPU encoding is faster; generous for long media
  ffprobeTimeoutMs: 10_000,
  imageWorkerCount: Math.min(Math.ceil(cpuCores / 2), 6),

  embeddingBatchConcurrency: 6,
  modelScanConcurrency: 6,

  sqliteCacheKb: 128_000, // 128MB cache
  sqliteMmapBytes: 512 * 1024 * 1024, // 512MB mmap

  // CUDA-accelerated FFmpeg
  ffmpegHwAccel: "cuda",
  ffmpegVideoEncoder: "h264_nvenc",
  ffmpegVideoDecoder: "h264_cuvid",

  uvThreadPoolSize: Math.min(cpuCores * 2, 32),
};

/**
 * CPU-only profile — conservative defaults for systems without GPU.
 */
const CPU_PROFILE: PerformanceProfile = {
  agentMaxConcurrent: Math.min(cpuCores, 4),
  subagentMaxConcurrent: Math.min(cpuCores, 8),

  ffmpegMaxBufferBytes: 10 * 1024 * 1024, // 10MB
  ffmpegTimeoutMs: 120_000, // CPU encoding needs more time
  ffprobeTimeoutMs: 10_000,
  imageWorkerCount: Math.min(Math.ceil(cpuCores / 4), 4),

  embeddingBatchConcurrency: 2,
  modelScanConcurrency: 4,

  sqliteCacheKb: 64_000, // 64MB cache
  sqliteMmapBytes: 256 * 1024 * 1024, // 256MB mmap

  ffmpegHwAccel: "none",
  ffmpegVideoEncoder: "libx264",
  ffmpegVideoDecoder: "", // Use FFmpeg default
  uvThreadPoolSize: Math.min(cpuCores, 16),
};

let cachedProfile: PerformanceProfile | null = null;

function resolveProfile(): PerformanceProfile {
  if (isCudaAvailable()) {
    return GPU_PROFILE;
  }
  return CPU_PROFILE;
}

/**
 * Active performance profile, auto-detected based on GPU availability.
 * Override with `DENEB_GPU_ACCEL=cuda|none` env var.
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
