// Memory index metadata read/write and scope hash helpers for MemoryManagerSyncOps.
import type { DatabaseSync } from "node:sqlite";
import { ResolvedMemorySearchConfig } from "../agents/memory-search.js";
import { hashText, normalizeExtraMemoryPaths } from "./internal.js";
import { META_KEY, type MemoryIndexMeta } from "./manager-sync-ops-types.js";
import type { MemorySource } from "./types.js";

export function readMeta(
  db: DatabaseSync,
  lastMetaSerializedRef: { value: string | null },
): MemoryIndexMeta | null {
  const row = db.prepare(`SELECT value FROM meta WHERE key = ?`).get(META_KEY) as
    | { value: string }
    | undefined;
  if (!row?.value) {
    lastMetaSerializedRef.value = null;
    return null;
  }
  try {
    const parsed = JSON.parse(row.value) as MemoryIndexMeta;
    lastMetaSerializedRef.value = row.value;
    return parsed;
  } catch {
    lastMetaSerializedRef.value = null;
    return null;
  }
}

export function writeMeta(
  db: DatabaseSync,
  meta: MemoryIndexMeta,
  lastMetaSerializedRef: { value: string | null },
): void {
  const value = JSON.stringify(meta);
  if (lastMetaSerializedRef.value === value) {
    return;
  }
  db.prepare(
    `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
  ).run(META_KEY, value);
  lastMetaSerializedRef.value = value;
}

export function resolveConfiguredSourcesForMeta(sources: Set<MemorySource>): MemorySource[] {
  const normalized = Array.from(sources)
    .filter((source): source is MemorySource => source === "memory" || source === "sessions")
    .toSorted();
  return normalized.length > 0 ? normalized : ["memory"];
}

export function normalizeMetaSources(meta: MemoryIndexMeta): MemorySource[] {
  if (!Array.isArray(meta.sources)) {
    // Backward compatibility for older indexes that did not persist sources.
    return ["memory"];
  }
  const normalized = Array.from(
    new Set(
      meta.sources.filter(
        (source): source is MemorySource => source === "memory" || source === "sessions",
      ),
    ),
  ).toSorted();
  return normalized.length > 0 ? normalized : ["memory"];
}

export function resolveConfiguredScopeHash(
  workspaceDir: string,
  settings: ResolvedMemorySearchConfig,
): string {
  const extraPaths = normalizeExtraMemoryPaths(workspaceDir, settings.extraPaths)
    .map((value) => value.replace(/\\/g, "/"))
    .toSorted();
  return hashText(
    JSON.stringify({
      extraPaths,
      multimodal: {
        enabled: settings.multimodal.enabled,
        modalities: [...settings.multimodal.modalities].toSorted(),
        maxFileBytes: settings.multimodal.maxFileBytes,
      },
    }),
  );
}

export function metaSourcesDiffer(
  meta: MemoryIndexMeta,
  configuredSources: MemorySource[],
): boolean {
  const metaSources = normalizeMetaSources(meta);
  if (metaSources.length !== configuredSources.length) {
    return true;
  }
  return metaSources.some((source, index) => source !== configuredSources[index]);
}
