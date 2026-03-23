import {
  buildPluginCompatibilityNotices,
  buildPluginStatusReport,
  formatPluginCompatibilityNotice,
} from "../plugins/status.js";
import { defaultRuntime } from "../runtime.js";
import { formatDocsLink } from "../terminal/links.js";
import { theme } from "../terminal/theme.js";

export function runPluginDoctorCommand() {
  const report = buildPluginStatusReport();
  const errors = report.plugins.filter((p) => p.status === "error");
  const diags = report.diagnostics.filter((d) => d.level === "error");
  const compatibility = buildPluginCompatibilityNotices({ report });

  if (errors.length === 0 && diags.length === 0 && compatibility.length === 0) {
    defaultRuntime.log("No plugin issues detected.");
    return;
  }

  const lines: string[] = [];
  if (errors.length > 0) {
    lines.push(theme.error("Plugin errors:"));
    for (const entry of errors) {
      lines.push(`- ${entry.id}: ${entry.error ?? "failed to load"} (${entry.source})`);
    }
  }
  if (diags.length > 0) {
    if (lines.length > 0) {
      lines.push("");
    }
    lines.push(theme.warn("Diagnostics:"));
    for (const diag of diags) {
      const target = diag.pluginId ? `${diag.pluginId}: ` : "";
      lines.push(`- ${target}${diag.message}`);
    }
  }
  if (compatibility.length > 0) {
    if (lines.length > 0) {
      lines.push("");
    }
    lines.push(theme.warn("Compatibility:"));
    for (const notice of compatibility) {
      const marker = notice.severity === "warn" ? theme.warn("warn") : theme.muted("info");
      lines.push(`- ${formatPluginCompatibilityNotice(notice)} [${marker}]`);
    }
  }
  const docs = formatDocsLink("/plugin", "docs.deneb.ai/plugin");
  lines.push("");
  lines.push(`${theme.muted("Docs:")} ${docs}`);
  defaultRuntime.log(lines.join("\n"));
}
