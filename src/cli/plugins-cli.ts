import type { Command } from "commander";
import { formatDocsLink } from "../terminal/links.js";
import { theme } from "../terminal/theme.js";
import { runPluginDoctorCommand } from "./plugins-cli-doctor.js";
import { runPluginDisableCommand, runPluginEnableCommand } from "./plugins-cli-enable.js";
import { runPluginInspectCommand } from "./plugins-cli-inspect.js";
import { runPluginInstallCommand } from "./plugins-cli-install.js";
import { runPluginListCommand } from "./plugins-cli-list.js";
import { runMarketplaceListCommand } from "./plugins-cli-marketplace.js";
import type {
  PluginInspectOptions,
  PluginMarketplaceListOptions,
  PluginUninstallOptions,
  PluginUpdateOptions,
  PluginsListOptions,
} from "./plugins-cli-shared.js";
import { runPluginUninstallCommand } from "./plugins-cli-uninstall.js";
import { runPluginUpdateCommand } from "./plugins-cli-update.js";

export function registerPluginsCli(program: Command) {
  const plugins = program
    .command("plugins")
    .description("Manage Deneb plugins and extensions")
    .addHelpText(
      "after",
      () =>
        `\n${theme.muted("Docs:")} ${formatDocsLink("/cli/plugins", "docs.deneb.ai/cli/plugins")}\n`,
    );

  plugins
    .command("list")
    .description("List discovered plugins")
    .option("--json", "Print JSON")
    .option("--enabled", "Only show enabled plugins", false)
    .option("--verbose", "Show detailed entries", false)
    .action((opts: PluginsListOptions) => {
      runPluginListCommand(opts);
    });

  plugins
    .command("inspect")
    .alias("info")
    .description("Inspect plugin details")
    .argument("[id]", "Plugin id")
    .option("--all", "Inspect all plugins")
    .option("--json", "Print JSON")
    .action((id: string | undefined, opts: PluginInspectOptions) => {
      runPluginInspectCommand(id, opts);
    });

  plugins
    .command("enable")
    .description("Enable a plugin in config")
    .argument("<id>", "Plugin id")
    .action(async (id: string) => {
      await runPluginEnableCommand(id);
    });

  plugins
    .command("disable")
    .description("Disable a plugin in config")
    .argument("<id>", "Plugin id")
    .action(async (id: string) => {
      await runPluginDisableCommand(id);
    });

  plugins
    .command("uninstall")
    .description("Uninstall a plugin")
    .argument("<id>", "Plugin id")
    .option("--keep-files", "Keep installed files on disk", false)
    .option("--keep-config", "Deprecated alias for --keep-files", false)
    .option("--force", "Skip confirmation prompt", false)
    .option("--dry-run", "Show what would be removed without making changes", false)
    .action(async (id: string, opts: PluginUninstallOptions) => {
      await runPluginUninstallCommand(id, opts);
    });

  plugins
    .command("install")
    .description("Install a plugin (path, archive, npm spec, or marketplace entry)")
    .argument(
      "<path-or-spec-or-plugin>",
      "Path (.ts/.js/.zip/.tgz/.tar.gz), npm package spec, or marketplace plugin name",
    )
    .option("-l, --link", "Link a local path instead of copying", false)
    .option("--pin", "Record npm installs as exact resolved <name>@<version>", false)
    .option(
      "--marketplace <source>",
      "Install a Claude marketplace plugin from a local repo/path or git/GitHub source",
    )
    .action(async (raw: string, opts: { link?: boolean; pin?: boolean; marketplace?: string }) => {
      await runPluginInstallCommand({ raw, opts });
    });

  plugins
    .command("update")
    .description("Update installed plugins (npm and marketplace installs)")
    .argument("[id]", "Plugin id (omit with --all)")
    .option("--all", "Update all tracked plugins", false)
    .option("--dry-run", "Show what would change without writing", false)
    .action(async (id: string | undefined, opts: PluginUpdateOptions) => {
      await runPluginUpdateCommand(id, opts);
    });

  plugins
    .command("doctor")
    .description("Report plugin load issues")
    .action(() => {
      runPluginDoctorCommand();
    });

  const marketplace = plugins
    .command("marketplace")
    .description("Inspect Claude-compatible plugin marketplaces");

  marketplace
    .command("list")
    .description("List plugins published by a marketplace source")
    .argument("<source>", "Local marketplace path/repo or git/GitHub source")
    .option("--json", "Print JSON")
    .action(async (source: string, opts: PluginMarketplaceListOptions) => {
      await runMarketplaceListCommand(source, opts);
    });
}
