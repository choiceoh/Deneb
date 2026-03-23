import { describe, expect, it } from "vitest";
import {
  normalizeInputProvenance,
  applyInputProvenanceToUserMessage,
  isInterSessionInputProvenance,
  hasInterSessionUserProvenance,
  INPUT_PROVENANCE_KIND_VALUES,
} from "./input-provenance.js";

describe("normalizeInputProvenance", () => {
  it("returns valid provenance for all kind values", () => {
    for (const kind of INPUT_PROVENANCE_KIND_VALUES) {
      const result = normalizeInputProvenance({ kind });
      expect(result).toBeDefined();
      expect(result!.kind).toBe(kind);
    }
  });

  it("returns undefined for invalid kind", () => {
    expect(normalizeInputProvenance({ kind: "invalid" })).toBeUndefined();
  });

  it("returns undefined for non-object values", () => {
    expect(normalizeInputProvenance(null)).toBeUndefined();
    expect(normalizeInputProvenance(undefined)).toBeUndefined();
    expect(normalizeInputProvenance("string")).toBeUndefined();
    expect(normalizeInputProvenance(42)).toBeUndefined();
  });

  it("trims and normalizes optional string fields", () => {
    const result = normalizeInputProvenance({
      kind: "inter_session",
      originSessionId: "  sess-123  ",
      sourceChannel: "  slack  ",
    });
    expect(result).toEqual({
      kind: "inter_session",
      originSessionId: "sess-123",
      sourceChannel: "slack",
      sourceSessionKey: undefined,
      sourceTool: undefined,
    });
  });

  it("ignores non-string optional fields", () => {
    const result = normalizeInputProvenance({
      kind: "external_user",
      originSessionId: 123,
      sourceChannel: true,
    });
    expect(result!.originSessionId).toBeUndefined();
    expect(result!.sourceChannel).toBeUndefined();
  });

  it("returns undefined for empty-after-trim strings", () => {
    const result = normalizeInputProvenance({
      kind: "external_user",
      originSessionId: "   ",
    });
    expect(result!.originSessionId).toBeUndefined();
  });
});

describe("applyInputProvenanceToUserMessage", () => {
  const provenance = { kind: "inter_session" as const };

  it("applies provenance to user messages", () => {
    const msg = { role: "user", content: "hello" } as unknown as Parameters<
      typeof applyInputProvenanceToUserMessage
    >[0];
    const result = applyInputProvenanceToUserMessage(msg, provenance);
    expect((result as Record<string, unknown>).provenance).toEqual(provenance);
  });

  it("does not apply to non-user messages", () => {
    const msg = { role: "assistant", content: "hi" } as unknown as Parameters<
      typeof applyInputProvenanceToUserMessage
    >[0];
    const result = applyInputProvenanceToUserMessage(msg, provenance);
    expect(result).toBe(msg);
  });

  it("returns original message when provenance is undefined", () => {
    const msg = { role: "user", content: "hello" } as unknown as Parameters<
      typeof applyInputProvenanceToUserMessage
    >[0];
    const result = applyInputProvenanceToUserMessage(msg, undefined);
    expect(result).toBe(msg);
  });

  it("does not overwrite existing provenance", () => {
    const existing = { kind: "external_user" as const };
    const msg = { role: "user", content: "hello", provenance: existing } as unknown as Parameters<
      typeof applyInputProvenanceToUserMessage
    >[0];
    const result = applyInputProvenanceToUserMessage(msg, provenance);
    expect(result).toBe(msg);
  });
});

describe("isInterSessionInputProvenance", () => {
  it("returns true for inter_session provenance", () => {
    expect(isInterSessionInputProvenance({ kind: "inter_session" })).toBe(true);
  });

  it("returns false for other kinds", () => {
    expect(isInterSessionInputProvenance({ kind: "external_user" })).toBe(false);
    expect(isInterSessionInputProvenance({ kind: "internal_system" })).toBe(false);
  });

  it("returns false for invalid values", () => {
    expect(isInterSessionInputProvenance(null)).toBe(false);
    expect(isInterSessionInputProvenance(undefined)).toBe(false);
  });
});

describe("hasInterSessionUserProvenance", () => {
  it("returns true for user message with inter_session provenance", () => {
    expect(
      hasInterSessionUserProvenance({
        role: "user",
        provenance: { kind: "inter_session" },
      }),
    ).toBe(true);
  });

  it("returns false for non-user role", () => {
    expect(
      hasInterSessionUserProvenance({
        role: "assistant",
        provenance: { kind: "inter_session" },
      }),
    ).toBe(false);
  });

  it("returns false for undefined message", () => {
    expect(hasInterSessionUserProvenance(undefined)).toBe(false);
  });

  it("returns false when provenance is not inter_session", () => {
    expect(
      hasInterSessionUserProvenance({
        role: "user",
        provenance: { kind: "external_user" },
      }),
    ).toBe(false);
  });
});
