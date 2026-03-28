---
summary: "Advanced setup and development workflows for Deneb"
read_when:
  - Setting up a new machine
  - You want “latest + greatest” without breaking your personal setup
title: "Setup"
---

# Setup

<Note>
If you are setting up for the first time, start with [Getting Started](/start/getting-started).
For onboarding details, see [Onboarding (CLI)](/start/wizard).
</Note>

Last updated: 2026-03-25

## TL;DR

- **Tailoring lives outside the repo:** `~/.deneb/workspace` (workspace) + `~/.deneb/deneb.json` (config).
- **Stable workflow:** install the macOS app; let it run the bundled Gateway.
- **Bleeding edge workflow:** run the Gateway yourself via `pnpm gateway:watch`, then let the macOS app attach in Local mode.

## Prereqs (from source)

- Node `>=22`
- `pnpm`
- Go `>=1.24` (for the Go gateway server)
- Rust stable (for the Rust core library; install via [rustup](https://rustup.rs))
- `buf` and `protoc` (for protobuf codegen; see `make proto`)
- Docker (optional; only for containerized setup/e2e — see [Docker](/install/docker))

## Tailoring strategy (so updates do not hurt)

If you want “100% tailored to me” _and_ easy updates, keep your customization in:

- **Config:** `~/.deneb/deneb.json` (JSON/JSON5-ish)
- **Workspace:** `~/.deneb/workspace` (skills, prompts, memories; make it a private git repo)

Bootstrap once:

```bash
deneb setup
```

From inside this repo, use the local CLI entry:

```bash
deneb setup
```

If you don’t have a global install yet, run it via `pnpm deneb setup`.

## Run the Gateway from this repo

After `pnpm build`, you can run the packaged CLI directly:

```bash
node deneb.mjs gateway --port 18789 --verbose
```

## Stable workflow (macOS app first)

1. Install + launch **Deneb.app** (menu bar).
2. Complete the onboarding/permissions checklist (TCC prompts).
3. Ensure Gateway is **Local** and running (the app manages it).
4. Link surfaces (example: WhatsApp):

```bash
deneb channels login
```

5. Sanity check:

```bash
deneb health
```

If onboarding is not available in your build:

- Run `deneb setup`, then `deneb channels login`, then start the Gateway manually (`deneb gateway`).

## Bleeding edge workflow (Gateway in a terminal)

Goal: work on the TypeScript Gateway, get hot reload, keep the macOS app UI attached.

### 0) (Optional) Run the macOS app from source too

If you also want the macOS app on the bleeding edge:

```bash
./scripts/restart-mac.sh
```

### 1) Start the dev Gateway

```bash
pnpm install
pnpm gateway:watch
```

`gateway:watch` runs the gateway in watch mode and reloads on relevant source,
config, and bundled-plugin metadata changes.

### 2) Point the macOS app at your running Gateway

In **Deneb.app**:

- Connection Mode: **Local**
  The app will attach to the running gateway on the configured port.

### 3) Verify

- In-app Gateway status should read **“Using existing gateway …”**
- Or via CLI:

```bash
deneb health
```

### Common footguns

- **Wrong port:** Gateway WS defaults to `ws://127.0.0.1:18789`; keep app + CLI on the same port.
- **Where state lives:**
  - Credentials: `~/.deneb/credentials/`
  - Sessions: `~/.deneb/agents/<agentId>/sessions/`
  - Logs: `/tmp/deneb/`

## Credential storage map

Use this when debugging auth or deciding what to back up:

- **WhatsApp**: `~/.deneb/credentials/whatsapp/<accountId>/creds.json`
- **Telegram bot token**: config/env or `channels.telegram.tokenFile` (regular file only; symlinks rejected)
- **Discord bot token**: config/env or SecretRef (env/file/exec providers)
- **Slack tokens**: config/env (`channels.slack.*`)
- **Pairing allowlists**:
  - `~/.deneb/credentials/<channel>-allowFrom.json` (default account)
  - `~/.deneb/credentials/<channel>-<accountId>-allowFrom.json` (non-default accounts)
- **Model auth profiles**: `~/.deneb/agents/<agentId>/agent/auth-profiles.json`
- **File-backed secrets payload (optional)**: `~/.deneb/secrets.json`
- **Legacy OAuth import**: `~/.deneb/credentials/oauth.json`
  More detail: [Security](/gateway/security#credential-storage-map).

## Updating (without wrecking your setup)

- Keep `~/.deneb/workspace` and `~/.deneb/` as “your stuff”; don’t put personal prompts/config into the `deneb` repo.
- Updating source: `git pull` + `pnpm install` (when lockfile changed) + keep using `pnpm gateway:watch`.

## Linux (systemd user service)

Linux installs use a systemd **user** service. By default, systemd stops user
services on logout/idle, which kills the Gateway. Onboarding attempts to enable
lingering for you (may prompt for sudo). If it’s still off, run:

```bash
sudo loginctl enable-linger $USER
```

For always-on or multi-user servers, consider a **system** service instead of a
user service (no lingering needed). See [Gateway runbook](/gateway) for the systemd notes.

## Related docs

- [Gateway runbook](/gateway) (flags, supervision, ports)
- [Gateway configuration](/gateway/configuration) (config schema + examples)
- [Telegram](/channels/telegram) (reply tags + replyToMode settings)
- [Deneb assistant setup](/start/deneb)
