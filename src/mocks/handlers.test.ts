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

  it("lists todos through the RPC envelope", async () => {
    const todos = await callRpc<{ title: string }[]>(cfg, "miniapp.todo.list");
    expect(todos.length).toBeGreaterThan(0);
    expect(todos[0]).toHaveProperty("title");
  });

  it("surfaces unknown methods as an envelope error", async () => {
    await expect(callRpc(cfg, "miniapp.nope")).rejects.toThrow(/unknown method/);
  });
});
