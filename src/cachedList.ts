import { useEffect, useMemo } from "react";
import { type BaseRecord, useList } from "@refinedev/core";

export const CACHED_LIST_STALE_MS = 60_000;
export const CACHED_LIST_GC_MS = 10 * 60_000;

interface CachedListSnapshot<T> {
  data: T[];
  total: number;
  savedAt: number;
}

export function cachedListStorageKey(resource: string): string {
  return `andromeda.listCache.${resource}`;
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

export function useCachedList<T extends BaseRecord>(resource: string, enabled: boolean) {
  const snapshot = useMemo(() => readCachedList<T>(resource), [resource]);
  const list = useList<T>({
    resource,
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
    writeCachedList(resource, list.result.data, list.result.total);
  }, [enabled, resource, list.query.dataUpdatedAt, list.query.isSuccess, list.result.data, list.result.total]);

  return {
    ...list,
    cache: { hasSnapshot: Boolean(snapshot), savedAt: snapshot?.savedAt },
  };
}
