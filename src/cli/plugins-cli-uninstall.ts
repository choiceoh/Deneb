import os from "node:os";
import path from "node:path";
import { loadConfig, writeConfigFile } from "../config/config.js";
import { resolveStateDir } from "../config/paths.js";
import { buildPluginStatusReport } from "../plugins/status.js";
import { resolveUninstallDirectoryTarget, uninstallPlugin } from "../plugins/uninstall.js";
import { defaultRuntime } from "../runtime.js";
import { theme } from "../terminal/theme.js";
import { shortenHomePath } from "../utils.js";
import type { PluginUninstallOptions } from "./plugins-cli-shared.js";
import { promptYesNo } from "./prompt.js";

export async function runPluginUninstallCommand(id: string, opts: PluginUninstallOptions) {
  const cfg = loadConfig();
  const report = buildPluginStatusReport({ config: cfg });
  const extensionsDir = path.join(resolveStateDir(process.env, os.homedir), "extensions");
  const keepFiles = Boolean(opts.keepFiles || opts.keepConfig);

  if (opts.keepConfig) {
    defaultRuntime.log(theme.warn("`--keep-config` is deprecated, use `--keep-files`."));
  }

  // Find plugin by id or name
  const plugin = report.plugins.find((p) => p.id === id || p.name === id);
  const pluginId = plugin?.id ?? id;

  // Check if plugin exists in config
  const hasEntry = pluginId in (cfg.plugins?.entries ?? {});
  const hasInstall = pluginId in (cfg.plugins?.installs ?? {});

  if (!hasEntry && !hasInstall) {
    if (plugin) {
      defaultRuntime.error(
        `Plugin "${pluginId}" is not managed by plugins config/install records and cannot be uninstalled.`,
      );
    } else {
      defaultRuntime.error(`Plugin not found: ${id}`);
    }
    return defaultRuntime.exit(1);
  }

  const install = cfg.plugins?.installs?.[pluginId];
  const isLinked = install?.source === "path";

  // Build preview of what will be removed
  const preview: string[] = [];
  if (hasEntry) {
    preview.push("config entry");
  }
  if (hasInstall) {
    preview.push("install record");
  }
  if (cfg.plugins?.allow?.includes(pluginId)) {
    preview.push("allowlist entry");
  }
  if (isLinked && install?.sourcePath && cfg.plugins?.load?.paths?.includes(install.sourcePath)) {
    preview.push("load path");
  }
  if (cfg.plugins?.slots?.memory === pluginId) {
    preview.push(`memory slot (will reset to "memory-core")`);
  }
  const deleteTarget = !keepFiles
    ? resolveUninstallDirectoryTarget({
        pluginId,
        hasInstall,
        installRecord: install,
        extensionsDir,
      })
    : null;
  if (deleteTarget) {
    preview.push(`directory: ${shortenHomePath(deleteTarget)}`);
  }

  const pluginName = plugin?.name || pluginId;
  defaultRuntime.log(
    `Plugin: ${theme.command(pluginName)}${pluginName !== pluginId ? theme.muted(` (${pluginId})`) : ""}`,
  );
  defaultRuntime.log(`Will remove: ${preview.length > 0 ? preview.join(", ") : "(nothing)"}`);

  if (opts.dryRun) {
    defaultRuntime.log(theme.muted("Dry run, no changes made."));
    return;
  }

  if (!opts.force) {
    const confirmed = await promptYesNo(`Uninstall plugin "${pluginId}"?`);
    if (!confirmed) {
      defaultRuntime.log("Cancelled.");
      return;
    }
  }

  const result = await uninstallPlugin({
    config: cfg,
    pluginId,
    deleteFiles: !keepFiles,
    extensionsDir,
  });

  if (!result.ok) {
    defaultRuntime.error(result.error);
    return defaultRuntime.exit(1);
  }
  for (const warning of result.warnings) {
    defaultRuntime.log(theme.warn(warning));
  }

  await writeConfigFile(result.config);

  const removed: string[] = [];
  if (result.actions.entry) {
    removed.push("config entry");
  }
  if (result.actions.install) {
    removed.push("install record");
  }
  if (result.actions.allowlist) {
    removed.push("allowlist");
  }
  if (result.actions.loadPath) {
    removed.push("load path");
  }
  if (result.actions.memorySlot) {
    removed.push("memory slot");
  }
  if (result.actions.directory) {
    removed.push("directory");
  }

  defaultRuntime.log(
    `Uninstalled plugin "${pluginId}". Removed: ${removed.length > 0 ? removed.join(", ") : "nothing"}.`,
  );
  defaultRuntime.log("Restart the gateway to apply changes.");
}
