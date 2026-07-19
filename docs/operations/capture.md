---
title: "Capture"
summary: "Share an image, recording, or text; the gateway OCRs, transcribes, or triages it."
read_when:
  - You want to share an image, recording, or text into Deneb for triage
  - You are operating or debugging the OCR and transcription sidecars
---

# Capture

Capture is the native client's reason to exist: throw anything on the phone at
Deneb — a screenshot, a meeting recording, a pasted KakaoTalk thread, a voice
note — and the gateway turns it into text and runs one agent turn over it, so
the reply is already a triage (summary, action items, what matters) rather than
a raw dump. Image OCR and audio transcription run on local GPU sidecars; text
and voice are turned into text on the phone and sent as a normal message.

## Entry Points

The same handful of capture flows reach Deneb through several doors:

| Door | Image | Audio | Text |
|---|---|---|---|
| Drawer **capture** footer | `image ocr` | `transcribe` | — |
| Android **share sheet** (from any app) | `image/*` | `audio/*` | `text/plain` |
| `deneb://voice` home-icon shortcut | — | voice (on-device) | — |

When two or more topics are configured, a cold share first asks
어느 토픽으로 보낼까요? (which topic?) before routing.

## Image OCR

Share a photo, screenshot, or receipt to Deneb — or pick one from the drawer's
`image ocr` — and the gateway extracts the text, prefixes it with
`📷 공유 이미지에서 추출한 텍스트 (OCR):`, and runs one agent turn so the agent
can read and act on it. The chat shows `📷 이미지 공유됨 (OCR 분석 중…)` while it
works. Gateway RPC: `miniapp.capture.image`.

- **Engine.** A local **PaddleOCR-VL** vision-language model served over a
  vLLM OpenAI-compatible endpoint, default `http://127.0.0.1:18011`. It is far
  stronger than plain OCR on Korean business documents — tables, formulas,
  stamps, mixed figures.
- **Fallback.** If the sidecar is down, the gateway falls back to **tesseract**
  (`kor+eng`) so capture degrades in quality but never breaks.
- **Override.** `DENEB_OCR_VL_URL` points at a non-default endpoint.

<Note>
  This is the same OCR path Gmail uses for image attachments and scanned PDF
  pages — one engine, one fallback, wherever an image needs reading.
</Note>

## Audio Transcription

Share a voice memo or meeting recording — or pick one from `transcribe` — and
the gateway returns a **speaker-diarized, timestamped** transcript, prefixes it
with `🎙️ 공유 녹음에서 받아쓴 내용 (화자분리·타임스탬프):`, and runs one agent
turn (summary, action items, capture to the wiki). The chat shows
`🎙️ 녹음 공유됨 (전사 중…)` while it works. Gateway RPC: `miniapp.capture.audio`.

- **Engine.** The resident **MOSS-Transcribe-Diarize** sidecar (0.9B; the
  2026-07-18 cutover from VibeVoice-ASR — same `/v1/transcribe` contract),
  default `http://127.0.0.1:18014`, prod `DENEB_ASR_URL=http://100.105.145.6:18014`.
  It handles up to 90 minutes of audio across 50+ languages including Korean,
  decoding common containers (opus/`.oga`, m4a, mp3, wav) server-side.
- **Output.** One line per segment, `[mm:ss 화자N] …` (and `[h:mm:ss …]` past an
  hour). If the model returns no segments it falls back to a flat transcript.
- **Proper-noun bias.** Korean common speech transcribes cleanly, but proper
  nouns (companies, people, deals) get mis-heard. Deneb biases the model with
  **hotwords** drawn from your wiki — the titles and tags of your pages, named
  entities first, capped around 200 terms — so names like 탑솔라 stop coming back
  as 팝솔라. The operator can add more with `DENEB_ASR_HOTWORDS` (a comma or
  space list), merged ahead of the wiki terms.
- **No fallback.** Unlike OCR there is no local ASR fallback; if the sidecar is
  unreachable the capture surfaces a clear error rather than degrading silently.
- **Override.** `DENEB_ASR_URL` points at a non-default endpoint.

<Tip>
  Voice capture and audio-share are different paths. **Voice** (below) is
  on-device speech recognition for short hands-free input. **Audio-share** is
  this path — long recordings that need diarization and proper-noun correction,
  routed to the ASR sidecar.
</Tip>

## Text Share

Sharing text from any app — a KakaoTalk message, a link, an article excerpt —
drops it straight into chat as a normal turn (no OCR or sidecar). It arrives
prefixed `📥 공유: …` and the agent triages it like any other message. There is
no dedicated capture RPC for text; it rides the ordinary `miniapp.chat.send`
path.

## Plaud meeting recordings

Autonomous ingest (`gateway-go/internal/runtime/meeting/plaud_recordings.go`) polls
the Plaud MCP tools, synthesizes a Korean meeting report, writes a wiki 회의록
page, and posts a `meeting_report` work-feed card.

Operator assets under the workspace `topics/` directory:

| File | Role |
|---|---|
| `plaud-glossary.md` | Canonical terms + 원문→교정 pairs (sliced per meeting) |
| `plaud-correction.md` | ASR correction instructions |
| `plaud-do-not-correct.md` | Forbidden false corrections |
| `plaud-promote-pending.json` | Sighting counters before auto-promote (≥2 recordings) <!-- docref:ignore --> |

ASR hotwords also merge glossary `From`/`To` terms (with wiki +
contacts). Disable the worker with `DENEB_PLAUD_RECORDINGS_DISABLE=1`. Token
expiry posts a throttled feed card; poison recordings (3 failed analyses) are
quarantined the same way.

Templates for a fresh workspace live in
`docs/reference/templates/topics/plaud-*.md`.

## Voice Input

The drawer's `voice`, and the `deneb://voice` home-icon shortcut, fire the
phone's **on-device** speech recognizer (Korean, `ko-KR`, prompt
`Deneb에게 말하세요`). It needs no microphone permission, and the recognized text
is sent to chat prefixed with `🎤`. This is purely on-device — the gateway only
ever sees the resulting text — which makes it the right tool for quick spoken
commands, while a long recording belongs in [audio transcription](#audio-transcription).

## Sidecars at a Glance

Two GPU sidecars back the capture flows that need real conversion. Both expose
an OpenAI-style local endpoint, both have an environment override, and they
differ on whether a fallback exists.

| Sidecar | Used by | Default endpoint | Override | Fallback |
|---|---|---|---|---|
| PaddleOCR-VL | Image OCR (and Gmail attachments) | `http://127.0.0.1:18011` | `DENEB_OCR_VL_URL` | tesseract (`kor+eng`) |
| MOSS-Transcribe-Diarize | Audio transcription | `http://127.0.0.1:18014` | `DENEB_ASR_URL` | none (clear error) |

Both capture RPCs are conditional on their sidecar being wired, run on the
`client:main` session, and reach the gateway over the same authenticated native
endpoint as the rest of the app.
