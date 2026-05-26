---
title: "Operations"
summary: "Runbooks for exposing Deneb to the outside world and keeping the gateway healthy."
read_when:
  - You are setting up Deneb for the first time and need to expose it externally
  - You are bringing up the Telegram Mini App and need a tunnel
  - You are debugging an outage in the public surface
---

# Operations

The Deneb gateway is designed to run on a single host you control — a DGX
Spark, a homelab box, a developer workstation — and bind to loopback by
default. These runbooks cover the small set of external surfaces you need to
stand up before users can reach the gateway from Telegram.

## Runbooks

<CardGroup cols={1}>
  <Card title="Cloudflare Tunnel Setup" icon="cloud" href="/operations/cloudflare-tunnel-setup">
    Expose the Mini App at a public HTTPS URL via Cloudflare Tunnel —
    without opening inbound ports on the gateway host.
  </Card>
</CardGroup>
