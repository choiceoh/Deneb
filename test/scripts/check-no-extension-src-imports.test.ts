import { describe, expect, it } from "vitest";
import {
  FORBIDDEN_REPO_SRC_IMPORT,
  isProductionExtensionFile,
} from "../../scripts/check-no-extension-src-imports.ts";

describe("check-no-extension-src-imports", () => {
  describe("FORBIDDEN_REPO_SRC_IMPORT", () => {
    it("matches relative import reaching into src/", () => {
      const content = `import { foo } from "../../src/utils/bar";`;

      expect(FORBIDDEN_REPO_SRC_IMPORT.test(content)).toBe(true);
    });

    it("matches deeply nested relative src import", () => {
      const content = `import { foo } from "../../../src/gateway/server";`;

      expect(FORBIDDEN_REPO_SRC_IMPORT.test(content)).toBe(true);
    });

    it("does not match plugin-sdk imports", () => {
      const content = `import { foo } from "deneb/plugin-sdk/channel";`;

      expect(FORBIDDEN_REPO_SRC_IMPORT.test(content)).toBe(false);
    });

    it("does not match local relative imports", () => {
      const content = `import { foo } from "./utils";`;

      expect(FORBIDDEN_REPO_SRC_IMPORT.test(content)).toBe(false);
    });

    it("does not match sibling-level relative imports", () => {
      const content = `import { foo } from "../shared/utils";`;

      expect(FORBIDDEN_REPO_SRC_IMPORT.test(content)).toBe(false);
    });
  });

  describe("isProductionExtensionFile", () => {
    it("returns true for regular source files", () => {
      expect(isProductionExtensionFile("extensions/telegram/src/channel.ts")).toBe(true);
    });

    it("returns false for test files", () => {
      expect(isProductionExtensionFile("extensions/telegram/src/channel.test.ts")).toBe(false);
    });

    it("returns false for spec files", () => {
      expect(isProductionExtensionFile("extensions/telegram/src/channel.spec.ts")).toBe(false);
    });

    it("returns false for runtime-api.ts files", () => {
      expect(isProductionExtensionFile("extensions/telegram/src/runtime-api.ts")).toBe(false);
    });

    it("returns false for test-harness files", () => {
      expect(isProductionExtensionFile("extensions/telegram/src/test-harness.ts")).toBe(false);
    });

    it("returns false for files in node_modules", () => {
      expect(isProductionExtensionFile("extensions/telegram/node_modules/foo/index.ts")).toBe(
        false,
      );
    });

    it("returns false for files in dist", () => {
      expect(isProductionExtensionFile("extensions/telegram/dist/channel.ts")).toBe(false);
    });
  });
});
