import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { isTauri, secureGetToken, secureSetToken } from "./tauri";

// jsdom has no Tauri internals, so these exercise the web (localStorage) path.
describe("tauri integration (web fallback)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => localStorage.clear());

  it("detects it is not running inside Tauri", () => {
    expect(isTauri()).toBe(false);
  });

  it("round-trips the token through localStorage off-desktop", async () => {
    expect(await secureGetToken()).toBeNull();
    await secureSetToken("hex64token");
    expect(await secureGetToken()).toBe("hex64token");
  });
});
