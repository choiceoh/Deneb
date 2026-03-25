import { describe, expect, it } from "vitest";
import { buildCompactionSummarizationInstructions } from "./compaction.js";

describe("compaction identifier policy (system-managed: always strict)", () => {
  it("always includes strict identifier preservation", () => {
    const built = buildCompactionSummarizationInstructions();
    expect(built).toContain("Preserve all opaque identifiers exactly as written");
    expect(built).toContain("UUIDs");
  });

  it("prepends identifier instructions before custom focus text", () => {
    const built = buildCompactionSummarizationInstructions("Track release blockers.");
    expect(built).toContain("Preserve all opaque identifiers exactly as written");
    expect(built).toContain("Additional focus:");
    expect(built).toContain("Track release blockers.");
  });
});
