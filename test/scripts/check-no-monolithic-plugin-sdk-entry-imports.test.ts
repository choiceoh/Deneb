import { describe, expect, it } from "vitest";
import {
  hasMonolithicRootImport,
  hasLegacyCompatImport,
} from "../../scripts/check-no-monolithic-plugin-sdk-entry-imports.ts";

describe("check-no-monolithic-plugin-sdk-entry-imports", () => {
  describe("hasMonolithicRootImport", () => {
    it("detects monolithic root import with double quotes", () => {
      const content = `import { something } from "deneb/plugin-sdk";`;

      expect(hasMonolithicRootImport(content)).toBe(true);
    });

    it("detects monolithic root import with single quotes", () => {
      const content = `import { something } from 'deneb/plugin-sdk';`;

      expect(hasMonolithicRootImport(content)).toBe(true);
    });

    it("passes scoped subpath imports", () => {
      const content = `import { something } from "deneb/plugin-sdk/channel";`;

      expect(hasMonolithicRootImport(content)).toBe(false);
    });

    it("passes unrelated imports", () => {
      const content = `import path from "node:path";`;

      expect(hasMonolithicRootImport(content)).toBe(false);
    });
  });

  describe("hasLegacyCompatImport", () => {
    it("detects legacy compat import", () => {
      const content = `import { something } from "deneb/plugin-sdk/compat";`;

      expect(hasLegacyCompatImport(content)).toBe(true);
    });

    it("passes non-compat subpath imports", () => {
      const content = `import { something } from "deneb/plugin-sdk/channel";`;

      expect(hasLegacyCompatImport(content)).toBe(false);
    });
  });
});
