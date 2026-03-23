import { listMarketplacePlugins } from "../plugins/marketplace.js";
import { defaultRuntime } from "../runtime.js";
import { theme } from "../terminal/theme.js";
import {
  createPluginInstallLogger,
  type PluginMarketplaceListOptions,
} from "./plugins-cli-shared.js";

export async function runMarketplaceListCommand(
  source: string,
  opts: PluginMarketplaceListOptions,
) {
  const result = await listMarketplacePlugins({
    marketplace: source,
    logger: createPluginInstallLogger(),
  });
  if (!result.ok) {
    defaultRuntime.error(result.error);
    return defaultRuntime.exit(1);
  }

  if (opts.json) {
    defaultRuntime.log(
      JSON.stringify(
        {
          source: result.sourceLabel,
          name: result.manifest.name,
          version: result.manifest.version,
          plugins: result.manifest.plugins,
        },
        null,
        2,
      ),
    );
    return;
  }

  if (result.manifest.plugins.length === 0) {
    defaultRuntime.log(`No plugins found in marketplace ${result.sourceLabel}.`);
    return;
  }

  defaultRuntime.log(
    `${theme.heading("Marketplace")} ${theme.muted(result.manifest.name ?? result.sourceLabel)}`,
  );
  for (const plugin of result.manifest.plugins) {
    const suffix = plugin.version ? theme.muted(` v${plugin.version}`) : "";
    const desc = plugin.description ? ` - ${theme.muted(plugin.description)}` : "";
    defaultRuntime.log(`${theme.command(plugin.name)}${suffix}${desc}`);
  }
}
