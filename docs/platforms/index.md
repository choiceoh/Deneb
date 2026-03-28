---
summary: "Platform support overview (Gateway + companion apps)"
read_when:
  - Looking for OS support or install paths
  - Deciding where to run the Gateway
title: "Platforms"
---

# Platforms

Deneb core is written in TypeScript. **Node is the recommended runtime**.
Bun is not recommended for the Gateway (WhatsApp/Telegram bugs).

The Gateway runs on macOS and Linux (including WSL2 on Windows).

## VPS & hosting

- VPS hub: [VPS hosting](/vps)
- Fly.io: [Fly.io](/install/fly)
- Hetzner (Docker): [Hetzner](/install/hetzner)
- GCP (Compute Engine): [GCP](/install/gcp)
- exe.dev (VM + HTTPS proxy): [exe.dev](/install/exe-dev)

## Common links

- Install guide: [Getting Started](/start/getting-started)
- Gateway runbook: [Gateway](/gateway)
- Gateway configuration: [Configuration](/gateway/configuration)
- Service status: `deneb gateway status`

## Gateway service install (CLI)

Use one of these (all supported):

- Wizard (recommended): `deneb onboard --install-daemon`
- Direct: `deneb gateway install`
- Configure flow: `deneb configure` → select **Gateway service**
- Repair/migrate: `deneb doctor` (offers to install or fix the service)

The service target depends on OS:

- macOS: LaunchAgent (`ai.deneb.gateway` or `ai.deneb.<profile>`; legacy `com.deneb.*`)
- Linux/WSL2: systemd user service (`deneb-gateway[-<profile>].service`)
