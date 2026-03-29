---
summary: "Uninstall Deneb completely (CLI, service, state, workspace)"
read_when:
  - You want to remove Deneb from a machine
  - The gateway service is still running after uninstall
title: "Uninstall"
---

# Uninstall

Two paths:

- **Easy path** if `deneb` is still installed.
- **Manual service removal** if the CLI is gone but the service is still running.

## Easy path (CLI still installed)

Recommended: use the built-in uninstaller:

```bash
deneb uninstall
```

Non-interactive (automation / npx):

```bash
deneb uninstall --all --yes --non-interactive
npx -y deneb uninstall --all --yes --non-interactive
```

Manual steps (same result):

1. Stop the gateway service:

```bash
deneb gateway stop
```

2. Uninstall the gateway service (launchd/systemd/schtasks):

```bash
deneb gateway uninstall
```

3. Delete state + config:

```bash
rm -rf "${DENEB_STATE_DIR:-$HOME/.deneb}"
```

If you set `DENEB_CONFIG_PATH` to a custom location outside the state dir, delete that file too.

4. Delete your workspace (optional, removes agent files):

```bash
rm -rf ~/.deneb/workspace
```

5. Remove the CLI install (pick the one you used):

```bash
npm rm -g deneb
pnpm remove -g deneb
bun remove -g deneb
```

Notes:

- If you used profiles (`--profile` / `DENEB_PROFILE`), repeat step 3 for each state dir (defaults are `~/.deneb-<profile>`).
- In remote mode, the state dir lives on the **gateway host**, so run steps 1-4 there too.

## Manual service removal (CLI not installed)

Use this if the gateway service keeps running but `deneb` is missing.

### Linux (systemd user unit)

Default unit name is `deneb-gateway.service` (or `deneb-gateway-<profile>.service`):

```bash
systemctl --user disable --now deneb-gateway.service
rm -f ~/.config/systemd/user/deneb-gateway.service
systemctl --user daemon-reload
```

## Normal install vs source checkout

### Normal install (install.sh / npm / pnpm / bun)

If you used `https://deneb.ai/install.sh` or `install.ps1`, the CLI was installed with `npm install -g deneb@latest`.
Remove it with `npm rm -g deneb` (or `pnpm remove -g` / `bun remove -g` if you installed that way).

### Source checkout (git clone)

If you run from a repo checkout (`git clone` + `deneb ...` / `bun run deneb ...`):

1. Uninstall the gateway service **before** deleting the repo (use the easy path above or manual service removal).
2. Delete the repo directory.
3. Remove state + workspace as shown above.
