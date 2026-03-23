import { describe, expect, it } from "vitest";
import { sanitizeToolUseResultPairing } from "./transcript-repair.js";

type TestMessage = {
  role: string;
  content?: unknown;
  toolCallId?: string;
  toolUseId?: string;
  toolName?: string;
  stopReason?: string;
  isError?: boolean;
};

function assistantWithToolCalls(...calls: Array<{ id: string; name?: string }>): TestMessage {
  return {
    role: "assistant",
    content: calls.map((c) => ({ type: "tool_use", id: c.id, name: c.name ?? "tool" })),
  };
}

function toolResult(toolCallId: string, text = "result"): TestMessage {
  return {
    role: "toolResult",
    toolCallId,
    content: [{ type: "text", text }],
  };
}

describe("sanitizeToolUseResultPairing", () => {
  it("returns original array when no repairs needed", () => {
    const messages: TestMessage[] = [
      { role: "user", content: "hello" },
      assistantWithToolCalls({ id: "c1" }),
      toolResult("c1"),
    ];
    const result = sanitizeToolUseResultPairing(messages);
    expect(result).toBe(messages);
  });

  it("inserts synthetic error result for missing tool results", () => {
    const messages: TestMessage[] = [
      { role: "user", content: "hello" },
      assistantWithToolCalls({ id: "c1", name: "myTool" }),
      // Missing tool result for c1
      { role: "user", content: "next" },
    ];
    const result = sanitizeToolUseResultPairing(messages);
    expect(result).not.toBe(messages);
    expect(result.some((m) => m.toolCallId === "c1" && m.isError === true)).toBe(true);
  });

  it("drops duplicate tool results for the same ID", () => {
    // Second assistant turn also has c1, creating a duplicate scenario
    const messages: TestMessage[] = [
      assistantWithToolCalls({ id: "c1" }),
      toolResult("c1", "first"),
      assistantWithToolCalls({ id: "c2" }),
      toolResult("c1", "duplicate"), // duplicate c1 result
      toolResult("c2"),
    ];
    const result = sanitizeToolUseResultPairing(messages);
    const c1Results = result.filter((m) => m.toolCallId === "c1" && m.role === "toolResult");
    // First c1 result is kept, duplicate is dropped
    expect(c1Results).toHaveLength(1);
  });

  it("drops orphaned tool results with no matching call", () => {
    const messages: TestMessage[] = [{ role: "user", content: "hello" }, toolResult("orphan")];
    const result = sanitizeToolUseResultPairing(messages);
    expect(result.some((m) => m.toolCallId === "orphan")).toBe(false);
  });

  it("handles multiple tool calls in one assistant message", () => {
    const messages: TestMessage[] = [
      assistantWithToolCalls({ id: "c1" }, { id: "c2" }),
      toolResult("c1"),
      toolResult("c2"),
    ];
    const result = sanitizeToolUseResultPairing(messages);
    expect(result).toBe(messages);
  });

  it("skips tool call extraction for aborted messages", () => {
    const messages: TestMessage[] = [
      { role: "assistant", content: [{ type: "tool_use", id: "c1" }], stopReason: "error" },
      { role: "user", content: "retry" },
    ];
    const result = sanitizeToolUseResultPairing(messages);
    // Should not insert synthetic results for aborted messages
    expect(result.filter((m) => m.isError === true)).toHaveLength(0);
  });

  it("normalizes reasoning blocks before tool calls", () => {
    const messages: TestMessage[] = [
      {
        role: "assistant",
        content: [
          { type: "function_call", id: "c1", name: "fn" },
          { type: "reasoning", text: "thought" },
        ],
      },
      toolResult("c1"),
    ];
    const result = sanitizeToolUseResultPairing(messages);
    // Reasoning should be moved before tool call
    const assistantMsg = result.find((m) => m.role === "assistant");
    const content = assistantMsg?.content as Array<{ type: string }>;
    expect(content[0].type).toBe("reasoning");
    expect(content[1].type).toBe("function_call");
  });

  it("preserves user messages between tool calls and results", () => {
    const messages: TestMessage[] = [
      assistantWithToolCalls({ id: "c1" }),
      { role: "user", content: "interruption" },
      toolResult("c1"),
    ];
    const result = sanitizeToolUseResultPairing(messages);
    // Tool result should be placed right after assistant, user message after
    const roles = result.map((m) => m.role);
    const assistantIdx = roles.indexOf("assistant");
    expect(roles[assistantIdx + 1]).toBe("toolResult");
    expect(roles[assistantIdx + 2]).toBe("user");
  });
});
