---
summary: "CLI reference for `deneb models` (status/list/set/scan, aliases, fallbacks, auth)"
read_when:
  - You want to change default models or view provider auth status
  - You want to scan available models/providers and debug auth profiles
title: "models"
---

# `deneb models`

Model discovery, scanning, and configuration (default model, fallbacks, auth profiles).

Related:

- Providers + models: [Models](/providers/models)
- Provider auth setup: [Getting started](/start/getting-started)

## Common commands

```bash
deneb models status
deneb models list
deneb models set <model-or-alias>
deneb models scan
```

`deneb models status` shows the resolved default/fallbacks plus an auth overview.
When provider usage snapshots are available, the OAuth/token status section includes
provider usage headers.
Add `--probe` to run live auth probes against each configured provider profile.
Probes are real requests (may consume tokens and trigger rate limits).
Use `--agent <id>` to inspect a configured agent’s model/auth state. When omitted,
the command uses `DENEB_AGENT_DIR`/`PI_CODING_AGENT_DIR` if set, otherwise the
configured default agent.

Notes:

- `models set <model-or-alias>` accepts `provider/model` or an alias.
- Model refs are parsed by splitting on the **first** `/`. If the model ID includes `/` (OpenRouter-style), include the provider prefix (example: `openrouter/moonshotai/kimi-k2`).
- If you omit the provider, Deneb treats the input as an alias or a model for the **default provider** (only works when there is no `/` in the model ID).
- `models status` may show `marker(<value>)` in auth output for non-secret placeholders (for example `OPENAI_API_KEY`, `secretref-managed`, `minimax-oauth`, `qwen-oauth`, `ollama-local`) instead of masking them as secrets.

### `models status`

Options:

- `--json`
- `--plain`
- `--check` (exit 1=expired/missing, 2=expiring)
- `--probe` (live probe of configured auth profiles)
- `--probe-provider <name>` (probe one provider)
- `--probe-profile <id>` (repeat or comma-separated profile ids)
- `--probe-timeout <ms>`
- `--probe-concurrency <n>`
- `--probe-max-tokens <n>`
- `--agent <id>` (configured agent id; overrides `DENEB_AGENT_DIR`/`PI_CODING_AGENT_DIR`)

## Aliases + fallbacks

```bash
deneb models aliases list
deneb models fallbacks list
```

## Auth profiles

```bash
deneb models auth add
deneb models auth login --provider <id>
deneb models auth setup-token
deneb models auth paste-token
```

`models auth login` runs a provider plugin’s auth flow (OAuth/API key). Use
`deneb plugins list` to see which providers are installed.

Notes:

- `setup-token` prompts for a setup-token value (generate it with `claude setup-token` on any machine).
- `paste-token` accepts a token string generated elsewhere or from automation.
- Anthropic policy note: setup-token support is technical compatibility. Anthropic has blocked some subscription usage outside Claude Code in the past, so verify current terms before using it broadly.
