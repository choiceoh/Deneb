import { PERF } from "./hardware-profile.js";

/**
 * Encoder swap map: software → NVENC hardware equivalent.
 * Blackwell supports H.264, HEVC, and AV1 hardware encoding.
 */
const NVENC_ENCODER_MAP: Record<string, string> = {
  libx264: "h264_nvenc",
  libx265: "hevc_nvenc",
  libaom: "av1_nvenc",
  "libaom-av1": "av1_nvenc",
  libsvtav1: "av1_nvenc",
};

/** Reverse map: NVENC → software fallback. */
const CPU_ENCODER_MAP: Record<string, string> = {
  h264_nvenc: "libx264",
  hevc_nvenc: "libx265",
  av1_nvenc: "libsvtav1",
};

/** NVDEC decoder names to strip during CPU fallback. */
const CUVID_DECODERS = new Set(["h264_cuvid", "hevc_cuvid", "av1_cuvid", "vp9_cuvid"]);

/**
 * Build FFmpeg arguments with CUDA hardware acceleration.
 *
 * - Prepends NVDEC decode flags before the first -i input
 * - Swaps software encoders for NVENC equivalents
 * - Appends profile-specific output flags (preset, tune, rc)
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
  const swapped = enhanced.map((arg) => NVENC_ENCODER_MAP[arg] ?? arg);

  // Append profile-specific output flags (e.g., -preset p4 -tune hq)
  if (PERF.ffmpegExtraOutputFlags.length > 0) {
    swapped.push(...PERF.ffmpegExtraOutputFlags);
  }

  return swapped;
}

/**
 * Strip GPU-specific flags and restore software encoders.
 * Used as a fallback when CUDA encoding fails.
 */
export function buildCpuFallbackArgs(args: string[]): string[] {
  const filtered: string[] = [];
  let skip = 0;

  // Collect flags to strip that come from ffmpegExtraOutputFlags
  const extraFlagsSet = new Set(PERF.ffmpegExtraOutputFlags);

  for (let i = 0; i < args.length; i++) {
    if (skip > 0) {
      skip--;
      continue;
    }

    const arg = args[i];

    // Remove -hwaccel <value> and -hwaccel_output_format <value> pairs
    if (arg === "-hwaccel" || arg === "-hwaccel_output_format") {
      skip = 1;
      continue;
    }

    // Swap NVENC encoders back to software
    if (CPU_ENCODER_MAP[arg]) {
      filtered.push(CPU_ENCODER_MAP[arg]);
      continue;
    }

    // Remove NVDEC decoder references
    if (CUVID_DECODERS.has(arg)) {
      continue;
    }

    // Remove GPU-specific extra flags (e.g., -preset p4 -tune hq -rc vbr)
    // These are flag-value pairs: -preset p4
    if ((arg === "-preset" || arg === "-tune" || arg === "-rc") && extraFlagsSet.has(arg)) {
      skip = 1; // Skip the value
      continue;
    }

    filtered.push(arg);
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
