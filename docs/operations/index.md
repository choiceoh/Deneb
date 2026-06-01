---
title: "Operations"
summary: "Guides for the Deneb clients, capture, mail analysis, proactive delivery, and exposing the gateway externally."
read_when:
  - You are setting up a Deneb client or exposing the gateway externally
  - You want to learn a client surface, capture, mail analysis, or proactive delivery
  - You are debugging an outage in the public surface
---

# Operations

The Deneb gateway runs on a single host you control — a DGX Spark, a homelab
box, a developer workstation — and binds to loopback by default. These pages
cover the two phone surfaces (the native Android client and the Telegram Mini
App), the capture flows that feed them, the day-to-day features built on top
(mail analysis, proactive delivery), and the external surface you stand up so
Telegram can reach the gateway.

## Guides

<CardGroup cols={2}>
  <Card title="Native Android Client" icon="smartphone" href="/operations/native-client">
    The daily-driver app: connect with a client token, then capture, glance at a
    home-screen widget, and get instant proactive push.
  </Card>
  <Card title="Mini App Guide" icon="layout-dashboard" href="/operations/mini-app-guide">
    A screen-by-screen tour of the Telegram Mini App: home, mail, search, wiki,
    and platform behavior.
  </Card>
  <Card title="Capture" icon="camera" href="/operations/capture">
    Share an image, recording, text, or notification into Deneb — OCR,
    transcription, and triage through one agent turn.
  </Card>
  <Card title="Mail Analysis" icon="mail" href="/operations/mail-analysis">
    The two-stage analysis pipeline, automatic polling, persistence, and the
    analyze tool.
  </Card>
  <Card title="Proactive Delivery" icon="send" href="/operations/proactive-delivery">
    Active home, delivery targets, forum topic routing, native-client push, and
    heartbeat versus cron.
  </Card>
  <Card title="Cloudflare Tunnel Setup" icon="cloud" href="/operations/cloudflare-tunnel-setup">
    Expose the Mini App at a public HTTPS URL without opening inbound ports on
    the gateway host.
  </Card>
</CardGroup>
