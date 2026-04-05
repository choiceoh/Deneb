#!/usr/bin/env bash
# Generate a dev-safe config from the production deneb.json.
#
# Copies ~/.deneb/deneb.json but strips the Telegram bot token to prevent
# 409 conflicts with the production gateway's long-poll.  Everything else
# (providers, agents, sessions, hooks, logging, auth, ...) is preserved so
# the dev instance exercises the same code paths as production.
#
# Usage:
#   scripts/dev-config-gen.sh              # generate /tmp/deneb-dev-config.json
#   scripts/dev-config-gen.sh --out FILE   # custom output path
#   scripts/dev-config-gen.sh --diff       # show what was stripped
#   scripts/dev-config-gen.sh --check      # exit 0 if prod config exists, 1 if not

set -euo pipefail

PROD_CONFIG="${DENEB_CONFIG_PATH:-${HOME}/.deneb/deneb.json}"
OUT="/tmp/deneb-dev-config.json"
MODE="generate"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out)  OUT="$2"; shift 2 ;;
    --diff) MODE="diff"; shift ;;
    --check) MODE="check"; shift ;;
    --prod) PROD_CONFIG="$2"; shift 2 ;;
    *) shift ;;
  esac
done

if [[ ! -f "$PROD_CONFIG" ]]; then
  if [[ "$MODE" == "check" ]]; then
    echo "no-prod-config"
    exit 1
  fi
  echo "WARN: production config not found at $PROD_CONFIG" >&2
  echo "      falling back to empty config {}" >&2
  echo '{}' > "$OUT"
  exit 0
fi

case "$MODE" in
  check)
    echo "ok"
    exit 0
    ;;

  diff)
    # Show what fields get stripped.
    python3 -c "
import json, sys

with open('$PROD_CONFIG') as f:
    cfg = json.load(f)

stripped = []

# Strip Telegram bot token (prevents 409 long-poll conflict).
tg = cfg.get('channels', {}).get('telegram', {})
if tg.get('botToken'):
    stripped.append('channels.telegram.botToken')
    tg['botToken'] = ''

# Strip Gmail polling (avoid duplicate poll cycles).
gp = cfg.get('gmailPoll', {})
if gp.get('enabled'):
    stripped.append('gmailPoll.enabled (set to false)')

if stripped:
    print('Stripped fields (dev safety):')
    for s in stripped:
        print(f'  - {s}')
else:
    print('No fields stripped (config is dev-safe as-is)')
"
    ;;

  generate)
    python3 -c "
import json, sys

with open('$PROD_CONFIG') as f:
    cfg = json.load(f)

# Strip Telegram bot token to prevent 409 conflict with production's long-poll.
# Keep the rest of channels.telegram so config validation paths run.
tg = cfg.get('channels', {}).get('telegram')
if tg and isinstance(tg, dict):
    tg['botToken'] = ''

# Disable Gmail polling to avoid duplicate poll cycles.
gp = cfg.get('gmailPoll')
if gp and isinstance(gp, dict):
    gp['enabled'] = False

with open('$OUT', 'w') as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write('\n')

print('$OUT')
" 2>/dev/null
    ;;
esac
