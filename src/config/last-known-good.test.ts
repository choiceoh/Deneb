import fs from "node:fs/promises";
import path from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { clearConfigCache } from "./config.js";
import { createConfigIO } from "./io-create.js";
import {
  isLastKnownGoodFallbackActive,
  resolveLkgPathForTest,
  setLastKnownGoodFallbackActive,
} from "./last-known-good.js";
import { withTempHome, writeDenebConfig } from "./test-helpers.js";

describe("last-known-good config recovery", () => {
  beforeEach(() => {
    clearConfigCache();
    setLastKnownGoodFallbackActive(false);
    vi.restoreAllMocks();
  });

  it("saves last-known-good on successful config load", async () => {
    await withTempHome(async (home) => {
      await writeDenebConfig(home, { agents: { list: [{ id: "main" }] } });
      const lkgPath = resolveLkgPathForTest(path.join(home, ".deneb", "deneb.json"));
      const io = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });

      io.loadConfig();

      const lkgExists = await fs
        .access(lkgPath)
        .then(() => true)
        .catch(() => false);
      expect(lkgExists).toBe(true);

      const lkgContent = JSON.parse(await fs.readFile(lkgPath, "utf-8"));
      expect(lkgContent).toEqual(expect.objectContaining({ agents: expect.any(Object) }));
    });
  });

  it("falls back to last-known-good when config becomes invalid", async () => {
    await withTempHome(async (home) => {
      // First: load a valid config to create LKG
      await writeDenebConfig(home, {
        agents: { list: [{ id: "main" }] },
        channels: { telegram: { dmPolicy: "allowlist", allowFrom: ["123"] } },
      });
      const io1 = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });
      const validConfig = io1.loadConfig();
      expect(validConfig.channels?.telegram?.dmPolicy).toBe("allowlist");
      expect(isLastKnownGoodFallbackActive()).toBe(false);

      // Now: break the config with an unknown key (strict mode rejects it)
      clearConfigCache();
      await writeDenebConfig(home, {
        agents: { list: [{ id: "main" }] },
        totallyInvalidKey: true,
      });

      vi.spyOn(console, "warn").mockImplementation(() => {});
      vi.spyOn(console, "error").mockImplementation(() => {});

      const io2 = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });
      const fallbackConfig = io2.loadConfig();

      // Should get the LKG config, not crash
      expect(fallbackConfig.channels?.telegram?.dmPolicy).toBe("allowlist");
      expect(isLastKnownGoodFallbackActive()).toBe(true);
    });
  });

  it("throws when config is invalid and no last-known-good exists", async () => {
    await withTempHome(async (home) => {
      // Write an invalid config (unknown key in strict mode)
      await writeDenebConfig(home, {
        agents: { list: [{ id: "main" }] },
        totallyInvalidKey: true,
      });

      vi.spyOn(console, "error").mockImplementation(() => {});

      const io = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });
      expect(() => io.loadConfig()).toThrow("Invalid config");
      expect(isLastKnownGoodFallbackActive()).toBe(false);
    });
  });

  it("falls back when config file has JSON parse errors", async () => {
    await withTempHome(async (home) => {
      const configDir = path.join(home, ".deneb");

      // First: create a valid LKG
      await writeDenebConfig(home, { agents: { list: [{ id: "main" }] } });
      const io1 = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });
      io1.loadConfig();

      // Now: corrupt the config file
      clearConfigCache();
      await fs.writeFile(path.join(configDir, "deneb.json"), "{ totally broken json !!!", "utf-8");

      vi.spyOn(console, "warn").mockImplementation(() => {});
      vi.spyOn(console, "error").mockImplementation(() => {});

      const io2 = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });
      const fallbackConfig = io2.loadConfig();

      expect(fallbackConfig).toBeDefined();
      expect(isLastKnownGoodFallbackActive()).toBe(true);
    });
  });

  it("does not activate fallback flag on normal successful loads", async () => {
    await withTempHome(async (home) => {
      await writeDenebConfig(home, { agents: { list: [{ id: "main" }] } });

      const io = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });
      io.loadConfig();

      expect(isLastKnownGoodFallbackActive()).toBe(false);
    });
  });

  it("LKG file has restrictive permissions (0o600)", async () => {
    await withTempHome(async (home) => {
      await writeDenebConfig(home, { agents: { list: [{ id: "main" }] } });
      const lkgPath = resolveLkgPathForTest(path.join(home, ".deneb", "deneb.json"));
      const io = createConfigIO({ env: {} as NodeJS.ProcessEnv, homedir: () => home });

      io.loadConfig();

      const stat = await fs.stat(lkgPath);
      // Check owner-only permissions (0o600 = 384 decimal, mask with 0o777)
      expect(stat.mode & 0o777).toBe(0o600);
    });
  });
});
