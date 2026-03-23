import { describe, expect, it } from "vitest";
import {
  parseAgentSessionKey,
  deriveSessionChatType,
  isCronRunSessionKey,
  isCronSessionKey,
  isSubagentSessionKey,
  getSubagentDepth,
  isAcpSessionKey,
  resolveThreadParentSessionKey,
} from "./session-key-utils.js";

describe("parseAgentSessionKey", () => {
  it("parses canonical agent:agentId:rest format", () => {
    const result = parseAgentSessionKey("agent:myAgent:direct:abc");
    expect(result).toEqual({ agentId: "myagent", rest: "direct:abc" });
  });

  it("normalizes to lowercase", () => {
    const result = parseAgentSessionKey("Agent:MyAgent:Direct:ABC");
    expect(result).toEqual({ agentId: "myagent", rest: "direct:abc" });
  });

  it("returns null for undefined/null/empty", () => {
    expect(parseAgentSessionKey(undefined)).toBeNull();
    expect(parseAgentSessionKey(null)).toBeNull();
    expect(parseAgentSessionKey("")).toBeNull();
    expect(parseAgentSessionKey("  ")).toBeNull();
  });

  it("returns null when fewer than 3 parts", () => {
    expect(parseAgentSessionKey("agent:myAgent")).toBeNull();
    expect(parseAgentSessionKey("agent")).toBeNull();
  });

  it("returns null when first part is not 'agent'", () => {
    expect(parseAgentSessionKey("session:myAgent:rest")).toBeNull();
  });

  it("joins remaining parts with colon for rest", () => {
    const result = parseAgentSessionKey("agent:a1:cron:job1:run:r1");
    expect(result).toEqual({ agentId: "a1", rest: "cron:job1:run:r1" });
  });
});

describe("deriveSessionChatType", () => {
  it("returns 'group' when tokens include 'group'", () => {
    expect(deriveSessionChatType("agent:a1:group:123")).toBe("group");
  });

  it("returns 'channel' when tokens include 'channel'", () => {
    expect(deriveSessionChatType("agent:a1:channel:456")).toBe("channel");
  });

  it("returns 'direct' when tokens include 'direct' or 'dm'", () => {
    expect(deriveSessionChatType("agent:a1:direct:789")).toBe("direct");
    expect(deriveSessionChatType("agent:a1:dm:789")).toBe("direct");
  });

  it("detects legacy Discord guild:channel pattern as 'channel'", () => {
    expect(deriveSessionChatType("discord:acct1:guild-123:channel-456")).toBe("channel");
  });

  it("returns 'unknown' for empty/null/undefined", () => {
    expect(deriveSessionChatType(undefined)).toBe("unknown");
    expect(deriveSessionChatType(null)).toBe("unknown");
    expect(deriveSessionChatType("")).toBe("unknown");
  });

  it("returns 'unknown' when no chat type token is found", () => {
    expect(deriveSessionChatType("agent:a1:something:else")).toBe("unknown");
  });
});

describe("isCronRunSessionKey", () => {
  it("returns true for valid cron run session keys", () => {
    expect(isCronRunSessionKey("agent:a1:cron:job1:run:r1")).toBe(true);
  });

  it("returns false for non-cron agent keys", () => {
    expect(isCronRunSessionKey("agent:a1:direct:abc")).toBe(false);
  });

  it("returns false for non-agent keys", () => {
    expect(isCronRunSessionKey("cron:job1:run:r1")).toBe(false);
  });

  it("returns false for empty/null", () => {
    expect(isCronRunSessionKey(null)).toBe(false);
    expect(isCronRunSessionKey(undefined)).toBe(false);
  });
});

describe("isCronSessionKey", () => {
  it("returns true when rest starts with 'cron:'", () => {
    expect(isCronSessionKey("agent:a1:cron:job1")).toBe(true);
    expect(isCronSessionKey("agent:a1:cron:job1:run:r1")).toBe(true);
  });

  it("returns false for non-cron keys", () => {
    expect(isCronSessionKey("agent:a1:direct:abc")).toBe(false);
  });

  it("returns false for null/undefined", () => {
    expect(isCronSessionKey(null)).toBe(false);
  });
});

describe("isSubagentSessionKey", () => {
  it("returns true when key starts with 'subagent:'", () => {
    expect(isSubagentSessionKey("subagent:abc")).toBe(true);
  });

  it("returns true when agent rest starts with 'subagent:'", () => {
    expect(isSubagentSessionKey("agent:a1:subagent:s1")).toBe(true);
  });

  it("returns false for non-subagent keys", () => {
    expect(isSubagentSessionKey("agent:a1:direct:abc")).toBe(false);
  });

  it("returns false for empty/null", () => {
    expect(isSubagentSessionKey(null)).toBe(false);
    expect(isSubagentSessionKey("")).toBe(false);
  });
});

describe("getSubagentDepth", () => {
  it("returns 0 for no subagent markers", () => {
    expect(getSubagentDepth("agent:a1:direct:abc")).toBe(0);
  });

  it("returns 1 for single subagent nesting", () => {
    expect(getSubagentDepth("agent:a1:subagent:s1")).toBe(1);
  });

  it("returns 2 for double nesting", () => {
    expect(getSubagentDepth("agent:a1:subagent:s1:subagent:s2")).toBe(2);
  });

  it("returns 0 for empty/null", () => {
    expect(getSubagentDepth(null)).toBe(0);
    expect(getSubagentDepth("")).toBe(0);
  });
});

describe("isAcpSessionKey", () => {
  it("returns true when key starts with 'acp:'", () => {
    expect(isAcpSessionKey("acp:something")).toBe(true);
  });

  it("returns true when agent rest starts with 'acp:'", () => {
    expect(isAcpSessionKey("agent:a1:acp:conn1")).toBe(true);
  });

  it("returns false for non-acp keys", () => {
    expect(isAcpSessionKey("agent:a1:direct:abc")).toBe(false);
  });

  it("returns false for empty/null", () => {
    expect(isAcpSessionKey(null)).toBe(false);
    expect(isAcpSessionKey("")).toBe(false);
  });
});

describe("resolveThreadParentSessionKey", () => {
  it("extracts parent key before :thread: marker", () => {
    expect(resolveThreadParentSessionKey("agent:a1:direct:abc:thread:t1")).toBe(
      "agent:a1:direct:abc",
    );
  });

  it("extracts parent key before :topic: marker", () => {
    expect(resolveThreadParentSessionKey("agent:a1:channel:c1:topic:t1")).toBe(
      "agent:a1:channel:c1",
    );
  });

  it("uses last marker when multiple exist", () => {
    expect(resolveThreadParentSessionKey("a:thread:b:topic:c")).toBe("a:thread:b");
  });

  it("returns null for no thread marker", () => {
    expect(resolveThreadParentSessionKey("agent:a1:direct:abc")).toBeNull();
  });

  it("returns null for empty/null/undefined", () => {
    expect(resolveThreadParentSessionKey(null)).toBeNull();
    expect(resolveThreadParentSessionKey(undefined)).toBeNull();
    expect(resolveThreadParentSessionKey("")).toBeNull();
  });

  it("returns null when marker is at position 0", () => {
    expect(resolveThreadParentSessionKey(":thread:abc")).toBeNull();
  });
});
