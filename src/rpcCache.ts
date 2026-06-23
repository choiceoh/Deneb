import { getJSON, setJSON } from "./storage";

export interface CachedRpcSnapshot<T> {
  data: T;
  savedAt: number;
}

const RPC_CACHE_PREFIX = "andromeda.rpcCache";

export function rpcCacheKey(method: string, params: Record<string, unknown> = {}): string {
  return `${method}:${stableStringify(params)}`;
}

export function cachedRpcStorageKey(resource: string, key: string): string {
  return `${RPC_CACHE_PREFIX}.${resource}.${encodeURIComponent(key)}`;
}

export function clearCachedRpcResource(resource: string): void {
  try {
    const prefix = `${RPC_CACHE_PREFIX}.${resource}.`;
    const keys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key?.startsWith(prefix)) keys.push(key);
    }
    for (const key of keys) localStorage.removeItem(key);
  } catch {
    /* ignore storage quota / private mode failures */
  }
}

export function isCachedRpcFresh(snapshot: CachedRpcSnapshot<unknown>, staleTimeMs: number): boolean {
  return staleTimeMs < 0 || Date.now() - snapshot.savedAt <= staleTimeMs;
}

export function readCachedRpc<T>(
  resource: string,
  key: string,
  options: { staleTimeMs?: number } = {},
): CachedRpcSnapshot<T> | undefined {
  const parsed = getJSON<Partial<CachedRpcSnapshot<T>>>(cachedRpcStorageKey(resource, key));
  if (!parsed || typeof parsed.savedAt !== "number" || !Object.prototype.hasOwnProperty.call(parsed, "data")) {
    return undefined;
  }
  const snapshot = { data: parsed.data as T, savedAt: parsed.savedAt };
  if (options.staleTimeMs !== undefined && !isCachedRpcFresh(snapshot, options.staleTimeMs)) return undefined;
  return snapshot;
}

export function writeCachedRpc<T>(resource: string, key: string, data: T): void {
  setJSON(cachedRpcStorageKey(resource, key), { data, savedAt: Date.now() });
}

function stableStringify(value: unknown): string {
  return JSON.stringify(stableValue(value));
}

function stableValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stableValue);
  if (!value || typeof value !== "object") return value;
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(value as Record<string, unknown>).sort()) {
    out[key] = stableValue((value as Record<string, unknown>)[key]);
  }
  return out;
}
