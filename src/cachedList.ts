import { useEffect, useMemo } from "react";
import { type BaseKey, type BaseRecord, useList, useOne } from "@refinedev/core";

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

function readCachedList<T>(resource: string): CachedListSnapshot<T> | undefined {
  try {
    const raw = localStorage.getItem(cachedListStorageKey(resource));
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as Partial<CachedListSnapshot<T>>;
    if (!Array.isArray(parsed.data) || typeof parsed.savedAt !== "number") return undefined;
    return {
      data: parsed.data,
      total: typeof parsed.total === "number" ? parsed.total : parsed.data.length,
      savedAt: parsed.savedAt,
    };
  } catch {
    return undefined;
  }
}

function writeCachedList<T>(resource: string, data: T[], total?: number): void {
  try {
    const snapshot: CachedListSnapshot<T> = {
      data,
      total: total ?? data.length,
      savedAt: Date.now(),
    };
    localStorage.setItem(cachedListStorageKey(resource), JSON.stringify(snapshot));
  } catch {
    /* ignore storage quota / private mode failures */
  }
}

function readCachedOne<T>(resource: string, id: BaseKey | undefined): CachedOneSnapshot<T> | undefined {
  if (id === undefined) return undefined;
  try {
    const raw = localStorage.getItem(cachedOneStorageKey(resource, id));
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as Partial<CachedOneSnapshot<T>>;
    if (!parsed.data || typeof parsed.savedAt !== "number") return undefined;
    return { data: parsed.data, savedAt: parsed.savedAt };
  } catch {
    return undefined;
  }
}

function writeCachedOne<T>(resource: string, id: BaseKey, data: T): void {
  try {
    const snapshot: CachedOneSnapshot<T> = { data, savedAt: Date.now() };
    localStorage.setItem(cachedOneStorageKey(resource, id), JSON.stringify(snapshot));
  } catch {
    /* ignore storage quota / private mode failures */
  }
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
