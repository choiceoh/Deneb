---
title: "Proactive Delivery"
summary: "How Deneb sends mail summaries and scheduled output to the native client's work session: the verbatim relay, instant push, and heartbeat versus cron."
read_when:
  - You want to know where proactive messages (mail summaries, cron output) land
  - You are setting the gmail poll cadence or a cron job schedule
  - You want to understand heartbeat versus cron
---

# Proactive Delivery

Deneb sends things you did not ask for in the moment — mail summaries,
scheduled cron output, overnight wiki synthesis. The design goal is simple to
state and easy to get wrong: the actual content has to arrive, intact, in the
right place. With the [native client](/operations/native-client) as the sole
surface, that place is the 업무 (work) session. This page covers how delivery
works and where it lands.

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
  the morning letter, and the calendar briefing. It appends the text as-is to
  the 업무 (`client:main`) transcript and live-pushes it to any connected
  native client.
- **Cron handoff (fallback to direct).** A completed cron run hands its output
  off to the main session through the same relay. If no handoff is available,
  it falls back to recording the output directly with the run.

## Where It Lands: the 업무 Session

Proactive output is **always delivered to the native client's 업무 (work)
session** — the `client:main` transcript. There is no per-recipient routing to
configure: the relay lands everything in `client:main`, which is the session
the native client's 업무 (General) tab opens. Now that the native client is the
sole surface, this is the single, restructure-proof target.

## Native Client Instant Push

When a proactive 업무 report is produced, the gateway pushes it to any connected
native client the instant it is produced, instead of waiting for the client's
next poll.

- **The stream.** The native app holds open an authenticated SSE subscription at
  `GET /api/v1/miniapp/events`. Each report is published as a small
  `{title, body}` frame — `body` is a one-line preview — and the app raises a
  local notification for it (only while backgrounded; in the foreground the
  report just lands in chat).
- **업무 only.** Reports are mirrored into the `client:main` session, which is
  why **tapping a push opens the 업무 tab**.
- **Best effort.** The push hub drops a frame for a slow or sleeping client
  rather than blocking — there is no delivery guarantee, and none is needed: the
  same content is in the `client:main` transcript regardless.
- **Sources.** The morning letter (overnight wiki synthesis), mail analyses, and
  the calendar briefing are the reports wired through this push.

## Schedules and Targets

**Gmail poll summaries.** `gmailPoll.intervalMin` sets the cadence; summaries
are delivered to the 업무 session through the verbatim relay.

**Cron jobs.** Each cron job carries a `delivery` section (`channel`, `to`,
`bestEffort`), but proactive output routes to the native client's 업무 session
via the main-session handoff regardless of the per-job target. A job with no
explicit target defaults to the native work session (`channel: "client"`,
`to: "main"`).

## Heartbeat and Cron

Two mechanisms drive proactive behavior, and they differ in important ways.

- **Heartbeat.** A fixed 30-minute pulse during active hours (08:00 to 23:00
  Asia/Seoul). It reads `HEARTBEAT.md` and runs a full agent turn in the user's
  most recent active session, so it shares the context of recent promises. It is
  ephemeral (neither the trigger nor the reply is persisted) and stays silent
  when there is nothing new (it emits `NO_REPLY`). Its schedule is fixed in
  code, not configured, and its progress state lives in `HEARTBEAT.md` itself —
  updated through the `heartbeat_update` tool, not ordinary file writes.
- **Cron.** User-defined schedules (at / every / cron expression). Each job
  carries its own delivery config, and its output goes through the verbatim
  relay (with a transcript record) so follow-up replies answer in the right
  context.

## Configuration

There are two levers, by scope:

1. **Gmail poll cadence** — `gmailPoll.intervalMin` in `~/.deneb/deneb.json`:

   ```json5
   {
     gmailPoll: {
       intervalMin: 30,
     },
   }
   ```

2. **Per-job cron target** — a cron job's `delivery` section (`channel`, `to`,
   `bestEffort`). Targetless jobs default to the native 업무 session.
