import { describe, expect, it } from "vitest";
import { formatLinkUnderstandingBody } from "./format.js";

describe("formatLinkUnderstandingBody", () => {
  it("returns body as-is when no outputs", () => {
    expect(formatLinkUnderstandingBody({ body: "hello", outputs: [] })).toBe("hello");
  });

  it("returns empty string when no body and no outputs", () => {
    expect(formatLinkUnderstandingBody({ body: undefined, outputs: [] })).toBe("");
  });

  it("returns joined outputs when no body", () => {
    expect(formatLinkUnderstandingBody({ body: undefined, outputs: ["a", "b"] })).toBe("a\nb");
  });

  it("returns joined outputs when body is empty", () => {
    expect(formatLinkUnderstandingBody({ body: "", outputs: ["a", "b"] })).toBe("a\nb");
  });

  it("combines body and outputs with double newline", () => {
    expect(formatLinkUnderstandingBody({ body: "hello", outputs: ["world"] })).toBe(
      "hello\n\nworld",
    );
  });

  it("filters blank outputs", () => {
    expect(formatLinkUnderstandingBody({ body: "hello", outputs: ["", "  ", "world"] })).toBe(
      "hello\n\nworld",
    );
  });

  it("trims output entries", () => {
    expect(formatLinkUnderstandingBody({ body: undefined, outputs: ["  trimmed  "] })).toBe(
      "trimmed",
    );
  });
});
