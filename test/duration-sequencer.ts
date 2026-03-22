import fs from "node:fs";
import path from "node:path";
import { BaseSequencer } from "vitest/node";

const TIMING_PATH = path.resolve(import.meta.dirname, "fixtures/test-timings.unit.json");
const DEFAULT_DURATION_MS = 250;

interface TimingManifest {
  defaultDurationMs?: number;
  files?: Record<string, { durationMs?: number }>;
}

let cachedTimings: TimingManifest | null = null;

function loadTimings(): TimingManifest {
  if (cachedTimings) {
    return cachedTimings;
  }
  try {
    cachedTimings = JSON.parse(fs.readFileSync(TIMING_PATH, "utf8")) as TimingManifest;
  } catch {
    cachedTimings = {};
  }
  return cachedTimings;
}

const repoRoot = path.resolve(import.meta.dirname, "..");

function estimateDuration(filePath: string): number {
  const timings = loadTimings();
  const relative = path.relative(repoRoot, filePath).split(path.sep).join("/");
  return timings.files?.[relative]?.durationMs ?? timings.defaultDurationMs ?? DEFAULT_DURATION_MS;
}

/**
 * Sorts test files by estimated duration (longest first) so workers stay utilized
 * throughout the run instead of stalling on a slow file at the end.
 */
export default class DurationSequencer extends BaseSequencer {
  override async sort(files: Parameters<BaseSequencer["sort"]>[0]) {
    return [...files].toSorted((a, b) => {
      // TestSpecification uses `moduleId` (absolute path) in vitest 4.x.
      const pathA = typeof a === "string" ? a : a.moduleId;
      const pathB = typeof b === "string" ? b : b.moduleId;
      return estimateDuration(pathB) - estimateDuration(pathA);
    });
  }
}
