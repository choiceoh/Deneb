---
summary: "Behavior and config for group message handling (mentionPatterns are shared across surfaces)"
read_when:
  - Changing group message rules or mentions
title: "Group Messages"
---

# Group messages

Goal: let Deneb sit in Telegram/Discord groups, wake up only when pinged, and keep that thread separate from the personal DM session.

Note: `agents.list[].groupChat.mentionPatterns` is used by both Telegram and Discord; this doc focuses on group-specific behavior. For multi-agent setups, set `agents.list[].groupChat.mentionPatterns` per agent (or use `messages.groupChat.mentionPatterns` as a global fallback).

## Current implementation

- Activation modes: `mention` (default) or `always`. `mention` requires a ping (native @-mentions, safe regex patterns, or the bot username anywhere in the text). `always` wakes the agent on every message but it should reply only when it can add meaningful value; otherwise it returns the silent token `NO_REPLY`. Defaults can be set in config (`channels.telegram.groups` or `channels.discord.guilds`) and overridden per group.
- Group policy: `channels.telegram.groupPolicy` (or `channels.discord.groupPolicy`) controls whether group messages are accepted (`open|disabled|allowlist`). `allowlist` uses `channels.telegram.groupAllowFrom` (fallback: explicit `channels.telegram.allowFrom`). Default is `allowlist` (blocked until you add senders).
- Per-group sessions: session keys look like `agent:<agentId>:telegram:group:<chatId>` so commands such as `/verbose on` or `/think high` (sent as standalone messages) are scoped to that group; personal DM state is untouched. Heartbeats are skipped for group threads.
- Context injection: **pending-only** group messages (default 50) that _did not_ trigger a run are prefixed under `[Chat messages since your last reply - for context]`, with the triggering line under `[Current message - respond to this]`. Messages already in the session are not re-injected.
- Sender surfacing: every group batch now ends with `[from: Sender Name]` so the agent knows who is speaking.
- Group system prompt: on the first turn of a group session we inject a short blurb into the system prompt like `You are replying inside the group "<subject>". Activation: trigger-only ... Address the specific sender noted in the message context.` If metadata is not available we still tell the agent it is a group chat.

## Config example (Telegram)

Add a `groupChat` block to `~/.deneb/deneb.json` so display-name pings work:

```json5
{
  channels: {
    telegram: {
      groups: {
        "*": { requireMention: true },
      },
    },
  },
  agents: {
    list: [
      {
        id: "main",
        groupChat: {
          historyLimit: 50,
          mentionPatterns: ["@?deneb", "deneb"],
        },
      },
    ],
  },
}
```

Notes:

- The regexes are case-insensitive and use the same safe-regex guardrails as other config regex surfaces; invalid patterns and unsafe nested repetition are ignored.
- Telegram sends canonical mentions via bot usernames, so the pattern fallback is rarely needed but is a useful safety net.

## How to use

1. Add your Telegram bot to the group (or invite your Discord bot to the guild/channel).
2. Say `@deneb ...` (or include the bot username). Only allowlisted senders can trigger it unless you set `groupPolicy: "open"`.
3. The agent prompt will include recent group context plus the trailing `[from: ...]` marker so it can address the right person.
4. Session-level directives (`/verbose on`, `/think high`, `/new` or `/reset`, `/compact`) apply only to that group's session; send them as standalone messages so they register. Your personal DM session remains independent.

## Testing / verification

- Manual smoke:
  - Send an `@deneb` ping in the group and confirm a reply that references the sender name.
  - Send a second ping and verify the history block is included then cleared on the next turn.
- Check gateway logs (run with `--verbose`) to see inbound message entries showing the group id and the `[from: ...]` suffix.

## Known considerations

- Heartbeats are intentionally skipped for groups to avoid noisy broadcasts.
- Echo suppression uses the combined batch string; if you send identical text twice without mentions, only the first will get a response.
- Session store entries will appear as `agent:<agentId>:<channel>:group:<id>` in the session store (`~/.deneb/agents/<agentId>/sessions/sessions.json` by default); a missing entry just means the group has not triggered a run yet.
- Typing indicators in groups follow `agents.defaults.typingMode` (default: `message` when unmentioned).
