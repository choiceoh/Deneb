import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";

import { fleetRecipeAction } from "@/fleet";
import { TOKEN_HEADER } from "@/gateway";
import { server } from "@/mocks/server";

const cfg = { url: "http://mock.local", token: "mock" };

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("fleet client", () => {
  it("accepts successful non-json recipe action responses", async () => {
    let token = "";
    server.use(
      http.post("*/api/v1/fleet/api/recipes/action", ({ request }) => {
        token = request.headers.get(TOKEN_HEADER) ?? "";
        return new HttpResponse("ok", { status: 200, headers: { "Content-Type": "text/plain" } });
      }),
    );

    await expect(fleetRecipeAction(cfg, "deepseek-v4-flash", "launch")).resolves.toEqual({});
    expect(token).toBe("mock");
  });

  it("accepts successful empty recipe action responses", async () => {
    server.use(http.post("*/api/v1/fleet/api/recipes/action", () => new HttpResponse(null, { status: 204 })));

    await expect(fleetRecipeAction(cfg, "qwen36", "stop")).resolves.toEqual({});
  });
});
