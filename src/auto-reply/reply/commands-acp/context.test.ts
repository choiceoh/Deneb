import { beforeEach, describe, expect, it } from "vitest";
import type { DenebConfig } from "../../../config/config.js";
import { __testing as sessionBindingTesting } from "../../../infra/outbound/session-binding-service.js";
import { buildCommandTestParams } from "../commands-spawn.test-harness.js";
import { resolveAcpCommandBindingContext, resolveAcpCommandConversationId } from "./context.js";

const baseCfg = {
  session: { mainKey: "main", scope: "per-sender" },
} satisfies DenebConfig;

describe("commands-acp context", () => {
  beforeEach(() => {
    sessionBindingTesting.resetSessionBindingAdaptersForTests();
  });

  it("builds canonical telegram topic conversation ids from originating chat + thread", () => {
    const params = buildCommandTestParams("/acp status", baseCfg, {
      Provider: "telegram",
      Surface: "telegram",
      OriginatingChannel: "telegram",
      OriginatingTo: "telegram:-1001234567890",
      MessageThreadId: "42",
    });

    expect(resolveAcpCommandBindingContext(params)).toEqual({
      channel: "telegram",
      accountId: "default",
      threadId: "42",
      conversationId: "-1001234567890:topic:42",
      parentConversationId: "-1001234567890",
    });
    expect(resolveAcpCommandConversationId(params)).toBe("-1001234567890:topic:42");
  });

  it("resolves Telegram DM conversation ids from telegram targets", () => {
    const params = buildCommandTestParams("/acp status", baseCfg, {
      Provider: "telegram",
      Surface: "telegram",
      OriginatingChannel: "telegram",
      OriginatingTo: "telegram:123456789",
    });

    expect(resolveAcpCommandBindingContext(params)).toEqual({
      channel: "telegram",
      accountId: "default",
      threadId: undefined,
      conversationId: "123456789",
      parentConversationId: "123456789",
    });
    expect(resolveAcpCommandConversationId(params)).toBe("123456789");
  });
});
