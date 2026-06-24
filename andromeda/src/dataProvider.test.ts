import { http, HttpResponse } from "msw";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";

import { denebDataProvider } from "./dataProvider";
import { server } from "./mocks/server";

const cfg = { url: "http://mock.local", token: "mock" };
const provider = denebDataProvider(cfg);

// Reply to the next RPC call with a given payload (wrapped in the ok envelope).
const rpcReturns = (payload: unknown) =>
  server.use(http.post("*/api/v1/miniapp/rpc", () => HttpResponse.json({ ok: true, payload })));

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("denebDataProvider getList — payload unwrapping", () => {
  it("unwraps rows from the registry listKey (people → { people: [...] })", async () => {
    rpcReturns({ people: [{ id: "p1", name: "김대희" }], windowDays: 30, scannedCount: 1 });
    const { data } = await provider.getList({ resource: "people" });
    expect(data).toHaveLength(1);
    expect(data[0]).toMatchObject({ name: "김대희" });
  });

  it("uses the gateway's total when present (crons → { jobs, total })", async () => {
    rpcReturns({ jobs: [{ id: "c1" }, { id: "c2" }], total: 7 });
    const { data, total } = await provider.getList({ resource: "crons" });
    expect(data).toHaveLength(2);
    expect(total).toBe(7); // not data.length — pagination-aware
  });

  it("falls back to the sole array property when no listKey matches", async () => {
    // Simulate a renamed wrapper key the registry doesn't know yet.
    rpcReturns({ rows: [{ id: "x1" }], note: "meta" });
    const { data } = await provider.getList({ resource: "people" });
    expect(data).toHaveLength(1);
  });

  it("still accepts a bare array payload", async () => {
    rpcReturns([{ id: "t1", title: "할일" }]);
    const { data } = await provider.getList({ resource: "todo" });
    expect(data).toHaveLength(1);
  });

  it("yields an empty list (not a throw) when the payload has no array", async () => {
    rpcReturns({ message: "no data" });
    const { data, total } = await provider.getList({ resource: "todo" });
    expect(data).toEqual([]);
    expect(total).toBe(0);
  });

  it("passes explicit RPC params from Refine meta", async () => {
    let seen: unknown;
    server.use(
      http.post("*/api/v1/miniapp/rpc", async ({ request }) => {
        seen = await request.json();
        return HttpResponse.json({ ok: true, payload: { events: [] } });
      }),
    );

    await provider.getList({
      resource: "calendar-range",
      meta: { rpcParams: { from: "2026-06-01T00:00:00Z", to: "2026-07-01T00:00:00Z" } },
    });

    expect(seen).toMatchObject({
      method: "miniapp.calendar.list_range",
      params: { from: "2026-06-01T00:00:00Z", to: "2026-07-01T00:00:00Z" },
    });
  });
});
