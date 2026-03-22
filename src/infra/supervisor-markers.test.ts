import { describe, expect, it } from "vitest";
import { detectRespawnSupervisor, SUPERVISOR_HINT_ENV_VARS } from "./supervisor-markers.js";

describe("SUPERVISOR_HINT_ENV_VARS", () => {
  it("includes systemd supervisor hint env vars", () => {
    expect(SUPERVISOR_HINT_ENV_VARS).toEqual(
      expect.arrayContaining(["INVOCATION_ID", "DENEB_SERVICE_MARKER", "DENEB_SERVICE_KIND"]),
    );
  });
});

describe("detectRespawnSupervisor", () => {
  it("detects systemd from non-blank hints", () => {
    expect(detectRespawnSupervisor({ INVOCATION_ID: "abc123" })).toBe("systemd");
    expect(detectRespawnSupervisor({ JOURNAL_STREAM: "" })).toBeNull();
  });

  it("returns null when no systemd hints are present", () => {
    expect(detectRespawnSupervisor({})).toBeNull();
  });
});
