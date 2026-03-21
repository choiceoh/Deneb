import { describe, expect, it } from "vitest";
import type { DenebConfig } from "../config/config.js";
import { resolveAckReaction } from "./identity.js";

describe("resolveAckReaction", () => {
  it("prefers account-level overrides", () => {
    const cfg: DenebConfig = {
      messages: { ackReaction: "👀" },
      agents: { list: [{ id: "main", identity: { emoji: "✅" } }] },
      channels: {
        slack: {
          ackReaction: "eyes",
          accounts: {
            acct1: { ackReaction: " party_parrot " },
          },
        },
      },
    };

    expect(resolveAckReaction(cfg, "main", { channel: "slack", accountId: "acct1" })).toBe(
      "party_parrot",
    );
  });

  it("falls back to channel-level overrides", () => {
    const cfg: DenebConfig = {
      messages: { ackReaction: "👀" },
      agents: { list: [{ id: "main", identity: { emoji: "✅" } }] },
      channels: {
        slack: {
          ackReaction: "eyes",
          accounts: {
            acct1: { ackReaction: "party_parrot" },
          },
        },
      },
    };

    expect(resolveAckReaction(cfg, "main", { channel: "slack", accountId: "missing" })).toBe(
      "eyes",
    );
  });

  it("uses the global ackReaction when channel overrides are missing", () => {
    const cfg: DenebConfig = {
      messages: { ackReaction: "✅" },
      agents: { list: [{ id: "main", identity: { emoji: "😺" } }] },
    };

    expect(resolveAckReaction(cfg, "main", { channel: "discord" })).toBe("✅");
  });

  it("falls back to the agent identity emoji when global config is unset", () => {
    const cfg: DenebConfig = {
      agents: { list: [{ id: "main", identity: { emoji: "🔥" } }] },
    };

    expect(resolveAckReaction(cfg, "main", { channel: "discord" })).toBe("🔥");
  });

  it("returns the default emoji when no config is present", () => {
    const cfg: DenebConfig = {};

    expect(resolveAckReaction(cfg, "main")).toBe("👀");
  });

  it("allows empty strings to disable reactions", () => {
    const cfg: DenebConfig = {
      messages: { ackReaction: "👀" },
      channels: {
        telegram: {
          ackReaction: "",
        },
      },
    };

    expect(resolveAckReaction(cfg, "main", { channel: "telegram" })).toBe("");
  });
});
