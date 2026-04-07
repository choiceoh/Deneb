#!/usr/bin/env bash
# Generate a dev-safe config from the production deneb.json.
#
# Copies ~/.deneb/deneb.json and replaces the Telegram bot token with a
# dev-specific token (from DENEB_DEV_TELEGRAM_TOKEN env var) to prevent
# 409 conflicts with the production gateway's long-poll.  If no dev token
# is set, the token is stripped entirely (Telegram disabled).
#
# Everything else (providers, agents, sessions, hooks, logging, auth, ...)
# is preserved so the dev instance exercises the same code paths as production.
#
# Usage:
#   scripts/dev-config-gen.sh              # generate /tmp/deneb-dev-config.json
#   scripts/dev-config-gen.sh --out FILE   # custom output path
#   scripts/dev-config-gen.sh --diff       # show what was stripped/replaced
#   scripts/dev-config-gen.sh --check      # exit 0 if prod config exists, 1 if not

set -euo pipefail

# Source .env for dev token (DENEB_DEV_TELEGRAM_TOKEN etc.) if not already set.
_dotenv="${HOME}/.deneb/.env"
if [[ -f "$_dotenv" ]]; then
  # Load KEY=VALUE lines without overriding existing env vars.
  while IFS='=' read -r key val; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    key="${key## }"; key="${key%% }"
    val="${val## }"; val="${val%% }"
    val="${val#\"}"; val="${val%\"}"
    val="${val#\'}"; val="${val%\'}"
    if [[ -z "${!key:-}" ]]; then
      export "$key=$val"
    fi
  done < "$_dotenv"
fi

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
    # Show what fields get stripped/replaced.
    python3 -c "
import json, os, sys

with open('$PROD_CONFIG') as f:
    cfg = json.load(f)

changes = []
dev_token = os.environ.get('DENEB_DEV_TELEGRAM_TOKEN', '')

# Telegram bot token: replace with dev token or strip.
tg = cfg.get('channels', {}).get('telegram', {})
if tg.get('botToken'):
    if dev_token:
        changes.append('channels.telegram.botToken (replaced with dev bot token)')
    else:
        changes.append('channels.telegram.botToken (stripped, no DENEB_DEV_TELEGRAM_TOKEN)')

# Strip Gmail polling (avoid duplicate poll cycles).
gp = cfg.get('gmailPoll', {})
if gp.get('enabled'):
    changes.append('gmailPoll.enabled (set to false)')

# Strip cron scheduler (avoid duplicate cron execution).
cr = cfg.get('cron', {})
if cr.get('enabled', True):
    changes.append('cron.enabled (set to false)')

if changes:
    print('Modified fields (dev safety):')
    for c in changes:
        print(f'  - {c}')
else:
    print('No fields modified (config is dev-safe as-is)')

print('')
print('Storage isolation (set by dev-live-test.sh / dev-iterate.sh):')
print('  - DENEB_STATE_DIR → /tmp/deneb-dev-state (or /tmp/deneb-iterate-state)')
print('  - DENEB_WIKI_DIR → isolated from ~/.deneb/wiki')
print('  - DENEB_WIKI_DIARY_DIR → isolated from ~/.deneb/memory/diary')
"
    ;;

  generate)
    python3 -c "
import json, os, sys

with open('$PROD_CONFIG') as f:
    cfg = json.load(f)

dev_token = os.environ.get('DENEB_DEV_TELEGRAM_TOKEN', '')

# Telegram bot token: replace with dev token (full parity) or strip (safe fallback).
tg = cfg.get('channels', {}).get('telegram')
if tg and isinstance(tg, dict):
    if dev_token:
        tg['botToken'] = dev_token
    else:
        tg['botToken'] = ''

# Disable Gmail polling to avoid duplicate poll cycles.
gp = cfg.get('gmailPoll')
if gp and isinstance(gp, dict):
    gp['enabled'] = False

# Disable cron scheduler to avoid duplicate cron execution.
cfg.setdefault('cron', {})['enabled'] = False

with open('$OUT', 'w') as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write('\n')

print('$OUT')
" 2>/dev/null
    ;;
esac
