import { describe, expect, it } from "vitest";
import {
  checkConfigIntegrity,
  ConfigIntegrityError,
  type IntegrityCheckParams,
} from "./integrity-guard.js";

describe("checkConfigIntegrity", () => {
  const baseConfig: Record<string, unknown> = {
    meta: { lastTouchedVersion: "1.0.0" },
    gateway: { mode: "local", port: 18789 },
    models: { default: "gpt-4o" },
    agents: { list: [{ id: "main" }] },
    channels: { discord: { token: "tok" } },
    secrets: { providers: {} },
    auth: { ownerPhone: "+1234567890" },
    session: { maxTokens: 4096 },
    messages: {},
  };

  function params(overrides: Partial<IntegrityCheckParams> = {}): IntegrityCheckParams {
    return {
      previous: baseConfig,
      next: { ...baseConfig },
      previousBytes: 2048,
      nextBytes: 2000,
      ...overrides,
    };
  }

  it("passes when config is unchanged", () => {
    const violations = checkConfigIntegrity(params());
    expect(violations).toHaveLength(0);
  });

  it("passes when non-critical keys are removed", () => {
    const next = { ...baseConfig };
    delete next.session;
    delete next.messages;
    const violations = checkConfigIntegrity(params({ next }));
    expect(violations).toHaveLength(0);
  });

  describe("critical key removal", () => {
    for (const key of ["gateway", "models", "agents", "channels", "secrets", "auth"]) {
      it(`detects removal of "${key}"`, () => {
        const next = { ...baseConfig };
        delete next[key];
        const violations = checkConfigIntegrity(params({ next }));
        const codes = violations.map((v) => v.code);
        expect(codes).toContain("CRITICAL_KEY_REMOVED");
        expect(violations.find((v) => v.code === "CRITICAL_KEY_REMOVED")?.message).toContain(key);
      });
    }

    it("allows removal of a critical key that was not present before", () => {
      const prev = { ...baseConfig };
      delete prev.auth;
      const next = { ...baseConfig };
      delete next.auth;
      const violations = checkConfigIntegrity(params({ previous: prev, next }));
      expect(violations.filter((v) => v.code === "CRITICAL_KEY_REMOVED")).toHaveLength(0);
    });
  });

  describe("bulk key removal", () => {
    it("detects removal of more than half of top-level keys", () => {
      // baseConfig has 9 keys; removing 5+ triggers the guard
      const next: Record<string, unknown> = {
        meta: baseConfig.meta,
        gateway: baseConfig.gateway,
        models: baseConfig.models,
      };
      const violations = checkConfigIntegrity(params({ next }));
      const codes = violations.map((v) => v.code);
      expect(codes).toContain("BULK_KEY_REMOVAL");
    });

    it("passes when only a few keys are removed", () => {
      const next = { ...baseConfig };
      delete next.session;
      const violations = checkConfigIntegrity(params({ next }));
      expect(violations.filter((v) => v.code === "BULK_KEY_REMOVAL")).toHaveLength(0);
    });

    it("skips check when previous config has fewer than 4 keys", () => {
      const small = { gateway: { mode: "local" }, meta: {} };
      const next = { meta: {} };
      const violations = checkConfigIntegrity(
        params({ previous: small, next, previousBytes: 50, nextBytes: 20 }),
      );
      expect(violations.filter((v) => v.code === "BULK_KEY_REMOVAL")).toHaveLength(0);
    });
  });

  describe("size drop", () => {
    it("detects >60% size reduction", () => {
      const violations = checkConfigIntegrity(params({ previousBytes: 2000, nextBytes: 600 }));
      const codes = violations.map((v) => v.code);
      expect(codes).toContain("SIZE_DROP");
    });

    it("passes when size reduction is moderate", () => {
      const violations = checkConfigIntegrity(params({ previousBytes: 2000, nextBytes: 1500 }));
      expect(violations.filter((v) => v.code === "SIZE_DROP")).toHaveLength(0);
    });

    it("skips check for small configs", () => {
      const violations = checkConfigIntegrity(params({ previousBytes: 100, nextBytes: 10 }));
      expect(violations.filter((v) => v.code === "SIZE_DROP")).toHaveLength(0);
    });

    it("skips check when bytes are null", () => {
      const violations = checkConfigIntegrity(params({ previousBytes: null, nextBytes: null }));
      expect(violations.filter((v) => v.code === "SIZE_DROP")).toHaveLength(0);
    });
  });

  describe("ConfigIntegrityError", () => {
    it("includes all violation codes in message", () => {
      const violations = [
        { code: "CRITICAL_KEY_REMOVED", message: 'Critical config key "gateway" was removed.' },
        { code: "SIZE_DROP", message: "Config size dropped from 2000 to 100 bytes." },
      ];
      const err = new ConfigIntegrityError(violations);
      expect(err.name).toBe("ConfigIntegrityError");
      expect(err.violations).toHaveLength(2);
      expect(err.message).toContain("CRITICAL_KEY_REMOVED");
      expect(err.message).toContain("SIZE_DROP");
    });
  });
});
