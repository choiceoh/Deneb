import { describe, expect, it } from "vitest";

import { windowKind } from "./windowKind";

describe("windowKind", () => {
  it("defaults to the main workstation", () => {
    expect(windowKind("")).toBe("main");
    expect(windowKind("?foo=bar")).toBe("main");
  });

  it("selects cygnus via the dev-server query param", () => {
    expect(windowKind("?window=cygnus")).toBe("cygnus");
    expect(windowKind("?a=1&window=cygnus")).toBe("cygnus");
  });

  it("selects cygnus via the Rust init-script flag", () => {
    expect(windowKind("", true)).toBe("cygnus");
  });

  it("ignores non-boolean flags and other window values", () => {
    expect(windowKind("?window=main", undefined)).toBe("main");
    expect(windowKind("", "cygnus")).toBe("main");
  });
});
