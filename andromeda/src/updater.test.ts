import { describe, expect, it } from "vitest";

import { checkForUpdate, checkForUpdates } from "./updater";

// jsdom has no Tauri internals, so checkForUpdates must short-circuit (returning
// the "unavailable" result) before it ever dynamic-imports the desktop-only
// updater plugin.
describe("updater (web fallback)", () => {
  it("reports auto-update is unavailable off-desktop and never throws", async () => {
    await expect(checkForUpdates()).resolves.toEqual({ status: "unavailable" });
  });

  it("check-only path returns null off-desktop (startup nudge stays hidden)", async () => {
    await expect(checkForUpdate()).resolves.toBeNull();
  });
});
