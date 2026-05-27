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
# - Record the last successfully deployed commit so a build failure after a
#   fast-forward pull is retried, but not hammered every minute.
# - Pull-based rather than push-based (GitHub Actions → SSH) because the DGX
#   is a single-operator local host and we do not want to expose SSH or a
#   webhook receiver to the internet.
# - **Always exit 0**, even on failure. The script writes errors to its log
#   so an operator can spot them via `tail`, but exiting non-zero would mark
#   the systemd service as "failed", which historically caused operators to
#   disable the timer entirely when they saw the red status. The May-2026
#   incident is the cautionary tale: a brief week of `git pull` divergence
#   produced a stream of failed status lines, the timer got disabled, and
#   prod stopped auto-pulling for two days. The FAIL_FILE/RETRY_SEC guard
#   already throttles retries on the same broken commit, so swallowing the
#   exit code costs nothing while protecting the watchdog.
set -euo pipefail

PROD_DIR="${DENEB_PROD_DIR:-$HOME/deneb}"
STATE_DIR="${DENEB_STATE_DIR:-$HOME/.deneb}"
STATE_FILE="$STATE_DIR/auto-deploy.deployed-head"
FAIL_FILE="$STATE_DIR/auto-deploy.failed-head"
LOCK_FILE="/tmp/deneb-auto-deploy.lock"
LOG_FILE="/tmp/deneb-auto-deploy.log"
RETRY_SEC="${DENEB_AUTO_DEPLOY_RETRY_SEC:-600}"

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

record_failure() {
    local head="$1"
    mkdir -p "$STATE_DIR"
    printf '%s %s\n' "$head" "$(date +%s)" > "$FAIL_FILE"
}

recent_failed_head() {
    local head="$1"
    [[ -f "$FAIL_FILE" ]] || return 1

    local failed_head=""
    local failed_at=""
    read -r failed_head failed_at < "$FAIL_FILE" || return 1
    [[ "$failed_head" == "$head" ]] || return 1

    local now
    now=$(date +%s)
    (( now - failed_at < RETRY_SEC ))
}

# Acquire lock or exit silently — the previous tick is still deploying.
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
    exit 0
fi

if [[ ! -d "$PROD_DIR/.git" ]]; then
    log "ERROR: $PROD_DIR is not a git repo; skipping"
    # Exit 0 even on a permanent config error — see header comment.
    # An operator who reads the log will see the ERROR line; systemd
    # stays happy so the watchdog doesn't get disabled.
    exit 0
fi

cd "$PROD_DIR"

branch=$(git branch --show-current)
if [[ "$branch" != "main" ]]; then
    log "WARN: production is on '$branch', not main; skipping"
    exit 0
fi

# Tracked local edits are operator intent until proven otherwise.
# Untracked scratch is tolerated (build / cache / .bak files etc.).
#
# Old behavior was to *skip* on any dirty tracked file, which produced
# wedged "skipping auto-deploy" ticks until an operator intervened. We
# now auto-stash so a transient dirty state (build artifact, half-saved
# operator edit) never blocks the watchdog — the stash entry stays in
# `git stash list` for inspection. The May-2026 incident: a single
# dirty tick from an unknown source held back a deploy for ~1min;
# auto-stash makes that case self-heal in 0 ticks.
#
# Tracked diff vs cached diff: `git diff` catches worktree edits,
# `git diff --cached` catches staged-but-uncommitted. Combined they
# cover everything `git stash push` will reach. Untracked files (the
# `-u` flag would include) stay untouched — they were tolerated before
# and an aggressive stash could swallow build outputs we'd want to
# keep around.
AUTOSTASH_REF=""
if ! git diff --quiet || ! git diff --cached --quiet; then
    dirty_summary=$(git status --porcelain --untracked-files=no | head -5)
    log "INFO: worktree dirty, auto-stashing for deploy:"
    printf '%s\n' "$dirty_summary" | sed 's/^/  /' >> "$LOG_FILE"
    stash_msg="auto-deploy stash $(date -Iseconds)"
    # `--keep-index` would re-stage staged changes after stash, but
    # we want a fully clean tree for the upcoming build, so omit it.
    if git stash push --message "$stash_msg" >>"$LOG_FILE" 2>&1; then
        AUTOSTASH_REF="$stash_msg"
    else
        log "WARN: auto-stash failed; skipping this tick — inspect with 'git -C $PROD_DIR status'"
        exit 0
    fi
fi

# Always try to restore stashed changes on exit, even if the deploy
# fails or the script bails early. `git stash pop` is best-effort: a
# pop conflict logs loudly but the script still exits 0 so systemd
# stays green (see the always-exit-0 rationale in the header).
restore_stash() {
    if [[ -z "$AUTOSTASH_REF" ]]; then
        return
    fi
    # Find the stash slot by message — index numbers shift if other
    # stashes happened in parallel (unlikely here but defensive).
    local slot
    slot=$(git stash list | grep -F -- "$AUTOSTASH_REF" | head -1 | cut -d: -f1)
    if [[ -z "$slot" ]]; then
        log "WARN: auto-stash entry not found; nothing to pop"
        return
    fi
    if git stash pop --quiet "$slot" >>"$LOG_FILE" 2>&1; then
        log "auto-stash popped successfully (slot $slot)"
    else
        log "WARN: auto-stash pop conflict; entry kept at $slot for operator review"
    fi
}
trap restore_stash EXIT

# Quiet fetch; tolerate transient failures (flaky network, GitHub blip).
if ! git fetch --quiet origin main 2>>"$LOG_FILE"; then
    log "WARN: git fetch failed; will retry on next tick"
    exit 0
fi

local_head=$(git rev-parse HEAD)
remote_head=$(git rev-parse origin/main)
deployed_head=""
if [[ -f "$STATE_FILE" ]]; then
    deployed_head=$(tr -d '[:space:]' < "$STATE_FILE")
fi

if [[ -z "$deployed_head" && "$local_head" == "$remote_head" ]]; then
    mkdir -p "$STATE_DIR"
    printf '%s\n' "$local_head" > "$STATE_FILE"
    exit 0
fi

if [[ "$local_head" == "$remote_head" && "$deployed_head" == "$remote_head" ]]; then
    # No-op ticks are common — stay quiet to keep the log readable.
    exit 0
fi

if recent_failed_head "$remote_head"; then
    exit 0
fi

log "new main candidate: current=$local_head deployed=${deployed_head:-none} remote=$remote_head; running deploy.sh"
# `set -e` would normally bail on a non-zero deploy.sh, so we disable it
# for this one block — both branches need to run (the failure branch
# records the bad head so we don't retry immediately, then exits 0 so
# systemd stays green).
set +e
"$PROD_DIR/scripts/deploy/deploy.sh" >>"$LOG_FILE" 2>&1
rc=$?
set -e
if (( rc == 0 )); then
    deployed_now=$(git rev-parse HEAD)
    mkdir -p "$STATE_DIR"
    printf '%s\n' "$deployed_now" > "$STATE_FILE"
    rm -f "$FAIL_FILE"
    log "deploy OK (head now $deployed_now)"
else
    record_failure "$remote_head"
    log "deploy FAILED (rc=$rc) — manual intervention may be required"
    # Exit 0 — see header. The FAIL_FILE/RETRY_SEC throttle stops the
    # next tick from re-running the same broken commit for 10 minutes,
    # so this isn't a hot loop.
fi
