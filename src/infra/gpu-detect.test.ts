import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { _resetGpuDetectCache, detectGpu } from "./gpu-detect.js";

type ExecFn = (cmd: string, args: string[], opts: object) => string;

function createNvidiaSmiExec(gpuLine: string, cudaLine: string): ExecFn {
  return (_cmd: string, args: string[]) => {
    if (args.length > 0 && args[0].includes("--query-gpu")) {
      return gpuLine;
    }
    return cudaLine;
  };
}

function createFailingExec(): ExecFn {
  return () => {
    throw new Error("ENOENT");
  };
}

beforeEach(() => {
  _resetGpuDetectCache();
  delete process.env.DENEB_GPU_ACCEL;
});

afterEach(() => {
  delete process.env.DENEB_GPU_ACCEL;
  _resetGpuDetectCache();
});

describe("detectGpu", () => {
  it("detects NVIDIA GPU via nvidia-smi", () => {
    const exec = createNvidiaSmiExec(
      "NVIDIA GeForce RTX 4090, 24564, 550.54.14",
      "| NVIDIA-SMI 550.54.14  Driver Version: 550.54.14  CUDA Version: 12.4 |",
    );

    const result = detectGpu(exec);
    expect(result.available).toBe(true);
    expect(result.gpuName).toBe("NVIDIA GeForce RTX 4090");
    expect(result.memoryMb).toBe(24564);
    expect(result.driverVersion).toBe("550.54.14");
    expect(result.cudaVersion).toBe("12.4");
  });

  it("returns not available when nvidia-smi is missing", () => {
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(false);
    expect(result.gpuName).toBeNull();
  });

  it("caches detection results across calls", () => {
    let callCount = 0;
    const exec: ExecFn = () => {
      callCount++;
      throw new Error("ENOENT");
    };

    detectGpu(exec);
    const first = callCount;

    // Second call returns cached result — exec should not be called again
    detectGpu(exec);
    expect(callCount).toBe(first);
  });

  it("respects DENEB_GPU_ACCEL=none to disable GPU", () => {
    process.env.DENEB_GPU_ACCEL = "none";
    let called = false;
    const exec: ExecFn = () => {
      called = true;
      return "";
    };

    const result = detectGpu(exec);
    expect(result.available).toBe(false);
    expect(called).toBe(false);
  });

  it("respects DENEB_GPU_ACCEL=0 to disable GPU", () => {
    process.env.DENEB_GPU_ACCEL = "0";
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(false);
  });

  it("force-enables with DENEB_GPU_ACCEL=cuda even when nvidia-smi fails", () => {
    process.env.DENEB_GPU_ACCEL = "cuda";
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(true);
    expect(result.gpuName).toContain("forced via DENEB_GPU_ACCEL");
  });

  it("force-enables with DENEB_GPU_ACCEL=1", () => {
    process.env.DENEB_GPU_ACCEL = "1";
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(true);
  });

  it("handles nvidia-smi returning empty output", () => {
    const exec: ExecFn = () => "";
    const result = detectGpu(exec);
    expect(result.available).toBe(false);
  });

  it("parses GPU with no CUDA version available", () => {
    const exec: ExecFn = (_cmd, args) => {
      if (args[0]?.includes("--query-gpu")) {
        return "NVIDIA A100, 40960, 525.60.13";
      }
      throw new Error("no cuda header");
    };

    const result = detectGpu(exec);
    expect(result.available).toBe(true);
    expect(result.gpuName).toBe("NVIDIA A100");
    expect(result.memoryMb).toBe(40960);
    expect(result.cudaVersion).toBeNull();
  });
});
