import { describe, expect, it } from "vitest";
import {
  isHeartbeatOnlyResponse,
  pickLastDeliverablePayload,
  pickLastNonEmptyTextFromPayloads,
  pickSummaryFromPayloads,
} from "./helpers.js";

/**
 * All three pickers share the same two-pass error-skipping pattern.
 * Consolidate the common "prefers non-error → falls back to error → empty" tests.
 */
describe.each([
  {
    name: "pickSummaryFromPayloads",
    pick: (p: Array<{ text?: string; isError?: boolean }>) => pickSummaryFromPayloads(p),
    realText: "Here is your summary",
    errorText: "Tool error: rate limited",
  },
  {
    name: "pickLastNonEmptyTextFromPayloads",
    pick: (p: Array<{ text?: string; isError?: boolean }>) => pickLastNonEmptyTextFromPayloads(p),
    realText: "Real output",
    errorText: "Service error",
  },
] as const)("$name – shared error-skipping behavior", ({ pick, realText, errorText }) => {
  it("picks real text over error payload", () => {
    expect(pick([{ text: realText }, { text: errorText, isError: true }])).toBe(realText);
  });

  it("falls back to error payload when no real text exists", () => {
    expect(pick([{ text: errorText, isError: true }])).toBe(errorText);
  });

  it("returns undefined for empty payloads", () => {
    expect(pick([])).toBeUndefined();
  });

  it("treats isError: undefined as non-error", () => {
    expect(
      pick([
        { text: "normal", isError: undefined },
        { text: "error", isError: true },
      ]),
    ).toBe("normal");
  });
});

describe("pickLastDeliverablePayload", () => {
  it("picks real payload over error payload", () => {
    const real = { text: "Delivered content" };
    const error = { text: "Error warning", isError: true as const };
    expect(pickLastDeliverablePayload([real, error])).toBe(real);
  });

  it("falls back to error payload when no real payload exists", () => {
    const error = { text: "Error warning", isError: true as const };
    expect(pickLastDeliverablePayload([error])).toBe(error);
  });

  it("returns undefined for empty payloads", () => {
    expect(pickLastDeliverablePayload([])).toBeUndefined();
  });

  it("picks media payload over error text payload", () => {
    const media = { mediaUrl: "https://example.com/img.png" };
    const error = { text: "Error warning", isError: true as const };
    expect(pickLastDeliverablePayload([media, error])).toBe(media);
  });

  it("treats isError: undefined as non-error", () => {
    const normal = { text: "ok", isError: undefined };
    const error = { text: "bad", isError: true as const };
    expect(pickLastDeliverablePayload([normal, error])).toBe(normal);
  });
});

describe("isHeartbeatOnlyResponse", () => {
  const ACK_MAX = 300;

  it("returns true for empty payloads", () => {
    expect(isHeartbeatOnlyResponse([], ACK_MAX)).toBe(true);
  });

  it("returns true for a single HEARTBEAT_OK payload", () => {
    expect(isHeartbeatOnlyResponse([{ text: "HEARTBEAT_OK" }], ACK_MAX)).toBe(true);
  });

  it("returns false for a single non-heartbeat payload", () => {
    expect(isHeartbeatOnlyResponse([{ text: "Something important happened" }], ACK_MAX)).toBe(
      false,
    );
  });

  it("returns true when multiple payloads include narration followed by HEARTBEAT_OK", () => {
    // Agent narrates its work then signals nothing needs attention.
    expect(
      isHeartbeatOnlyResponse(
        [
          { text: "It's 12:49 AM — quiet hours. Let me run the checks quickly." },
          { text: "Emails: Just 2 calendar invites. Not urgent." },
          { text: "HEARTBEAT_OK" },
        ],
        ACK_MAX,
      ),
    ).toBe(true);
  });

  it("returns false when media is present even with HEARTBEAT_OK text", () => {
    expect(
      isHeartbeatOnlyResponse(
        [{ text: "HEARTBEAT_OK", mediaUrl: "https://example.com/img.png" }],
        ACK_MAX,
      ),
    ).toBe(false);
  });

  it("returns false when media is in a different payload than HEARTBEAT_OK", () => {
    expect(
      isHeartbeatOnlyResponse(
        [
          { text: "HEARTBEAT_OK" },
          { text: "Here's an image", mediaUrl: "https://example.com/img.png" },
        ],
        ACK_MAX,
      ),
    ).toBe(false);
  });

  it("returns false when no payload contains HEARTBEAT_OK", () => {
    expect(
      isHeartbeatOnlyResponse(
        [{ text: "Checked emails — found 3 urgent messages from your manager." }],
        ACK_MAX,
      ),
    ).toBe(false);
  });
});
