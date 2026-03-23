// Database open, source filter, embedding cache seeding, and index file swap
// utilities for MemoryManagerSyncOps.
import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import type { DatabaseSync } from "node:sqlite";
import { ResolvedMemorySearchConfig } from "../agents/memory-search.js";
import { resolveUserPath } from "../utils.js";
import { ensureDir } from "./internal.js";
import { EMBEDDING_CACHE_TABLE, FTS_TABLE } from "./manager-sync-ops-types.js";
import { ensureMemoryIndexSchema } from "./memory-schema.js";
import { requireNodeSqlite } from "./sqlite.js";
import type { MemorySource } from "./types.js";

export function buildSourceFilter(
  sources: Set<MemorySource>,
  alias?: string,
): { sql: string; params: MemorySource[] } {
  const sourcesArr = Array.from(sources);
  if (sourcesArr.length === 0) {
    return { sql: "", params: [] };
  }
  const column = alias ? `${alias}.source` : "source";
  const placeholders = sourcesArr.map(() => "?").join(", ");
  return { sql: ` AND ${column} IN (${placeholders})`, params: sourcesArr };
}

export function openDatabaseAtPath(
  settings: ResolvedMemorySearchConfig,
  dbPath: string,
): DatabaseSync {
  const dir = path.dirname(dbPath);
  ensureDir(dir);
  const { DatabaseSync } = requireNodeSqlite();
  const db = new DatabaseSync(dbPath, { allowExtension: settings.store.vector.enabled });
  // busy_timeout is per-connection and resets to 0 on restart.
  // Set it on every open so concurrent processes retry instead of
  // failing immediately with SQLITE_BUSY.
  db.exec("PRAGMA busy_timeout = 5000");
  return db;
}

export function openDatabase(settings: ResolvedMemorySearchConfig): DatabaseSync {
  const dbPath = resolveUserPath(settings.store.path);
  return openDatabaseAtPath(settings, dbPath);
}

export function seedEmbeddingCache(
  cache: { enabled: boolean },
  targetDb: DatabaseSync,
  sourceDb: DatabaseSync,
): void {
  if (!cache.enabled) {
    return;
  }
  try {
    const rows = sourceDb
      .prepare(
        `SELECT provider, model, provider_key, hash, embedding, dims, updated_at FROM ${EMBEDDING_CACHE_TABLE}`,
      )
      .all() as Array<{
      provider: string;
      model: string;
      provider_key: string;
      hash: string;
      embedding: string;
      dims: number | null;
      updated_at: number;
    }>;
    if (!rows.length) {
      return;
    }
    const insert = targetDb.prepare(
      `INSERT INTO ${EMBEDDING_CACHE_TABLE} (provider, model, provider_key, hash, embedding, dims, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(provider, model, provider_key, hash) DO UPDATE SET
         embedding=excluded.embedding,
         dims=excluded.dims,
         updated_at=excluded.updated_at`,
    );
    targetDb.exec("BEGIN");
    for (const row of rows) {
      insert.run(
        row.provider,
        row.model,
        row.provider_key,
        row.hash,
        row.embedding,
        row.dims,
        row.updated_at,
      );
    }
    targetDb.exec("COMMIT");
  } catch (err) {
    try {
      targetDb.exec("ROLLBACK");
    } catch {
      // ROLLBACK may fail if the connection is already broken; the original error is rethrown below.
    }
    throw err;
  }
}

export async function swapIndexFiles(targetPath: string, tempPath: string): Promise<void> {
  const backupPath = `${targetPath}.backup-${randomUUID()}`;
  await moveIndexFiles(targetPath, backupPath);
  try {
    await moveIndexFiles(tempPath, targetPath);
  } catch (err) {
    await moveIndexFiles(backupPath, targetPath);
    throw err;
  }
  await removeIndexFiles(backupPath);
}

export async function moveIndexFiles(sourceBase: string, targetBase: string): Promise<void> {
  const suffixes = ["", "-wal", "-shm"];
  for (const suffix of suffixes) {
    const source = `${sourceBase}${suffix}`;
    const target = `${targetBase}${suffix}`;
    try {
      await fs.rename(source, target);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
        throw err;
      }
    }
  }
}

export async function removeIndexFiles(basePath: string): Promise<void> {
  const suffixes = ["", "-wal", "-shm"];
  await Promise.all(suffixes.map((suffix) => fs.rm(`${basePath}${suffix}`, { force: true })));
}

export function runEnsureSchema(
  db: DatabaseSync,
  fts: { enabled: boolean; available: boolean; loadError?: string },
): void {
  const result = ensureMemoryIndexSchema({
    db,
    embeddingCacheTable: EMBEDDING_CACHE_TABLE,
    ftsTable: FTS_TABLE,
    ftsEnabled: fts.enabled,
  });
  fts.available = result.ftsAvailable;
  if (result.ftsError) {
    fts.loadError = result.ftsError;
  }
}
