import { mkdirSync } from "fs";
import { DatabaseSync } from "node:sqlite";
import { dirname } from "path";

type ConnectionEntry = {
  db: DatabaseSync;
  refs: number;
};

const _connections = new Map<string, ConnectionEntry>();

function isConnectionHealthy(db: DatabaseSync): boolean {
  try {
    db.prepare("SELECT 1").get();
    return true;
  } catch {
    return false;
  }
}

function forceCloseConnection(entry: ConnectionEntry): void {
  try {
    entry.db.close();
  } catch {
    // Ignore close failures; caller is already replacing/removing this handle.
  }
}

export function getLcmConnection(dbPath: string): DatabaseSync {
  const existing = _connections.get(dbPath);
  if (existing) {
    if (isConnectionHealthy(existing.db)) {
      existing.refs += 1;
      return existing.db;
    }
    // Reset refs before force-close so stale callers decrementing later
    // won't accidentally close the replacement connection.
    existing.refs = 0;
    forceCloseConnection(existing);
    _connections.delete(dbPath);
  }

  // Ensure parent directory exists
  mkdirSync(dirname(dbPath), { recursive: true });

  const db = new DatabaseSync(dbPath);

  // Enable WAL mode for better concurrent read performance
  db.exec("PRAGMA journal_mode = WAL");
  // Enable foreign key enforcement
  db.exec("PRAGMA foreign_keys = ON");

  _connections.set(dbPath, { db, refs: 1 });
  return db;
}

/**
 * Decrement the ref-count for the given dbPath and close if it drops to zero.
 */
export function closeLcmConnection(dbPath: string): void {
  const entry = _connections.get(dbPath);
  if (!entry) {
    return;
  }
  entry.refs = Math.max(0, entry.refs - 1);
  if (entry.refs === 0) {
    forceCloseConnection(entry);
    _connections.delete(dbPath);
  }
}

/**
 * Force-close ALL open LCM connections. Only for process shutdown / cleanup.
 */
export function closeAllLcmConnections(): void {
  for (const entry of _connections.values()) {
    forceCloseConnection(entry);
  }
  _connections.clear();
}
