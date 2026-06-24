import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fleetState } from "@/fleet";
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

  it("answers notebook list, detail, and delete RPCs", async () => {
    const list = await callRpc<{ notebooks: { id: string; name: string }[] }>(cfg, "miniapp.notebook.list");
    expect(list.notebooks[0]).toMatchObject({ id: "nb1", name: "탑솔라 2차 계약" });

    const detail = await callRpc<{ sources?: { title?: string }[] }>(cfg, "miniapp.notebook.get", { id: "nb1" });
    expect(detail.sources?.[0]).toMatchObject({ title: "잔금 안내" });

    await expect(callRpc(cfg, "miniapp.notebook.delete", { id: "nb1" })).resolves.toMatchObject({
      deleted: true,
      id: "nb1",
    });
  });

  it("answers prompt list, detail, update, and reset RPCs", async () => {
    const list = await callRpc<{ prompts: { id: string; title: string }[] }>(cfg, "miniapp.prompts.list");
    expect(list.prompts[0]).toMatchObject({ id: "mail.analysis", title: "메일 분석" });

    const detail = await callRpc<{ text?: string }>(cfg, "miniapp.prompts.get", { id: "mail.analysis" });
    expect(detail.text).toMatch(/메일 본문/);

    await expect(
      callRpc(cfg, "miniapp.prompts.update", { id: "mail.analysis", text: "새 지시" }),
    ).resolves.toMatchObject({
      id: "mail.analysis",
      text: "새 지시",
      overridden: true,
    });

    await expect(callRpc(cfg, "miniapp.prompts.reset", { id: "mail.analysis" })).resolves.toMatchObject({
      id: "mail.analysis",
      overridden: false,
    });
  });

  it("answers fleet passthrough reads", async () => {
    const state = await fleetState(cfg);
    expect(state.nodes?.some((node) => node.name === "srv1")).toBe(true);
  });
});
