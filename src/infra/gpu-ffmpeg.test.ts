import { describe, expect, it } from "vitest";
import { buildCpuFallbackArgs, buildGpuFfmpegArgs } from "./gpu-ffmpeg.js";

describe("gpu-ffmpeg", () => {
  describe("buildGpuFfmpegArgs", () => {
    it("returns args unchanged when no -i flag present", () => {
      const args = ["-f", "mp4", "output.mp4"];
      const result = buildGpuFfmpegArgs(args);
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

    it("swaps av1_nvenc back to libsvtav1", () => {
      const gpuArgs = ["-i", "input.mp4", "-c:v", "av1_nvenc", "output.mp4"];
      const cpuArgs = buildCpuFallbackArgs(gpuArgs);
      expect(cpuArgs).toEqual(["-i", "input.mp4", "-c:v", "libsvtav1", "output.mp4"]);
    });

    it("removes cuvid decoder references", () => {
      const gpuArgs = ["-c:v", "h264_cuvid", "-i", "input.mp4", "output.mp4"];
      const cpuArgs = buildCpuFallbackArgs(gpuArgs);
      expect(cpuArgs).toEqual(["-c:v", "-i", "input.mp4", "output.mp4"]);
    });

    it("removes av1_cuvid and vp9_cuvid decoder references", () => {
      expect(buildCpuFallbackArgs(["av1_cuvid"])).toEqual([]);
      expect(buildCpuFallbackArgs(["vp9_cuvid"])).toEqual([]);
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
