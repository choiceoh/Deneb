#!/usr/bin/env bash
# crs812-config-backup.sh — snapshot the CRS812 switch config into the backed-up
# state tree.
#
# Why: the switch's config (jumbo l2mtu, fan tuning, NTP client, port setup)
# lives ONLY on the switch. A dead unit or a bad change would mean rebuilding it
# from memory. ~/.deneb/network rides the daily offsite memory backup
# (backup.DefaultTargets covers the state dir), so a snapshot here is both
# versioned locally and shipped offsite for free.
#
# The switch is reachable only from srv2 (192.168.88.10), hence the double hop.
# Writes only when the config actually changed, so the file's mtime is a real
# "last changed" signal rather than "last polled".
set -euo pipefail

DEST="${DENEB_STATE_DIR:-$HOME/.deneb}/network/crs812-config.rsc"
mkdir -p "$(dirname "$DEST")"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

if ! ssh -o BatchMode=yes -o ConnectTimeout=10 srv2 \
        "ssh -o BatchMode=yes -o ConnectTimeout=10 admin@192.168.88.1 '/export'" > "$tmp" 2>/dev/null; then
    echo "crs812-config-backup: export failed (switch unreachable)" >&2
    exit 1
fi
# A valid export always carries the RouterOS header; refuse to overwrite a good
# snapshot with a truncated or error-page response.
if ! head -1 "$tmp" | grep -q "by RouterOS"; then
    echo "crs812-config-backup: refusing to save malformed export" >&2
    exit 1
fi

# Ignore the timestamp comment on line 1 when comparing — it changes every run.
if [[ -f "$DEST" ]] && diff -q <(tail -n +2 "$DEST") <(tail -n +2 "$tmp") >/dev/null; then
    exit 0
fi
cp "$tmp" "$DEST"
echo "crs812-config-backup: config changed, snapshot updated ($(wc -l < "$DEST") lines)"
