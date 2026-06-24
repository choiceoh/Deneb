import { useCallback, useRef, useState } from "react";

import { type GatewayConfig, callRpc } from "@/gateway";
import { errText } from "@/format";
import { readCachedRpc, rpcCacheKey, writeCachedRpc } from "@/rpcCache";

const DEFAULT_STALE_MS = 5 * 60_000;

export type RpcResult<T> = { ok: true; data: T } | { ok: false };
export type CachedRpcResult<T> = { ok: true; data: T; applied: boolean } | { ok: false };

type Source = "cache" | "network";

interface CachedCallOptions<T> {
  cacheKey?: string;
  pending?: string;
  staleTimeMs?: number;
  scope?: string;
  apply?: (data: T, source: Source) => void;
}

export function useCachedRpc(cfg: GatewayConfig, resource: string, defaultStaleTimeMs = DEFAULT_STALE_MS) {
  const [status, setStatus] = useState("");
  const [busyCount, setBusyCount] = useState(0);
  const seqRef = useRef(0);
  const latestByScopeRef = useRef(new Map<string, number>());

  const readCache = useCallback(
    <T>(method: string, params: Record<string, unknown> = {}, staleTimeMs = defaultStaleTimeMs) =>
      readCachedRpc<T>(resource, rpcCacheKey(method, params), { staleTimeMs }),
    [defaultStaleTimeMs, resource],
  );

  const writeCache = useCallback(
    <T>(method: string, params: Record<string, unknown> = {}, data: T) => {
      writeCachedRpc(resource, rpcCacheKey(method, params), data);
    },
    [resource],
  );

  const begin = useCallback((pending?: string) => {
    setBusyCount((n) => n + 1);
    if (pending !== undefined) setStatus(pending);
  }, []);

  const end = useCallback(() => {
    setBusyCount((n) => Math.max(0, n - 1));
  }, []);

  const call = useCallback(
    async <T>(method: string, params: Record<string, unknown> = {}, pending?: string): Promise<RpcResult<T>> => {
      begin(pending);
      try {
        return { ok: true, data: await callRpc<T>(cfg, method, params) };
      } catch (e) {
        setStatus(`오류: ${errText(e)}`);
        return { ok: false };
      } finally {
        end();
      }
    },
    [begin, cfg, end],
  );

  const callCached = useCallback(
    async <T>(
      method: string,
      params: Record<string, unknown> = {},
      options: CachedCallOptions<T> = {},
    ): Promise<CachedRpcResult<T>> => {
      const cacheKey = options.cacheKey ?? rpcCacheKey(method, params);
      const staleTimeMs = options.staleTimeMs ?? defaultStaleTimeMs;
      const scope = options.scope ?? cacheKey;
      const requestId = ++seqRef.current;
      latestByScopeRef.current.set(scope, requestId);

      const cached = readCachedRpc<T>(resource, cacheKey, { staleTimeMs });
      if (cached) options.apply?.(cached.data, "cache");

      begin(options.pending);
      try {
        const data = await callRpc<T>(cfg, method, params);
        const applied = latestByScopeRef.current.get(scope) === requestId;
        if (applied) {
          writeCachedRpc(resource, cacheKey, data);
          options.apply?.(data, "network");
        }
        return { ok: true, data, applied };
      } catch (e) {
        if (latestByScopeRef.current.get(scope) === requestId) setStatus(`오류: ${errText(e)}`);
        return { ok: false };
      } finally {
        end();
      }
    },
    [begin, cfg, defaultStaleTimeMs, end, resource],
  );

  return {
    call,
    callCached,
    readCache,
    writeCache,
    status,
    setStatus,
    busy: busyCount > 0,
  };
}
