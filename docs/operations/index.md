---
title: "Operations"
summary: "Guides for the Mini App, mail analysis, and proactive delivery, plus exposing the gateway to the outside world."
read_when:
  - You are setting up Deneb and need to expose it externally
  - You want to learn the Mini App or how mail analysis and proactive delivery work
  - You are debugging an outage in the public surface
---

# Operations

The Deneb gateway runs on a single host you control — a DGX Spark, a homelab
box, a developer workstation — and binds to loopback by default. These pages
cover the Mini App and the day-to-day features built on top of it (mail
analysis, proactive delivery), plus the external surfaces you stand up so
Telegram can reach the gateway.

## Guides

<CardGroup cols={2}>
  <Card title="Mini App Guide" icon="layout-dashboard" href="/operations/mini-app-guide">
    A screen-by-screen tour of the Telegram Mini App: home, mail, search, wiki,
    and platform behavior.
  </Card>
  <Card title="Mail Analysis" icon="mail" href="/operations/mail-analysis">
    The two-stage analysis pipeline, automatic polling, persistence, and the
    analyze tool.
  </Card>
  <Card title="Proactive Delivery" icon="send" href="/operations/proactive-delivery">
    Active home, delivery targets, forum topic routing, and heartbeat versus
    cron.
  </Card>
  <Card title="Cloudflare Tunnel Setup" icon="cloud" href="/operations/cloudflare-tunnel-setup">
    Expose the Mini App at a public HTTPS URL without opening inbound ports on
    the gateway host.
  </Card>
</CardGroup>
