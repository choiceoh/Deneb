import { describe, expect, it } from "vitest";
import { resolveCliSpawnInvocation } from "./cli-process.js";

describe("resolveCliSpawnInvocation", () => {
  it("returns command and args as-is", () => {
    const invocation = resolveCliSpawnInvocation({
      command: "vega",
      args: ["query", "hello"],
    });

    expect(invocation.command).toBe("vega");
    expect(invocation.argv).toEqual(["query", "hello"]);
  });
});
