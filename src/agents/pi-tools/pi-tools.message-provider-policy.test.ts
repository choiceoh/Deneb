import { describe, expect, it } from "vitest";
import { createDenebCodingTools } from "./pi-tools.js";

describe("createDenebCodingTools message provider policy", () => {
  it("creates tools for a standard provider", () => {
    const tools = createDenebCodingTools({ messageProvider: "discord" });
    expect(tools.length).toBeGreaterThan(0);
  });

  it("creates tools for voice provider", () => {
    const tools = createDenebCodingTools({ messageProvider: "voice" });
    expect(tools.length).toBeGreaterThan(0);
  });
});
