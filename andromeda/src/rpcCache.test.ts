import { afterEach, describe, expect, it } from "vitest";
import { cachedRpcStorageKey, clearCachedRpcResource, readCachedRpc, rpcCacheKey, writeCachedRpc } from "./rpcCache";

afterEach(() => {
  localStorage.clear();
});

describe("rpcCache", () => {
  it("builds stable keys for equivalent params", () => {
    expect(rpcCacheKey("miniapp.memory.search", { limit: 20, query: "설계" })).toBe(
      rpcCacheKey("miniapp.memory.search", { query: "설계", limit: 20 }),
    );
  });

  it("round-trips one RPC result and clears by resource", () => {
    const key = rpcCacheKey("miniapp.files.list", { path: "", limit: 300 });
    writeCachedRpc("files", key, { entries: [{ name: "cached.pdf" }] });
    localStorage.setItem(cachedRpcStorageKey("wiki", key), "keep");

    expect(readCachedRpc<{ entries: Array<{ name: string }> }>("files", key)?.data.entries[0].name).toBe("cached.pdf");

    clearCachedRpcResource("files");

    expect(readCachedRpc("files", key)).toBeUndefined();
    expect(localStorage.getItem(cachedRpcStorageKey("wiki", key))).toBe("keep");
  });
});
