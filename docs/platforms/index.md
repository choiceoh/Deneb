---
summary: "Platform support overview (Gateway + companion apps)"
read_when:
  - Looking for OS support or install paths
  - Deciding where to run the Gateway
title: "Platforms"
---

# Platforms

Deneb runs on **Linux** (DGX Spark). **Node is the recommended runtime**.
Bun is not recommended for the Gateway (Telegram bugs).

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

The service target on Linux:

- systemd user service (`deneb-gateway[-<profile>].service`)
