---
summary: "Deneb capabilities across channels, routing, media, and UX."
read_when:
  - You want a full list of what Deneb supports
title: "Features"
---

# Features

## Highlights

<Columns>
  <Card title="Channels" icon="message-square">
    Telegram with a single Gateway.
  </Card>
  <Card title="Plugins" icon="plug">
    Add more channels with extensions.
  </Card>
  <Card title="Routing" icon="route">
    Multi-agent routing with isolated sessions.
  </Card>
  <Card title="Media" icon="image">
    Images, audio, and documents in and out.
  </Card>
  <Card title="Apps and UI" icon="monitor">
    Web Control UI.
  </Card>
  <Card title="Mobile nodes" icon="smartphone">
    Android nodes with pairing, voice/chat, and rich device commands.
  </Card>
</Columns>

## Full list

- Telegram bot support (custom Go implementation)
- Agent runtime with tool streaming
- Streaming and chunking for long responses
- Multi-agent routing for isolated sessions per workspace or sender
- Subscription auth for Anthropic and OpenAI via OAuth
- Sessions: direct chats collapse into shared `main`; groups are isolated
- Group chat support with mention based activation
- Media support for images, audio, and documents
- Optional voice note transcription hook
- Web Control UI
- Android node with pairing, Connect tab, chat sessions, voice tab, Canvas/camera, plus device, notifications, contacts/calendar, motion, photos, and SMS commands

<Note>
Legacy Claude, Codex, Gemini, and Opencode paths have been removed. Pi is the only
coding agent path.
</Note>
