import { resolvePluginSourceRoots, formatPluginSourceForTable } from "../plugins/source-display.js";
import { buildPluginStatusReport } from "../plugins/status.js";
import { defaultRuntime } from "../runtime.js";
import { getTerminalTableWidth, renderTable } from "../terminal/table.js";
import { theme } from "../terminal/theme.js";
import { formatPluginLine, type PluginsListOptions } from "./plugins-cli-shared.js";

export function runPluginListCommand(opts: PluginsListOptions) {
  const report = buildPluginStatusReport();
  const list = opts.enabled ? report.plugins.filter((p) => p.status === "loaded") : report.plugins;

  if (opts.json) {
    const payload = {
      workspaceDir: report.workspaceDir,
      plugins: list,
      diagnostics: report.diagnostics,
    };
    defaultRuntime.log(JSON.stringify(payload, null, 2));
    return;
  }

  if (list.length === 0) {
    defaultRuntime.log(theme.muted("No plugins found."));
    return;
  }

  const loaded = list.filter((p) => p.status === "loaded").length;
  defaultRuntime.log(
    `${theme.heading("Plugins")} ${theme.muted(`(${loaded}/${list.length} loaded)`)}`,
  );

  if (!opts.verbose) {
    const tableWidth = getTerminalTableWidth();
    const sourceRoots = resolvePluginSourceRoots({
      workspaceDir: report.workspaceDir,
    });
    const usedRoots = new Set<keyof typeof sourceRoots>();
    const rows = list.map((plugin) => {
      const desc = plugin.description ? theme.muted(plugin.description) : "";
      const formattedSource = formatPluginSourceForTable(plugin, sourceRoots);
      if (formattedSource.rootKey) {
        usedRoots.add(formattedSource.rootKey);
      }
      const sourceLine = desc ? `${formattedSource.value}\n${desc}` : formattedSource.value;
      return {
        Name: plugin.name || plugin.id,
        ID: plugin.name && plugin.name !== plugin.id ? plugin.id : "",
        Format: plugin.format ?? "deneb",
        Status:
          plugin.status === "loaded"
            ? theme.success("loaded")
            : plugin.status === "disabled"
              ? theme.warn("disabled")
              : theme.error("error"),
        Source: sourceLine,
        Version: plugin.version ?? "",
      };
    });

    if (usedRoots.size > 0) {
      defaultRuntime.log(theme.muted("Source roots:"));
      for (const key of ["stock", "workspace", "global"] as const) {
        if (!usedRoots.has(key)) {
          continue;
        }
        const dir = sourceRoots[key];
        if (!dir) {
          continue;
        }
        defaultRuntime.log(`  ${theme.command(`${key}:`)} ${theme.muted(dir)}`);
      }
      defaultRuntime.log("");
    }

    defaultRuntime.log(
      renderTable({
        width: tableWidth,
        columns: [
          { key: "Name", header: "Name", minWidth: 14, flex: true },
          { key: "ID", header: "ID", minWidth: 10, flex: true },
          { key: "Format", header: "Format", minWidth: 9 },
          { key: "Status", header: "Status", minWidth: 10 },
          { key: "Source", header: "Source", minWidth: 26, flex: true },
          { key: "Version", header: "Version", minWidth: 8 },
        ],
        rows,
      }).trimEnd(),
    );
    return;
  }

  const lines: string[] = [];
  for (const plugin of list) {
    lines.push(formatPluginLine(plugin, true));
    lines.push("");
  }
  defaultRuntime.log(lines.join("\n").trim());
}
