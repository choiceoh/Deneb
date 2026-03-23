import { describe, it } from "vitest";
import { setupContractRegistry } from "./registry.js";
import { installChannelSetupContractSuite } from "./suites.js";

if (setupContractRegistry.length === 0) {
  it("no setup contract entries registered", () => {});
}

for (const entry of setupContractRegistry) {
  describe(`${entry.id} setup contract`, () => {
    installChannelSetupContractSuite({
      plugin: entry.plugin,
      cases: entry.cases as never,
    });
  });
}
