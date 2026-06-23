import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

import { type GatewayConfig } from "@/gateway";
import { cachedRpcStorageKey, readCachedRpc, rpcCacheKey } from "@/rpcCache";
import { type CachedRpcResult, useCachedRpc } from "./useCachedRpc";

const cfg: GatewayConfig = { url: "http://test", token: "tok" };

function rpcResponse(payload: unknown): Response {
  return { ok: true, json: async () => ({ ok: true, payload }) } as unknown as Response;
}

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("useCachedRpc", () => {
  it("uses fresh cached data before refreshing from the gateway", async () => {
    const method = "miniapp.search.all";
    const params = { query: "설계" };
    localStorage.setItem(
      cachedRpcStorageKey("search", rpcCacheKey(method, params)),
      JSON.stringify({ data: { results: ["cache"] }, savedAt: Date.now() }),
    );
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => rpcResponse({ results: ["network"] })),
    );

    const applied: string[] = [];
    const { result } = renderHook(() => useCachedRpc(cfg, "search"));

    await act(async () => {
      const r = await result.current.callCached<{ results: string[] }>(method, params, {
        apply: (data) => applied.push(data.results[0]),
      });
      expect(r.ok && r.applied).toBe(true);
    });

    expect(applied).toEqual(["cache", "network"]);
  });

  it("ignores expired snapshots", () => {
    const method = "miniapp.memory.search";
    const params = { query: "오래됨" };
    localStorage.setItem(
      cachedRpcStorageKey("wiki", rpcCacheKey(method, params)),
      JSON.stringify({ data: { results: ["old"] }, savedAt: Date.now() - 10_000 }),
    );

    const { result } = renderHook(() => useCachedRpc(cfg, "wiki", 1000));

    expect(result.current.readCache(method, params)).toBeUndefined();
  });

  it("does not apply or cache a late response after a newer request in the same scope", async () => {
    type Payload = { path: string };
    const method = "miniapp.files.list";
    const resolvers: Array<(value: Response) => void> = [];
    vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>((resolve) => resolvers.push(resolve))),
    );

    const applied: string[] = [];
    const { result } = renderHook(() => useCachedRpc(cfg, "files"));

    let first!: Promise<CachedRpcResult<Payload>>;
    let second!: Promise<CachedRpcResult<Payload>>;
    act(() => {
      first = result.current.callCached<Payload>(
        method,
        { path: "", limit: 300 },
        {
          scope: "files:entries",
          apply: (data) => applied.push(data.path),
        },
      );
      second = result.current.callCached<Payload>(
        method,
        { path: "projects", limit: 300 },
        {
          scope: "files:entries",
          apply: (data) => applied.push(data.path),
        },
      );
    });

    await act(async () => {
      resolvers[1](rpcResponse({ path: "projects" }));
      await second;
    });
    await act(async () => {
      resolvers[0](rpcResponse({ path: "" }));
      await first;
    });

    expect(applied).toEqual(["projects"]);
    expect(readCachedRpc("files", rpcCacheKey(method, { path: "", limit: 300 }))).toBeUndefined();
    expect(readCachedRpc<Payload>("files", rpcCacheKey(method, { path: "projects", limit: 300 }))?.data.path).toBe(
      "projects",
    );
  });
});
