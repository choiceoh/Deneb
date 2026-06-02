---
title: "Native Android Client"
summary: "Connect the native Android client and use its capture, widget, and instant-push features."
read_when:
  - You are connecting the native Android app to your gateway
  - You want native-only features like share capture, a home-screen widget, or instant push
  - You are setting the client token or the per-role model pickers
---

# Native Android Client

Deneb's user surface is the native Android client. The app owns daily chat,
share-sheet capture from any app, the home-screen widget, notification
ingestion, and instant proactive push, while the agent, tools, memory, and
always-on work stay on the gateway.

## What It Is

The client is built by vendoring the UI of [Kai](https://github.com/SimonSchubert/Kai)
(Apache-2.0) and replacing its brain with calls to the Deneb gateway. The phone
keeps Kai's rich chat UI and its interactive `kai-ui` renderer; every turn,
tool call, and memory write happens on the DGX Spark gateway over one
authenticated endpoint.

- **Chat is the home screen.** There is a single conversation view; replies
  stream in token by token, and a reply carrying a `kai-ui` fence renders as an
  interactive screen rather than plain text.
- **Text-first.** Kai's text-to-speech was removed — the client is for reading
  and capturing, not narration.
- **Android only.** The vendored project drops the iOS, desktop, and web KMP
  targets. The build and the daily target is a Samsung Galaxy S25; the UI is
  Korean-first throughout.

<Info>
  The native client is a standalone APK that authenticates with a client token.
  It talks to the gateway over the native `miniapp.*` API. A generated client
  token is the credential; the phone never receives provider keys or tool
  credentials.
</Info>

## Connecting to the Gateway

The client needs two values, entered on the **게이트웨이** (gateway) settings
tab: the gateway URL and a client token.

<Steps>
  <Step title="Generate a client token on the gateway host">
    Run this once on the machine the gateway runs on:

    ```bash
    go run ./gateway-go/cmd/deneb-client-token
    ```

    It prints a token and writes it to `client_token` in the gateway state
    directory (mode `0600`). Re-running rotates the token.
  </Step>
  <Step title="Make the gateway reachable from the phone">
    The gateway binds to loopback by default. Reach it from the phone over your
    private network — a Tailscale-style address such as
    `http://100.x.x.x:18789` is typical.
  </Step>
  <Step title="Paste both into the app">
    Open settings, the 게이트웨이 tab, and paste the gateway URL and the token,
    then save. Every request from then on carries the token in the
    `X-Deneb-Client-Token` header.
  </Step>
</Steps>

<Note>
  Client-token auth is **opt-in**. With no token file on the gateway the path is
  disabled entirely, so a stock gateway is not reachable by a standalone app
  until you generate one. The client token is the only credential the client API
  accepts: a request with a wrong token — or no token — is rejected with `401`.
</Note>

## Chat

The chat path is built for long agent turns, not just quick replies.

- **Token streaming.** Replies stream over Server-Sent Events; a typing cursor
  blinks while text arrives. There is no fixed client timeout — a turn that runs
  tools can take a while — and if nothing has streamed yet the client falls back
  to a blocking send, so a partial answer is never discarded.
- **Regenerate.** The last assistant message has a 재생성 (regenerate) control
  that pops the last exchange and re-asks.
- **Interactive `kai-ui`.** A reply containing a `kai-ui` fence renders as a
  full interactive screen; pressing a button sends a structured callback as the
  next turn.
- **Tool and fallback cues.** Running tools surface as activity chips, and a
  small badge appears when the gateway answered with a fallback-role model.

## Topics

The conversation drawer lists native topics first, followed by recent sessions.
Topics come from the gateway's `miniapp.topics.list` contract and carry the
session key the app should open.

- **Labels come from the gateway.** The entries and their names (in practice
  **업무 / 잡담 / 코딩**) are derived from the gateway's `deneb.json` topics map;
  the client renders them without knowing any legacy transport details.
- **업무 is the home topic.** The 업무 (work) topic maps to `client:main`, so
  proactive reports and existing home history carry over.
- Selecting a topic repoints the session, loads that topic's transcript, and
  uses the same per-topic knowledge injection as the gateway.

## Getting Around

A left navigation drawer opens from the top-bar hamburger or a left-edge swipe.
It is a flat, lowercase typographic menu — no icons, no group headers. The
destinations are:

| Label | Goes to |
|---|---|
| `mail` | The Gmail inbox, with analysis and inline follow-up |
| `calendar` | Upcoming calendar events |
| `search` | Unified search across wiki, diary, and people |
| `people` | People ranked by message volume |
| `categories` | The wiki category browser |
| `history` | The conversation browser (shown only when there are saved conversations) |
| `settings` | The settings hub (see below) |

Below the menu sits a small **capture** footer covered in
[Capture](/operations/capture).

## Capture

The native client is where capture lives — share an image, a recording, or text
into Deneb and the gateway triages it. The drawer's capture footer offers
`image ocr`, `transcribe`, and `voice`; the Android share sheet routes shared
content by type; a `deneb://voice` home-icon shortcut starts hands-free voice
capture; and a notification-capture tab can ingest other apps' notifications.
All of these — what each does, which sidecar runs, and the per-app allowlist —
are documented in [Capture](/operations/capture).

## Home-Screen Widget

A home-screen widget (labelled **Deneb — 다음 일정·미읽음**) shows two lines at a
glance: the next meeting (`M/D HH:mm · title`, from the upcoming calendar) and
the unread-mail count (`받은편지함 미읽음 N`). Tapping anywhere on it opens the
Deneb chat. It refreshes on the system's roughly 30-minute cycle. The widget is
composed client-side from the calendar and Gmail data the gateway already
serves.

## Settings

The 설정 hub has five tabs:

- **게이트웨이** — the gateway URL and client token, a live gateway status card
  (version, native API version, current model, capabilities, topics), a link to
  the surviving upstream Kai settings (providers, MCP, inference), and a version
  card with a **패치노트 보기** (view patch notes) sheet and an **업데이트 확인**
  (check for update) button that offers an in-place APK download when a newer
  build exists.
- **모델** — per-role model pickers. A **메인 / 경량 / 폴백** selector
  (main / lightweight / fallback) with the gateway's models listed below it;
  picking one binds that model to the selected role and takes effect without a
  restart. The roles map to work: main is chat, lightweight runs mail analysis
  and summaries, fallback is used when main fails.
- **크론** — the gateway's scheduled tasks, with a per-task detail screen
  (schedule, instruction, delivery target, state; enable, run now, delete).
- **토픽문서** — the per-topic injected documents, viewable and editable, with a
  새 문서 (new document) action.
- **알림** — the notification-capture tab and its per-app allowlist (see
  [Capture](/operations/capture)).

## Instant Proactive Push

The client holds a long-lived subscription to the gateway's push stream so a
proactive 업무 report (the morning letter, a mail analysis) raises a local
notification the moment it is produced, instead of waiting for a poll.
Notifications fire only while the app is backgrounded — in the foreground the
report simply lands in chat. **Tapping a push opens the 업무 (work) topic** where
the report was mirrored. The gateway side of this — which reports push, and why
named topics are excluded — is in
[Proactive Delivery](/operations/proactive-delivery#native-client-instant-push).
