---
summary: "Where Deneb loads environment variables and the precedence order"
read_when:
  - You need to know which env vars are loaded, and in what order
  - You are debugging missing API keys in the Gateway
  - You are documenting provider auth or deployment environments
title: "Environment Variables"
---

# Environment variables

Deneb pulls environment variables from multiple sources. The rule is **never override existing values**.

## Precedence (highest → lowest)

1. **Process environment** (what the Gateway process already has from the parent shell/daemon).
2. **`.env` in the current working directory** (dotenv default; does not override).
3. **Global `.env`** at `~/.deneb/.env` (aka `$DENEB_STATE_DIR/.env`; does not override).
4. **Config `env` block** in `~/.deneb/deneb.json` (applied only if missing).
5. **Optional login-shell import** (`env.shellEnv.enabled` or `DENEB_LOAD_SHELL_ENV=1`), applied only for missing expected keys.

If the config file is missing entirely, step 4 is skipped; shell import still runs if enabled.

## Config `env` block

Two equivalent ways to set inline env vars (both are non-overriding):

```json5
{
  env: {
    OPENROUTER_API_KEY: "sk-or-...",
    vars: {
      GROQ_API_KEY: "gsk-...",
    },
  },
}
```

## Shell env import

`env.shellEnv` runs your login shell and imports only **missing** expected keys:

```json5
{
  env: {
    shellEnv: {
      enabled: true,
      timeoutMs: 15000,
    },
  },
}
```

Env var equivalents:

- `DENEB_LOAD_SHELL_ENV=1`
- `DENEB_SHELL_ENV_TIMEOUT_MS=15000`

## Runtime-injected env vars

Deneb also injects context markers into spawned child processes:

- `DENEB_SHELL=exec`: set for commands run through the `exec` tool.
- `DENEB_SHELL=acp`: set for ACP runtime backend process spawns (for example `acpx`).
- `DENEB_SHELL=acp-client`: set for `deneb acp client` when it spawns the ACP bridge process.
- `DENEB_SHELL=tui-local`: set for local TUI `!` shell commands.

These are runtime markers (not required user config). They can be used in shell/profile logic
to apply context-specific rules.

## UI env vars

- `DENEB_THEME=light`: force the light TUI palette when your terminal has a light background.
- `DENEB_THEME=dark`: force the dark TUI palette.
- `COLORFGBG`: if your terminal exports it, Deneb uses the background color hint to auto-pick the TUI palette.

## Env var substitution in config

You can reference env vars directly in config string values using `${VAR_NAME}` syntax:

```json5
{
  models: {
    providers: {
      "vercel-gateway": {
        apiKey: "${VERCEL_GATEWAY_API_KEY}",
      },
    },
  },
}
```

See [Configuration: Env var substitution](/gateway/configuration#env-var-substitution-in-config) for full details.

## Secret refs vs `${ENV}` strings

Deneb supports two env-driven patterns:

- `${VAR}` string substitution in config values.
- SecretRef objects (`{ source: "env", provider: "default", id: "VAR" }`) for fields that support secrets references.

Both resolve from process env at activation time. SecretRef details are documented in [Secrets Management](/gateway/secrets).

## Path-related env vars

| Variable            | Purpose                                                                                                                                                                    |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DENEB_HOME`        | Override the home directory used for all internal path resolution (`~/.deneb/`, agent dirs, sessions, credentials). Useful when running Deneb as a dedicated service user. |
| `DENEB_STATE_DIR`   | Override the state directory (default `~/.deneb`).                                                                                                                         |
| `DENEB_CONFIG_PATH` | Override the config file path (default `~/.deneb/deneb.json`).                                                                                                             |

## Logging

| Variable          | Purpose                                                                                                                                                                                      |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DENEB_LOG_LEVEL` | Override log level for both file and console (e.g. `debug`, `trace`). Takes precedence over `logging.level` and `logging.consoleLevel` in config. Invalid values are ignored with a warning. |

### `DENEB_HOME`

When set, `DENEB_HOME` replaces the system home directory (`$HOME` / `os.homedir()`) for all internal path resolution. This enables full filesystem isolation for headless service accounts.

**Precedence:** `DENEB_HOME` > `$HOME` > `USERPROFILE` > `os.homedir()`

**Example** (macOS LaunchDaemon):

```xml
<key>EnvironmentVariables</key>
<dict>
  <key>DENEB_HOME</key>
  <string>/Users/kira</string>
</dict>
```

`DENEB_HOME` can also be set to a tilde path (e.g. `~/svc`), which gets expanded using `$HOME` before use.

## Related

- [Gateway configuration](/gateway/configuration)
- [FAQ: env vars and .env loading](/help/faq#env-vars-and-env-loading)
- [Models overview](/concepts/models)
