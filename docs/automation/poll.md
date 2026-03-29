---
summary: "Poll sending via gateway + CLI"
read_when:
  - Adding or modifying poll support
  - Debugging poll sends from the CLI or gateway
title: "Polls"
---

# Polls

## Supported channels

- Telegram
- Discord

## CLI

```bash
# Telegram
deneb message poll --channel telegram --target 123456789 \
  --poll-question "Ship it?" --poll-option "Yes" --poll-option "No"
deneb message poll --channel telegram --target -1001234567890:topic:42 \
  --poll-question "Pick a time" --poll-option "10am" --poll-option "2pm" \
  --poll-duration-seconds 300

# Discord
deneb message poll --channel discord --target channel:123456789 \
  --poll-question "Snack?" --poll-option "Pizza" --poll-option "Sushi"
deneb message poll --channel discord --target channel:123456789 \
  --poll-question "Plan?" --poll-option "A" --poll-option "B" --poll-duration-hours 48
```

Options:

- `--channel`: `telegram` (default) or `discord`
- `--poll-multi`: allow selecting multiple options
- `--poll-duration-hours`: Discord-only (defaults to 24 when omitted)
- `--poll-duration-seconds`: Telegram-only (5-600 seconds)
- `--poll-anonymous` / `--poll-public`: Telegram-only poll visibility

## Gateway RPC

Method: `poll`

Params:

- `to` (string, required)
- `question` (string, required)
- `options` (string[], required)
- `maxSelections` (number, optional)
- `durationHours` (number, optional)
- `durationSeconds` (number, optional, Telegram-only)
- `isAnonymous` (boolean, optional, Telegram-only)
- `channel` (string, optional, default: `telegram`)
- `idempotencyKey` (string, required)

## Channel differences

- Telegram: 2-10 options. Supports forum topics via `threadId` or `:topic:` targets. Uses `durationSeconds` instead of `durationHours`, limited to 5-600 seconds. Supports anonymous and public polls.
- Discord: 2-10 options, `durationHours` clamped to 1-768 hours (default 24). `maxSelections > 1` enables multi-select; Discord does not support a strict selection count.

## Agent tool (Message)

Use the `message` tool with `poll` action (`to`, `pollQuestion`, `pollOption`, optional `pollMulti`, `pollDurationHours`, `channel`).

For Telegram, the tool also accepts `pollDurationSeconds`, `pollAnonymous`, and `pollPublic`.

Use `action: "poll"` for poll creation. Poll fields passed with `action: "send"` are rejected.

Note: Discord has no “pick exactly N” mode; `pollMulti` maps to multi-select.
