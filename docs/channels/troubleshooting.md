---
summary: "Fast channel level troubleshooting with per channel failure signatures and fixes"
read_when:
  - Channel transport says connected but replies fail
  - You need channel specific checks before deep provider docs
title: "Channel Troubleshooting"
---

# Channel troubleshooting

Use this page when a channel connects but behavior is wrong.

## Command ladder

Run these in order first:

```bash
deneb status
deneb gateway status
deneb logs --follow
deneb doctor
deneb channels status --probe
```

Healthy baseline:

- `Runtime: running`
- `RPC probe: ok`
- Channel probe shows connected/ready

## Telegram

### Telegram failure signatures

| Symptom                             | Fastest check                                   | Fix                                                                      |
| ----------------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------ |
| `/start` but no usable reply flow   | `deneb pairing list telegram`                   | Approve pairing or change DM policy.                                     |
| Bot online but group stays silent   | Verify mention requirement and bot privacy mode | Disable privacy mode for group visibility or mention bot.                |
| Send failures with network errors   | Inspect logs for Telegram API call failures     | Fix DNS/IPv6/proxy routing to `api.telegram.org`.                        |
| `setMyCommands` rejected at startup | Inspect logs for `BOT_COMMANDS_TOO_MUCH`        | Reduce plugin/skill/custom Telegram commands or disable native menus.    |
| Upgraded and allowlist blocks you   | `deneb security audit` and config allowlists    | Run `deneb doctor --fix` or replace `@username` with numeric sender IDs. |

Full troubleshooting: [Telegram](/channels/telegram).

