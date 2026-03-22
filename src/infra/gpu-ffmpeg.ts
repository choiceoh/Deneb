import { PERF } from "./hardware-profile.js";

/**
 * Build FFmpeg arguments with CUDA hardware acceleration.
 * Prepends NVDEC decode flags before the first -i input and
 * swaps software encoders for NVENC equivalents.
 */
export function buildGpuFfmpegArgs(args: string[]): string[] {
  if (PERF.ffmpegHwAccel !== "cuda") {
    return args;
  }

  const enhanced: string[] = [];
  let hasInputFlag = false;

  for (const arg of args) {
    // Insert hwaccel flags before the first -i (input file)
    if (arg === "-i" && !hasInputFlag) {
      hasInputFlag = true;
      enhanced.push("-hwaccel", "cuda", "-hwaccel_output_format", "cuda");
    }
    enhanced.push(arg);
  }

  // Swap software encoders for NVENC
  return enhanced.map((arg) => {
    if (arg === "libx264") {
      return "h264_nvenc";
    }
    if (arg === "libx265") {
      return "hevc_nvenc";
    }
    return arg;
  });
}

/**
 * Optimized FFmpeg timeout for GPU-accelerated encoding.
 */
export function getOptimizedFfmpegTimeout(): number {
  return PERF.ffmpegTimeoutMs;
}

/**
 * Optimized FFmpeg max buffer for DGX SPARK.
 */
export function getOptimizedFfmpegMaxBuffer(): number {
  return PERF.ffmpegMaxBufferBytes;
}

/**
 * Optimized FFprobe timeout.
 */
export function getOptimizedFfprobeTimeout(): number {
  return PERF.ffprobeTimeoutMs;
}
