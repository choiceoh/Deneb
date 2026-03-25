# DenebDock <!-- omit in toc -->

Stop typing `docker-compose` commands. Just type `deneb-dock-start`.

Inspired by Simon Willison's [Running Deneb in Docker](https://til.simonwillison.net/llms/deneb-docker).

- [Quickstart](#quickstart)
- [Available Commands](#available-commands)
  - [Basic Operations](#basic-operations)
  - [Container Access](#container-access)
  - [Web UI \& Devices](#web-ui--devices)
  - [Setup \& Configuration](#setup--configuration)
  - [Maintenance](#maintenance)
  - [Utilities](#utilities)
- [Common Workflows](#common-workflows)
  - [Check Status and Logs](#check-status-and-logs)
  - [Set Up WhatsApp Bot](#set-up-whatsapp-bot)
  - [Troubleshooting Device Pairing](#troubleshooting-device-pairing)
  - [Fix Token Mismatch Issues](#fix-token-mismatch-issues)
  - [Permission Denied](#permission-denied)
- [Requirements](#requirements)

## Quickstart

**Install:**

```bash
mkdir -p ~/.deneb-dock && curl -sL https://raw.githubusercontent.com/deneb/deneb/main/scripts/shell-helpers/deneb-dock-helpers.sh -o ~/.deneb-dock/deneb-dock-helpers.sh
```

```bash
echo 'source ~/.deneb-dock/deneb-dock-helpers.sh' >> ~/.zshrc && source ~/.zshrc
```

**See what you get:**

```bash
deneb-dock-help
```

On first command, DenebDock auto-detects your Deneb directory:

- Checks common paths (`~/deneb`, `~/workspace/deneb`, etc.)
- If found, asks you to confirm
- Saves to `~/.deneb-dock/config`

**First time setup:**

```bash
deneb-dock-start
```

```bash
deneb-dock-fix-token
```

```bash
deneb-dock-dashboard
```

If you see "pairing required":

```bash
deneb-dock-devices
```

And approve the request for the specific device:

```bash
deneb-dock-approve <request-id>
```

## Available Commands

### Basic Operations

| Command              | Description                     |
| -------------------- | ------------------------------- |
| `deneb-dock-start`   | Start the gateway               |
| `deneb-dock-stop`    | Stop the gateway                |
| `deneb-dock-restart` | Restart the gateway             |
| `deneb-dock-status`  | Check container status          |
| `deneb-dock-logs`    | View live logs (follows output) |

### Container Access

| Command                     | Description                                    |
| --------------------------- | ---------------------------------------------- |
| `deneb-dock-shell`          | Interactive shell inside the gateway container |
| `deneb-dock-cli <command>`  | Run Deneb CLI commands                         |
| `deneb-dock-exec <command>` | Execute arbitrary commands in the container    |

### Web UI & Devices

| Command                   | Description                                |
| ------------------------- | ------------------------------------------ |
| `deneb-dock-dashboard`    | Open web UI in browser with authentication |
| `deneb-dock-devices`      | List device pairing requests               |
| `deneb-dock-approve <id>` | Approve a device pairing request           |

### Setup & Configuration

| Command                | Description                                       |
| ---------------------- | ------------------------------------------------- |
| `deneb-dock-fix-token` | Configure gateway authentication token (run once) |

### Maintenance

| Command              | Description                                      |
| -------------------- | ------------------------------------------------ |
| `deneb-dock-rebuild` | Rebuild the Docker image                         |
| `deneb-dock-clean`   | Remove all containers and volumes (destructive!) |

### Utilities

| Command                | Description                               |
| ---------------------- | ----------------------------------------- |
| `deneb-dock-health`    | Run gateway health check                  |
| `deneb-dock-token`     | Display the gateway authentication token  |
| `deneb-dock-cd`        | Jump to the Deneb project directory       |
| `deneb-dock-config`    | Open the Deneb config directory           |
| `deneb-dock-workspace` | Open the workspace directory              |
| `deneb-dock-help`      | Show all available commands with examples |

## Common Workflows

### Check Status and Logs

**Restart the gateway:**

```bash
deneb-dock-restart
```

**Check container status:**

```bash
deneb-dock-status
```

**View live logs:**

```bash
deneb-dock-logs
```

### Set Up WhatsApp Bot

**Shell into the container:**

```bash
deneb-dock-shell
```

**Inside the container, login to WhatsApp:**

```bash
deneb channels login --channel whatsapp --verbose
```

Scan the QR code with WhatsApp on your phone.

**Verify connection:**

```bash
deneb status
```

### Troubleshooting Device Pairing

**Check for pending pairing requests:**

```bash
deneb-dock-devices
```

**Copy the Request ID from the "Pending" table, then approve:**

```bash
deneb-dock-approve <request-id>
```

Then refresh your browser.

### Fix Token Mismatch Issues

If you see "gateway token mismatch" errors:

```bash
deneb-dock-fix-token
```

This will:

1. Read the token from your `.env` file
2. Configure it in the Deneb config
3. Restart the gateway
4. Verify the configuration

### Permission Denied

**Ensure Docker is running and you have permission:**

```bash
docker ps
```

## Requirements

- Docker and Docker Compose installed
- Bash or Zsh shell
- Deneb project (from `docker-setup.sh`)

## Development

**Test with fresh config (mimics first-time install):**

```bash
unset DENEB_DOCK_DIR && rm -f ~/.deneb-dock/config && source scripts/shell-helpers/deneb-dock-helpers.sh
```

Then run any command to trigger auto-detect:

```bash
deneb-dock-start
```
