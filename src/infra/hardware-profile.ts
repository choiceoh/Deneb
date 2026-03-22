import os from "node:os";

/**
 * DGX SPARK performance profile.
 *
 * Single-user DGX SPARK + CUDA + SGLang environment.
 * Focus: speed (lower latency, higher concurrency, GPU offload).
 * Limits kept close to upstream defaults to avoid stability issues.
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
 * Speed-focused profile for DGX SPARK.
 *
 * What changed from defaults (and why):
 * - Concurrency: 2x agents/subagents — single-user, no contention
 * - FFmpeg: CUDA NVENC/NVDEC — hardware encode/decode is 5-10x faster
 * - FFmpeg buffer: 32MB (up from 10MB) — avoids re-reads on larger media
 * - Embedding batch: 6 concurrent (up from 2) — SGLang handles parallel well
 * - SQLite: 128MB cache + 512MB mmap — keeps hot data in memory
 * - UV threadpool: 2x cores — saturates async I/O on many-core ARM
 */
export const PERF: PerformanceProfile = {
  agentMaxConcurrent: Math.min(cpuCores, 8),
  subagentMaxConcurrent: Math.min(cpuCores, 16),

  ffmpegMaxBufferBytes: 32 * 1024 * 1024, // 32MB (up from 10MB)
  ffmpegTimeoutMs: 90_000, // GPU encoding is faster; generous for long media
  ffprobeTimeoutMs: 10_000,
  imageWorkerCount: Math.min(Math.ceil(cpuCores / 2), 6),

  embeddingBatchConcurrency: 6,
  modelScanConcurrency: 6,

  sqliteCacheKb: 128_000, // 128MB cache (up from ~64MB default)
  sqliteMmapBytes: 512 * 1024 * 1024, // 512MB mmap

  // CUDA-accelerated FFmpeg
  ffmpegHwAccel: "cuda",
  ffmpegVideoEncoder: "h264_nvenc",
  ffmpegVideoDecoder: "h264_cuvid",

  uvThreadPoolSize: Math.min(cpuCores * 2, 32),
};

export function getCachedPerformanceProfile(): PerformanceProfile {
  return PERF;
}
