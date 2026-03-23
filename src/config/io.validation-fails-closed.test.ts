import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { clearConfigCache, loadConfig } from "./config.js";
import { createConfigIO } from "./io.js";
import { withTempHomeConfig } from "./test-helpers.js";

describe("config validation fail-closed behavior", () => {
  beforeEach(() => {
    clearConfigCache();
    vi.restoreAllMocks();
  });

  it("throws INVALID_CONFIG instead of returning an empty config", async () => {
    await withTempHomeConfig(
      {
        agents: { list: [{ id: "main" }] },
        nope: true,
        channels: {
          whatsapp: {
            dmPolicy: "allowlist",
            allowFrom: ["+1234567890"],
          },
        },
      },
      async () => {
        const spy = vi.spyOn(console, "error").mockImplementation(() => {});
        let thrown: unknown;
        try {
          loadConfig();
        } catch (err) {
          thrown = err;
        }

        expect(thrown).toBeInstanceOf(Error);
        expect((thrown as { code?: string } | undefined)?.code).toBe("INVALID_CONFIG");
        expect(spy).toHaveBeenCalled();
      },
    );
  });

  it("throws INVALID_CONFIG when config root is a non-object value", async () => {
    const home = await fs.mkdtemp(path.join(os.tmpdir(), "deneb-config-nonobj-"));
    try {
      const configDir = path.join(home, ".deneb");
      await fs.mkdir(configDir, { recursive: true });
      const configPath = path.join(configDir, "deneb.json");

      for (const content of ['"just a string"', "42", "null", "true"]) {
        await fs.writeFile(configPath, content, "utf-8");
        const io = createConfigIO({
          env: {} as NodeJS.ProcessEnv,
          homedir: () => home,
        });

        let thrown: unknown;
        try {
          io.loadConfig();
        } catch (err) {
          thrown = err;
        }

        expect(thrown).toBeInstanceOf(Error);
        expect((thrown as { code?: string }).code).toBe("INVALID_CONFIG");
        expect((thrown as Error).message).toContain("expected an object at the root");
      }
    } finally {
      await fs.rm(home, { recursive: true, force: true });
    }
  });

  it("snapshot marks non-object root as invalid", async () => {
    const home = await fs.mkdtemp(path.join(os.tmpdir(), "deneb-config-nonobj-snap-"));
    try {
      const configDir = path.join(home, ".deneb");
      await fs.mkdir(configDir, { recursive: true });
      const configPath = path.join(configDir, "deneb.json");
      await fs.writeFile(configPath, '"just a string"', "utf-8");

      const io = createConfigIO({
        env: {} as NodeJS.ProcessEnv,
        homedir: () => home,
      });

      const snapshot = await io.readConfigFileSnapshot();
      expect(snapshot.valid).toBe(false);
      expect(snapshot.issues).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            message: expect.stringContaining("Expected an object at the root"),
          }),
        ]),
      );
    } finally {
      await fs.rm(home, { recursive: true, force: true });
    }
  });

  it("still loads valid security settings unchanged", async () => {
    await withTempHomeConfig(
      {
        agents: { list: [{ id: "main" }] },
        channels: {
          whatsapp: {
            dmPolicy: "allowlist",
            allowFrom: ["+1234567890"],
          },
        },
      },
      async () => {
        const cfg = loadConfig();
        expect(cfg.channels?.whatsapp?.dmPolicy).toBe("allowlist");
        expect(cfg.channels?.whatsapp?.allowFrom).toEqual(["+1234567890"]);
      },
    );
  });
});
