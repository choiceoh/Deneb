---
title: "Cloudflare Tunnel Setup"
summary: "Expose the Mini App at a public HTTPS URL via Cloudflare Tunnel without opening inbound ports on the gateway host."
read_when:
  - You are exposing the Mini App externally for the first time
  - The Telegram bot menu button is missing or the Mini App returns 401
  - You are rotating the public domain or recreating the tunnel
  - You already have a Cloudflare Tunnel for another service and want to add the Mini App to it
---

# Cloudflare Tunnel Setup

Deneb's HTTP server binds to the loopback or Tailscale interface by
default. To let Telegram open the Mini App, the gateway needs a public
HTTPS URL that fronts the same `<bind-ip>:18789` socket. Cloudflare Tunnel
(`cloudflared`) gives you that URL without opening an inbound port on the
gateway host — the daemon makes an outbound connection to Cloudflare's
edge and tunnels HTTPS traffic back to your local service.

<Info>
  This runbook covers the production deployment path. For local development
  you do not need a tunnel — use `pnpm dev` in `frontend/` and the Vite
  proxy handles the API calls.
</Info>

## Why Cloudflare Tunnel

Two constraints push us here:

1. **The gateway binds to a private interface by default** for safety on a
   multi-user host. Inbound firewall rules and reverse proxies all require
   opening a public port — `cloudflared` does not.
2. **Telegram Mini Apps require HTTPS** with a valid certificate. Self-signed
   or `http://` URLs are silently refused by the Telegram client.

`cloudflared` solves both: the public side is `https://<your-domain>/` on
Cloudflare's edge, the private side is a process on your host, and the
tunnel takes care of TLS termination and cert renewal.

## Prerequisites

- A Cloudflare account with a domain on it (any plan — the free tier is
  enough).
- Subdomain you can dedicate to the Mini App (`miniapp.example.com` in the
  examples below; pick your own).
- A Telegram bot whose token is already in Deneb's config (`channels.telegram.botToken`).
- The Deneb gateway built with `make gateway-prod` and running on port `18789`.

## Choose a deployment path

There are two reasonable layouts. Pick once; either works.

| | **A. Dedicated `deneb-miniapp` tunnel** | **B. Add ingress to an existing tunnel** |
|---|---|---|
| When | You have no other Cloudflare-tunnelled services on this host. | You already run a tunnel (e.g. for an API or other web app) — just add another hostname to it. |
| Pros | Clean isolation. Tunnel lifecycle decoupled from other services. | Zero extra processes, no extra credentials, one log to watch. |
| Cons | Another daemon + systemd unit to manage. | Restart of the existing tunnel briefly drops other services it fronts. |

Both paths share the gateway-side bind, BotFather, and Deneb config steps;
they only differ in how the tunnel itself is configured (Step 5).

## Choose a gateway bind

`cloudflared` reaches the gateway over a TCP socket on the host. The
gateway's `--bind` flag determines which IP it listens on. Pick whichever
your tunnel can reach:

| Bind mode | Listens on | Use when |
|---|---|---|
| `--bind loopback` (default) | `127.0.0.1:18789` | `cloudflared` runs **on the host** (systemd / direct binary). |
| `--bind tailnet` | The host's Tailscale IP (e.g. `100.105.145.6:18789`) | `cloudflared` runs **inside a Docker container** — loopback inside the container is the container, not the host. Tailscale interface is reachable from both. |
| `--bind lan` | `0.0.0.0:18789` | You explicitly want LAN-wide access. **Combine with a host firewall**; otherwise the gateway's `/debug/pprof/`, `/api/cron/run`, etc. become reachable from anything on the LAN. |

For the Mini App, only `/app/` and `/api/v1/miniapp/` need to be exposed.
The tunnel ingress (Step 5 below) enforces that path filter, so even a
wide bind still ships a small attack surface to the internet — but it
opens the same surface to anything else that can reach the bind IP. Prefer
`loopback` or `tailnet`.

Set this in `~/.config/systemd/user/deneb-gateway.service.d/override.conf`
(`ExecStart=… --bind tailnet --port 18789`) and `systemctl --user daemon-reload && systemctl --user restart deneb-gateway.service`.

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

    If you intend to run `cloudflared` in Docker (Path B is the common
    case here), skip this step — pull `cloudflare/cloudflared:latest`
    when wiring the container.
  </Step>

  <Step title="Authenticate with Cloudflare (path A only)">
    ```bash
    cloudflared tunnel login
    ```

    Browser opens; pick the zone (the domain you intend to use) and
    approve. A cert is written to `~/.cloudflared/cert.pem`.

    Path B skips this — the existing tunnel already has its credentials.
  </Step>

  <Step title="Path A — Create a new tunnel">
    ```bash
    cloudflared tunnel create deneb-miniapp
    ```

    Output ends with something like:

    ```
    Created tunnel deneb-miniapp with id 9c3b1234-...-aabbccddeeff
    ```

    The credentials file (`~/.cloudflared/<tunnel-id>.json`) holds the
    tunnel's secret — treat it like a private key.

    **Path B users**: do nothing here; you will reuse the existing
    tunnel's UUID in Step 5.
  </Step>

  <Step title="Route a hostname to the tunnel">
    ```bash
    # Path A:
    cloudflared tunnel route dns deneb-miniapp miniapp.example.com

    # Path B (substitute your tunnel name):
    cloudflared tunnel route dns my-existing-tunnel miniapp.example.com
    ```

    This creates a CNAME from `miniapp.example.com` to the tunnel's
    internal `<tunnel-id>.cfargotunnel.com` endpoint. Cloudflare manages the
    TLS cert automatically.
  </Step>

  <Step title="Wire the ingress">
    **Path A** — create `~/.cloudflared/config.yml`:

    ```yaml
    tunnel: deneb-miniapp
    credentials-file: /home/YOUR_USER/.cloudflared/9c3b1234-...-aabbccddeeff.json

    ingress:
      # Mini App static + RPC: route only the paths the webview needs.
      - hostname: miniapp.example.com
        path: ^/(app/|api/v1/miniapp/)
        service: http://127.0.0.1:18789   # or http://<tailscale-ip>:18789

      # Same hostname, anything else: 404 from the tunnel.
      - hostname: miniapp.example.com
        service: http_status:404

      # Catch-all (other hostnames pointing here).
      - service: http_status:404
    ```

    **Path B** — edit the existing tunnel's config (often a
    bind-mounted file inside the container). Insert the same two
    `hostname: miniapp.example.com` blocks **above** the existing
    catch-all `- service: http_status:404`:

    ```yaml
    ingress:
      # ... your existing rules ...

      - hostname: miniapp.example.com
        path: ^/(app/|api/v1/miniapp/)
        service: http://<bind-ip>:18789   # match gateway --bind choice
      - hostname: miniapp.example.com
        service: http_status:404

      - service: http_status:404
    ```

    The `path:` filter is the safety belt that keeps `/api/cron/run`,
    `/debug/pprof/`, and `/health` unreachable from the public hostname
    even though the tunnel exists.
  </Step>

  <Step title="Reload the tunnel">
    **Path A — systemd**:

    ```bash
    sudo cloudflared service install
    sudo systemctl enable --now cloudflared
    sudo systemctl status cloudflared
    ```

    The service rereads `~/.cloudflared/config.yml` and restarts on
    reboot. Logs land in `journalctl -u cloudflared -f`.

    **Path B — Docker container**:

    <Warning>
      Do not `docker kill --signal SIGHUP <name>` to reload — recent
      `cloudflare/cloudflared` images **exit on HUP**. Use a full
      restart instead.
    </Warning>

    ```bash
    docker restart <existing-tunnel-container>
    # confirm the new hostname is registered
    docker logs --tail 30 <existing-tunnel-container>
    ```

    You should see four `Registered tunnel connection` lines (one per
    Cloudflare edge POP); the new hostname is implied by the matched
    ingress rule.
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

    Restart the gateway. On startup it calls `setChatMenuButton` against
    the Bot API; the journal should show
    `telegram WebApp menu button installed url=https://miniapp.example.com/app/ label=Deneb`. If you see
    `telegram setChatMenuButton failed error=API error 404`, the domain
    is not registered yet — re-run the previous step.
  </Step>

  <Step title="Verify end-to-end">
    From any internet-connected machine:

    ```bash
    curl -i https://miniapp.example.com/app/
    # → 200 + text/html; charset=utf-8

    curl -i https://miniapp.example.com/app/assets/$(curl -s https://miniapp.example.com/app/ | grep -oE 'index-[A-Za-z0-9_-]+\.js')
    # → 200 + application/javascript; charset=utf-8 + immutable cache

    curl -i -X POST https://miniapp.example.com/api/v1/miniapp/rpc
    # → 401 (no Authorization header — auth is wired)

    curl -i https://miniapp.example.com/health
    # → 404 (path filter blocks)
    ```

    Then open the bot in Telegram. The chat shows a menu button labeled
    **Deneb** next to the attachment paperclip; tap it — the Mini App opens
    inline and renders "Authenticated as &lt;your name&gt;".
  </Step>
</Steps>

## Troubleshooting

<AccordionGroup>
  <Accordion title="Tap menu button → blank page or `502 Bad Gateway`">
    The tunnel is up but the gateway isn't responding on the
    bind socket. From the **tunnel host**:

    ```bash
    ss -ltnp | grep 18789           # confirm gateway is listening
    journalctl -u cloudflared -f    # path A
    docker logs -f <tunnel-name>    # path B
    ```

    Common cause: `cloudflared` runs in Docker and is trying to reach
    `127.0.0.1:18789` — that's the container's loopback, not the
    host's. Re-bind the gateway with `--bind tailnet` and point the
    tunnel's `service:` at the Tailscale IP.
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

  <Accordion title="`/app/` loads but the page is blank, console says assets 404">
    The gateway binary was built before `make embed-frontend` ran, so
    only the placeholder is embedded. Re-deploy:

    ```bash
    cd ~/deneb
    make build-frontend && make embed-frontend
    make gateway-prod
    systemctl --user restart deneb-gateway.service
    ```

    Verify the new hashed asset filenames appear in
    `curl https://miniapp.example.com/app/` after the restart.
  </Accordion>

  <Accordion title="Menu button never appears">
    Four things to check in order:

    1. `webAppURL` is non-empty in `~/.deneb/deneb.json` — when blank the
       gateway intentionally skips the `setChatMenuButton` call.
    2. `/setdomain` step ran (and the reply was "Success") — without it
       Telegram silently refuses to render the button **and**
       `setChatMenuButton` returns API error 404.
    3. The gateway started after the config was edited and the journal
       shows `telegram WebApp menu button installed`. If you see
       `setChatMenuButton failed`, double-check the registered domain
       matches `webAppURL` host exactly.
    4. The Telegram client may cache the menu button state — force-quit
       and re-open the chat once.
  </Accordion>

  <Accordion title="Korean text renders as `???`">
    Cloudflare passes bytes through, so this almost always means the
    response is missing `charset=utf-8`. The Mini App's HTML serves with
    `text/html; charset=utf-8` by default; if you front it with another
    proxy in between, make sure the proxy preserves the header.
  </Accordion>

  <Accordion title="`docker kill --signal SIGHUP` stopped the cloudflared container">
    Recent `cloudflare/cloudflared:*` images do not handle SIGHUP — the
    process exits and the container stops. Use
    `docker restart <name>` to apply a new ingress config; the
    bind-mounted YAML is re-read on startup.
  </Accordion>
</AccordionGroup>

## Security notes

- **No inbound port is opened.** `cloudflared` makes an outbound TCP
  connection to Cloudflare; the gateway socket stays on a private interface
  (loopback / Tailscale).
- **Path filtering is enforced at the tunnel.** The `path: ^/(app/|api/v1/miniapp/)` rule means `/api/cron/run`,
  `/debug/pprof/`, `/health`, and every other gateway endpoint return 404
  via the public hostname even though the tunnel is wired up. The gateway
  itself still binds those routes on the chosen interface for local
  scripts and Tailscale operators.
- **`--bind tailnet` widens the local surface a little.** Anything on
  your Tailnet can reach the gateway socket, not just `cloudflared`.
  Restrict the tailnet ACLs accordingly if you share the network.
- **The tunnel credentials file is a long-lived secret.** Store it like an
  SSH key: file permissions `600`, never check it into git, and rotate it
  by running `cloudflared tunnel delete` followed by a fresh `create` if
  it leaks.
- **Cloudflare sees the plaintext.** Traffic is TLS-terminated at
  Cloudflare's edge and re-encrypted to the tunnel. If you need
  end-to-end encryption to the gateway you would need a different setup
  (mTLS reverse proxy, Tailscale Funnel, etc.) — out of scope here.
