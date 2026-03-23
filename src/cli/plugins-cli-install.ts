import fs from "node:fs";
import type { DenebConfig } from "../config/config.js";
import { loadConfig, writeConfigFile } from "../config/config.js";
import { resolveArchiveKind } from "../infra/archive.js";
import { findBundledPluginSource } from "../plugins/bundled-sources.js";
import { enablePluginInConfig } from "../plugins/enable.js";
import { installPluginFromNpmSpec, installPluginFromPath } from "../plugins/install.js";
import { recordPluginInstall } from "../plugins/installs.js";
import { clearPluginManifestRegistryCache } from "../plugins/manifest-registry.js";
import {
  installPluginFromMarketplace,
  resolveMarketplaceInstallShortcut,
} from "../plugins/marketplace.js";
import { defaultRuntime } from "../runtime.js";
import { theme } from "../terminal/theme.js";
import { resolveUserPath, shortenHomePath } from "../utils.js";
import { looksLikeLocalInstallSpec } from "./install-spec.js";
import { resolvePinnedNpmInstallRecordForCli } from "./npm-resolution.js";
import {
  resolveBundledInstallPlanBeforeNpm,
  resolveBundledInstallPlanForNpmFailure,
} from "./plugin-install-plan.js";
import {
  applySlotSelectionForPlugin,
  createPluginInstallLogger,
  installBundledPluginSource,
  logSlotWarnings,
  resolveFileNpmSpecToLocalPath,
} from "./plugins-cli-shared.js";

export async function runPluginInstallCommand(params: {
  raw: string;
  opts: { link?: boolean; pin?: boolean; marketplace?: string };
}) {
  const shorthand = !params.opts.marketplace
    ? await resolveMarketplaceInstallShortcut(params.raw)
    : null;
  if (shorthand?.ok === false) {
    defaultRuntime.error(shorthand.error);
    return defaultRuntime.exit(1);
  }

  const raw = shorthand?.ok ? shorthand.plugin : params.raw;
  const opts = {
    ...params.opts,
    marketplace:
      params.opts.marketplace ?? (shorthand?.ok ? shorthand.marketplaceSource : undefined),
  };

  if (opts.marketplace) {
    if (opts.link) {
      defaultRuntime.error("`--link` is not supported with `--marketplace`.");
      return defaultRuntime.exit(1);
    }
    if (opts.pin) {
      defaultRuntime.error("`--pin` is not supported with `--marketplace`.");
      return defaultRuntime.exit(1);
    }

    const cfg = loadConfig();
    const result = await installPluginFromMarketplace({
      marketplace: opts.marketplace,
      plugin: raw,
      logger: createPluginInstallLogger(),
    });
    if (!result.ok) {
      defaultRuntime.error(result.error);
      return defaultRuntime.exit(1);
    }

    clearPluginManifestRegistryCache();

    let next = enablePluginInConfig(cfg, result.pluginId).config;
    next = recordPluginInstall(next, {
      pluginId: result.pluginId,
      source: "marketplace",
      installPath: result.targetDir,
      version: result.version,
      marketplaceName: result.marketplaceName,
      marketplaceSource: result.marketplaceSource,
      marketplacePlugin: result.marketplacePlugin,
    });
    const slotResult = applySlotSelectionForPlugin(next, result.pluginId);
    next = slotResult.config;
    await writeConfigFile(next);
    logSlotWarnings(slotResult.warnings);
    defaultRuntime.log(`Installed plugin: ${result.pluginId}`);
    defaultRuntime.log(`Restart the gateway to load plugins.`);
    return;
  }

  const fileSpec = resolveFileNpmSpecToLocalPath(raw);
  if (fileSpec && !fileSpec.ok) {
    defaultRuntime.error(fileSpec.error);
    return defaultRuntime.exit(1);
  }
  const normalized = fileSpec && fileSpec.ok ? fileSpec.path : raw;
  const resolved = resolveUserPath(normalized);
  const cfg = loadConfig();

  if (fs.existsSync(resolved)) {
    if (opts.link) {
      const existing = cfg.plugins?.load?.paths ?? [];
      const merged = Array.from(new Set([...existing, resolved]));
      const probe = await installPluginFromPath({ path: resolved, dryRun: true });
      if (!probe.ok) {
        defaultRuntime.error(probe.error);
        return defaultRuntime.exit(1);
      }

      let next: DenebConfig = enablePluginInConfig(
        {
          ...cfg,
          plugins: {
            ...cfg.plugins,
            load: {
              ...cfg.plugins?.load,
              paths: merged,
            },
          },
        },
        probe.pluginId,
      ).config;
      next = recordPluginInstall(next, {
        pluginId: probe.pluginId,
        source: "path",
        sourcePath: resolved,
        installPath: resolved,
        version: probe.version,
      });
      const slotResult = applySlotSelectionForPlugin(next, probe.pluginId);
      next = slotResult.config;
      await writeConfigFile(next);
      logSlotWarnings(slotResult.warnings);
      defaultRuntime.log(`Linked plugin path: ${shortenHomePath(resolved)}`);
      defaultRuntime.log(`Restart the gateway to load plugins.`);
      return;
    }

    const result = await installPluginFromPath({
      path: resolved,
      logger: createPluginInstallLogger(),
    });
    if (!result.ok) {
      defaultRuntime.error(result.error);
      return defaultRuntime.exit(1);
    }
    // Plugin CLI registrars may have warmed the manifest registry cache before install;
    // force a rescan so config validation sees the freshly installed plugin.
    clearPluginManifestRegistryCache();

    let next = enablePluginInConfig(cfg, result.pluginId).config;
    const source: "archive" | "path" = resolveArchiveKind(resolved) ? "archive" : "path";
    next = recordPluginInstall(next, {
      pluginId: result.pluginId,
      source,
      sourcePath: resolved,
      installPath: result.targetDir,
      version: result.version,
    });
    const slotResult = applySlotSelectionForPlugin(next, result.pluginId);
    next = slotResult.config;
    await writeConfigFile(next);
    logSlotWarnings(slotResult.warnings);
    defaultRuntime.log(`Installed plugin: ${result.pluginId}`);
    defaultRuntime.log(`Restart the gateway to load plugins.`);
    return;
  }

  if (opts.link) {
    defaultRuntime.error("`--link` requires a local path.");
    return defaultRuntime.exit(1);
  }

  if (
    looksLikeLocalInstallSpec(raw, [
      ".ts",
      ".js",
      ".mjs",
      ".cjs",
      ".tgz",
      ".tar.gz",
      ".tar",
      ".zip",
    ])
  ) {
    defaultRuntime.error(`Path not found: ${resolved}`);
    return defaultRuntime.exit(1);
  }

  const bundledPreNpmPlan = resolveBundledInstallPlanBeforeNpm({
    rawSpec: raw,
    findBundledSource: (lookup) => findBundledPluginSource({ lookup }),
  });
  if (bundledPreNpmPlan) {
    await installBundledPluginSource({
      config: cfg,
      rawSpec: raw,
      bundledSource: bundledPreNpmPlan.bundledSource,
      warning: bundledPreNpmPlan.warning,
    });
    return;
  }

  const result = await installPluginFromNpmSpec({
    spec: raw,
    logger: createPluginInstallLogger(),
  });
  if (!result.ok) {
    const bundledFallbackPlan = resolveBundledInstallPlanForNpmFailure({
      rawSpec: raw,
      code: result.code,
      findBundledSource: (lookup) => findBundledPluginSource({ lookup }),
    });
    if (!bundledFallbackPlan) {
      defaultRuntime.error(result.error);
      return defaultRuntime.exit(1);
    }

    await installBundledPluginSource({
      config: cfg,
      rawSpec: raw,
      bundledSource: bundledFallbackPlan.bundledSource,
      warning: bundledFallbackPlan.warning,
    });
    return;
  }
  // Ensure config validation sees newly installed plugin(s) even if the cache was warmed at startup.
  clearPluginManifestRegistryCache();

  let next = enablePluginInConfig(cfg, result.pluginId).config;
  const installRecord = resolvePinnedNpmInstallRecordForCli(
    raw,
    Boolean(opts.pin),
    result.targetDir,
    result.version,
    result.npmResolution,
    defaultRuntime.log,
    theme.warn,
  );
  next = recordPluginInstall(next, {
    pluginId: result.pluginId,
    ...installRecord,
  });
  const slotResult = applySlotSelectionForPlugin(next, result.pluginId);
  next = slotResult.config;
  await writeConfigFile(next);
  logSlotWarnings(slotResult.warnings);
  defaultRuntime.log(`Installed plugin: ${result.pluginId}`);
  defaultRuntime.log(`Restart the gateway to load plugins.`);
}
