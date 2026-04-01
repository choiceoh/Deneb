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

## CLI

```bash
deneb message poll --channel telegram --target 123456789 \
  --poll-question "Ship it?" --poll-option "Yes" --poll-option "No"
deneb message poll --channel telegram --target -1001234567890:topic:42 \
  --poll-question "Pick a time" --poll-option "10am" --poll-option "2pm" \
  --poll-duration-seconds 300
```

Options:

- `--channel`: `telegram` (default)
- `--poll-multi`: allow selecting multiple options
- `--poll-duration-seconds`: 5-600 seconds
- `--poll-anonymous` / `--poll-public`: poll visibility

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

- Telegram: 2-10 options. Supports forum topics via `threadId` or `:topic:` targets. Uses `durationSeconds`, limited to 5-600 seconds. Supports anonymous and public polls.

## Agent tool (Message)

Use the `message` tool with `poll` action (`to`, `pollQuestion`, `pollOption`, optional `pollMulti`, `pollDurationHours`, `channel`).

For Telegram, the tool also accepts `pollDurationSeconds`, `pollAnonymous`, and `pollPublic`.

Use `action: "poll"` for poll creation. Poll fields passed with `action: "send"` are rejected.

Note: Telegram has no “pick exactly N” mode; `pollMulti` maps to multi-select.
