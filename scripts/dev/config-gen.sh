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
#   scripts/config-gen.sh              # generate /tmp/deneb-dev-config.json
#   scripts/config-gen.sh --out FILE   # custom output path
#   scripts/config-gen.sh --diff       # show what was stripped/replaced
#   scripts/config-gen.sh --check      # exit 0 if prod config exists, 1 if not

set -euo pipefail

# Source shared library for .env loading.
source "$(cd "$(dirname "$0")" && pwd)/lib-server.sh"
devlib_load_dotenv

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
  echo "      generating minimal dev config with mock Telegram channel" >&2
  # Write a minimal dev-safe config. We still wire the Telegram channel
  # because live-test.sh points TELEGRAM_API_BASE at the local mock server,
  # so the plugin needs *some* channels.telegram section to boot.
  python3 -c "
import json, os
dev_token = os.environ.get('DENEB_DEV_TELEGRAM_TOKEN') or 'mock-dev-token'
cfg = {
    'channels': {
        'telegram': {
            'botToken': dev_token,
            'chatID': 10000001,
        }
    },
    'cron': {'enabled': False},
    'gmailPoll': {'enabled': False},
}
with open('$OUT', 'w') as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write('\n')
"
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
print('Storage isolation (set by live-test.sh / iterate.sh):')
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

# live-test.sh always passes a mock token (via DENEB_DEV_TELEGRAM_TOKEN) so
# the Telegram plugin has something to boot with. The real bot is never
# contacted because TELEGRAM_API_BASE points at the local mock server.
dev_token = os.environ.get('DENEB_DEV_TELEGRAM_TOKEN', '') or 'mock-dev-token'

# Ensure channels.telegram exists so the plugin actually starts in dev mode.
channels = cfg.setdefault('channels', {})
tg = channels.get('telegram')
if not isinstance(tg, dict):
    tg = {}
    channels['telegram'] = tg
tg['botToken'] = dev_token
# Seed a chat ID so the primary-DM path has a valid target. The value is
# synthetic — messages flow through the mock, never a real chat.
if not tg.get('chatID'):
    tg['chatID'] = 10000001

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
