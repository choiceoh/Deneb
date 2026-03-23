import { describe, expect, it } from "vitest";
import { buildUntrustedChannelMetadata } from "./channel-metadata.js";

describe("buildUntrustedChannelMetadata", () => {
  it("returns undefined when no valid entries", () => {
    expect(
      buildUntrustedChannelMetadata({
        source: "test",
        label: "Info",
        entries: [null, undefined, ""],
      }),
    ).toBeUndefined();
  });

  it("builds metadata from valid entries", () => {
    const result = buildUntrustedChannelMetadata({
      source: "discord",
      label: "Channel Info",
      entries: ["Server Name", "Channel Topic"],
    });
    expect(result).toBeDefined();
    expect(result).toContain("Channel Info");
    expect(result).toContain("Server Name");
    expect(result).toContain("Channel Topic");
  });

  it("deduplicates entries", () => {
    const result = buildUntrustedChannelMetadata({
      source: "test",
      label: "Info",
      entries: ["same", "same", "different"],
    });
    expect(result).toBeDefined();
    // Should contain "same" only once
    const matches = result!.match(/same/g);
    expect(matches!.length).toBe(1);
  });

  it("normalizes whitespace in entries", () => {
    const result = buildUntrustedChannelMetadata({
      source: "test",
      label: "Info",
      entries: ["  multiple   spaces  here  "],
    });
    expect(result).toContain("multiple spaces here");
  });

  it("truncates entries exceeding max entry length", () => {
    const longEntry = "a".repeat(500);
    const result = buildUntrustedChannelMetadata({
      source: "test",
      label: "Info",
      entries: [longEntry],
    });
    expect(result).toBeDefined();
    expect(result!.length).toBeLessThan(longEntry.length + 200);
  });

  it("respects maxChars parameter", () => {
    const short = buildUntrustedChannelMetadata({
      source: "test",
      label: "Info",
      entries: ["entry1", "entry2", "entry3"],
      maxChars: 100,
    });
    const long = buildUntrustedChannelMetadata({
      source: "test",
      label: "Info",
      entries: ["entry1", "entry2", "entry3"],
      maxChars: 2000,
    });
    expect(short).toBeDefined();
    expect(long).toBeDefined();
    // Shorter maxChars produces shorter or equal output
    expect(short!.length).toBeLessThanOrEqual(long!.length);
  });
});
