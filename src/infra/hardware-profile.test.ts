import { describe, expect, it } from "vitest";
import { PERF, getCachedPerformanceProfile } from "./hardware-profile.js";

describe("hardware-profile", () => {
  it("PERF has a profile name", () => {
    expect(PERF.name).toBeTruthy();
  });

  it("getCachedPerformanceProfile returns a valid profile", () => {
    const profile = getCachedPerformanceProfile();
    expect(profile.agentMaxConcurrent).toBeGreaterThanOrEqual(1);
    expect(profile.uvThreadPoolSize).toBeGreaterThanOrEqual(4);
  });

  it("computePoolSize is at least 2", () => {
    expect(PERF.computePoolSize).toBeGreaterThanOrEqual(2);
  });

  it("all concurrency values are positive", () => {
    expect(PERF.agentMaxConcurrent).toBeGreaterThanOrEqual(1);
    expect(PERF.subagentMaxConcurrent).toBeGreaterThanOrEqual(1);
    expect(PERF.imageWorkerCount).toBeGreaterThanOrEqual(1);
    expect(PERF.embeddingBatchConcurrency).toBeGreaterThanOrEqual(1);
    expect(PERF.modelScanConcurrency).toBeGreaterThanOrEqual(1);
  });
});
