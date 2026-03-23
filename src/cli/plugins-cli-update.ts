import { loadConfig, writeConfigFile } from "../config/config.js";
import { updateNpmInstalledPlugins } from "../plugins/update.js";
import { defaultRuntime } from "../runtime.js";
import { theme } from "../terminal/theme.js";
import type { PluginUpdateOptions } from "./plugins-cli-shared.js";
import { promptYesNo } from "./prompt.js";

export async function runPluginUpdateCommand(id: string | undefined, opts: PluginUpdateOptions) {
  const cfg = loadConfig();
  const installs = cfg.plugins?.installs ?? {};
  const targets = opts.all ? Object.keys(installs) : id ? [id] : [];

  if (targets.length === 0) {
    if (opts.all) {
      defaultRuntime.log("No tracked plugins to update.");
      return;
    }
    defaultRuntime.error("Provide a plugin id or use --all.");
    return defaultRuntime.exit(1);
  }

  const result = await updateNpmInstalledPlugins({
    config: cfg,
    pluginIds: targets,
    dryRun: opts.dryRun,
    logger: {
      info: (msg) => defaultRuntime.log(msg),
      warn: (msg) => defaultRuntime.log(theme.warn(msg)),
    },
    onIntegrityDrift: async (drift) => {
      const specLabel = drift.resolvedSpec ?? drift.spec;
      defaultRuntime.log(
        theme.warn(
          `Integrity drift detected for "${drift.pluginId}" (${specLabel})` +
            `\nExpected: ${drift.expectedIntegrity}` +
            `\nActual:   ${drift.actualIntegrity}`,
        ),
      );
      if (drift.dryRun) {
        return true;
      }
      return await promptYesNo(`Continue updating "${drift.pluginId}" with this artifact?`);
    },
  });

  for (const outcome of result.outcomes) {
    if (outcome.status === "error") {
      defaultRuntime.log(theme.error(outcome.message));
      continue;
    }
    if (outcome.status === "skipped") {
      defaultRuntime.log(theme.warn(outcome.message));
      continue;
    }
    defaultRuntime.log(outcome.message);
  }

  if (!opts.dryRun && result.changed) {
    await writeConfigFile(result.config);
    defaultRuntime.log("Restart the gateway to load plugins.");
  }
}
