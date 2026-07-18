#!/usr/bin/env bash
# Install the user-level (~/.claude/hooks) shims for Deneb's global hooks.
#
# Canonical hook logic lives in the repo (scripts/dev/); the home directory only
# holds stable shims that exec the production checkout's copy. Re-run after
# adding a new global hook; re-running is idempotent. Wiring itself
# (~/.claude/settings.json hooks.PreToolUse) is left to the operator — this
# script only reports whether it is present.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
hooks_dir="${HOME}/.claude/hooks"
mkdir -p "$hooks_dir"

install -m 0755 "$here/deneb-concurrency-guard-shim.py" \
  "$hooks_dir/deneb-concurrency-guard.py"
echo "installed: $hooks_dir/deneb-concurrency-guard.py (shim → ~/deneb/scripts/dev/deneb-concurrency-guard.py)"

settings="${HOME}/.claude/settings.json"
if [ -f "$settings" ] && grep -q "deneb-concurrency-guard" "$settings"; then
  echo "wiring OK: $settings references deneb-concurrency-guard"
else
  echo "WARN: $settings has no deneb-concurrency-guard PreToolUse entry — add matcher Write|Edit|MultiEdit|Bash"
fi
