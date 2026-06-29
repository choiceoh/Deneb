// The work feed must refresh when a proactive card arrives — both instantly off
// the live `events` SSE nudge (hooks.useEvents) and via the durable catch-up poll
// that recovers a missed push (sync.useNativeSync). Both paths reconcile by
// invalidating the Refine "workfeed" list, so we assert the list refetches.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { waitFor } from "@testing-library/react";
import { type DataProvider, useList } from "@refinedev/core";

import type { GatewayConfig } from "@/gateway";
import { useEvents } from "@/hooks";
import { loadSyncCursor, saveSyncCursor, useNativeSync } from "@/sync";
import { fakeProvider, renderWithProviders } from "@/test/util";

const CFG: GatewayConfig = { url: "http://test", token: "tok" };

interface RpcBody {
  method?: string;
  params?: Record<string, unknown>;
}

// A data provider whose getList we can count per resource — refetch == a second
// call for "workfeed".
function spyProvider() {
  const base = fakeProvider();
  const resources: string[] = [];
  const provider: DataProvider = {
    ...base,
    getList: async (params) => {
      resources.push(params.resource);
      return base.getList!(params);
    },
  };
  return { provider, workfeedCalls: () => resources.filter((r) => r === "workfeed").length };
}

function sseResponse(body: string): Response {
  const enc = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(body));
      controller.close();
    },
  });
  return new Response(stream, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

// Stub fetch: RPCs return an envelope from `rpc(method)`, the events SSE returns
// `eventsBody`. Mirrors the gateway wire shape the real callRpc/subscribeEvents read.
function stubFetch(rpc: (method: string, params: Record<string, unknown>) => unknown, eventsBody = "") {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).includes("/miniapp/events")) return sseResponse(eventsBody);
      const body = JSON.parse(String(init?.body ?? "{}")) as RpcBody;
      const payload = rpc(String(body.method ?? ""), body.params ?? {});
      return new Response(JSON.stringify({ ok: true, payload }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

beforeEach(() => {
  if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("useNativeSync (durable catch-up poll)", () => {
  function Probe() {
    useNativeSync(CFG, true);
    useList({ resource: "workfeed", queryOptions: { enabled: true } });
    return null;
  }

  it("refetches the work feed when the catch-up pull reports a new card", async () => {
    saveSyncCursor(0); // an existing session — not a fresh-install baseline
    stubFetch((method) =>
      method === "miniapp.sync.pull"
        ? { events: [{ seq: 1, type: "workfeed.created" }], cursor: 1, latestSeq: 1, hasMore: false, count: 1 }
        : { ok: true },
    );
    const { provider, workfeedCalls } = spyProvider();

    renderWithProviders(<Probe />, { connected: true, dataProvider: provider, cfg: CFG });

    await waitFor(() => expect(workfeedCalls()).toBeGreaterThanOrEqual(2));
    expect(loadSyncCursor()).toBe(1);
  });

  it("only baselines the cursor on a fresh install (no startup refetch)", async () => {
    // No stored cursor → first run: jump to latestSeq, do not invalidate.
    let sawWorkfeedPull = false;
    stubFetch((method, params) => {
      if (method === "miniapp.sync.pull") {
        if (params.cursor === 0 && params.limit === 1) sawWorkfeedPull = true;
        return { events: [], cursor: 0, latestSeq: 7, hasMore: false, count: 0 };
      }
      return { ok: true };
    });
    const { provider } = spyProvider();

    renderWithProviders(<Probe />, { connected: true, dataProvider: provider, cfg: CFG });

    await waitFor(() => expect(loadSyncCursor()).toBe(7));
    expect(sawWorkfeedPull).toBe(true);
  });
});

describe("useEvents (instant refresh on a live nudge)", () => {
  function Probe() {
    useEvents(CFG, true);
    useList({ resource: "workfeed", queryOptions: { enabled: true } });
    return null;
  }

  it("refetches the work feed the moment a workfeed nudge arrives", async () => {
    stubFetch(() => ({ ok: true }), 'data: {"id":"e1","kind":"workfeed","title":"새 작업"}\n\n');
    const { provider, workfeedCalls } = spyProvider();

    renderWithProviders(<Probe />, { connected: true, dataProvider: provider, cfg: CFG });

    await waitFor(() => expect(workfeedCalls()).toBeGreaterThanOrEqual(2));
  });
});
