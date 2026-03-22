import { PERF } from "../infra/hardware-profile.js";

// DGX SPARK: larger buffer + GPU-accelerated encoding allows longer timeouts
export const MEDIA_FFMPEG_MAX_BUFFER_BYTES = PERF.ffmpegMaxBufferBytes;
export const MEDIA_FFPROBE_TIMEOUT_MS = PERF.ffprobeTimeoutMs;
export const MEDIA_FFMPEG_TIMEOUT_MS = PERF.ffmpegTimeoutMs;
export const MEDIA_FFMPEG_MAX_AUDIO_DURATION_SECS = 30 * 60; // 30 min (up from 20 min)
