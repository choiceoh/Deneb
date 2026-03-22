import fs from "node:fs";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DenebConfig } from "../../../config/config.js";
import { resolveAcpInstallCommandHint, resolveConfiguredAcpBackendId } from "./install-hints.js";

function withAcpConfig(acp: DenebConfig["acp"]): DenebConfig {
  return { acp } as DenebConfig;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ACP install hints", () => {
  it("prefers explicit runtime install command", () => {
    const cfg = withAcpConfig({
      runtime: { installCommand: "pnpm deneb plugins install acpx" },
    });
    expect(resolveAcpInstallCommandHint(cfg)).toBe("pnpm deneb plugins install acpx");
  });

  it("uses local acpx extension path when present", () => {
    vi.spyOn(fs, "existsSync").mockReturnValue(true);

    const cfg = withAcpConfig({ backend: "acpx" });
    const hint = resolveAcpInstallCommandHint(cfg);
    expect(hint).toContain("deneb plugins install ");
    expect(hint).toContain(path.join("extensions", "acpx"));
  });

  it("falls back to npm install hint for acpx when local extension is absent", () => {
    vi.spyOn(fs, "existsSync").mockReturnValue(false);

    const cfg = withAcpConfig({ backend: "acpx" });
    expect(resolveAcpInstallCommandHint(cfg)).toBe("deneb plugins install acpx");
  });

  it("returns generic plugin hint for non-acpx backend", () => {
    const cfg = withAcpConfig({ backend: "custom-backend" });
    expect(resolveConfiguredAcpBackendId(cfg)).toBe("custom-backend");
    expect(resolveAcpInstallCommandHint(cfg)).toContain('ACP backend "custom-backend"');
  });
});
