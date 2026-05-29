---
title: "Mini App Guide"
summary: "What each screen of the Telegram Mini App does, from the home menu to mail analysis and inline Q and A."
read_when:
  - You are learning what the Mini App can do screen by screen
  - You opened the Mini App and want to know how mail analysis or inline Q and A works
  - You are onboarding someone to the Telegram Mini App surface
---

# Mini App Guide

The Mini App is a Telegram Web App that opens from the bot menu button and
runs full screen inside Telegram. It is a single-page app bundled into the
gateway binary and served at `/app/`, so deploying it is just the usual
`git pull` and restart. Every screen talks to the gateway over one
authenticated endpoint and is Korean-first throughout.

<Info>
  The Mini App is a separate frontend, not something the agent renders. The
  agent and the Mini App share the same data (wiki, mail analysis, sessions)
  but are different surfaces. To make the bot menu button appear and serve the
  app over HTTPS, see [Cloudflare Tunnel Setup](/operations/cloudflare-tunnel-setup).
</Info>

## What the Mini App Is

The app is built as a vanilla TypeScript and Vite single-page app — no UI
framework — and the gateway embeds the compiled bundle directly. Its design
idiom is deliberate and narrow:

- **Black and white typography.** Dark mode is true AMOLED black, the
  typeface is Pretendard, and screens favor lowercase typographic menus over
  icons and chrome.
- **No chat surface.** There is intentionally no conversation screen in the
  app — Telegram itself is the chat surface. The Mini App is for the
  structured views (mail, wiki, calendar) that a chat thread renders poorly.
- **Home is the single index.** Earlier layouts had a panorama tab strip and
  a separate "more" tab; both are gone. Everything is one drill-down from the
  home menu, and Telegram's back button always returns to the parent screen.

## Home and Getting Around

Home is a single vertical menu of lowercase labels with no icons or counts.
The destinations, in order, are:

<CardGroup cols={2}>
  <Card title="calendar" icon="calendar">Upcoming events for the next 7 days.</Card>
  <Card title="mail" icon="mail">Recent mail, with analysis and inline Q and A.</Card>
  <Card title="search" icon="search">One box across wiki, diary, and people.</Card>
  <Card title="topics" icon="messages-square">Recent conversation topics, and a new-topic chip.</Card>
  <Card title="categories" icon="folder">Browse wiki pages by category.</Card>
  <Card title="crons" icon="clock">Scheduled automatic tasks (read only).</Card>
  <Card title="settings" icon="settings">The active model picker.</Card>
</CardGroup>

Home shows nothing else — the "most recent important mail" card and the
pull-to-refresh that used to live here were removed once mail became one tap
away. Each drill-down screen has its own refresh, so there is no global
refresh button.

<Tip>
  The label reads **topics**, but the underlying route, files, and RPC are
  still named `sessions`. This only matters if you are reading the code or
  logs — to the user it is consistently "topics".
</Tip>

## Mail

Mail is the deepest surface in the app. The list and the detail screen are
where most of the analysis work shows up.

### The list

The mail list shows one row per message. Unread messages are marked with a
single circle marker (no numeric badge). Tapping a row opens the detail
screen; the app prefetches the detail and sender context on press so it paints
instantly. A long press enters multi-select mode, which raises a fixed action
bar at the bottom for bulk read, archive, and trash. When more mail is
available, a "more" button pages the next batch.

### Mail detail

Opening a message marks it read automatically. The detail screen stacks, top
to bottom:

1. **The message** — body and metadata.
2. **Sender context** (`보낸이 컨텍스트`) — recent messages from this sender
   over the last window.
3. **Related projects** (`관련 프로젝트`) — chips linking the message to wiki
   project pages.
4. **The analysis card.**
5. **Inline follow-up Q and A.**

Mail actions are optimistic. Marking read disables the button immediately and
syncs in the background; archive and trash move the message out of the inbox
right away and roll back with a toast if the sync fails. Trash asks for
confirmation first.

### The analysis card

The analysis card (`analysis`) is the agent's read of the message — summary,
stakeholders, importance, risks and deadlines, and next steps, rendered as
formatted text. There are two ways it appears:

- **Pre-computed.** A background poller analyzes important mail as it arrives
  and stores the result. When that cached analysis exists, the card hydrates
  on open with a `저장된 분석` (saved analysis) label and a relative
  timestamp — no tap required.
- **On demand.** If there is no cached analysis, an `analyze` button runs it
  live. Analysis can take up to a few minutes; the result is cached and also
  written back to the wiki so future context builds on it.

Each card has a per-card rerun control to force a fresh analysis.

### Inline follow-up Q and A

Below the analysis card is a small composer (`이 메일에 대해 질문` — "ask about
this mail") for follow-up questions grounded in that single message. The
placeholder suggests the shape: `예: 가장 급한 건 뭐야? 답장 초안 잡아줘`
("e.g. what is most urgent? draft a reply").

The Q and A is **ephemeral and context-grounded**:

- It answers only from this message's context — the body, its analysis, and
  related projects — and is instructed to say it does not know rather than
  guess beyond that context.
- It is **not** persisted to the main conversation. Nothing you ask here shows
  up in your Telegram thread or session history.
- It is stateless multi-turn: the running exchange is kept locally and resent
  each time, so you can ask follow-ups within the card, but it resets when you
  leave.

For how the analysis pipeline and the background poller actually work — and
how to configure proactive mail summaries — see
[Mail Analysis](/operations/mail-analysis) and
[Proactive Delivery](/operations/proactive-delivery).

## Search, Wiki, and People

**Search** is a single box that fans out across three indexes at once — wiki,
diary, and people — and stacks the results in that order. There is no separate
per-domain browse screen here; an empty query simply shows an empty screen. A
chip row offers a shortcut to create a new wiki page.

**Wiki pages** open from search hits or from category browsing. A page shows
its metadata (title, summary, tags) and rendered body, plus chips to related
pages. Pages are editable in place: a view/edit toggle lets you change the
frontmatter and body and save in one write. The category is locked while
editing because it is encoded in the page's path. Creating a new page requires
a title and a category; the file path is computed from those on the server.

<Note>
  Pressing back from a wiki page returns you to the screen you actually came
  from, not always to search. Earlier builds hard-coded a back link to search,
  which was wrong for every entry point except search itself.
</Note>

**People** profiles open from a person card in search results and show that
person's recent mail and a wiki graph summary. **Categories** browse wiki
pages grouped by category, drilling from the category list into the pages
inside each one.

## Calendar, Topics, Crons, and Settings

- **Calendar** lists upcoming events for the next 7 days, drilling into an
  event detail screen.
- **Topics** lists recent conversation topics and offers a `+ 새 토픽`
  (new topic) chip to start a fresh one. Tapping a topic opens its transcript.
- **Crons** lists scheduled automatic tasks. This view is read only — tasks
  are created and edited with operator tools, not from the app.
- **Settings** is pared down to the one preference a single operator changes
  mid-use: the active model. It shows the current model and drills into a model
  picker with per-model health dots and a form to add a custom endpoint. Model
  changes apply from the next conversation, not retroactively.

## Platform Behavior

The Mini App adapts to where Telegram opens it.

<Tabs>
  <Tab title="Mobile">
    The app requests fullscreen on launch and absorbs the device safe-area
    insets into its layout, so content clears the notch and status bar. It also
    disables Telegram's vertical swipe so pull gestures inside the app do not
    fight the swipe-to-minimize.
  </Tab>
  <Tab title="Desktop">
    Telegram opens Mini Apps in a medium windowed panel, so the app
    deliberately stays windowed rather than blowing the panel up to fullscreen.
    Keyboard navigation is enabled: j/k and the arrow keys move between rows,
    Esc returns home, and `/` jumps to search.
  </Tab>
</Tabs>

### Authentication and updates

Every request is authenticated with Telegram's `initData`, verified by the
gateway with HMAC-SHA256 and a time-to-live, so the app only works when opened
through Telegram. The frontend bundle is embedded in the gateway and served
pre-compressed from `/app/`. After a redeploy, if the app loads a stale code
chunk it recovers by reloading itself once (guarded against reload loops), so
users do not get stuck on a blank screen following an update.
