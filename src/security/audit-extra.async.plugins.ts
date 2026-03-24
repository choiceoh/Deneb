/**
 * Async security audit: plugin trust, install integrity, and code safety checks.
 */
import path from "node:path";
import { resolveSandboxConfigForAgent } from "../agents/sandbox/config.js";
import { isToolAllowedByPolicies } from "../agents/tool-policy-match.js";
import { resolveToolProfilePolicy } from "../agents/tool-policy.js";
import { resolveNativeSkillsEnabled } from "../config/commands.js";
import type { DenebConfig } from "../config/config.js";
import type { NativeCommandsSetting } from "../config/types.js";
import { hasConfiguredSecretInput } from "../config/types.secrets.js";
import type { AgentToolsConfig } from "../config/types.tools.js";
import { normalizePluginsConfig } from "../plugins/config-state.js";
import { resolveToolPolicies, type SecurityAuditFinding } from "./audit-extra-shared.js";
import {
  listInstalledPluginDirs,
  isPinnedRegistrySpec,
  readInstalledPackageVersion,
  readPluginManifestExtensions,
  formatCodeSafetyDetails,
  getCodeSafetySummary,
  type CodeSafetySummaryCache,
} from "./audit-extra.async.helpers.js";
import { extensionUsesSkippedScannerPath, isPathInside } from "./scan-paths.js";

function normalizePluginIdSet(entries: string[]): Set<string> {
  return new Set(entries.map((entry) => entry.trim().toLowerCase()).filter(Boolean));
}

function resolveEnabledExtensionPluginIds(params: {
  cfg: DenebConfig;
  pluginDirs: string[];
}): string[] {
  const normalized = normalizePluginsConfig(params.cfg.plugins);
  if (!normalized.enabled) {
    return [];
  }

  const allowSet = normalizePluginIdSet(normalized.allow);
  const denySet = normalizePluginIdSet(normalized.deny);
  const entryById = new Map<string, { enabled?: boolean }>();
  for (const [id, entry] of Object.entries(normalized.entries)) {
    entryById.set(id.trim().toLowerCase(), entry);
  }

  const enabled: string[] = [];
  for (const id of params.pluginDirs) {
    const normalizedId = id.trim().toLowerCase();
    if (!normalizedId) {
      continue;
    }
    if (denySet.has(normalizedId)) {
      continue;
    }
    if (allowSet.size > 0 && !allowSet.has(normalizedId)) {
      continue;
    }
    if (entryById.get(normalizedId)?.enabled === false) {
      continue;
    }
    enabled.push(normalizedId);
  }
  return enabled;
}

function collectAllowEntries(config?: { allow?: string[]; alsoAllow?: string[] }): string[] {
  const out: string[] = [];
  if (Array.isArray(config?.allow)) {
    out.push(...config.allow);
  }
  if (Array.isArray(config?.alsoAllow)) {
    out.push(...config.alsoAllow);
  }
  return out.map((entry) => entry.trim().toLowerCase()).filter(Boolean);
}

function hasExplicitPluginAllow(params: {
  allowEntries: string[];
  enabledPluginIds: Set<string>;
}): boolean {
  return params.allowEntries.some(
    (entry) => entry === "group:plugins" || params.enabledPluginIds.has(entry),
  );
}

function hasProviderPluginAllow(params: {
  byProvider?: Record<string, { allow?: string[]; alsoAllow?: string[]; deny?: string[] }>;
  enabledPluginIds: Set<string>;
}): boolean {
  if (!params.byProvider) {
    return false;
  }
  for (const policy of Object.values(params.byProvider)) {
    if (
      hasExplicitPluginAllow({
        allowEntries: collectAllowEntries(policy),
        enabledPluginIds: params.enabledPluginIds,
      })
    ) {
      return true;
    }
  }
  return false;
}

export async function collectPluginsTrustFindings(params: {
  cfg: DenebConfig;
  stateDir: string;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  const { extensionsDir, pluginDirs } = await listInstalledPluginDirs({
    stateDir: params.stateDir,
  });
  if (pluginDirs.length > 0) {
    const allow = params.cfg.plugins?.allow;
    const allowConfigured = Array.isArray(allow) && allow.length > 0;
    if (!allowConfigured) {
      const hasString = (value: unknown) => typeof value === "string" && value.trim().length > 0;
      const hasSecretInput = (value: unknown) =>
        hasConfiguredSecretInput(value, params.cfg.secrets?.defaults);
      const hasAccountStringKey = (account: unknown, key: string) =>
        Boolean(
          account &&
          typeof account === "object" &&
          hasString((account as Record<string, unknown>)[key]),
        );
      const hasAccountSecretInputKey = (account: unknown, key: string) =>
        Boolean(
          account &&
          typeof account === "object" &&
          hasSecretInput((account as Record<string, unknown>)[key]),
        );

      const discordConfigured =
        hasSecretInput(params.cfg.channels?.discord?.token) ||
        Boolean(
          params.cfg.channels?.discord?.accounts &&
          Object.values(params.cfg.channels.discord.accounts).some((a) =>
            hasAccountSecretInputKey(a, "token"),
          ),
        ) ||
        hasString(process.env.DISCORD_BOT_TOKEN);

      const telegramConfigured =
        hasSecretInput(params.cfg.channels?.telegram?.botToken) ||
        hasString(params.cfg.channels?.telegram?.tokenFile) ||
        Boolean(
          params.cfg.channels?.telegram?.accounts &&
          Object.values(params.cfg.channels.telegram.accounts).some(
            (a) => hasAccountSecretInputKey(a, "botToken") || hasAccountStringKey(a, "tokenFile"),
          ),
        ) ||
        hasString(process.env.TELEGRAM_BOT_TOKEN);

      const slackConfigured =
        hasSecretInput(params.cfg.channels?.slack?.botToken) ||
        hasSecretInput(params.cfg.channels?.slack?.appToken) ||
        Boolean(
          params.cfg.channels?.slack?.accounts &&
          Object.values(params.cfg.channels.slack.accounts).some(
            (a) =>
              hasAccountSecretInputKey(a, "botToken") || hasAccountSecretInputKey(a, "appToken"),
          ),
        ) ||
        hasString(process.env.SLACK_BOT_TOKEN) ||
        hasString(process.env.SLACK_APP_TOKEN);

      const skillCommandsLikelyExposed =
        (discordConfigured &&
          resolveNativeSkillsEnabled({
            providerId: "discord",
            providerSetting: (
              params.cfg.channels?.discord?.commands as Record<string, unknown> | undefined
            )?.nativeSkills as NativeCommandsSetting | undefined,
            globalSetting: (params.cfg.commands as Record<string, unknown> | undefined)
              ?.nativeSkills as NativeCommandsSetting | undefined,
          })) ||
        (telegramConfigured &&
          resolveNativeSkillsEnabled({
            providerId: "telegram",
            providerSetting: (
              params.cfg.channels?.telegram?.commands as Record<string, unknown> | undefined
            )?.nativeSkills as NativeCommandsSetting | undefined,
            globalSetting: (params.cfg.commands as Record<string, unknown> | undefined)
              ?.nativeSkills as NativeCommandsSetting | undefined,
          })) ||
        (slackConfigured &&
          resolveNativeSkillsEnabled({
            providerId: "slack",
            providerSetting: (
              params.cfg.channels?.slack?.commands as Record<string, unknown> | undefined
            )?.nativeSkills as NativeCommandsSetting | undefined,
            globalSetting: (params.cfg.commands as Record<string, unknown> | undefined)
              ?.nativeSkills as NativeCommandsSetting | undefined,
          }));

      findings.push({
        checkId: "plugins.extensions_no_allowlist",
        severity: skillCommandsLikelyExposed ? "critical" : "warn",
        title: "Extensions exist but plugins.allow is not set",
        detail:
          `Found ${pluginDirs.length} extension(s) under ${extensionsDir}. Without plugins.allow, any discovered plugin id may load (depending on config and plugin behavior).` +
          (skillCommandsLikelyExposed
            ? "\nNative skill commands are enabled on at least one configured chat surface; treat unpinned/unallowlisted extensions as high risk."
            : ""),
        remediation: "Set plugins.allow to an explicit list of plugin ids you trust.",
      });
    }

    const enabledExtensionPluginIds = resolveEnabledExtensionPluginIds({
      cfg: params.cfg,
      pluginDirs,
    });
    if (enabledExtensionPluginIds.length > 0) {
      const enabledPluginSet = new Set(enabledExtensionPluginIds);
      const contexts: Array<{
        label: string;
        agentId?: string;
        tools?: AgentToolsConfig;
      }> = [{ label: "default" }];
      for (const entry of params.cfg.agents?.list ?? []) {
        if (!entry || typeof entry !== "object" || typeof entry.id !== "string") {
          continue;
        }
        contexts.push({
          label: `agents.list.${entry.id}`,
          agentId: entry.id,
          tools: entry.tools,
        });
      }

      const permissiveContexts: string[] = [];
      for (const context of contexts) {
        const profile = context.tools?.profile ?? params.cfg.tools?.profile;
        const restrictiveProfile = Boolean(resolveToolProfilePolicy(profile));
        const sandboxMode = resolveSandboxConfigForAgent(params.cfg, context.agentId).mode;
        const policies = resolveToolPolicies({
          cfg: params.cfg,
          agentTools: context.tools,
          sandboxMode,
          agentId: context.agentId,
        });
        const broadPolicy = isToolAllowedByPolicies("__deneb_plugin_probe__", policies);
        const explicitPluginAllow =
          !restrictiveProfile &&
          (hasExplicitPluginAllow({
            allowEntries: collectAllowEntries(params.cfg.tools),
            enabledPluginIds: enabledPluginSet,
          }) ||
            hasProviderPluginAllow({
              byProvider: params.cfg.tools?.byProvider,
              enabledPluginIds: enabledPluginSet,
            }) ||
            hasExplicitPluginAllow({
              allowEntries: collectAllowEntries(context.tools),
              enabledPluginIds: enabledPluginSet,
            }) ||
            hasProviderPluginAllow({
              byProvider: context.tools?.byProvider,
              enabledPluginIds: enabledPluginSet,
            }));

        if (broadPolicy || explicitPluginAllow) {
          permissiveContexts.push(context.label);
        }
      }

      if (permissiveContexts.length > 0) {
        findings.push({
          checkId: "plugins.tools_reachable_permissive_policy",
          severity: "warn",
          title: "Extension plugin tools may be reachable under permissive tool policy",
          detail:
            `Enabled extension plugins: ${enabledExtensionPluginIds.join(", ")}.\n` +
            `Permissive tool policy contexts:\n${permissiveContexts.map((entry) => `- ${entry}`).join("\n")}`,
          remediation:
            "Use restrictive profiles (`minimal`/`coding`) or explicit tool allowlists that exclude plugin tools for agents handling untrusted input.",
        });
      }
    }
  }

  const pluginInstalls = params.cfg.plugins?.installs ?? {};
  const npmPluginInstalls = Object.entries(pluginInstalls).filter(
    ([, record]) => record?.source === "npm",
  );
  if (npmPluginInstalls.length > 0) {
    const unpinned = npmPluginInstalls
      .filter(([, record]) => typeof record.spec === "string" && !isPinnedRegistrySpec(record.spec))
      .map(([pluginId, record]) => `${pluginId} (${record.spec})`);
    if (unpinned.length > 0) {
      findings.push({
        checkId: "plugins.installs_unpinned_npm_specs",
        severity: "warn",
        title: "Plugin installs include unpinned npm specs",
        detail: `Unpinned plugin install records:\n${unpinned.map((entry) => `- ${entry}`).join("\n")}`,
        remediation:
          "Pin install specs to exact versions (for example, `@scope/pkg@1.2.3`) for higher supply-chain stability.",
      });
    }

    const missingIntegrity = npmPluginInstalls
      .filter(
        ([, record]) => typeof record.integrity !== "string" || record.integrity.trim() === "",
      )
      .map(([pluginId]) => pluginId);
    if (missingIntegrity.length > 0) {
      findings.push({
        checkId: "plugins.installs_missing_integrity",
        severity: "warn",
        title: "Plugin installs are missing integrity metadata",
        detail: `Plugin install records missing integrity:\n${missingIntegrity.map((entry) => `- ${entry}`).join("\n")}`,
        remediation:
          "Reinstall or update plugins to refresh install metadata with resolved integrity hashes.",
      });
    }

    const pluginVersionDrift: string[] = [];
    for (const [pluginId, record] of npmPluginInstalls) {
      const recordedVersion = record.resolvedVersion ?? record.version;
      if (!recordedVersion) {
        continue;
      }
      const installPath = record.installPath ?? path.join(params.stateDir, "extensions", pluginId);

      const installedVersion = await readInstalledPackageVersion(installPath);
      if (!installedVersion || installedVersion === recordedVersion) {
        continue;
      }
      pluginVersionDrift.push(
        `${pluginId} (recorded ${recordedVersion}, installed ${installedVersion})`,
      );
    }
    if (pluginVersionDrift.length > 0) {
      findings.push({
        checkId: "plugins.installs_version_drift",
        severity: "warn",
        title: "Plugin install records drift from installed package versions",
        detail: `Detected plugin install metadata drift:\n${pluginVersionDrift.map((entry) => `- ${entry}`).join("\n")}`,
        remediation:
          "Run `deneb plugins update --all` (or reinstall affected plugins) to refresh install metadata.",
      });
    }
  }

  const hookInstalls = params.cfg.hooks?.internal?.installs ?? {};
  const npmHookInstalls = Object.entries(hookInstalls).filter(
    ([, record]) => record?.source === "npm",
  );
  if (npmHookInstalls.length > 0) {
    const unpinned = npmHookInstalls
      .filter(([, record]) => typeof record.spec === "string" && !isPinnedRegistrySpec(record.spec))
      .map(([hookId, record]) => `${hookId} (${record.spec})`);
    if (unpinned.length > 0) {
      findings.push({
        checkId: "hooks.installs_unpinned_npm_specs",
        severity: "warn",
        title: "Hook installs include unpinned npm specs",
        detail: `Unpinned hook install records:\n${unpinned.map((entry) => `- ${entry}`).join("\n")}`,
        remediation:
          "Pin hook install specs to exact versions (for example, `@scope/pkg@1.2.3`) for higher supply-chain stability.",
      });
    }

    const missingIntegrity = npmHookInstalls
      .filter(
        ([, record]) => typeof record.integrity !== "string" || record.integrity.trim() === "",
      )
      .map(([hookId]) => hookId);
    if (missingIntegrity.length > 0) {
      findings.push({
        checkId: "hooks.installs_missing_integrity",
        severity: "warn",
        title: "Hook installs are missing integrity metadata",
        detail: `Hook install records missing integrity:\n${missingIntegrity.map((entry) => `- ${entry}`).join("\n")}`,
        remediation:
          "Reinstall or update hooks to refresh install metadata with resolved integrity hashes.",
      });
    }

    const hookVersionDrift: string[] = [];
    for (const [hookId, record] of npmHookInstalls) {
      const recordedVersion = record.resolvedVersion ?? record.version;
      if (!recordedVersion) {
        continue;
      }
      const installPath = record.installPath ?? path.join(params.stateDir, "hooks", hookId);

      const installedVersion = await readInstalledPackageVersion(installPath);
      if (!installedVersion || installedVersion === recordedVersion) {
        continue;
      }
      hookVersionDrift.push(
        `${hookId} (recorded ${recordedVersion}, installed ${installedVersion})`,
      );
    }
    if (hookVersionDrift.length > 0) {
      findings.push({
        checkId: "hooks.installs_version_drift",
        severity: "warn",
        title: "Hook install records drift from installed package versions",
        detail: `Detected hook install metadata drift:\n${hookVersionDrift.map((entry) => `- ${entry}`).join("\n")}`,
        remediation:
          "Run `deneb hooks update --all` (or reinstall affected hooks) to refresh install metadata.",
      });
    }
  }

  return findings;
}

export async function collectPluginsCodeSafetyFindings(params: {
  stateDir: string;
  summaryCache?: CodeSafetySummaryCache;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  const { extensionsDir, pluginDirs } = await listInstalledPluginDirs({
    stateDir: params.stateDir,
    onReadError: (err) => {
      findings.push({
        checkId: "plugins.code_safety.scan_failed",
        severity: "warn",
        title: "Plugin extensions directory scan failed",
        detail: `Static code scan could not list extensions directory: ${String(err)}`,
        remediation:
          "Check file permissions and plugin layout, then rerun `deneb security audit --deep`.",
      });
    },
  });

  for (const pluginName of pluginDirs) {
    const pluginPath = path.join(extensionsDir, pluginName);
    const extensionEntries = await readPluginManifestExtensions(pluginPath).catch(() => []);
    const forcedScanEntries: string[] = [];
    const escapedEntries: string[] = [];

    for (const entry of extensionEntries) {
      const resolvedEntry = path.resolve(pluginPath, entry);
      if (!isPathInside(pluginPath, resolvedEntry)) {
        escapedEntries.push(entry);
        continue;
      }
      if (extensionUsesSkippedScannerPath(entry)) {
        findings.push({
          checkId: "plugins.code_safety.entry_path",
          severity: "warn",
          title: `Plugin "${pluginName}" entry path is hidden or node_modules`,
          detail: `Extension entry "${entry}" points to a hidden or node_modules path. Deep code scan will cover this entry explicitly, but review this path choice carefully.`,
          remediation: "Prefer extension entrypoints under normal source paths like dist/ or src/.",
        });
      }
      forcedScanEntries.push(resolvedEntry);
    }

    if (escapedEntries.length > 0) {
      findings.push({
        checkId: "plugins.code_safety.entry_escape",
        severity: "critical",
        title: `Plugin "${pluginName}" has extension entry path traversal`,
        detail: `Found extension entries that escape the plugin directory:\n${escapedEntries.map((entry) => `  - ${entry}`).join("\n")}`,
        remediation:
          "Update the plugin manifest so all deneb.extensions entries stay inside the plugin directory.",
      });
    }

    const summary = await getCodeSafetySummary({
      dirPath: pluginPath,
      includeFiles: forcedScanEntries,
      summaryCache: params.summaryCache,
    }).catch((err) => {
      findings.push({
        checkId: "plugins.code_safety.scan_failed",
        severity: "warn",
        title: `Plugin "${pluginName}" code scan failed`,
        detail: `Static code scan could not complete: ${String(err)}`,
        remediation:
          "Check file permissions and plugin layout, then rerun `deneb security audit --deep`.",
      });
      return null;
    });
    if (!summary) {
      continue;
    }

    if (summary.critical > 0) {
      const criticalFindings = summary.findings.filter((f) => f.severity === "critical");
      const details = formatCodeSafetyDetails(criticalFindings, pluginPath);

      findings.push({
        checkId: "plugins.code_safety",
        severity: "critical",
        title: `Plugin "${pluginName}" contains dangerous code patterns`,
        detail: `Found ${summary.critical} critical issue(s) in ${summary.scannedFiles} scanned file(s):\n${details}`,
        remediation:
          "Review the plugin source code carefully before use. If untrusted, remove the plugin from your Deneb extensions state directory.",
      });
    } else if (summary.warn > 0) {
      const warnFindings = summary.findings.filter((f) => f.severity === "warn");
      const details = formatCodeSafetyDetails(warnFindings, pluginPath);

      findings.push({
        checkId: "plugins.code_safety",
        severity: "warn",
        title: `Plugin "${pluginName}" contains suspicious code patterns`,
        detail: `Found ${summary.warn} warning(s) in ${summary.scannedFiles} scanned file(s):\n${details}`,
        remediation: `Review the flagged code to ensure it is intentional and safe.`,
      });
    }
  }

  return findings;
}
