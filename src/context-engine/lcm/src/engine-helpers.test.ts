import { describe, expect, it } from "vitest";
import {
  estimateTokens,
  toJson,
  safeString,
  asRecord,
  safeBoolean,
  appendTextValue,
  extractReasoningText,
  normalizeUnknownBlock,
  toPartType,
  extractMessageContent,
  toRuntimeRoleForTokenEstimate,
  isTextBlock,
  toDbRole,
  messageIdentity,
  isBootstrapMessage,
  estimateMessageContentTokensForAfterTurn,
  estimateSessionTokenCountForAfterTurn,
} from "./engine-helpers.js";

describe("estimateTokens", () => {
  it("estimates ~4 chars per token", () => {
    expect(estimateTokens("1234")).toBe(1);
    expect(estimateTokens("12345678")).toBe(2);
    expect(estimateTokens("123")).toBe(1); // ceil(3/4) = 1
  });

  it("returns 0 for empty string", () => {
    expect(estimateTokens("")).toBe(0);
  });
});

describe("toJson", () => {
  it("serializes objects", () => {
    expect(toJson({ a: 1 })).toBe('{"a":1}');
  });

  it("handles undefined (returns empty string)", () => {
    expect(toJson(undefined)).toBe("");
  });

  it("handles strings", () => {
    expect(toJson("hello")).toBe('"hello"');
  });
});

describe("safeString", () => {
  it("returns string values", () => {
    expect(safeString("hello")).toBe("hello");
  });

  it("returns undefined for non-strings", () => {
    expect(safeString(123)).toBeUndefined();
    expect(safeString(null)).toBeUndefined();
    expect(safeString(undefined)).toBeUndefined();
  });
});

describe("asRecord", () => {
  it("returns object values", () => {
    const obj = { a: 1 };
    expect(asRecord(obj)).toBe(obj);
  });

  it("returns undefined for non-objects", () => {
    expect(asRecord(null)).toBeUndefined();
    expect(asRecord("string")).toBeUndefined();
    expect(asRecord([1, 2])).toBeUndefined();
  });
});

describe("safeBoolean", () => {
  it("returns boolean values", () => {
    expect(safeBoolean(true)).toBe(true);
    expect(safeBoolean(false)).toBe(false);
  });

  it("returns undefined for non-booleans", () => {
    expect(safeBoolean(1)).toBeUndefined();
    expect(safeBoolean("true")).toBeUndefined();
  });
});

describe("appendTextValue", () => {
  it("appends string values", () => {
    const out: string[] = [];
    appendTextValue("hello", out);
    expect(out).toEqual(["hello"]);
  });

  it("recurses into arrays", () => {
    const out: string[] = [];
    appendTextValue(["a", "b"], out);
    expect(out).toEqual(["a", "b"]);
  });

  it("extracts text/value from objects", () => {
    const out: string[] = [];
    appendTextValue({ text: "hello" }, out);
    expect(out).toEqual(["hello"]);
  });

  it("ignores non-string, non-array, non-object values", () => {
    const out: string[] = [];
    appendTextValue(123, out);
    appendTextValue(null, out);
    expect(out).toEqual([]);
  });
});

describe("extractReasoningText", () => {
  it("extracts text from summary field", () => {
    expect(extractReasoningText({ summary: "thinking about it" })).toBe("thinking about it");
  });

  it("returns undefined when no summary", () => {
    expect(extractReasoningText({ other: "value" })).toBeUndefined();
  });

  it("deduplicates chunks", () => {
    expect(extractReasoningText({ summary: ["same", "same", "different"] })).toBe(
      "same\ndifferent",
    );
  });
});

describe("normalizeUnknownBlock", () => {
  it("normalizes text block", () => {
    const result = normalizeUnknownBlock({ type: "text", text: "hello" });
    expect(result.type).toBe("text");
    expect(result.text).toBe("hello");
  });

  it("normalizes reasoning block", () => {
    const result = normalizeUnknownBlock({ type: "reasoning", summary: "thought" });
    expect(result.type).toBe("reasoning");
    expect(result.text).toBe("thought");
  });

  it("defaults to 'agent' for non-objects", () => {
    expect(normalizeUnknownBlock(null).type).toBe("agent");
    expect(normalizeUnknownBlock("string").type).toBe("agent");
  });
});

describe("toPartType", () => {
  it("maps text type", () => {
    expect(toPartType("text")).toBe("text");
  });

  it("maps thinking/reasoning variants", () => {
    expect(toPartType("thinking")).toBe("reasoning");
    expect(toPartType("reasoning")).toBe("reasoning");
  });

  it("maps tool variants", () => {
    expect(toPartType("tool_use")).toBe("tool");
    expect(toPartType("toolUse")).toBe("tool");
    expect(toPartType("tool_result")).toBe("tool");
    expect(toPartType("functionCall")).toBe("tool");
    expect(toPartType("function_call")).toBe("tool");
    expect(toPartType("tool")).toBe("tool");
  });

  it("maps file types", () => {
    expect(toPartType("file")).toBe("file");
    expect(toPartType("image")).toBe("file");
  });

  it("maps step types", () => {
    expect(toPartType("step_start")).toBe("step_start");
    expect(toPartType("step-start")).toBe("step_start");
    expect(toPartType("step_finish")).toBe("step_finish");
    expect(toPartType("step-finish")).toBe("step_finish");
  });

  it("defaults to 'agent' for unknown types", () => {
    expect(toPartType("unknown")).toBe("agent");
    expect(toPartType("")).toBe("agent");
  });
});

describe("extractMessageContent", () => {
  it("returns string content directly", () => {
    expect(extractMessageContent("hello")).toBe("hello");
  });

  it("extracts text from content block arrays", () => {
    expect(
      extractMessageContent([
        { type: "text", text: "hello" },
        { type: "image", url: "..." },
        { type: "text", text: "world" },
      ]),
    ).toBe("hello\nworld");
  });

  it("returns empty string for empty array", () => {
    expect(extractMessageContent([])).toBe("");
  });

  it("serializes other content types", () => {
    expect(extractMessageContent({ key: "value" })).toBe('{"key":"value"}');
  });
});

describe("toRuntimeRoleForTokenEstimate", () => {
  it("maps tool roles", () => {
    expect(toRuntimeRoleForTokenEstimate("tool")).toBe("toolResult");
    expect(toRuntimeRoleForTokenEstimate("toolResult")).toBe("toolResult");
  });

  it("maps user/system roles", () => {
    expect(toRuntimeRoleForTokenEstimate("user")).toBe("user");
    expect(toRuntimeRoleForTokenEstimate("system")).toBe("user");
  });

  it("defaults to assistant", () => {
    expect(toRuntimeRoleForTokenEstimate("assistant")).toBe("assistant");
    expect(toRuntimeRoleForTokenEstimate("unknown")).toBe("assistant");
  });
});

describe("isTextBlock", () => {
  it("returns true for text blocks", () => {
    expect(isTextBlock({ type: "text", text: "hello" })).toBe(true);
  });

  it("returns false for non-text blocks", () => {
    expect(isTextBlock({ type: "image", url: "..." })).toBe(false);
    expect(isTextBlock(null)).toBe(false);
    expect(isTextBlock("string")).toBe(false);
    expect(isTextBlock([1, 2])).toBe(false);
  });
});

describe("toDbRole", () => {
  it("maps tool/toolResult to tool", () => {
    expect(toDbRole("tool")).toBe("tool");
    expect(toDbRole("toolResult")).toBe("tool");
  });

  it("maps standard roles", () => {
    expect(toDbRole("user")).toBe("user");
    expect(toDbRole("assistant")).toBe("assistant");
    expect(toDbRole("system")).toBe("system");
  });

  it("defaults to assistant for unknown", () => {
    expect(toDbRole("unknown")).toBe("assistant");
  });
});

describe("messageIdentity", () => {
  it("creates role + null-byte + content identity", () => {
    expect(messageIdentity("user", "hello")).toBe("user\u0000hello");
  });
});

describe("isBootstrapMessage", () => {
  it("returns true for messages with role and content", () => {
    expect(isBootstrapMessage({ role: "user", content: "hello" })).toBe(true);
  });

  it("returns true for bash-exec style messages", () => {
    expect(isBootstrapMessage({ role: "assistant", command: "ls", output: "files" })).toBe(true);
  });

  it("returns false for non-objects", () => {
    expect(isBootstrapMessage(null)).toBe(false);
    expect(isBootstrapMessage("string")).toBe(false);
  });

  it("returns false when role is not a string", () => {
    expect(isBootstrapMessage({ role: 123, content: "hi" })).toBe(false);
  });
});

describe("estimateMessageContentTokensForAfterTurn", () => {
  it("estimates string content", () => {
    expect(estimateMessageContentTokensForAfterTurn("12345678")).toBe(2);
  });

  it("estimates array content by text parts", () => {
    expect(
      estimateMessageContentTokensForAfterTurn([
        { text: "1234" },
        { thinking: "5678" },
        { image: "..." },
      ]),
    ).toBe(2);
  });

  it("returns 0 for null", () => {
    expect(estimateMessageContentTokensForAfterTurn(null)).toBe(0);
  });
});

describe("estimateSessionTokenCountForAfterTurn", () => {
  it("sums tokens across messages", () => {
    const messages = [
      { role: "user", content: "12345678" },
      { role: "assistant", content: "1234" },
    ] as Array<{ role: string; content: string }>;
    expect(estimateSessionTokenCountForAfterTurn(messages)).toBe(3);
  });

  it("handles bash-exec messages", () => {
    const messages = [{ role: "assistant", command: "ls", output: "file.txt" }] as Array<{
      role: string;
      command: string;
      output: string;
    }>;
    expect(estimateSessionTokenCountForAfterTurn(messages)).toBeGreaterThan(0);
  });

  it("returns 0 for empty array", () => {
    expect(estimateSessionTokenCountForAfterTurn([])).toBe(0);
  });
});
