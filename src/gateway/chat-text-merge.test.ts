import { describe, expect, it } from "vitest";
import { appendUniqueSuffix, resolveMergedAssistantText } from "./chat-text-merge.js";

describe("appendUniqueSuffix", () => {
  it("returns suffix when base is empty", () => {
    expect(appendUniqueSuffix("", "hello")).toBe("hello");
  });

  it("returns base when suffix is empty", () => {
    expect(appendUniqueSuffix("hello", "")).toBe("hello");
  });

  it("deduplicates trailing overlap", () => {
    expect(appendUniqueSuffix("hello wo", "wo world")).toBe("hello wo world");
  });

  it("concatenates when no overlap", () => {
    expect(appendUniqueSuffix("abc", "xyz")).toBe("abcxyz");
  });

  it("returns base when suffix is already at the end", () => {
    expect(appendUniqueSuffix("hello world", "world")).toBe("hello world");
  });
});

describe("resolveMergedAssistantText", () => {
  it("returns nextText when it extends previousText", () => {
    expect(
      resolveMergedAssistantText({
        previousText: "Hello",
        nextText: "Hello world",
        nextDelta: "",
      }),
    ).toBe("Hello world");
  });

  it("keeps previousText when nextText is a stale shorter segment", () => {
    expect(
      resolveMergedAssistantText({
        previousText: "Hello world",
        nextText: "Hello",
        nextDelta: "",
      }),
    ).toBe("Hello world");
  });

  it("appends delta to previousText", () => {
    expect(
      resolveMergedAssistantText({
        previousText: "Hello",
        nextText: "",
        nextDelta: " world",
      }),
    ).toBe("Hello world");
  });

  it("uses nextText when no previousText exists", () => {
    expect(
      resolveMergedAssistantText({
        previousText: "",
        nextText: "Hello",
        nextDelta: "",
      }),
    ).toBe("Hello");
  });

  it("returns previousText when both nextText and nextDelta are empty", () => {
    expect(
      resolveMergedAssistantText({
        previousText: "Hello",
        nextText: "",
        nextDelta: "",
      }),
    ).toBe("Hello");
  });

  // OpenClaw #36957: tool-boundary text retention
  it("preserves pre-tool text when post-tool segment is completely disjoint", () => {
    const result = resolveMergedAssistantText({
      previousText: "Here is my analysis:",
      nextText: "The result is 42.",
      nextDelta: "",
    });
    expect(result).toContain("Here is my analysis:");
    expect(result).toContain("The result is 42.");
    expect(result).toBe("Here is my analysis:\n\nThe result is 42.");
  });

  it("preserves pre-tool text with delta when segments are disjoint", () => {
    const result = resolveMergedAssistantText({
      previousText: "Step 1 done.",
      nextText: "Step 2:",
      nextDelta: " complete",
    });
    // When both nextText and nextDelta are present and disjoint,
    // delta is appended to previousText.
    expect(result).toBe("Step 1 done. complete");
  });

  it("handles multiple tool boundaries by accumulating text", () => {
    // First segment
    let buffer = resolveMergedAssistantText({
      previousText: "",
      nextText: "First part.",
      nextDelta: "",
    });
    expect(buffer).toBe("First part.");

    // Tool call happens, then new text arrives
    buffer = resolveMergedAssistantText({
      previousText: buffer,
      nextText: "Second part.",
      nextDelta: "",
    });
    expect(buffer).toBe("First part.\n\nSecond part.");

    // Another tool call, then more text
    buffer = resolveMergedAssistantText({
      previousText: buffer,
      nextText: "Third part.",
      nextDelta: "",
    });
    expect(buffer).toContain("First part.");
    expect(buffer).toContain("Second part.");
    expect(buffer).toContain("Third part.");
  });
});
