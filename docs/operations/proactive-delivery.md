---
title: "Proactive Delivery"
summary: "How Deneb sends mail summaries and scheduled output: active home, delivery targets, forum topics, and heartbeat versus cron."
read_when:
  - You want proactive messages to land in a specific chat or forum topic
  - You ran use-forum and want to know what it changed
  - You are setting the gmail poll delivery target or a cron job target
---

# Proactive Delivery

Deneb sends things you did not ask for in the moment — mail summaries,
scheduled cron output, overnight wiki synthesis. The design goal is simple to
state and easy to get wrong: the actual content has to arrive, in the right
chat. This page covers how delivery works and where it lands.

## How Delivery Works

The key design choice is that **the model is removed from the delivery path**.
Earlier, completed cron and dreaming runs were handed to the model with
"deliver this text" — and the model would call wiki and memory tools, report a
side effect like "wiki updated", and drop the actual content. To prevent that
structurally, a relay now sends the content verbatim and, separately, appends
it to the session transcript so a follow-up like "tell me more" still has the
context.

There are two delivery paths:

- **Proactive relay (verbatim).** Used by gmail poll summaries, wiki dreaming,
  and cron handoff. It resolves the target chat, sends the text as-is over
  Telegram, and records it in the transcript.
- **Direct cron delivery (fallback).** If a cron run cannot hand off to the
  main session, it delivers its output directly, splitting long output into
  chunks and attaching any media to the last one.

## Active Home

The central concept for "where do proactive messages go" is the **active
home** — the chat that owns the bot conversation.

- **Setting it.** Run `/use-forum` inside a Telegram supergroup. Deneb records
  that supergroup as the active home and persists it (separately from the
  deployment config), so it survives restarts. Running it in a one-to-one chat
  is rejected.
- **How it is used.** Proactive senders are wired to a `telegram:home`
  sentinel and resolve it to the active home's chat ID at send time. If no
  active home is set, delivery falls back to the static configured chat ID; if
  that is also missing, it is a no-op.

<Note>
  The no-op-when-unresolved behavior is deliberate: it stops proactive messages
  from being sent to a stale chat ID left in config. By default messages land
  in the supergroup's General topic, which Telegram does not allow you to
  delete — the most restructure-proof target.
</Note>

## Delivery Targets

**Gmail poll summaries.** Set `gmailPoll.deliverTo` in the config to override
the target:

- empty — use the active home (follows `/use-forum`). Recommended.
- `"<chat-id>"` — a specific chat.
- `"<chat-id>:thread:<n>"` — a specific forum topic.

The value is validated on startup; an unparseable target logs a warning and
falls back to the active home rather than dropping summaries.

**Forum topic routing.** When a message arrives in a forum topic, its thread ID
flows through the whole pipeline — into the session key, the delivery context,
and any cron job created there — and back out to Telegram's
`message_thread_id`. The practical result: **a cron job you create inside a
forum topic sends its output back to that same topic**, instead of leaking into
General.

<Warning>
  Non-General topic IDs can change if a topic is recreated, which makes a
  pinned `:thread:<n>` target go stale. If stability matters more than
  precision, leave `deliverTo` empty and let it ride the active home.
</Warning>

## Heartbeat and Cron

Two mechanisms drive proactive behavior, and they differ in important ways.

- **Heartbeat.** A fixed 30-minute pulse during active hours (roughly 08:00 to
  23:00). It reads `HEARTBEAT.md` and runs a full agent turn in the user's most
  recent active Telegram session, so it shares the context of recent promises.
  It is ephemeral (neither the trigger nor the reply is persisted), stays
  silent when there is nothing new, and skips if you are actively typing. Its
  schedule is fixed in code, not configured.
- **Cron.** User-defined schedules (at / every / cron expression). Each job
  carries its own delivery config, and its output goes through the verbatim
  relay (with a transcript record) so follow-up replies answer in the right
  context.

## Configuration

There are three levers, by scope:

1. **Global home** — run `/use-forum` in the target supergroup. Dreaming,
   heartbeat, and cron defaults all follow it.
2. **Gmail poll delivery** — `gmailPoll.deliverTo` (target) and
   `gmailPoll.intervalMin` (cadence) in `~/.deneb/deneb.json`:

   ```json5
   {
     gmailPoll: {
       intervalMin: 30,
       deliverTo: "",   // "" = active home; "<chat-id>:thread:<n>" for a topic
     },
   }
   ```

3. **Per-job cron target** — a cron job's `delivery` section (`channel`, `to`,
   `threadId`, `bestEffort`). A job created inside a forum topic captures that
   topic automatically.
