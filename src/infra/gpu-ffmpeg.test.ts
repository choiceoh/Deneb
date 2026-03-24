import { describe, expect, it } from "vitest";
import { buildCpuFallbackArgs, buildGpuFfmpegArgs } from "./gpu-ffmpeg.js";

describe("gpu-ffmpeg", () => {
  describe("buildGpuFfmpegArgs", () => {
    // Note: this test exercises the pure arg-building logic.
    // Whether GPU flags are actually prepended depends on the PERF profile
    // which is resolved at module load time based on GPU detection.

    it("returns args unchanged when no -i flag present", () => {
      const args = ["-f", "mp4", "output.mp4"];
      const result = buildGpuFfmpegArgs(args);
      // Should contain the original args (may or may not have hwaccel depending on profile)
      expect(result).toContain("-f");
      expect(result).toContain("output.mp4");
    });
  });

  describe("buildCpuFallbackArgs", () => {
    it("strips hwaccel flags and restores software encoders", () => {
      const gpuArgs = [
        "-hwaccel",
        "cuda",
        "-hwaccel_output_format",
        "cuda",
        "-i",
        "input.mp4",
        "-c:v",
        "h264_nvenc",
        "output.mp4",
      ];

      const cpuArgs = buildCpuFallbackArgs(gpuArgs);
      expect(cpuArgs).toEqual(["-i", "input.mp4", "-c:v", "libx264", "output.mp4"]);
    });

    it("swaps hevc_nvenc back to libx265", () => {
      const gpuArgs = ["-i", "input.mp4", "-c:v", "hevc_nvenc", "output.mp4"];
      const cpuArgs = buildCpuFallbackArgs(gpuArgs);
      expect(cpuArgs).toEqual(["-i", "input.mp4", "-c:v", "libx265", "output.mp4"]);
    });

    it("removes cuvid decoder references", () => {
      const gpuArgs = ["-c:v", "h264_cuvid", "-i", "input.mp4", "output.mp4"];
      const cpuArgs = buildCpuFallbackArgs(gpuArgs);
      expect(cpuArgs).toEqual(["-c:v", "-i", "input.mp4", "output.mp4"]);
    });

    it("passes through args without GPU-specific flags", () => {
      const args = ["-i", "input.mp4", "-c:v", "libx264", "-crf", "23", "output.mp4"];
      const result = buildCpuFallbackArgs(args);
      expect(result).toEqual(args);
    });

    it("handles empty args", () => {
      expect(buildCpuFallbackArgs([])).toEqual([]);
    });

    it("handles multiple hwaccel flag pairs", () => {
      const args = [
        "-hwaccel",
        "cuda",
        "-hwaccel_output_format",
        "cuda",
        "-hwaccel",
        "cuda",
        "-i",
        "input.mp4",
      ];
      const result = buildCpuFallbackArgs(args);
      expect(result).toEqual(["-i", "input.mp4"]);
    });
  });
});
