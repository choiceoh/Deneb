import { describe } from "vitest";
import { resolveTelegramRuntimeGroupPolicy } from "../../../../extensions/telegram/src/group-access.js";
import { installChannelRuntimeGroupPolicyFallbackSuite } from "./suites.js";

describe("channel runtime group policy contract", () => {
  describe("telegram", () => {
    installChannelRuntimeGroupPolicyFallbackSuite({
      resolve: resolveTelegramRuntimeGroupPolicy,
      configuredLabel: "keeps open fallback when channels.telegram is configured",
      defaultGroupPolicyUnderTest: "disabled",
      missingConfigLabel: "fails closed when channels.telegram is missing and no defaults are set",
      missingDefaultLabel: "ignores explicit defaults when provider config is missing",
    });
  });
});
