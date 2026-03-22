import { describe, expect, it, vi } from "vitest";
import { PLUGIN_INSTALL_ERROR_CODE } from "../plugins/install.js";
import {
  resolveBundledInstallPlanForCatalogEntry,
  resolveBundledInstallPlanBeforeNpm,
  resolveBundledInstallPlanForNpmFailure,
} from "./plugin-install-plan.js";

describe("plugin install plan helpers", () => {
  it("prefers bundled plugin for bare plugin-id specs", () => {
    const findBundledSource = vi.fn().mockReturnValue({
      pluginId: "telegram",
      localPath: "/tmp/extensions/telegram",
      npmSpec: "@deneb/telegram",
    });

    const result = resolveBundledInstallPlanBeforeNpm({
      rawSpec: "telegram",
      findBundledSource,
    });

    expect(findBundledSource).toHaveBeenCalledWith({ kind: "pluginId", value: "telegram" });
    expect(result?.bundledSource.pluginId).toBe("telegram");
    expect(result?.warning).toContain('bare install spec "telegram"');
  });

  it("skips bundled pre-plan for scoped npm specs", () => {
    const findBundledSource = vi.fn();
    const result = resolveBundledInstallPlanBeforeNpm({
      rawSpec: "@deneb/telegram",
      findBundledSource,
    });

    expect(findBundledSource).not.toHaveBeenCalled();
    expect(result).toBeNull();
  });

  it("prefers bundled catalog plugin by id before npm spec", () => {
    const findBundledSource = vi
      .fn()
      .mockImplementation(({ kind, value }: { kind: "pluginId" | "npmSpec"; value: string }) => {
        if (kind === "pluginId" && value === "telegram") {
          return {
            pluginId: "telegram",
            localPath: "/tmp/extensions/telegram",
            npmSpec: "@deneb/telegram",
          };
        }
        return undefined;
      });

    const result = resolveBundledInstallPlanForCatalogEntry({
      pluginId: "telegram",
      npmSpec: "@deneb/telegram",
      findBundledSource,
    });

    expect(findBundledSource).toHaveBeenCalledWith({ kind: "pluginId", value: "telegram" });
    expect(result?.bundledSource.localPath).toBe("/tmp/extensions/telegram");
  });

  it("rejects npm-spec matches that resolve to a different plugin id", () => {
    const findBundledSource = vi
      .fn()
      .mockImplementation(({ kind }: { kind: "pluginId" | "npmSpec"; value: string }) => {
        if (kind === "npmSpec") {
          return {
            pluginId: "not-telegram",
            localPath: "/tmp/extensions/not-telegram",
            npmSpec: "@deneb/telegram",
          };
        }
        return undefined;
      });

    const result = resolveBundledInstallPlanForCatalogEntry({
      pluginId: "telegram",
      npmSpec: "@deneb/telegram",
      findBundledSource,
    });

    expect(result).toBeNull();
  });

  it("uses npm-spec bundled fallback only for package-not-found", () => {
    const findBundledSource = vi.fn().mockReturnValue({
      pluginId: "telegram",
      localPath: "/tmp/extensions/telegram",
      npmSpec: "@deneb/telegram",
    });
    const result = resolveBundledInstallPlanForNpmFailure({
      rawSpec: "@deneb/telegram",
      code: PLUGIN_INSTALL_ERROR_CODE.NPM_PACKAGE_NOT_FOUND,
      findBundledSource,
    });

    expect(findBundledSource).toHaveBeenCalledWith({
      kind: "npmSpec",
      value: "@deneb/telegram",
    });
    expect(result?.warning).toContain("npm package unavailable");
  });

  it("skips fallback for non-not-found npm failures", () => {
    const findBundledSource = vi.fn();
    const result = resolveBundledInstallPlanForNpmFailure({
      rawSpec: "@deneb/telegram",
      code: "INSTALL_FAILED",
      findBundledSource,
    });

    expect(findBundledSource).not.toHaveBeenCalled();
    expect(result).toBeNull();
  });
});
