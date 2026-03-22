import { execFile, type ExecFileOptions } from "node:child_process";
import { promisify } from "node:util";
import {
  buildGpuFfmpegArgs,
  getOptimizedFfmpegMaxBuffer,
  getOptimizedFfmpegTimeout,
  getOptimizedFfprobeTimeout,
} from "../infra/gpu-ffmpeg.js";

const execFileAsync = promisify(execFile);

export type MediaExecOptions = {
  timeoutMs?: number;
  maxBufferBytes?: number;
};

function resolveExecOptions(
  defaultTimeoutMs: number,
  options: MediaExecOptions | undefined,
): ExecFileOptions {
  return {
    timeout: options?.timeoutMs ?? defaultTimeoutMs,
    maxBuffer: options?.maxBufferBytes ?? getOptimizedFfmpegMaxBuffer(),
  };
}

export async function runFfprobe(args: string[], options?: MediaExecOptions): Promise<string> {
  const { stdout } = await execFileAsync(
    "ffprobe",
    args,
    resolveExecOptions(getOptimizedFfprobeTimeout(), options),
  );
  return stdout.toString();
}

export async function runFfmpeg(args: string[], options?: MediaExecOptions): Promise<string> {
  // Apply GPU acceleration flags when available (NVENC/NVDEC on DGX SPARK)
  const enhancedArgs = buildGpuFfmpegArgs(args);
  const { stdout } = await execFileAsync(
    "ffmpeg",
    enhancedArgs,
    resolveExecOptions(getOptimizedFfmpegTimeout(), options),
  );
  return stdout.toString();
}

export function parseFfprobeCsvFields(stdout: string, maxFields: number): string[] {
  return stdout
    .trim()
    .toLowerCase()
    .split(/[,\r\n]+/, maxFields)
    .map((field) => field.trim());
}

export function parseFfprobeCodecAndSampleRate(stdout: string): {
  codec: string | null;
  sampleRateHz: number | null;
} {
  const [codecRaw, sampleRateRaw] = parseFfprobeCsvFields(stdout, 2);
  const codec = codecRaw ? codecRaw : null;
  const sampleRate = sampleRateRaw ? Number.parseInt(sampleRateRaw, 10) : Number.NaN;
  return {
    codec,
    sampleRateHz: Number.isFinite(sampleRate) ? sampleRate : null,
  };
}
