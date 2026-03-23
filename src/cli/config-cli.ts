/**
 * Config CLI barrel: re-exports run* handlers and exposes registerConfigCli.
 *
 * Implementation is split across:
 *   config-cli-path.ts   — path parsing/traversal utilities
 *   config-cli-ops.ts    — ConfigSetOperation builders (value/ref/provider/batch)
 *   config-cli-dryrun.ts — dry-run validation helpers
 *   config-cli-run.ts    — exported run* command handlers
 */

import type { Command } from "commander";
import { defaultRuntime } from "../runtime.js";
import { formatDocsLink } from "../terminal/links.js";
import { theme } from "../terminal/theme.js";
import { formatCliCommand } from "./command-format.js";
import {
  runConfigSet,
  runConfigGet,
  runConfigUnset,
  runConfigFile,
  runConfigValidate,
} from "./config-cli-run.js";
import type { ConfigSetOptions } from "./config-set-input.js";

export { runConfigSet, runConfigGet, runConfigUnset, runConfigFile, runConfigValidate };

const CONFIG_SET_EXAMPLE_VALUE = formatCliCommand(
  "deneb config set gateway.port 19001 --strict-json",
);
const CONFIG_SET_EXAMPLE_REF = formatCliCommand(
  "deneb config set channels.discord.token --ref-provider default --ref-source env --ref-id DISCORD_BOT_TOKEN",
);
const CONFIG_SET_EXAMPLE_PROVIDER = formatCliCommand(
  "deneb config set secrets.providers.vault --provider-source file --provider-path /etc/deneb/secrets.json --provider-mode json",
);
const CONFIG_SET_EXAMPLE_BATCH = formatCliCommand(
  "deneb config set --batch-file ./config-set.batch.json --dry-run",
);
const CONFIG_SET_DESCRIPTION = [
  "Set config values by path (value mode, ref/provider builder mode, or batch JSON mode).",
  "Examples:",
  CONFIG_SET_EXAMPLE_VALUE,
  CONFIG_SET_EXAMPLE_REF,
  CONFIG_SET_EXAMPLE_PROVIDER,
  CONFIG_SET_EXAMPLE_BATCH,
].join("\n");

export function registerConfigCli(program: Command) {
  const cmd = program
    .command("config")
    .description(
      "Non-interactive config helpers (get/set/unset/file/validate). Run without subcommand for guided setup.",
    )
    .addHelpText(
      "after",
      () =>
        `\n${theme.muted("Docs:")} ${formatDocsLink("/cli/config", "docs.deneb.ai/cli/config")}\n`,
    )
    .option(
      "--section <section>",
      "Configuration sections for guided setup (repeatable). Use with no subcommand.",
      (value: string, previous: string[]) => [...previous, value],
      [] as string[],
    )
    .action(async (opts) => {
      const { configureCommandFromSectionsArg } = await import("../commands/configure.js");
      await configureCommandFromSectionsArg(opts.section, defaultRuntime);
    });

  cmd
    .command("get")
    .description("Get a config value by dot path")
    .argument("<path>", "Config path (dot or bracket notation)")
    .option("--json", "Output JSON", false)
    .action(async (path: string, opts) => {
      await runConfigGet({ path, json: Boolean(opts.json) });
    });

  cmd
    .command("set")
    .description(CONFIG_SET_DESCRIPTION)
    .argument("[path]", "Config path (dot or bracket notation)")
    .argument("[value]", "Value (JSON5 or raw string)")
    .option("--strict-json", "Strict JSON5 parsing (error instead of raw string fallback)", false)
    .option("--json", "Legacy alias for --strict-json", false)
    .option(
      "--dry-run",
      "Validate changes without writing deneb.json (checks run in builder/json/batch modes; exec SecretRefs are skipped unless --allow-exec is set)",
      false,
    )
    .option(
      "--allow-exec",
      "Dry-run only: allow exec SecretRef resolvability checks (may execute provider commands)",
      false,
    )
    .option("--ref-provider <alias>", "SecretRef builder: provider alias")
    .option("--ref-source <source>", "SecretRef builder: source (env|file|exec)")
    .option("--ref-id <id>", "SecretRef builder: ref id")
    .option("--provider-source <source>", "Provider builder: source (env|file|exec)")
    .option(
      "--provider-allowlist <envVar>",
      "Provider builder (env): allowlist entry (repeatable)",
      (value: string, previous: string[]) => [...previous, value],
      [] as string[],
    )
    .option("--provider-path <path>", "Provider builder (file): path")
    .option("--provider-mode <mode>", "Provider builder (file): mode (singleValue|json)")
    .option("--provider-timeout-ms <ms>", "Provider builder (file|exec): timeout ms")
    .option("--provider-max-bytes <bytes>", "Provider builder (file): max bytes")
    .option("--provider-command <path>", "Provider builder (exec): absolute command path")
    .option(
      "--provider-arg <arg>",
      "Provider builder (exec): command arg (repeatable)",
      (value: string, previous: string[]) => [...previous, value],
      [] as string[],
    )
    .option("--provider-no-output-timeout-ms <ms>", "Provider builder (exec): no-output timeout ms")
    .option("--provider-max-output-bytes <bytes>", "Provider builder (exec): max output bytes")
    .option("--provider-json-only", "Provider builder (exec): require JSON output", false)
    .option(
      "--provider-env <key=value>",
      "Provider builder (exec): env assignment (repeatable)",
      (value: string, previous: string[]) => [...previous, value],
      [] as string[],
    )
    .option(
      "--provider-pass-env <envVar>",
      "Provider builder (exec): pass host env var (repeatable)",
      (value: string, previous: string[]) => [...previous, value],
      [] as string[],
    )
    .option(
      "--provider-trusted-dir <path>",
      "Provider builder (exec): trusted directory (repeatable)",
      (value: string, previous: string[]) => [...previous, value],
      [] as string[],
    )
    .option(
      "--provider-allow-insecure-path",
      "Provider builder (exec): bypass strict path permission checks",
      false,
    )
    .option(
      "--provider-allow-symlink-command",
      "Provider builder (exec): allow command symlink path",
      false,
    )
    .option("--batch-json <json>", "Batch mode: JSON array of set operations")
    .option("--batch-file <path>", "Batch mode: read JSON array of set operations from file")
    .action(async (path: string | undefined, value: string | undefined, opts: ConfigSetOptions) => {
      await runConfigSet({
        path,
        value,
        cliOptions: opts,
      });
    });

  cmd
    .command("unset")
    .description("Remove a config value by dot path")
    .argument("<path>", "Config path (dot or bracket notation)")
    .action(async (path: string) => {
      await runConfigUnset({ path });
    });

  cmd
    .command("file")
    .description("Print the active config file path")
    .action(async () => {
      await runConfigFile({});
    });

  cmd
    .command("validate")
    .description("Validate the current config against the schema without starting the gateway")
    .option("--json", "Output validation result as JSON", false)
    .action(async (opts) => {
      await runConfigValidate({ json: Boolean(opts.json) });
    });
}
