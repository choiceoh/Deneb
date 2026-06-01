---
title: "Operations"
summary: "Guides for the Deneb native client, capture, mail analysis, and proactive delivery."
read_when:
  - You are setting up the Deneb native client
  - You want to learn a client surface, capture, mail analysis, or proactive delivery
  - You are debugging mail polling or delivery
---

# Operations

The Deneb gateway runs on a single host you control — a DGX Spark, a homelab
box, a developer workstation — and binds to loopback (or your Tailscale
interface) by default. These pages cover the native Android client, the capture
flows that feed it, and the day-to-day features built on top (mail analysis,
proactive delivery).

## Guides

<CardGroup cols={2}>
  <Card title="Native Android Client" icon="smartphone" href="/operations/native-client">
    The daily-driver app: connect with a client token, then capture, glance at a
    home-screen widget, and get instant proactive push.
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
</CardGroup>
