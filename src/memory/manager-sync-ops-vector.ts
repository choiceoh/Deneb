// Vector extension management for MemoryManagerSyncOps.
import type { DatabaseSync } from "node:sqlite";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { resolveUserPath } from "../utils.js";
import { VECTOR_TABLE } from "./manager-sync-ops-types.js";
import { loadSqliteVecExtension } from "./sqlite-vec.js";

const log = createSubsystemLogger("memory");

export type VectorState = {
  enabled: boolean;
  available: boolean | null;
  extensionPath?: string;
  loadError?: string;
  dims?: number;
};

export async function loadVectorExtension(vector: VectorState, db: DatabaseSync): Promise<boolean> {
  if (vector.available !== null) {
    return vector.available;
  }
  if (!vector.enabled) {
    vector.available = false;
    return false;
  }
  try {
    const resolvedPath = vector.extensionPath?.trim()
      ? resolveUserPath(vector.extensionPath)
      : undefined;
    const loaded = await loadSqliteVecExtension({ db, extensionPath: resolvedPath });
    if (!loaded.ok) {
      throw new Error(loaded.error ?? "unknown sqlite-vec load error");
    }
    vector.extensionPath = loaded.extensionPath;
    vector.available = true;
    return true;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    vector.available = false;
    vector.loadError = message;
    log.warn(`sqlite-vec unavailable: ${message}`);
    return false;
  }
}

export function ensureVectorTable(vector: VectorState, db: DatabaseSync, dimensions: number): void {
  if (vector.dims === dimensions) {
    return;
  }
  if (vector.dims && vector.dims !== dimensions) {
    dropVectorTable(vector, db);
  }
  db.exec(
    `CREATE VIRTUAL TABLE IF NOT EXISTS ${VECTOR_TABLE} USING vec0(\n` +
      `  id TEXT PRIMARY KEY,\n` +
      `  embedding FLOAT[${dimensions}]\n` +
      `)`,
  );
  vector.dims = dimensions;
}

export function dropVectorTable(vector: VectorState, db: DatabaseSync): void {
  try {
    db.exec(`DROP TABLE IF EXISTS ${VECTOR_TABLE}`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    log.debug(`Failed to drop ${VECTOR_TABLE}: ${message}`);
  }
}
