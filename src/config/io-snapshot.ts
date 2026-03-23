import { coerceConfig, createMergePatch } from "./io-path-ops.js";
import { applyMergePatch } from "./merge-patch.js";
import type { DenebConfig } from "./types.js";

export type RuntimeConfigSnapshotRefreshParams = {
  sourceConfig: DenebConfig;
};

export type RuntimeConfigSnapshotRefreshHandler = {
  refresh: (params: RuntimeConfigSnapshotRefreshParams) => boolean | Promise<boolean>;
  clearOnRefreshFailure?: () => void;
};

export class ConfigRuntimeRefreshError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "ConfigRuntimeRefreshError";
  }
}

let runtimeConfigSnapshot: DenebConfig | null = null;
let runtimeConfigSourceSnapshot: DenebConfig | null = null;
let runtimeConfigSnapshotRefreshHandler: RuntimeConfigSnapshotRefreshHandler | null = null;

export function setRuntimeConfigSnapshot(config: DenebConfig, sourceConfig?: DenebConfig): void {
  runtimeConfigSnapshot = config;
  runtimeConfigSourceSnapshot = sourceConfig ?? null;
}

export function clearRuntimeConfigSnapshot(): void {
  runtimeConfigSnapshot = null;
  runtimeConfigSourceSnapshot = null;
}

export function getRuntimeConfigSnapshot(): DenebConfig | null {
  return runtimeConfigSnapshot;
}

export function getRuntimeConfigSourceSnapshot(): DenebConfig | null {
  return runtimeConfigSourceSnapshot;
}

export function setRuntimeConfigSnapshotRefreshHandler(
  refreshHandler: RuntimeConfigSnapshotRefreshHandler | null,
): void {
  runtimeConfigSnapshotRefreshHandler = refreshHandler;
}

export function getRuntimeConfigSnapshotRefreshHandler(): RuntimeConfigSnapshotRefreshHandler | null {
  return runtimeConfigSnapshotRefreshHandler;
}

function isCompatibleTopLevelRuntimeProjectionShape(params: {
  runtimeSnapshot: DenebConfig;
  candidate: DenebConfig;
}): boolean {
  const runtime = params.runtimeSnapshot as Record<string, unknown>;
  const candidate = params.candidate as Record<string, unknown>;
  for (const key of Object.keys(runtime)) {
    if (!Object.hasOwn(candidate, key)) {
      return false;
    }
    const runtimeValue = runtime[key];
    const candidateValue = candidate[key];
    const runtimeType = Array.isArray(runtimeValue)
      ? "array"
      : runtimeValue === null
        ? "null"
        : typeof runtimeValue;
    const candidateType = Array.isArray(candidateValue)
      ? "array"
      : candidateValue === null
        ? "null"
        : typeof candidateValue;
    if (runtimeType !== candidateType) {
      return false;
    }
  }
  return true;
}

export function projectConfigOntoRuntimeSourceSnapshot(config: DenebConfig): DenebConfig {
  if (!runtimeConfigSnapshot || !runtimeConfigSourceSnapshot) {
    return config;
  }
  if (config === runtimeConfigSnapshot) {
    return runtimeConfigSourceSnapshot;
  }
  // This projection expects callers to pass config objects derived from the
  // active runtime snapshot (for example shallow/deep clones with targeted edits).
  // For structurally unrelated configs, skip projection to avoid accidental
  // merge-patch deletions or reintroducing resolved values into source refs.
  if (
    !isCompatibleTopLevelRuntimeProjectionShape({
      runtimeSnapshot: runtimeConfigSnapshot,
      candidate: config,
    })
  ) {
    return config;
  }
  const runtimePatch = createMergePatch(runtimeConfigSnapshot, config);
  return coerceConfig(applyMergePatch(runtimeConfigSourceSnapshot, runtimePatch));
}
