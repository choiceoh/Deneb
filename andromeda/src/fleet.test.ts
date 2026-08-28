import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";

import { TOKEN_HEADER } from "@/gateway";
import { fleetJobs, fleetState } from "@/fleet";
import { server } from "@/mocks/server";

const cfg = { url: "http://mock.local", token: "mock" };

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// The write path (recipe actions) left the human client on 2026-08-28 —
// recipes are AI-only via the gateway fleet chat tool. What remains here is
// the read passthrough contract: token header forwarded, JSON decoded.
describe("fleet client", () => {
  it("when reading state then forwards the client token through the passthrough", async () => {
    let seenToken = "";
    server.use(
      http.get("*/api/v1/fleet/api/state", ({ request }) => {
        seenToken = request.headers.get(TOKEN_HEADER) ?? "";
        return HttpResponse.json({ nodes: [{ name: "spark-1" }] });
      }),
    );

    const state = await fleetState(cfg);

    expect(seenToken).toBe("mock");
    expect(state.nodes?.[0]?.name).toBe("spark-1");
  });

  it("when reading jobs then decodes the list shape", async () => {
    server.use(http.get("*/api/v1/fleet/api/jobs", () => HttpResponse.json([{ id: "job-1", state: "running" }])));

    await expect(fleetJobs(cfg)).resolves.toEqual([{ id: "job-1", state: "running" }]);
  });
});
