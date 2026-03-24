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
 * Strip GPU-specific flags and restore software encoders.
 * Used as a fallback when CUDA encoding fails.
 */
export function buildCpuFallbackArgs(args: string[]): string[] {
  const filtered: string[] = [];
  let skip = 0;

  for (let i = 0; i < args.length; i++) {
    if (skip > 0) {
      skip--;
      continue;
    }

    // Remove -hwaccel <value> and -hwaccel_output_format <value> pairs
    if (args[i] === "-hwaccel" || args[i] === "-hwaccel_output_format") {
      skip = 1; // Skip the next argument (the value)
      continue;
    }

    // Swap NVENC encoders back to software
    if (args[i] === "h264_nvenc") {
      filtered.push("libx264");
      continue;
    }
    if (args[i] === "hevc_nvenc") {
      filtered.push("libx265");
      continue;
    }
    // Remove NVDEC decoder references
    if (args[i] === "h264_cuvid" || args[i] === "hevc_cuvid") {
      continue;
    }

    filtered.push(args[i]);
  }

  return filtered;
}

/**
 * Optimized FFmpeg timeout for GPU-accelerated encoding.
 */
export function getOptimizedFfmpegTimeout(): number {
  return PERF.ffmpegTimeoutMs;
}

/**
 * Optimized FFmpeg max buffer.
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
