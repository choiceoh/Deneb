import { describe, expect, it } from "vitest";
import type { SessionSendPolicyConfig } from "../config/types.base.js";
import { deriveScopeChannel, deriveScopeChatType, isScopeAllowed } from "./session-scope.js";

describe("session scope", () => {
  const allowDirect: SessionSendPolicyConfig = {
    default: "deny",
    rules: [{ action: "allow", match: { chatType: "direct" } }],
  };

  it("derives channel and chat type from canonical keys once", () => {
    expect(deriveScopeChannel("Workspace:group:123")).toBe("workspace");
    expect(deriveScopeChatType("Workspace:group:123")).toBe("group");
  });

  it("derives channel and chat type from stored key suffixes", () => {
    expect(deriveScopeChannel("agent:agent-1:workspace:channel:chan-123")).toBe("workspace");
    expect(deriveScopeChatType("agent:agent-1:workspace:channel:chan-123")).toBe("channel");
  });

  it("treats parsed keys with no chat prefix as direct", () => {
    expect(deriveScopeChannel("agent:agent-1:peer-direct")).toBeUndefined();
    expect(deriveScopeChatType("agent:agent-1:peer-direct")).toBe("direct");
    expect(isScopeAllowed(allowDirect, "agent:agent-1:peer-direct")).toBe(true);
    expect(isScopeAllowed(allowDirect, "agent:agent-1:peer:group:abc")).toBe(false);
  });

  it("applies scoped key-prefix checks against normalized key", () => {
    const scope: SessionSendPolicyConfig = {
      default: "deny",
      rules: [{ action: "allow", match: { keyPrefix: "workspace:" } }],
    };
    expect(isScopeAllowed(scope, "agent:agent-1:workspace:group:123")).toBe(true);
    expect(isScopeAllowed(scope, "agent:agent-1:other:group:123")).toBe(false);
  });

  it("supports rawKeyPrefix matches for agent-prefixed keys", () => {
    const scope: SessionSendPolicyConfig = {
      default: "allow",
      rules: [{ action: "deny", match: { rawKeyPrefix: "agent:main:discord:" } }],
    };
    expect(isScopeAllowed(scope, "agent:main:discord:channel:c123")).toBe(false);
    expect(isScopeAllowed(scope, "agent:main:slack:channel:c123")).toBe(true);
  });

  it("keeps legacy agent-prefixed keyPrefix rules working", () => {
    const scope: SessionSendPolicyConfig = {
      default: "allow",
      rules: [{ action: "deny", match: { keyPrefix: "agent:main:discord:" } }],
    };
    expect(isScopeAllowed(scope, "agent:main:discord:channel:c123")).toBe(false);
    expect(isScopeAllowed(scope, "agent:main:slack:channel:c123")).toBe(true);
  });
});
