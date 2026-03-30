---
summary: "Health check steps for channel connectivity"
read_when:
  - Diagnosing channel health
title: "Health Checks"
---

# Health Checks (CLI)

Short guide to verify channel connectivity without guessing.

## Quick checks

- `deneb status` — local summary: gateway reachability/mode, update hint, linked channel auth age, sessions + recent activity.
- `deneb status --all` — full local diagnosis (read-only, color, safe to paste for debugging).
- `deneb status --deep` — also probes the running Gateway (per-channel probes when supported).
- `deneb health --json` — asks the running Gateway for a full health snapshot.
- Send `/status` as a standalone message in Telegram to get a status reply without invoking the agent.
- Logs: tail `/tmp/deneb/deneb-*.log`.

## Deep diagnostics

- Session store: `ls -l ~/.deneb/agents/<agentId>/sessions/sessions.json` (path can be overridden in config). Count and recent recipients are surfaced via `status`.

## Health monitor config

- `gateway.channelHealthCheckMinutes`: how often the gateway checks channel health. Default: `5`. Set `0` to disable health-monitor restarts globally.
- `gateway.channelStaleEventThresholdMinutes`: how long a connected channel can stay idle before the health monitor treats it as stale and restarts it. Default: `30`. Keep this greater than or equal to `gateway.channelHealthCheckMinutes`.
- `gateway.channelMaxRestartsPerHour`: rolling one-hour cap for health-monitor restarts per channel/account. Default: `10`.
- `channels.<provider>.healthMonitor.enabled`: disable health-monitor restarts for a specific channel while leaving global monitoring enabled.
- `channels.<provider>.accounts.<accountId>.healthMonitor.enabled`: multi-account override that wins over the channel-level setting.
- These per-channel overrides apply to the built-in channel monitors that expose them today: Telegram.

## When something fails

- Gateway unreachable → start it: `deneb gateway --port 18789` (use `--force` if the port is busy).
- No inbound messages → confirm the sender is allowed (`channels.telegram.allowFrom` or `channels.telegram.allowFrom`); for group chats, ensure allowlist + mention rules match (`channels.telegram.groups`, `channels.telegram.guilds`, `agents.list[].groupChat.mentionPatterns`).

## Dedicated "health" command

`deneb health --json` asks the running Gateway for its health snapshot (no direct channel sockets from the CLI). It reports linked creds/auth age when available, per-channel probe summaries, session-store summary, and a probe duration. It exits non-zero if the Gateway is unreachable or the probe fails/timeouts. Use `--timeout <ms>` to override the 10s default.
