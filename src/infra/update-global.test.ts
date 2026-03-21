import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { captureEnv } from "../test-utils/env.js";
import {
  canResolveRegistryVersionForPackageTarget,
  cleanupGlobalRenameDirs,
  detectGlobalInstallManagerByPresence,
  detectGlobalInstallManagerForRoot,
  globalInstallArgs,
  globalInstallFallbackArgs,
  isExplicitPackageInstallSpec,
  isMainPackageTarget,
  DENEB_MAIN_PACKAGE_SPEC,
  resolveGlobalPackageRoot,
  resolveGlobalInstallSpec,
  resolveGlobalRoot,
  type CommandRunner,
} from "./update-global.js";

describe("update global helpers", () => {
  let envSnapshot: ReturnType<typeof captureEnv> | undefined;

  afterEach(() => {
    envSnapshot?.restore();
    envSnapshot = undefined;
  });

  it("prefers explicit package spec overrides", () => {
    envSnapshot = captureEnv(["DENEB_UPDATE_PACKAGE_SPEC"]);
    process.env.DENEB_UPDATE_PACKAGE_SPEC = "file:/tmp/deneb.tgz";

    expect(resolveGlobalInstallSpec({ packageName: "deneb", tag: "latest" })).toBe(
      "file:/tmp/deneb.tgz",
    );
    expect(
      resolveGlobalInstallSpec({
        packageName: "deneb",
        tag: "beta",
        env: { DENEB_UPDATE_PACKAGE_SPEC: "deneb@next" },
      }),
    ).toBe("deneb@next");
  });

  it("resolves global roots and package roots from runner output", async () => {
    const runCommand: CommandRunner = async (argv) => {
      if (argv[0] === "npm") {
        return { stdout: "/tmp/npm-root\n", stderr: "", code: 0 };
      }
      if (argv[0] === "pnpm") {
        return { stdout: "", stderr: "", code: 1 };
      }
      throw new Error(`unexpected command: ${argv.join(" ")}`);
    };

    await expect(resolveGlobalRoot("npm", runCommand, 1000)).resolves.toBe("/tmp/npm-root");
    await expect(resolveGlobalRoot("pnpm", runCommand, 1000)).resolves.toBeNull();
    await expect(resolveGlobalRoot("bun", runCommand, 1000)).resolves.toContain(
      path.join(".bun", "install", "global", "node_modules"),
    );
    await expect(resolveGlobalPackageRoot("npm", runCommand, 1000)).resolves.toBe(
      path.join("/tmp/npm-root", "deneb"),
    );
  });

  it("maps main and explicit install specs for global installs", () => {
    expect(resolveGlobalInstallSpec({ packageName: "deneb", tag: "main" })).toBe(
      DENEB_MAIN_PACKAGE_SPEC,
    );
    expect(
      resolveGlobalInstallSpec({
        packageName: "deneb",
        tag: "github:deneb/deneb#feature/my-branch",
      }),
    ).toBe("github:deneb/deneb#feature/my-branch");
    expect(
      resolveGlobalInstallSpec({
        packageName: "deneb",
        tag: "https://example.com/deneb-main.tgz",
      }),
    ).toBe("https://example.com/deneb-main.tgz");
  });

  it("classifies main and raw install specs separately from registry selectors", () => {
    expect(isMainPackageTarget("main")).toBe(true);
    expect(isMainPackageTarget(" MAIN ")).toBe(true);
    expect(isMainPackageTarget("beta")).toBe(false);

    expect(isExplicitPackageInstallSpec("github:deneb/deneb#main")).toBe(true);
    expect(isExplicitPackageInstallSpec("https://example.com/deneb-main.tgz")).toBe(true);
    expect(isExplicitPackageInstallSpec("file:/tmp/deneb-main.tgz")).toBe(true);
    expect(isExplicitPackageInstallSpec("beta")).toBe(false);

    expect(canResolveRegistryVersionForPackageTarget("latest")).toBe(true);
    expect(canResolveRegistryVersionForPackageTarget("2026.3.14")).toBe(true);
    expect(canResolveRegistryVersionForPackageTarget("main")).toBe(false);
    expect(canResolveRegistryVersionForPackageTarget("github:deneb/deneb#main")).toBe(false);
  });

  it("detects install managers from resolved roots and on-disk presence", async () => {
    const base = await fs.mkdtemp(path.join(os.tmpdir(), "deneb-update-global-"));
    const npmRoot = path.join(base, "npm-root");
    const pnpmRoot = path.join(base, "pnpm-root");
    const bunRoot = path.join(base, ".bun", "install", "global", "node_modules");
    const pkgRoot = path.join(pnpmRoot, "deneb");
    await fs.mkdir(pkgRoot, { recursive: true });
    await fs.mkdir(path.join(npmRoot, "deneb"), { recursive: true });
    await fs.mkdir(path.join(bunRoot, "deneb"), { recursive: true });

    envSnapshot = captureEnv(["BUN_INSTALL"]);
    process.env.BUN_INSTALL = path.join(base, ".bun");

    const runCommand: CommandRunner = async (argv) => {
      if (argv[0] === "npm") {
        return { stdout: `${npmRoot}\n`, stderr: "", code: 0 };
      }
      if (argv[0] === "pnpm") {
        return { stdout: `${pnpmRoot}\n`, stderr: "", code: 0 };
      }
      throw new Error(`unexpected command: ${argv.join(" ")}`);
    };

    await expect(detectGlobalInstallManagerForRoot(runCommand, pkgRoot, 1000)).resolves.toBe(
      "pnpm",
    );
    await expect(detectGlobalInstallManagerByPresence(runCommand, 1000)).resolves.toBe("npm");

    await fs.rm(path.join(npmRoot, "deneb"), { recursive: true, force: true });
    await fs.rm(path.join(pnpmRoot, "deneb"), { recursive: true, force: true });
    await expect(detectGlobalInstallManagerByPresence(runCommand, 1000)).resolves.toBe("bun");
  });

  it("builds install argv and npm fallback argv", () => {
    expect(globalInstallArgs("npm", "deneb@latest")).toEqual([
      "npm",
      "i",
      "-g",
      "deneb@latest",
      "--no-fund",
      "--no-audit",
      "--loglevel=error",
    ]);
    expect(globalInstallArgs("pnpm", "deneb@latest")).toEqual([
      "pnpm",
      "add",
      "-g",
      "deneb@latest",
    ]);
    expect(globalInstallArgs("bun", "deneb@latest")).toEqual(["bun", "add", "-g", "deneb@latest"]);

    expect(globalInstallFallbackArgs("npm", "deneb@latest")).toEqual([
      "npm",
      "i",
      "-g",
      "deneb@latest",
      "--omit=optional",
      "--no-fund",
      "--no-audit",
      "--loglevel=error",
    ]);
    expect(globalInstallFallbackArgs("pnpm", "deneb@latest")).toBeNull();
  });

  it("cleans only renamed package directories", async () => {
    const root = await fs.mkdtemp(path.join(os.tmpdir(), "deneb-update-cleanup-"));
    await fs.mkdir(path.join(root, ".deneb-123"), { recursive: true });
    await fs.mkdir(path.join(root, ".deneb-456"), { recursive: true });
    await fs.writeFile(path.join(root, ".deneb-file"), "nope", "utf8");
    await fs.mkdir(path.join(root, "deneb"), { recursive: true });

    await expect(
      cleanupGlobalRenameDirs({
        globalRoot: root,
        packageName: "deneb",
      }),
    ).resolves.toEqual({
      removed: [".deneb-123", ".deneb-456"],
    });
    await expect(fs.stat(path.join(root, "deneb"))).resolves.toBeDefined();
    await expect(fs.stat(path.join(root, ".deneb-file"))).resolves.toBeDefined();
  });
});
