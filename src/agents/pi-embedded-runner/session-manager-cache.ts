import fs from "node:fs/promises";
import { isCacheEnabled, resolveCacheTtlMs } from "../../config/cache-utils.js";

type SessionManagerCacheEntry = {
  sessionFile: string;
  loadedAt: number;
};

const SESSION_MANAGER_CACHE = new Map<string, SessionManagerCacheEntry>();
const DEFAULT_SESSION_MANAGER_TTL_MS = 45_000; // 45 seconds

function getSessionManagerTtl(): number {
  return resolveCacheTtlMs({
    envValue: process.env.DENEB_SESSION_MANAGER_CACHE_TTL_MS,
    defaultTtlMs: DEFAULT_SESSION_MANAGER_TTL_MS,
  });
}

function isSessionManagerCacheEnabled(): boolean {
  return isCacheEnabled(getSessionManagerTtl());
}

export function trackSessionManagerAccess(sessionFile: string): void {
  if (!isSessionManagerCacheEnabled()) {
    return;
  }
  const now = Date.now();
  SESSION_MANAGER_CACHE.set(sessionFile, {
    sessionFile,
    loadedAt: now,
  });
}

function isSessionManagerCached(sessionFile: string): boolean {
  if (!isSessionManagerCacheEnabled()) {
    return false;
  }
  const entry = SESSION_MANAGER_CACHE.get(sessionFile);
  if (!entry) {
    return false;
  }
  const now = Date.now();
  const ttl = getSessionManagerTtl();
  return now - entry.loadedAt <= ttl;
}

export async function prewarmSessionFile(sessionFile: string): Promise<void> {
  if (!isSessionManagerCacheEnabled()) {
    return;
  }
  if (isSessionManagerCached(sessionFile)) {
    return;
  }

  try {
    // Read the full file to pull it into the OS page cache so SessionManager.open
    // doesn't block on disk I/O. For typical session files (< 2 MB) this is cheap;
    // for very large files, readFile still beats a 4 KB partial read that leaves most
    // pages uncached.
    await fs.readFile(sessionFile);
    trackSessionManagerAccess(sessionFile);
  } catch {
    // File doesn't exist yet, SessionManager will create it
  }
}
