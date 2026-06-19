---
name: node-connect
version: "1.0.0"
category: devops
description: "Diagnose Deneb native app to gateway connectivity, pairing, route, auth, and listener issues. Use when: the Android/native app cannot connect, pairing fails, QR/manual host is wrong, Tailscale/LAN/public URL is unclear, or gateway reachability must be proven. NOT for: model/provider failures after a connected session exists."
metadata:
  {
    "deneb":
      {
        "emoji": "📱",
        "requires": { "anyBins": ["curl", "tailscale", "ssh"] },
        "tags": ["native", "Android", "gateway", "pairing", "Tailscale", "LAN", "connection"],
        "related_skills": ["healthcheck", "remote-validation"],
        "install":
          [
            {
              "id": "brew-curl",
              "kind": "brew",
              "formula": "curl",
              "bins": ["curl"],
              "label": "Install curl (brew)",
            },
            {
              "id": "apt-curl",
              "kind": "apt",
              "package": "curl",
              "bins": ["curl"],
              "label": "Install curl (apt)",
            },
          ],
      },
  }
---

# Node Connect

Use this for native app to gateway connection or pairing failures.

## Topology First

Pick the intended route before changing anything:

- same machine or emulator
- same LAN/Wi-Fi
- Tailscale tailnet
- public URL/reverse proxy
- SSH/remote host through `srv1`

Do not debug `localhost` for a remote phone. Do not switch LAN to Tailscale
unless remote access is actually intended.

## Procedure

1. Ask for the exact app text/status only if it is not already visible.
2. Identify the route the app is supposed to use.
3. Verify the gateway listener on that route.
4. Verify auth/pairing separately from reachability.
5. Regenerate or re-enter pairing/setup data only after the route is fixed.
6. Stop when the app reaches the gateway and the remaining error is clearly
   auth/model/session-specific.

## Checks

Use what exists in the current checkout/config. Common checks:

```bash
ss -ltnp 2>/dev/null || lsof -nP -iTCP -sTCP:LISTEN
curl -fsS http://127.0.0.1:<port>/health
tailscale status --json
ssh srv1 'ss -ltnp 2>/dev/null || lsof -nP -iTCP -sTCP:LISTEN'
```

If the gateway exposes a pairing/status command or miniapp health route, prefer
that over guessing from config files.

## Root Cause Map

- App points at `127.0.0.1` or `localhost` from a phone: wrong route.
- LAN route fails but local health works: firewall, bind address, or Wi-Fi
  isolation.
- Tailscale route fails: tailnet not logged in, wrong MagicDNS/IP, or gateway
  not listening on the tailnet path.
- Public URL fails while local works: reverse proxy, TLS, auth header, or route
  config.
- App says unauthorized/pairing required after a successful HTTP reachability
  check: stop changing network config and fix auth/pairing.

## Report

Return one diagnosis and one next action:

```text
route=tailnet
proof=gateway health works locally, tailnet IP does not answer port 18789
diagnosis=gateway is not bound or served on the tailnet route
next=enable the intended tailnet/public route, restart gateway, then regenerate pairing data
```
