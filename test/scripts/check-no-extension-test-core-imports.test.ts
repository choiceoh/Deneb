import { describe, expect, it } from "vitest";
import {
  FORBIDDEN_PATTERNS,
  isExtensionTestFile,
} from "../../scripts/check-no-extension-test-core-imports.ts";

describe("check-no-extension-test-core-imports", () => {
  describe("isExtensionTestFile", () => {
    it("matches .test.ts files", () => {
      expect(isExtensionTestFile("channel.test.ts")).toBe(true);
    });

    it("matches .e2e.test.ts files", () => {
      expect(isExtensionTestFile("channel.e2e.test.ts")).toBe(true);
    });

    it("matches .test.mts files", () => {
      expect(isExtensionTestFile("channel.test.mts")).toBe(true);
    });

    it("does not match production source files", () => {
      expect(isExtensionTestFile("channel.ts")).toBe(false);
    });

    it("does not match declaration files", () => {
      expect(isExtensionTestFile("channel.d.ts")).toBe(false);
    });
  });

  describe("FORBIDDEN_PATTERNS", () => {
    it("detects monolithic deneb/plugin-sdk import", () => {
      const content = `import { foo } from "deneb/plugin-sdk";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeDefined();
      expect(match?.hint).toContain("subpath");
    });

    it("detects deneb/plugin-sdk/compat import", () => {
      const content = `import { foo } from "deneb/plugin-sdk/compat";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeDefined();
      expect(match?.hint).toContain("compat");
    });

    it("detects deneb/plugin-sdk/test-utils import", () => {
      const content = `import { foo } from "deneb/plugin-sdk/test-utils";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeDefined();
      expect(match?.hint).toContain("testing");
    });

    it("detects relative test-utils import", () => {
      const content = `import { foo } from "../../test-utils/helpers";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeDefined();
    });

    it("detects relative src/test-utils import", () => {
      const content = `import { foo } from "../../../src/test-utils/mock";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeDefined();
    });

    it("detects relative src/plugins/types.js import", () => {
      const content = `import { foo } from "../../../src/plugins/types.js";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeDefined();
    });

    it("passes valid plugin-sdk subpath imports", () => {
      const content = `import { foo } from "deneb/plugin-sdk/channel";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeUndefined();
    });

    it("passes deneb/plugin-sdk/testing import", () => {
      const content = `import { foo } from "deneb/plugin-sdk/testing";`;
      const match = FORBIDDEN_PATTERNS.find((p) => p.pattern.test(content));

      expect(match).toBeUndefined();
    });
  });
});
