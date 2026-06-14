#!/usr/bin/env bash
# Generate a dev-safe config from the production deneb.json.
#
# Copies ~/.deneb/deneb.json and disables Gmail polling and the cron scheduler
# so the dev instance never collides with the production gateway's background
# work (duplicate poll cycles / duplicate cron runs).
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
  echo "      generating minimal dev config" >&2
  # Write a minimal dev-safe config. Chat injection uses the native
  # miniapp.chat.send RPC, so no channel section is needed to boot.
  python3 -c "
import json
cfg = {
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
import json

with open('$PROD_CONFIG') as f:
    cfg = json.load(f)

changes = []

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
import json

with open('$PROD_CONFIG') as f:
    cfg = json.load(f)

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
