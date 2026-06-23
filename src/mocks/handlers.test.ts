import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { callRpc, ping } from "@/gateway";
import { server } from "./server";

const cfg = { url: "http://mock.local", token: "mock" };

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("mock gateway handlers", () => {
  it("answers ping with a version", async () => {
    expect(await ping(cfg)).toMatchObject({ ok: true, version: "mock" });
  });

  it("lists todos through the RPC envelope (rows wrapped under a payload key)", async () => {
    // The gateway wraps lists as { todos: [...] }; callRpc returns the raw
    // payload and the data provider is what unwraps it (see dataProvider.test).
    const payload = await callRpc<{ todos: { title: string }[] }>(cfg, "miniapp.todo.list");
    expect(payload.todos.length).toBeGreaterThan(0);
    expect(payload.todos[0]).toHaveProperty("title");
  });

  it("surfaces unknown methods as an envelope error", async () => {
    await expect(callRpc(cfg, "miniapp.nope")).rejects.toThrow(/unknown method/);
  });

  it("answers wiki browse and file browse RPCs", async () => {
    const categories = await callRpc<{ categories: { name: string }[] }>(cfg, "miniapp.memory.categories");
    expect(categories.categories[0]).toHaveProperty("name");

    const files = await callRpc<{ entries: { name: string }[] }>(cfg, "miniapp.files.list", { path: "" });
    expect(files.entries.some((entry) => entry.name === "projects")).toBe(true);
  });
});
