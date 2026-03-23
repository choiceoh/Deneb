import type { DenebConfig } from "../config/config.js";
import { writeConfigFile } from "../config/config.js";
import type { PluginInstallRecord } from "../config/types.plugins.js";
import type { BundledPluginSource } from "../plugins/bundled-sources.js";
import { recordPluginInstall } from "../plugins/installs.js";
import type { PluginRecord } from "../plugins/registry.js";
import { applyExclusiveSlotSelection } from "../plugins/slots.js";
import { buildPluginStatusReport } from "../plugins/status.js";
import { defaultRuntime } from "../runtime.js";
import { theme } from "../terminal/theme.js";
import { shortenHomeInString, shortenHomePath } from "../utils.js";

export type PluginsListOptions = {
  json?: boolean;
  enabled?: boolean;
  verbose?: boolean;
};

export type PluginInspectOptions = {
  json?: boolean;
  all?: boolean;
};

export type PluginUpdateOptions = {
  all?: boolean;
  dryRun?: boolean;
};

export type PluginMarketplaceListOptions = {
  json?: boolean;
};

export type PluginUninstallOptions = {
  keepFiles?: boolean;
  keepConfig?: boolean;
  force?: boolean;
  dryRun?: boolean;
};

export function resolveFileNpmSpecToLocalPath(
  raw: string,
): { ok: true; path: string } | { ok: false; error: string } | null {
  const trimmed = raw.trim();
  if (!trimmed.toLowerCase().startsWith("file:")) {
    return null;
  }
  const rest = trimmed.slice("file:".length);
  if (!rest) {
    return { ok: false, error: "unsupported file: spec: missing path" };
  }
  if (rest.startsWith("///")) {
    // file:///abs/path -> /abs/path
    return { ok: true, path: rest.slice(2) };
  }
  if (rest.startsWith("//localhost/")) {
    // file://localhost/abs/path -> /abs/path
    return { ok: true, path: rest.slice("//localhost".length) };
  }
  if (rest.startsWith("//")) {
    return {
      ok: false,
      error: 'unsupported file: URL host (expected "file:<path>" or "file:///abs/path")',
    };
  }
  return { ok: true, path: rest };
}

export function formatPluginLine(plugin: PluginRecord, verbose = false): string {
  const status =
    plugin.status === "loaded"
      ? theme.success("loaded")
      : plugin.status === "disabled"
        ? theme.warn("disabled")
        : theme.error("error");
  const name = theme.command(plugin.name || plugin.id);
  const idSuffix = plugin.name && plugin.name !== plugin.id ? theme.muted(` (${plugin.id})`) : "";
  const desc = plugin.description
    ? theme.muted(
        plugin.description.length > 60
          ? `${plugin.description.slice(0, 57)}...`
          : plugin.description,
      )
    : theme.muted("(no description)");
  const format = plugin.format ?? "deneb";

  if (!verbose) {
    return `${name}${idSuffix} ${status} ${theme.muted(`[${format}]`)} - ${desc}`;
  }

  const parts = [
    `${name}${idSuffix} ${status}`,
    `  format: ${format}`,
    `  source: ${theme.muted(shortenHomeInString(plugin.source))}`,
    `  origin: ${plugin.origin}`,
  ];
  if (plugin.bundleFormat) {
    parts.push(`  bundle format: ${plugin.bundleFormat}`);
  }
  if (plugin.version) {
    parts.push(`  version: ${plugin.version}`);
  }
  if (plugin.providerIds.length > 0) {
    parts.push(`  providers: ${plugin.providerIds.join(", ")}`);
  }
  if (plugin.error) {
    parts.push(theme.error(`  error: ${plugin.error}`));
  }
  return parts.join("\n");
}

export function formatInspectSection(title: string, lines: string[]): string[] {
  if (lines.length === 0) {
    return [];
  }
  return ["", theme.muted(`${title}:`), ...lines];
}

export function formatCapabilityKinds(
  capabilities: Array<{
    kind: string;
  }>,
): string {
  if (capabilities.length === 0) {
    return "-";
  }
  return capabilities.map((entry) => entry.kind).join(", ");
}

export function formatHookSummary(params: {
  usesLegacyBeforeAgentStart: boolean;
  typedHookCount: number;
  customHookCount: number;
}): string {
  const parts: string[] = [];
  if (params.usesLegacyBeforeAgentStart) {
    parts.push("before_agent_start");
  }
  const nonLegacyTypedHookCount =
    params.typedHookCount - (params.usesLegacyBeforeAgentStart ? 1 : 0);
  if (nonLegacyTypedHookCount > 0) {
    parts.push(`${nonLegacyTypedHookCount} typed`);
  }
  if (params.customHookCount > 0) {
    parts.push(`${params.customHookCount} custom`);
  }
  return parts.length > 0 ? parts.join(", ") : "-";
}

export function formatInstallLines(install: PluginInstallRecord | undefined): string[] {
  if (!install) {
    return [];
  }
  const lines = [`Source: ${install.source}`];
  if (install.spec) {
    lines.push(`Spec: ${install.spec}`);
  }
  if (install.sourcePath) {
    lines.push(`Source path: ${shortenHomePath(install.sourcePath)}`);
  }
  if (install.installPath) {
    lines.push(`Install path: ${shortenHomePath(install.installPath)}`);
  }
  if (install.version) {
    lines.push(`Recorded version: ${install.version}`);
  }
  if (install.installedAt) {
    lines.push(`Installed at: ${install.installedAt}`);
  }
  return lines;
}

export function applySlotSelectionForPlugin(
  config: DenebConfig,
  pluginId: string,
): { config: DenebConfig; warnings: string[] } {
  const report = buildPluginStatusReport({ config });
  const plugin = report.plugins.find((entry) => entry.id === pluginId);
  if (!plugin) {
    return { config, warnings: [] };
  }
  const result = applyExclusiveSlotSelection({
    config,
    selectedId: plugin.id,
    selectedKind: plugin.kind,
    registry: report,
  });
  return { config: result.config, warnings: result.warnings };
}

export function createPluginInstallLogger(): {
  info: (msg: string) => void;
  warn: (msg: string) => void;
} {
  return {
    info: (msg) => defaultRuntime.log(msg),
    warn: (msg) => defaultRuntime.log(theme.warn(msg)),
  };
}

export function logSlotWarnings(warnings: string[]) {
  if (warnings.length === 0) {
    return;
  }
  for (const warning of warnings) {
    defaultRuntime.log(theme.warn(warning));
  }
}

export async function installBundledPluginSource(params: {
  config: DenebConfig;
  rawSpec: string;
  bundledSource: BundledPluginSource;
  warning: string;
}) {
  const existing = params.config.plugins?.load?.paths ?? [];
  const mergedPaths = Array.from(new Set([...existing, params.bundledSource.localPath]));
  let next: DenebConfig = {
    ...params.config,
    plugins: {
      ...params.config.plugins,
      load: {
        ...params.config.plugins?.load,
        paths: mergedPaths,
      },
      entries: {
        ...params.config.plugins?.entries,
        [params.bundledSource.pluginId]: {
          ...(params.config.plugins?.entries?.[params.bundledSource.pluginId] as
            | object
            | undefined),
          enabled: true,
        },
      },
    },
  };
  next = recordPluginInstall(next, {
    pluginId: params.bundledSource.pluginId,
    source: "path",
    spec: params.rawSpec,
    sourcePath: params.bundledSource.localPath,
    installPath: params.bundledSource.localPath,
  });
  const slotResult = applySlotSelectionForPlugin(next, params.bundledSource.pluginId);
  next = slotResult.config;
  await writeConfigFile(next);
  logSlotWarnings(slotResult.warnings);
  defaultRuntime.log(theme.warn(params.warning));
  defaultRuntime.log(`Installed plugin: ${params.bundledSource.pluginId}`);
  defaultRuntime.log(`Restart the gateway to load plugins.`);
}
