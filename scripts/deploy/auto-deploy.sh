#!/usr/bin/env bash
# auto-deploy.sh — Pull origin/main and redeploy if there are new commits.
# Intended to run on a short interval (e.g. every minute) via user cron or a
# systemd user timer on the DGX Spark host, so merged PRs reach production
# without manual intervention. Pair with scripts/deploy/deploy.sh which does
# the actual build + graceful restart.
#
# Design choices:
# - flock on /tmp/deneb-auto-deploy.lock so a slow deploy never overlaps with
#   the next tick. Cron fires every minute; a build + restart can exceed that.
# - git fetch first, then compare HEAD vs origin/main. Skip entirely if nothing
#   changed — avoids invoking `make gateway-prod` on every tick, which would
#   pin CPU and spin fans for no reason.
# - Always log to /tmp/deneb-auto-deploy.log with ISO timestamps. Short log so
#   an operator can `tail` it to see the last few decisions at a glance.
# - Pull-based rather than push-based (GitHub Actions → SSH) because the DGX
#   is a single-operator local host and we do not want to expose SSH or a
#   webhook receiver to the internet.
set -euo pipefail

PROD_DIR="${DENEB_PROD_DIR:-$HOME/deneb}"
LOCK_FILE="/tmp/deneb-auto-deploy.lock"
LOG_FILE="/tmp/deneb-auto-deploy.log"

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

# Acquire lock or exit silently — the previous tick is still deploying.
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
    exit 0
fi

if [[ ! -d "$PROD_DIR/.git" ]]; then
    log "ERROR: $PROD_DIR is not a git repo; skipping"
    exit 1
fi

cd "$PROD_DIR"

branch=$(git branch --show-current)
if [[ "$branch" != "main" ]]; then
    log "WARN: production is on '$branch', not main; skipping"
    exit 0
fi

# Quiet fetch; tolerate transient failures (flaky network, GitHub blip).
if ! git fetch --quiet origin main 2>>"$LOG_FILE"; then
    log "WARN: git fetch failed; will retry on next tick"
    exit 0
fi

local_head=$(git rev-parse HEAD)
remote_head=$(git rev-parse origin/main)

if [[ "$local_head" == "$remote_head" ]]; then
    # No-op ticks are common — stay quiet to keep the log readable.
    exit 0
fi

log "new commit: $local_head -> $remote_head; running deploy.sh"
if "$PROD_DIR/scripts/deploy/deploy.sh" >>"$LOG_FILE" 2>&1; then
    log "deploy OK (head now $(git rev-parse HEAD))"
else
    rc=$?
    log "deploy FAILED (rc=$rc) — manual intervention may be required"
    exit $rc
fi
