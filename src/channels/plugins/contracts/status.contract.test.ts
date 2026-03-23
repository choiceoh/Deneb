import { describe, it } from "vitest";
import { statusContractRegistry } from "./registry.js";
import { installChannelStatusContractSuite } from "./suites.js";

if (statusContractRegistry.length === 0) {
  it("no status contract entries registered", () => {});
}

for (const entry of statusContractRegistry) {
  describe(`${entry.id} status contract`, () => {
    installChannelStatusContractSuite({
      plugin: entry.plugin,
      cases: entry.cases as never,
    });
  });
}
