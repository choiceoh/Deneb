import { useEffect, useMemo } from "react";
import { type BaseKey, type BaseRecord, useList, useOne } from "@refinedev/core";

import { getJSON, setJSON } from "./storage";

export const CACHED_LIST_STALE_MS = 60_000;
export const CACHED_LIST_GC_MS = 10 * 60_000;
export const CACHED_ONE_STALE_MS = 5 * 60_000;
export const CACHED_ONE_GC_MS = 30 * 60_000;

interface CachedListSnapshot<T> {
  data: T[];
  total: number;
  savedAt: number;
}

interface CachedOneSnapshot<T> {
  data: T;
  savedAt: number;
}

export function cachedListStorageKey(resource: string): string {
  return `andromeda.listCache.${resource}`;
}

export function cachedOneStorageKey(resource: string, id: BaseKey): string {
  return `andromeda.oneCache.${resource}.${encodeURIComponent(String(id))}`;
}

export function clearCachedResource(resource: string): void {
  try {
    const listExact = cachedListStorageKey(resource);
    const listPrefix = `${listExact}.`;
    const onePrefix = `andromeda.oneCache.${resource}.`;
    const keys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (!key) continue;
      if (key === listExact || key.startsWith(listPrefix) || key.startsWith(onePrefix)) keys.push(key);
    }
    for (const key of keys) localStorage.removeItem(key);
  } catch {
    /* ignore storage quota / private mode failures */
  }
}

function readCachedList<T>(resource: string): CachedListSnapshot<T> | undefined {
  const parsed = getJSON<Partial<CachedListSnapshot<T>>>(cachedListStorageKey(resource));
  if (!parsed || !Array.isArray(parsed.data) || typeof parsed.savedAt !== "number") return undefined;
  return {
    data: parsed.data,
    total: typeof parsed.total === "number" ? parsed.total : parsed.data.length,
    savedAt: parsed.savedAt,
  };
}

function writeCachedList<T>(resource: string, data: T[], total?: number): void {
  const snapshot: CachedListSnapshot<T> = { data, total: total ?? data.length, savedAt: Date.now() };
  setJSON(cachedListStorageKey(resource), snapshot);
}

function readCachedOne<T>(resource: string, id: BaseKey | undefined): CachedOneSnapshot<T> | undefined {
  if (id === undefined) return undefined;
  const parsed = getJSON<Partial<CachedOneSnapshot<T>>>(cachedOneStorageKey(resource, id));
  if (!parsed || !parsed.data || typeof parsed.savedAt !== "number") return undefined;
  return { data: parsed.data, savedAt: parsed.savedAt };
}

function writeCachedOne<T>(resource: string, id: BaseKey, data: T): void {
  const snapshot: CachedOneSnapshot<T> = { data, savedAt: Date.now() };
  setJSON(cachedOneStorageKey(resource, id), snapshot);
}

export function useCachedList<T extends BaseRecord>(
  resource: string,
  enabled: boolean,
  options: { cacheKey?: string; meta?: Record<string, unknown> } = {},
) {
  const cacheKey = options.cacheKey ?? resource;
  const snapshot = useMemo(() => readCachedList<T>(cacheKey), [cacheKey]);
  const list = useList<T>({
    resource,
    meta: options.meta,
    queryOptions: {
      enabled,
      initialData: snapshot ? { data: snapshot.data, total: snapshot.total } : undefined,
      initialDataUpdatedAt: snapshot?.savedAt,
      staleTime: CACHED_LIST_STALE_MS,
      gcTime: CACHED_LIST_GC_MS,
      refetchOnWindowFocus: false,
    },
  });

  useEffect(() => {
    if (!enabled || !list.query.isSuccess) return;
    writeCachedList(cacheKey, list.result.data, list.result.total);
  }, [cacheKey, enabled, list.query.dataUpdatedAt, list.query.isSuccess, list.result.data, list.result.total]);

  return {
    ...list,
    cache: { hasSnapshot: Boolean(snapshot), savedAt: snapshot?.savedAt, key: cacheKey },
  };
}

export function useCachedOne<T extends BaseRecord>(resource: string, id: BaseKey | undefined, enabled: boolean) {
  const snapshot = useMemo(() => readCachedOne<T>(resource, id), [resource, id]);
  const one = useOne<T>({
    resource,
    id,
    queryOptions: {
      enabled,
      initialData: snapshot ? { data: snapshot.data } : undefined,
      initialDataUpdatedAt: snapshot?.savedAt,
      staleTime: CACHED_ONE_STALE_MS,
      gcTime: CACHED_ONE_GC_MS,
      refetchOnWindowFocus: false,
    },
  });

  useEffect(() => {
    if (!enabled || id === undefined || !one.query.isSuccess || !one.result) return;
    writeCachedOne(resource, id, one.result);
  }, [enabled, id, resource, one.query.dataUpdatedAt, one.query.isSuccess, one.result]);

  return {
    ...one,
    cache: { hasSnapshot: Boolean(snapshot), savedAt: snapshot?.savedAt },
  };
}
