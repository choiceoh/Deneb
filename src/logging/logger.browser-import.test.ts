import { afterEach, describe, expect, it, vi } from "vitest";

type LoggerModule = typeof import("./logger.js");

const originalGetBuiltinModule = (
  process as NodeJS.Process & { getBuiltinModule?: (id: string) => unknown }
).getBuiltinModule;

async function importBrowserSafeLogger(params?: {
  resolvePreferredDenebTmpDir?: ReturnType<typeof vi.fn>;
}): Promise<{
  module: LoggerModule;
  resolvePreferredDenebTmpDir: ReturnType<typeof vi.fn>;
}> {
  vi.resetModules();
  const resolvePreferredDenebTmpDir =
    params?.resolvePreferredDenebTmpDir ??
    vi.fn(() => {
      throw new Error("resolvePreferredDenebTmpDir should not run during browser-safe import");
    });

  vi.doMock("../infra/tmp-deneb-dir.js", async () => {
    const actual = await vi.importActual<typeof import("../infra/tmp-deneb-dir.js")>(
      "../infra/tmp-deneb-dir.js",
    );
    return {
      ...actual,
      resolvePreferredDenebTmpDir,
    };
  });

  Object.defineProperty(process, "getBuiltinModule", {
    configurable: true,
    value: undefined,
  });

  const module = await import("./logger.js");
  return { module, resolvePreferredDenebTmpDir };
}

describe("logging/logger browser-safe import", () => {
  afterEach(() => {
    vi.resetModules();
    vi.doUnmock("../infra/tmp-deneb-dir.js");
    Object.defineProperty(process, "getBuiltinModule", {
      configurable: true,
      value: originalGetBuiltinModule,
    });
  });

  it("does not resolve the preferred temp dir at import time when node fs is unavailable", async () => {
    const { module, resolvePreferredDenebTmpDir } = await importBrowserSafeLogger();

    expect(resolvePreferredDenebTmpDir).not.toHaveBeenCalled();
    expect(module.DEFAULT_LOG_DIR).toBe("/tmp/deneb");
    expect(module.DEFAULT_LOG_FILE).toBe("/tmp/deneb/deneb.log");
  });

  it("disables file logging when imported in a browser-like environment", async () => {
    const { module, resolvePreferredDenebTmpDir } = await importBrowserSafeLogger();

    expect(module.getResolvedLoggerSettings()).toMatchObject({
      level: "silent",
      file: "/tmp/deneb/deneb.log",
    });
    expect(module.isFileLogLevelEnabled("info")).toBe(false);
    expect(() => module.getLogger().info("browser-safe")).not.toThrow();
    expect(resolvePreferredDenebTmpDir).not.toHaveBeenCalled();
  });
});
