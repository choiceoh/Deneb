---
title: "Mail Analysis"
summary: "How Deneb analyzes Gmail: the two-stage pipeline, automatic polling, persistence, and the analyze tool."
read_when:
  - You want to understand how Deneb analyzes and summarizes mail
  - You are tuning the Gmail poll interval, query, or model
  - You are debugging why a message was or was not analyzed
---

# Mail Analysis

Deneb turns a Gmail message into a structured read — summary, stakeholders,
importance, risks and deadlines, next steps. One pipeline serves two entry
points: an automatic background poller, and on-demand analysis from the Mini
App or the agent's `gmail` tool. Both share the same analysis engine, so the
output is consistent wherever it is triggered.

## How Analysis Works

Analysis runs in two stages. When a local model is available it uses both;
otherwise it falls back to a single synthesis call.

- **Stage 1 — context (parallel, best-effort).** Two extractors run side by
  side with a short timeout. The first builds thread context: Gmail has no
  `thread:` search operator, so Deneb gathers related messages by subject and
  by sender over a recent window, then has a local model distil a thread
  summary, prior exchanges, ongoing topics, and the sender relationship. The
  second pulls sender facts from the wiki knowledge graph. Either can fail
  without blocking the analysis.
- **Stage 2 — synthesis (main model).** The message body, the thread context,
  and the memory context are combined and the main model writes the analysis.

The analysis covers:

- A short summary of what the sender is asking or telling you
- Stakeholders — people, roles, and decision-makers
- Importance, high / normal / low, with the reasoning
- Risks and deadlines — payment dates, due dates, amounts, open issues, flagged with a warning marker
- Next steps — one to three concrete actions with owners

## Automatic Polling

A background poller analyzes important mail as it arrives, so it is ready
before you ask.

- **When it runs.** Weekdays only, roughly 09:00 to 19:00 in the operator's
  timezone. Outside those hours and on weekends it skips the cycle entirely.
- **What it fetches.** By default `is:unread newer_than:1h`, up to 5 messages
  per cycle, every 30 minutes. All three are configurable (see below).
- **Single vs batch.** A single new message gets an individual analysis. When
  several arrive together, the poller analyzes each and then synthesizes one
  prioritized report, grouped the way that day's mail calls for — by project,
  timeline, stakeholder, or urgency — leading with whatever matters most rather
  than a fixed template.
- **Reasoning stays internal.** Polling keeps the model's extended thinking on
  because it sharpens the analysis, but the delivered summary never shows it.
  Reasoning is dropped on the normal path, and a guard strips any
  chain-of-thought markers that leak into the text before it is sent — so a poll
  summary is the conclusion, never the model thinking out loud.

<Note>
  **Polling analyzes, it does not mark mail read.** The poller tracks which
  messages it has already seen with its own list and never touches Gmail's
  UNREAD label, so a polled-and-analyzed message still shows as unread in your
  inbox. Marking read, archiving, and trashing happen only through the native
  client. In the native client's mail list, unread messages carry a single
  circle marker.
</Note>

Where the analysis is delivered (the native client's 업무 session) is covered in
[Proactive Delivery](/operations/proactive-delivery).

## Storage, Citations, and Cleanup

**Persistence.** Each analyzed message is stored in two places, keyed by
message ID: a disk cache under the Deneb data directory, and a wiki page at
`mail-analyses/<message-id>.md` (category `mail-analysis`, type `log`). Because
of this, a polled message opens in the native client already analyzed, with no tap
required. The cache carries a prompt version, so when the analysis prompt
changes the old entries miss and are re-analyzed.

**Related project citations.** Analysis can link a message to the projects it
concerns. Deneb feeds the titles of pages in the wiki `프로젝트` (projects)
category to the model as candidates, the model answers with a trailing
`RELATED_PROJECTS:` line, and Deneb keeps only the paths that actually exist in
the candidate list — hallucinated or stale paths are dropped. Confirmed paths
land in the wiki page's `Related` frontmatter.

**Body cleanup.** Before analysis, the message body is stripped of chrome at
read time on every path:

- Marketing preamble at the top ("view in browser", ad banners) and footers at
  the bottom (unsubscribe, copyright, business registration lines).
- Reply quotes — "On ... wrote:", Korean "작성:" markers, "Original Message"
  separators — are cut so the analysis sees only the new content.

Two safety gates prevent over-stripping: very short bodies are left untouched
(to protect one-line replies and OTP codes), and if chrome stripping would
remove more than three quarters of the body it rolls back to the original.

## The analyze Tool

The agent can analyze mail directly with the `gmail` tool:

```text
gmail(action="analyze", query="is:unread newer_than:1h", max=5)
gmail(action="analyze", message_id="<id>")
```

- `message_id` analyzes one specific message.
- `query` is used when `message_id` is absent; it defaults to
  `is:unread newer_than:1h`.
- `max` caps how many search results are analyzed (default 5, clamped to
  1–50).

The tool folds attachment text into the body before analyzing and returns the
per-message analyses as Markdown.

<Warning>
  The `gmail` tool path does not wire up the project candidate list, so
  analyses run through the tool do **not** include related-project citations.
  The native client and the background poller do. If citations matter, prefer those
  paths.
</Warning>

## Configuration

Mail polling is configured under `gmailPoll` in the Deneb config
(`~/.deneb/deneb.json`):

```json5
{
  gmailPoll: {
    enabled: true,
    intervalMin: 30,                    // poll cadence in minutes
    query: "is:unread newer_than:1h",   // Gmail search for each cycle
    maxPerCycle: 5,                      // messages analyzed per cycle
    model: "",                          // overrides the stage-2 model
    promptFile: "",                     // custom analysis prompt path
    deliverTo: "",                      // delivery target — see Proactive Delivery
  },
}
```

`deliverTo` controls where poll summaries are sent and is documented in
[Proactive Delivery](/operations/proactive-delivery).
