import { inspectBundleMcpRuntimeSupport } from "./bundle-mcp.js";
import type { PluginRecord } from "./registry.js";
import type { PluginDiagnostic } from "./types.js";

/**
 * Validate bundle capabilities and emit diagnostics for unsupported/partial
 * capabilities. Returns true if the plugin should skip further loading
 * (bundle plugins are registered without module import).
 */
export function validateBundlePlugin(params: {
  record: PluginRecord;
  enabled: boolean;
  diagnostics: PluginDiagnostic[];
}): boolean {
  const { record, diagnostics } = params;
  if (record.format !== "bundle") {
    return false;
  }

  const unsupportedCapabilities = (record.bundleCapabilities ?? []).filter(
    (capability) =>
      capability !== "skills" &&
      capability !== "mcpServers" &&
      capability !== "settings" &&
      !(
        (capability === "commands" ||
          capability === "agents" ||
          capability === "outputStyles" ||
          capability === "lspServers") &&
        (record.bundleFormat === "claude" || record.bundleFormat === "cursor")
      ) &&
      !(
        capability === "hooks" &&
        (record.bundleFormat === "codex" || record.bundleFormat === "claude")
      ),
  );

  for (const capability of unsupportedCapabilities) {
    diagnostics.push({
      level: "warn",
      pluginId: record.id,
      source: record.source,
      message: `bundle capability detected but not wired into Deneb yet: ${capability}`,
    });
  }

  if (
    params.enabled &&
    record.rootDir &&
    record.bundleFormat &&
    (record.bundleCapabilities ?? []).includes("mcpServers")
  ) {
    const runtimeSupport = inspectBundleMcpRuntimeSupport({
      pluginId: record.id,
      rootDir: record.rootDir,
      bundleFormat: record.bundleFormat,
    });
    for (const message of runtimeSupport.diagnostics) {
      diagnostics.push({
        level: "warn",
        pluginId: record.id,
        source: record.source,
        message,
      });
    }
    if (runtimeSupport.unsupportedServerNames.length > 0) {
      diagnostics.push({
        level: "warn",
        pluginId: record.id,
        source: record.source,
        message:
          "bundle MCP servers use unsupported transports or incomplete configs " +
          `(stdio only today): ${runtimeSupport.unsupportedServerNames.join(", ")}`,
      });
    }
  }

  return true;
}
