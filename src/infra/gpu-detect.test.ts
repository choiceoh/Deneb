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
    expect(result.tier).toBe("desktop");
  });

  it("classifies DGX Spark GB10 by GPU name", () => {
    const exec = createNvidiaSmiExec(
      "NVIDIA Grace Blackwell GB10, 131072, 560.35.03",
      "CUDA Version: 12.6",
    );

    const result = detectGpu(exec);
    expect(result.available).toBe(true);
    expect(result.tier).toBe("dgx-spark");
    expect(result.memoryMb).toBe(131072);
  });

  it("classifies high-VRAM unified memory system as dgx-spark", () => {
    const exec = createNvidiaSmiExec("NVIDIA Custom GPU, 131072, 560.35.03", "CUDA Version: 12.6");

    const result = detectGpu(exec);
    expect(result.tier).toBe("dgx-spark");
  });

  it("returns not available when nvidia-smi is missing", () => {
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(false);
    expect(result.gpuName).toBeNull();
    expect(result.tier).toBe("none");
  });

  it("caches detection results across calls", () => {
    let callCount = 0;
    const exec: ExecFn = () => {
      callCount++;
      throw new Error("ENOENT");
    };

    detectGpu(exec);
    const first = callCount;

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
    expect(result.tier).toBe("none");
    expect(called).toBe(false);
  });

  it("respects DENEB_GPU_ACCEL=0 to disable GPU", () => {
    process.env.DENEB_GPU_ACCEL = "0";
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(false);
  });

  it("force-enables DGX Spark profile with DENEB_GPU_ACCEL=dgx-spark", () => {
    process.env.DENEB_GPU_ACCEL = "dgx-spark";
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(true);
    expect(result.tier).toBe("dgx-spark");
  });

  it("force-enables desktop GPU with DENEB_GPU_ACCEL=cuda", () => {
    process.env.DENEB_GPU_ACCEL = "cuda";
    const result = detectGpu(createFailingExec());
    expect(result.available).toBe(true);
    expect(result.tier).toBe("desktop");
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
    expect(result.tier).toBe("desktop");
  });
});
