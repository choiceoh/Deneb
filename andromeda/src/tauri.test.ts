import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { isTauri, secureGetToken, secureSetToken } from "./tauri";

// jsdom has no Tauri internals, so these exercise the web (localStorage) path.
describe("tauri integration (web fallback)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => localStorage.clear());

  it("returns false when not running inside Tauri", () => {
    expect(isTauri()).toBe(false);
  });

  it("returns stored token when reading from localStorage off-desktop", async () => {
    expect(await secureGetToken()).toBeNull();
    await secureSetToken("hex64token");
    expect(await secureGetToken()).toBe("hex64token");
  });
});
