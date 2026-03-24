import { execFileSync } from "node:child_process";

export type GpuInfo = {
  available: boolean;
  gpuName: string | null;
  cudaVersion: string | null;
  driverVersion: string | null;
  memoryMb: number | null;
  /** Detected GPU tier for profile selection. */
  tier: GpuTier;
};

/**
 * GPU tier determines which performance profile to use.
 *
 * - `"dgx-spark"` — DGX Spark GB10 (Grace Blackwell, 128GB unified memory)
 * - `"desktop"` — Consumer/workstation NVIDIA GPU (discrete VRAM)
 * - `"none"` — No GPU detected
 */
export type GpuTier = "dgx-spark" | "desktop" | "none";

const GPU_NOT_AVAILABLE: GpuInfo = {
  available: false,
  gpuName: null,
  cudaVersion: null,
  driverVersion: null,
  memoryMb: null,
  tier: "none",
};

let cachedResult: GpuInfo | null = null;

type ExecFn = (cmd: string, args: string[], opts: object) => string;

/** Default exec implementation using child_process. */
const defaultExec: ExecFn = (cmd, args, opts) =>
  String(execFileSync(cmd, args, { ...opts, encoding: "utf-8" }));

/** Patterns that identify DGX Spark / GB10 Blackwell GPUs. */
const DGX_SPARK_PATTERNS = [/\bGB10\b/i, /\bGrace\s*Blackwell\b/i, /\bDGX\s*Spark\b/i];

/**
 * Detect NVIDIA GPU and CUDA availability via `nvidia-smi`.
 *
 * Respects `DENEB_GPU_ACCEL` env var:
 * - `"cuda"` / `"1"` — force-enable CUDA (skip detection)
 * - `"dgx-spark"` — force DGX Spark GB10 profile
 * - `"none"` / `"0"` — force-disable GPU acceleration
 * - unset — auto-detect via `nvidia-smi`
 *
 * Results are cached for the process lifetime.
 *
 * @param exec - optional exec function for testing
 */
export function detectGpu(exec?: ExecFn): GpuInfo {
  if (cachedResult) {
    return cachedResult;
  }

  const override = process.env.DENEB_GPU_ACCEL?.trim().toLowerCase();
  if (override === "none" || override === "0") {
    cachedResult = GPU_NOT_AVAILABLE;
    return cachedResult;
  }

  const run = exec ?? defaultExec;

  if (override === "dgx-spark") {
    cachedResult = queryNvidiaSmi(run) ?? {
      available: true,
      gpuName: "DGX Spark GB10 (forced via DENEB_GPU_ACCEL)",
      cudaVersion: null,
      driverVersion: null,
      memoryMb: null,
      tier: "dgx-spark",
    };
    // Force tier regardless of detection
    cachedResult.tier = "dgx-spark";
    return cachedResult;
  }

  if (override === "cuda" || override === "1") {
    cachedResult = queryNvidiaSmi(run) ?? {
      available: true,
      gpuName: "NVIDIA GPU (forced via DENEB_GPU_ACCEL)",
      cudaVersion: null,
      driverVersion: null,
      memoryMb: null,
      tier: "desktop",
    };
    return cachedResult;
  }

  // Auto-detect
  const detected = queryNvidiaSmi(run);
  cachedResult = detected ?? GPU_NOT_AVAILABLE;
  return cachedResult;
}

/**
 * Classify GPU tier from the GPU name string.
 */
function classifyGpuTier(gpuName: string | null, memoryMb: number | null): GpuTier {
  if (!gpuName) {
    return "desktop";
  }
  // DGX Spark GB10: Grace Blackwell with 128GB unified memory
  if (DGX_SPARK_PATTERNS.some((pattern) => pattern.test(gpuName))) {
    return "dgx-spark";
  }
  // High-VRAM (>100GB) unified memory systems are likely DGX Spark
  if (memoryMb && memoryMb >= 100_000) {
    return "dgx-spark";
  }
  return "desktop";
}

/**
 * Query `nvidia-smi` for GPU details.
 * Returns `null` if `nvidia-smi` is not available or fails.
 */
function queryNvidiaSmi(exec: ExecFn): GpuInfo | null {
  try {
    const stdout = exec(
      "nvidia-smi",
      ["--query-gpu=name,memory.total,driver_version", "--format=csv,noheader,nounits"],
      { timeout: 5_000, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"] },
    );

    const line = stdout.trim().split("\n")[0];
    if (!line) {
      return null;
    }

    const [gpuName, memoryRaw, driverVersion] = line.split(",").map((s) => s.trim());

    const memoryMb = memoryRaw ? Number.parseInt(memoryRaw, 10) : Number.NaN;
    const resolvedMemory = Number.isFinite(memoryMb) ? memoryMb : null;

    // Try to get CUDA version separately (nvidia-smi main output header)
    const cudaVersion = queryCudaVersion(exec);

    return {
      available: true,
      gpuName: gpuName || null,
      cudaVersion,
      driverVersion: driverVersion || null,
      memoryMb: resolvedMemory,
      tier: classifyGpuTier(gpuName || null, resolvedMemory),
    };
  } catch {
    return null;
  }
}

/**
 * Extract CUDA version from `nvidia-smi` output.
 */
function queryCudaVersion(exec: ExecFn): string | null {
  try {
    const stdout = exec("nvidia-smi", [], {
      timeout: 5_000,
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
    });
    const match = stdout.match(/CUDA Version:\s*([\d.]+)/);
    return match?.[1] ?? null;
  } catch {
    return null;
  }
}

/** Check if CUDA GPU acceleration is available. */
export function isCudaAvailable(): boolean {
  return detectGpu().available;
}

/**
 * Reset cached GPU detection result (for testing).
 * @internal
 */
export function _resetGpuDetectCache(): void {
  cachedResult = null;
}
