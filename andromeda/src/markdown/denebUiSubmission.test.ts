import { describe, expect, it } from "vitest";
import { parseUiSubmission } from "./denebUiParse";

// The card-answer wire format is composed by DenebUi.dispatch (and the native
// ChatViewModel.submitUiCallback); parseUiSubmission must round-trip exactly
// what dispatch emits so the transcript can humanize it.
describe("parseUiSubmission", () => {
  it("parses a bare button press", () => {
    expect(parseUiSubmission("Pressed: confirm_slot")).toEqual({ event: "confirm_slot", values: [] });
  });

  it("parses collected values in order", () => {
    expect(parseUiSubmission("Responded with: slot: 14:00, dept: 영업")).toEqual({
      event: "",
      values: [
        ["slot", "14:00"],
        ["dept", "영업"],
      ],
    });
  });

  it("keeps a comma run inside a value attached to its pair", () => {
    // multi-select chip_group joins values with ", " (coerce) — the run must
    // not split into phantom pairs.
    expect(parseUiSubmission("Responded with: sites: A, B, C")).toEqual({
      event: "",
      values: [["sites", "A, B, C"]],
    });
  });

  it("keeps a multiline textarea value whole", () => {
    expect(parseUiSubmission("Responded with: note: 첫 줄\n둘째 줄")).toEqual({
      event: "",
      values: [["note", "첫 줄\n둘째 줄"]],
    });
  });

  it("returns null for ordinary messages", () => {
    expect(parseUiSubmission("오늘 일정 알려줘")).toBeNull();
    expect(parseUiSubmission("Pressed: two words")).toBeNull();
    expect(parseUiSubmission("Responded with: 콜론 없는 본문")).toBeNull();
    expect(parseUiSubmission("")).toBeNull();
  });
});
