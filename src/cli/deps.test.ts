import { beforeEach, describe, expect, it, vi } from "vitest";
import { createDefaultDeps } from "./deps.js";

const moduleLoads = vi.hoisted(() => ({
  telegram: vi.fn(),
}));

const sendFns = vi.hoisted(() => ({
  telegram: vi.fn(async () => ({ messageId: "t1", chatId: "telegram:1" })),
}));

vi.mock("./send-runtime/telegram.js", () => {
  moduleLoads.telegram();
  return { runtimeSend: { sendMessage: sendFns.telegram } };
});

describe("createDefaultDeps", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("does not load provider modules until a dependency is used", async () => {
    const deps = createDefaultDeps();

    expect(moduleLoads.telegram).not.toHaveBeenCalled();

    const sendTelegram = deps["telegram"] as (...args: unknown[]) => Promise<unknown>;
    await sendTelegram("chat", "hello", { verbose: false });

    expect(moduleLoads.telegram).toHaveBeenCalledTimes(1);
    expect(sendFns.telegram).toHaveBeenCalledTimes(1);
  });

  it("reuses module cache after first dynamic import", async () => {
    const deps = createDefaultDeps();
    const sendTelegram = deps["telegram"] as (...args: unknown[]) => Promise<unknown>;

    await sendTelegram("chat", "first", { verbose: false });
    await sendTelegram("chat", "second", { verbose: false });

    expect(moduleLoads.telegram).toHaveBeenCalledTimes(1);
    expect(sendFns.telegram).toHaveBeenCalledTimes(2);
  });
});
