---
title: "Cloudflare Tunnel Setup"
summary: "Expose the Mini App at a public HTTPS URL via Cloudflare Tunnel without opening inbound ports on the gateway host."
read_when:
  - You are exposing the Mini App externally for the first time
  - The Telegram bot menu button is missing or the Mini App returns 401
  - You are rotating the public domain or recreating the tunnel
---

# Cloudflare Tunnel Setup

Deneb's HTTP server binds to loopback by default. To let Telegram open the
Mini App, the gateway needs a public HTTPS URL that fronts the same
`127.0.0.1:18789` socket. Cloudflare Tunnel (`cloudflared`) gives you that
URL without opening an inbound port on the gateway host — the daemon makes
an outbound connection to Cloudflare's edge and tunnels HTTPS traffic back
to your loopback service.

<Info>
  This runbook covers the production deployment path. For local development
  you do not need a tunnel — use `pnpm dev` in `frontend/` and the Vite
  proxy handles the API calls.
</Info>

## Why Cloudflare Tunnel

Two constraints push us here:

1. **The gateway binds loopback by default** for safety on a multi-user
   host. Inbound firewall rules and reverse proxies all require opening a
   port — `cloudflared` does not.
2. **Telegram Mini Apps require HTTPS** with a valid certificate. Self-signed
   or `http://` URLs are silently refused by the Telegram client.

`cloudflared` solves both: the public side is `https://<your-domain>/` on
Cloudflare's edge, the private side is `localhost:18789`, and the tunnel
takes care of TLS termination and cert renewal.

## Prerequisites

- A Cloudflare account with a domain on it (any plan — the free tier is
  enough).
- Subdomain you can dedicate to the Mini App (`miniapp.example.com` in the
  examples below; pick your own).
- A Telegram bot whose token is already in Deneb's config (`channels.telegram.botToken`).
- The Deneb gateway built with `make gateway-prod` and running on
  `127.0.0.1:18789`.

## Steps

<Steps>
  <Step title="Install cloudflared">
    On Ubuntu/Debian on the gateway host:

    ```bash
    curl -L --output cloudflared.deb \
      https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-arm64.deb
    sudo dpkg -i cloudflared.deb
    cloudflared --version
    ```

    For ARM hosts (DGX Spark is ARM) use `-arm64`; on x86_64 swap in `-amd64`.
  </Step>

  <Step title="Authenticate with Cloudflare">
    ```bash
    cloudflared tunnel login
    ```

    This opens a URL in your browser. Pick the zone (the domain you intend
    to use) and approve. A cert is written to `~/.cloudflared/cert.pem`.
  </Step>

  <Step title="Create the tunnel">
    ```bash
    cloudflared tunnel create deneb-miniapp
    ```

    Output ends with something like:

    ```
    Created tunnel deneb-miniapp with id 9c3b1234-...-aabbccddeeff
    ```

    The credentials file (`~/.cloudflared/<tunnel-id>.json`) holds the
    tunnel's secret — treat it like a private key.
  </Step>

  <Step title="Route a hostname to the tunnel">
    ```bash
    cloudflared tunnel route dns deneb-miniapp miniapp.example.com
    ```

    This creates a CNAME from `miniapp.example.com` to the tunnel's
    internal `<tunnel-id>.cfargotunnel.com` endpoint. Cloudflare manages the
    TLS cert automatically.
  </Step>

  <Step title="Write the ingress config">
    Create `~/.cloudflared/config.yml`:

    ```yaml
    tunnel: deneb-miniapp
    credentials-file: /home/YOUR_USER/.cloudflared/9c3b1234-...-aabbccddeeff.json

    ingress:
      # Mini App static + RPC: route only the paths the webview needs.
      - hostname: miniapp.example.com
        path: ^/(app/|api/v1/miniapp/)
        service: http://127.0.0.1:18789

      # Catch-all: refuse anything else so the gateway's other endpoints
      # (cron webhook, pprof, etc.) stay loopback-only.
      - service: http_status:404
    ```

    Replace `YOUR_USER`, the tunnel UUID, and `miniapp.example.com` with
    your values. The `path:` filter is the safety belt that keeps
    `/api/cron/run`, `/debug/pprof/`, and `/health` unreachable from the
    internet even though the tunnel exists.
  </Step>

  <Step title="Install and start the systemd service">
    ```bash
    sudo cloudflared service install
    sudo systemctl enable --now cloudflared
    sudo systemctl status cloudflared
    ```

    The service rereads the same `~/.cloudflared/config.yml` and restarts
    on reboot. Logs land in `journalctl -u cloudflared -f`.
  </Step>

  <Step title="Register the domain with the bot">
    Open Telegram, chat with [@BotFather](https://t.me/BotFather), and run:

    ```
    /setdomain
    ```

    Pick your bot, then paste `miniapp.example.com`. BotFather replies with
    "Success!". This step is mandatory — Telegram refuses to open Mini Apps
    on unregistered domains, even with a valid HTTPS cert.
  </Step>

  <Step title="Tell Deneb about the URL">
    Edit `~/.deneb/deneb.json` (or whichever config file you load via
    `--config`):

    ```json5
    {
      "channels": {
        "telegram": {
          "botToken": "…",
          "webAppURL": "https://miniapp.example.com/app/",
          "webAppMenuLabel": "Deneb"
        }
      }
    }
    ```

    Restart the gateway. On startup it calls `setMenuButton` against the
    Bot API; if the URL or domain is wrong the gateway logs a warning and
    keeps running normally (other channels are unaffected).
  </Step>

  <Step title="Verify end-to-end">
    1. Open your bot in Telegram on a phone or desktop client.
    2. The chat shows a menu button labeled **Deneb** next to the
       attachment paperclip.
    3. Tap it — the Mini App opens inline. Within a second you should see
       "Authenticated as &lt;your name&gt;" plus a backend latency in ms.

    If the Mini App fails with **401**, the most likely causes are
    `webAppURL` pointing at a different host than the bot's registered
    domain, or `botToken` mismatched between the config the gateway loaded
    and the one BotFather knows about.
  </Step>
</Steps>

## Troubleshooting

<AccordionGroup>
  <Accordion title="Tap menu button → blank page or `502 Bad Gateway`">
    The tunnel is up but the gateway isn't responding on `127.0.0.1:18789`.
    Check `ss -ltnp | grep 18789` on the host and `journalctl -u cloudflared -f` for the matching error. If the gateway is on a different
    port, update `service: http://127.0.0.1:<port>` in `config.yml` and
    restart `cloudflared`.
  </Accordion>

  <Accordion title="Mini App opens, says `401 — telegram: init data signature mismatch`">
    The `botToken` in `~/.deneb/deneb.json` does not match the bot that
    issued the launch. Common causes: a stale dev token left in config, or
    the token was rotated via BotFather. Re-copy the token from the bot's
    @BotFather page and restart the gateway.
  </Accordion>

  <Accordion title="`401 — telegram: init data expired`">
    The launch payload is older than 24 hours (the default TTL).
    Close and reopen the Mini App — the Telegram client mints a fresh
    `initData` on every launch.
  </Accordion>

  <Accordion title="Korean text renders as `???`">
    Cloudflare passes bytes through, so this almost always means the
    response is missing `charset=utf-8`. The Mini App's HTML serves with
    `text/html; charset=utf-8` by default; if you front it with another
    proxy in between, make sure the proxy preserves the header.
  </Accordion>

  <Accordion title="Menu button never appears">
    Three things to check in order:

    1. `webAppURL` is non-empty in `~/.deneb/deneb.json` — when blank the
       gateway intentionally skips the `setMenuButton` call.
    2. `/setdomain` step ran (and the reply was "Success") — without it
       Telegram silently refuses to render the button.
    3. The gateway started after the config was edited. Restart with
       `pkill -9 -f deneb-gateway; ./gateway-go/deneb-gateway --port 18789 &` and watch the log for `telegram WebApp menu button installed`.
  </Accordion>
</AccordionGroup>

## Security notes

- **No inbound port is opened.** `cloudflared` makes an outbound TCP
  connection to Cloudflare; the gateway socket stays loopback.
- **Path filtering is enforced at the tunnel.** The `path: ^/(app/|api/v1/miniapp/)` rule in `config.yml` means `/api/cron/run`,
  `/debug/pprof/`, `/health`, and every other gateway endpoint return 404
  via the public hostname even though the tunnel is wired up. The gateway
  itself still binds those routes on loopback for local scripts.
- **The tunnel credentials file is a long-lived secret.** Store it like an
  SSH key: file permissions `600`, never check it into git, and rotate it
  by running `cloudflared tunnel delete` followed by a fresh `create` if
  it leaks.
- **Cloudflare sees the plaintext.** Traffic is TLS-terminated at
  Cloudflare's edge and re-encrypted to the tunnel. If you need
  end-to-end encryption to the gateway you would need a different setup
  (mTLS reverse proxy, Tailscale Funnel, etc.) — out of scope here.
