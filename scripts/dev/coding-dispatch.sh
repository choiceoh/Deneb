#!/usr/bin/env bash
# coding-dispatch.sh — RSI L4 실행 레인: 자기교정 큐의 소스 후보를 코딩
# 에이전트(Codex CLI 헤드리스)에 자동 배차한다.
#
# 오퍼레이터 승인(2026-07-12, memory: source-self-edit-authorization): dev
# 트리에서만 편집, 전체 게이트 그린일 때만 랜딩·핫스왑. 이 스크립트는 그
# 계약의 배차원이다:
#   1. gateway tracker의 공통 lifecycle policy에서 미배차 후보
#      (scope=code, 증거 기반 Source, status=accepted 우선 → proposed) 1건을 고른다.
#   2. 프로덕션 클론의 시도별 워크트리(~/deneb-agent-worktrees/<attempt-id>)를 만들고
#   3. Codex CLI를 헤드리스로 실행 — CLAUDE.md 게이트 규약이 세션에 그대로
#      적용되고, 프롬프트가 랜딩까지 지시한다 (체크 그린 시 pr.sh land).
#   4. 배차 마커(~/.deneb/data/coding_dispatch/<id>.json)로 재배차를 막고
#      일일 배차 상한으로 토큰 예산을 지킨다. 마커 파일 존재만으로는 영구
#      스킵하지 않는다 — landed/attempted/declined는 차단하고, 실제 프로세스
#      실패·타임아웃만 재시도한다. outcome 없는 마커는 세션 타임아웃 경과 후
#      포기(abandoned)로 본다 (dispatch_outcome.blocks_redispatch). abandoned
#      마커는 authoritative lifecycle·GitHub·git 사실을 함께 확인한 뒤에만 회수한다.
#   5. 매 시도는 고유 branch/worktree로 최신 origin/main에서 시작한다. dirty,
#      ahead, remote 보존이 필요한 시도는 강제 삭제하지 않으며, 안전하게 회수할
#      수 없는 경우 그대로 남긴다. 셋업 실패 시 같은 틱에서 다음 후보로 넘어간다
#      (헤드오브큐 독성 방지).
#
# 안전:
# - 항상 exit 0 (systemd 타이머 컨벤션). flock 단일 인스턴스 + 세션 타임아웃.
# - 수용 게이트 회로·보안 CODEOWNERS 후보는 gateway의 canonical selection
#   policy(genesis/surfaces.go)에서 차단한다 — 여기서 재검사하지 않는다.
# - 프로드 트리는 읽기 전용(워크트리 분기만); 편집은 워크트리에서.
set -euo pipefail

STATE_DIR="${DENEB_STATE_DIR:-$HOME/.deneb}"
DISPATCH_DIR="$STATE_DIR/data/coding_dispatch"
PROD_DIR="${DENEB_PROD_DIR:-$HOME/deneb}"
WORKTREE_ROOT="${DENEB_DISPATCH_WORKTREE_ROOT:-$HOME/deneb-agent-worktrees}"
LOCK_FILE="/tmp/deneb-coding-dispatch.lock"
LOG_FILE="/tmp/deneb-coding-dispatch.log"
# Daily cap resolution: explicit env wins → executed graduation-ladder unlock
# (~/.deneb/data/graduation_state.json, dispatch-cap row — operator delegated
# unlock execution 2026-07-14) → compiled default 2.
GRADUATION_STATE="$HOME/.deneb/data/graduation_state.json"
if [[ -n "${DENEB_DISPATCH_DAILY_CAP:-}" ]]; then
    DAILY_CAP="$DENEB_DISPATCH_DAILY_CAP"
else
    DAILY_CAP=$(python3 - "$GRADUATION_STATE" <<'CAPEOF' 2>/dev/null || echo 2
import json, sys
try:
    rows = json.load(open(sys.argv[1])).get("rows") or {}
    row = rows.get("dispatch-cap") or {}
    v = int(row.get("value") or 0)
    print(v if row.get("unlocked") and v > 0 else 2)
except Exception:
    print(2)
CAPEOF
    )
fi
SESSION_TIMEOUT="${DENEB_DISPATCH_TIMEOUT_SEC:-7200}"
# Terminal dispatch markers accrete forever (one per candidate ever dispatched);
# the daily cap only counts today's window and the gateway ledger is the
# authoritative state, so old markers are pure disk residue. Prune terminal
# markers older than this many days. 0 disables.
MARKER_RETENTION_DAYS="${DENEB_DISPATCH_MARKER_RETENTION_DAYS:-30}"
# Consecutive instant-failure cap per candidate. Below this, an instant failure
# (<60s, rc!=0, no PR) is treated as a transient environment problem and the
# marker is released for a free retry (no daily-cap slot burned). At/above it,
# the candidate stops getting the free environment pass and flows through normal
# outcome accounting — otherwise a candidate that instant-fails every tick would
# re-dispatch forever, consuming no cap and silently starving the lane. See the
# instant-failure block in the dispatch flow below.
INSTANT_FAIL_MAX="${DENEB_DISPATCH_INSTANT_FAIL_MAX:-3}"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DISPATCH_RPC="$SCRIPT_DIR/self_correction_dispatch.py"
DISPATCH_RECLAIM="$SCRIPT_DIR/dispatch_reclaim.py"
DISPATCH_OUTCOME="$SCRIPT_DIR/dispatch_outcome.py"
DISPATCH_EXECUTOR="$SCRIPT_DIR/coding_dispatch_executor.py"
DISPATCH_STATUS_WRITER="$SCRIPT_DIR/coding_dispatch_status.py"
DISPATCH_STATUS_FILE="$STATE_DIR/data/coding_dispatch_status.json"

resolve_gh_bin() {
    if [[ -n "${DENEB_DISPATCH_GH_BIN:-}" ]]; then
        [[ -x "$DENEB_DISPATCH_GH_BIN" ]] || return 1
        printf '%s\n' "$DENEB_DISPATCH_GH_BIN"
        return 0
    fi
    if command -v gh >/dev/null 2>&1; then
        command -v gh
        return 0
    fi
    if [[ -x "$HOME/.local/bin/gh" ]]; then
        printf '%s\n' "$HOME/.local/bin/gh"
        return 0
    fi
    return 1
}

# systemd user services commonly omit ~/.local/bin from PATH. Keep GitHub
# lookup explicit so a successfully merged squash PR cannot be recorded as a
# failed or merely attempted dispatch just because the service PATH is narrow.
GH_BIN=$(resolve_gh_bin || true)

# Per-candidate consecutive instant-failure streak. Stored as a tiny integer
# sidecar next to the marker; the ".instantfail" suffix keeps it out of the
# ".json"-only daily-cap scan and the marker pick lane. bump prints the new
# count; reset clears the streak once a real (non-instant) session runs.
instant_fail_file() { printf '%s/%s.instantfail' "$DISPATCH_DIR" "$1"; }
read_instant_fails() {
    local f; f=$(instant_fail_file "$1")
    if [[ -f "$f" ]]; then cat "$f" 2>/dev/null || echo 0; else echo 0; fi
}
bump_instant_fails() {
    local f n; f=$(instant_fail_file "$1")
    n=$(read_instant_fails "$1"); n=$((n + 1))
    printf '%s' "$n" >"$f" 2>/dev/null || true
    echo "$n"
}
reset_instant_fails() { rm -f "$(instant_fail_file "$1")" 2>/dev/null || true; }

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

# Seed the CodeGraph symbol index into a fresh dispatch worktree.
#
# The contract's first step is `codegraph impact <symbol>` — a dependency check
# before any edit. That step is only real if the worktree HAS an index: a fresh
# `git worktree add` carries none (.codegraph is gitignored), and a full `init`
# would cost minutes of every session's budget. Copying the production index
# and syncing the (near-empty) delta from origin/main takes seconds.
#
# Only codegraph.db* is copied — the semantic-code.* vectors are 296MB of
# `make codesearch` state that impact/node/callers never read, so seeding them
# would nearly triple the per-worktree disk cost for nothing.
#
# Fully soft: a missing binary, missing donor index, or failed sync leaves the
# worktree index-less. The contract tells the agent to say so rather than to
# claim a blast radius it never checked.
seed_codegraph_index() {
    local wt="$1"
    command -v codegraph >/dev/null 2>&1 || { log "codegraph absent — $wt gets no index"; return 0; }
    local donor="$PROD_DIR/.codegraph"
    [[ -d "$donor" && -f "$donor/codegraph.db" ]] || { log "no donor index at $donor — $wt gets no index"; return 0; }
    mkdir -p "$wt/.codegraph" || return 0
    # db + its WAL/SHM sidecars must travel together: the donor daemon writes
    # through a WAL, so the .db alone can be a torn read.
    cp -f "$donor"/codegraph.db "$donor"/codegraph.db-wal "$donor"/codegraph.db-shm \
        "$wt/.codegraph/" 2>/dev/null || true
    [[ -f "$donor/.gitignore" ]] && cp -f "$donor/.gitignore" "$wt/.codegraph/" 2>/dev/null
    if [[ ! -f "$wt/.codegraph/codegraph.db" ]]; then
        log "codegraph index copy failed for $wt"
        rm -rf "$wt/.codegraph"
        return 0
    fi
    if codegraph sync "$wt" >>"$LOG_FILE" 2>&1; then
        log "codegraph index seeded for $wt"
    else
        log "codegraph sync failed for $wt — serving the copied index as-is"
    fi
    return 0
}

record_runtime_status() {
    local result="$1" detail="${2:-}" candidate="${3:-}"
    if [[ -f "$DISPATCH_STATUS_WRITER" ]]; then
        python3 "$DISPATCH_STATUS_WRITER" --state-file "$DISPATCH_STATUS_FILE" \
            --result "$result" --detail "$detail" --candidate "$candidate" \
            >>"$LOG_FILE" 2>&1 || log "WARN: failed to persist dispatch runtime status ($result)"
    fi
}

# Print: local calendar date, attempts charged that day, effective timezone.
# The same state/env precedence as gateway dentime keeps the cap and status
# card on the operator's day boundary (including DST transitions).
dispatch_cap_usage() {
    python3 - "$1" "$2" "${3:-}" <<'PY'
import json, os, sys
from datetime import datetime, time as daytime, timedelta
from zoneinfo import ZoneInfo

dispatch_dir, state_dir, now_ms = sys.argv[1], sys.argv[2], sys.argv[3]
zone_name = os.environ.get("DENEB_TIMEZONE", "").strip()
if not zone_name:
    try:
        with open(os.path.join(state_dir, "deneb.json"), encoding="utf-8") as f:
            zone_name = str((json.load(f) or {}).get("timezone") or "").strip()
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        pass
try:
    zone = ZoneInfo(zone_name) if zone_name else datetime.now().astimezone().tzinfo
except (KeyError, ValueError):
    zone = datetime.now().astimezone().tzinfo
    zone_name = ""
now = datetime.fromtimestamp(int(now_ms) / 1000, tz=zone) if now_ms else datetime.now(zone)
start_dt = datetime.combine(now.date(), daytime.min, tzinfo=zone)
end_dt = datetime.combine(now.date() + timedelta(days=1), daytime.min, tzinfo=zone)
start = int(start_dt.timestamp() * 1000)
end = int(end_dt.timestamp() * 1000)
n = 0
try:
    names = os.listdir(dispatch_dir)
except OSError:
    print(f"{now.date().isoformat()}\t0\t{zone_name or str(zone)}")
    raise SystemExit
for name in names:
    if not name.endswith(".json"):
        continue
    path = os.path.join(dispatch_dir, name)
    timestamps = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            rec = json.load(f)
        if isinstance(rec, dict) and isinstance(rec.get("dispatchedAt"), (int, float)):
            timestamps.append(int(rec["dispatchedAt"]))
        if isinstance(rec, dict):
            for attempt in rec.get("attempts") or []:
                if isinstance(attempt, dict) and isinstance(attempt.get("dispatchedAt"), (int, float)):
                    timestamps.append(int(attempt["dispatchedAt"]))
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        pass
    if not timestamps:
        try:
            timestamps.append(int(os.path.getmtime(path) * 1000))
        except OSError:
            continue
    n += sum(1 for ts in timestamps if start <= ts < end)
print(f"{now.date().isoformat()}\t{n}\t{zone_name or str(zone)}")
PY
}

# A clean no-change verdict is a healthy dispatcher completion, not a broken
# agent session. Keep actual process/timeout/unlanded-work failures visible.
record_session_status() {
    local candidate="$1" pr_outcome="$2" outcome="$3" rc="$4"
    case "$pr_outcome" in
        merged) record_runtime_status merged "PR merged" "$candidate" ;;
        pr_opened) record_runtime_status pr_opened "PR open" "$candidate" ;;
        *)
            case "$outcome" in
                declined)
                    record_runtime_status completed \
                        "session declined safely; no code or PR" "$candidate"
                    ;;
                timeout)
                    record_runtime_status session_failed \
                        "session timed out; no open or merged PR" "$candidate"
                    ;;
                attempted)
                    record_runtime_status session_failed \
                        "session left unlanded work; no open PR" "$candidate"
                    ;;
                *)
                    record_runtime_status session_failed \
                        "session rc=$rc; no open or merged PR" "$candidate"
                    ;;
            esac
            ;;
    esac
}

record_event() {
    python3 "$DISPATCH_RPC" --state-dir "$STATE_DIR" record "$@" >>"$LOG_FILE" 2>&1
}

release_owned_marker() {
    local marker="$1" attempt="$2" marker_attempt=""
    [[ -f "$marker" ]] || return 0
    marker_attempt=$(jq -r '.attemptId // empty' "$marker" 2>/dev/null || true)
    if [[ "$marker_attempt" == "$attempt" ]]; then
        rm -f "$marker"
    fi
}

release_clean_attempt() {
    local wt="$1" branch="$2"
    if [[ -d "$wt" ]] && ! git -C "$PROD_DIR" worktree remove "$wt" >>"$LOG_FILE" 2>&1; then
        log "preserving non-clean or changed worktree $wt"
        return 1
    fi
    if git -C "$PROD_DIR" show-ref --verify --quiet "refs/heads/$branch"; then
        git -C "$PROD_DIR" branch -d "$branch" >>"$LOG_FILE" 2>&1 || \
            log "preserving unmerged local branch $branch"
    fi
}

pr_json_for_branch() {
    local branch="$1"
    [[ -n "$GH_BIN" ]] || return 1
    command -v jq >/dev/null 2>&1 || return 1
    ( cd "$PROD_DIR" && "$GH_BIN" pr list --head "$branch" --state all --limit 1 \
        --json number,url,state,mergeCommit 2>/dev/null )
}

# A session can exit while GitHub checks are still finishing. Reconcile every
# durable marker before selecting new work so an open PR that later merges does
# not remain stuck at pr_opened forever. Tracker-side idempotency makes repeats
# cheap and keeps this safe across timer restarts.
reconcile_dispatches() {
    local marker cid attempt branch pr_json state number url merge_sha ledger_json phase
    [[ -x "$DISPATCH_RPC" || -f "$DISPATCH_RPC" ]] || return 0
    if ! ledger_json=$(python3 "$DISPATCH_RPC" --state-dir "$STATE_DIR" list --json 2>>"$LOG_FILE") || \
            ! jq -e 'type == "array"' >/dev/null 2>&1 <<<"$ledger_json"; then
        log "WARN: authoritative dispatch ledger unavailable — PR reconciliation deferred"
        return 0
    fi
    for marker in "$DISPATCH_DIR"/*.json; do
        [[ -f "$marker" ]] || continue
        command -v jq >/dev/null 2>&1 || return 0
        cid=$(jq -r '.id // empty' "$marker" 2>/dev/null || true)
        attempt=$(jq -r '.attemptId // empty' "$marker" 2>/dev/null || true)
        branch=$(jq -r '.branch // empty' "$marker" 2>/dev/null || true)
        [[ -n "$cid" && -n "$attempt" && -n "$branch" ]] || continue # legacy marker
        phase=$(jq -r --arg id "$cid" --arg attempt "$attempt" \
            '[.[] | select(.id == $id and .attemptId == $attempt)][0].dispatchPhase // empty' \
            <<<"$ledger_json")
        [[ -n "$phase" ]] || continue
        pr_json=$(pr_json_for_branch "$branch" || true)
        [[ -n "$pr_json" ]] || continue
        state=$(jq -r '.[0].state // empty' <<<"$pr_json")
        number=$(jq -r '.[0].number // 0' <<<"$pr_json")
        url=$(jq -r '.[0].url // empty' <<<"$pr_json")
        merge_sha=$(jq -r '.[0].mergeCommit.oid // empty' <<<"$pr_json")
        if [[ "$state" == "MERGED" && -n "$merge_sha" ]]; then
            case "$phase" in
                started|pr_opened|failed)
                    record_event --id "$cid" --phase merged --attempt-id "$attempt" --branch "$branch" \
                        --pr-number "$number" --pr-url "$url" --commit-sha "$merge_sha" \
                        --note "reconciled merged PR" || log "WARN: failed to reconcile merged dispatch $cid"
                    ;;
                merged|deployed|watch_passed)
                    ;; # already at or beyond merged — idempotent marker refresh only
                *)
                    continue # rolled_back/declined are terminal; never regress them
                    ;;
            esac
            python3 "$DISPATCH_OUTCOME" --marker "$marker" --rc 0 --pr-state MERGED \
                --upgrade-only --preserve-mtime >>"$LOG_FILE" 2>&1 || true
        elif [[ "$state" == "OPEN" ]]; then
            case "$phase" in
                started|failed)
                    record_event --id "$cid" --phase pr_opened --attempt-id "$attempt" --branch "$branch" \
                        --pr-number "$number" --pr-url "$url" --note "reconciled open PR" \
                        || log "WARN: failed to reconcile open dispatch $cid"
                    ;;
                pr_opened)
                    ;;
                *)
                    continue # never regress merged/deployed/terminal states
                    ;;
            esac
            python3 "$DISPATCH_OUTCOME" --marker "$marker" --rc 0 --ahead 0 --pr-state OPEN \
                --preserve-mtime >>"$LOG_FILE" 2>&1 || true
        fi
    done
}

# prune_stale_markers removes terminal dispatch markers older than the
# retention window. Safe because: (a) the daily cap only sums today's window,
# (b) the gateway ledger — not the marker — is the authoritative candidate
# state, and (c) a real in-flight dispatch is bounded to SESSION_TIMEOUT, so
# nothing legitimately open is this old. Never touches phase="started" markers
# (reclaim owns those on a much shorter timescale).
prune_stale_markers() {
    [[ "$MARKER_RETENTION_DAYS" -gt 0 ]] || return 0
    local pruned
    pruned=$(python3 - "$DISPATCH_DIR" "$MARKER_RETENTION_DAYS" <<'PY' 2>>"$LOG_FILE" || true
import json, os, sys, time
dispatch_dir, days = sys.argv[1], int(sys.argv[2])
cutoff = time.time() - days * 86400
pruned = 0
try:
    names = os.listdir(dispatch_dir)
except OSError:
    print(0); raise SystemExit
for name in names:
    if not name.endswith(".json"):
        continue
    path = os.path.join(dispatch_dir, name)
    try:
        if os.path.getmtime(path) >= cutoff:
            continue
        with open(path, encoding="utf-8", errors="replace") as f:
            rec = json.load(f)
        if isinstance(rec, dict) and rec.get("_dispatchPhase") == "started":
            continue  # in-flight — reclaim owns it
        os.unlink(path)
        # A sibling .instantfail retry marker, if any, goes with it.
        sib = path[:-5] + ".instantfail"
        if os.path.exists(sib):
            os.unlink(sib)
        pruned += 1
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        continue
print(pruned)
PY
)
    if [[ "${pruned:-0}" -gt 0 ]]; then
        log "pruned $pruned terminal dispatch marker(s) older than ${MARKER_RETENTION_DAYS}d"
    fi
}

reclaim_abandoned_dispatches() {
    [[ -f "$DISPATCH_RPC" && -f "$DISPATCH_RECLAIM" ]] || return 0
    local ledger_file reclaim_file
    ledger_file=$(mktemp)
    reclaim_file=$(mktemp)
    if ! python3 "$DISPATCH_RPC" --state-dir "$STATE_DIR" list --json >"$ledger_file" 2>>"$LOG_FILE"; then
        log "WARN: authoritative dispatch ledger unavailable — abandoned cleanup deferred"
        rm -f "$ledger_file" "$reclaim_file"
        return 0
    fi
    if ! python3 "$DISPATCH_RECLAIM" \
        --dispatch-dir "$DISPATCH_DIR" --ledger-json "$ledger_file" \
        --prod-dir "$PROD_DIR" --worktree-root "$WORKTREE_ROOT" \
        --abandon-after "$SESSION_TIMEOUT" >"$reclaim_file" 2>>"$LOG_FILE"; then
        log "WARN: abandoned dispatch safety scan failed — cleanup deferred"
        rm -f "$ledger_file" "$reclaim_file"
        return 0
    fi

    local cid attempt phase branch wt marker
    while IFS=$'\t' read -r cid attempt phase branch; do
        [[ -n "$cid" && -n "$attempt" && -n "$branch" ]] || continue
        marker="$DISPATCH_DIR/$cid.json"
        wt="$WORKTREE_ROOT/dispatch-$attempt"
        if [[ ! -e "$wt" && -e "$WORKTREE_ROOT/dispatch-$cid" ]]; then
            wt="$WORKTREE_ROOT/dispatch-$cid"
        fi
        if [[ "$phase" == "started" ]] && ! record_event \
            --id "$cid" --phase failed --attempt-id "$attempt" --branch "$branch" \
            --note "abandoned clean attempt reclaimed after ${SESSION_TIMEOUT}s"; then
            log "WARN: failed to close abandoned dispatch $cid in authoritative ledger"
            continue
        fi
        # The scanner proved clean/ahead=0, but use non-force deletion so a
        # last-moment edit or checkout race preserves work instead of erasing it.
        if [[ -d "$wt" ]] && ! git -C "$PROD_DIR" worktree remove "$wt" >>"$LOG_FILE" 2>&1; then
            log "WARN: abandoned worktree changed during reclaim — preserved $cid"
            continue
        fi
        if git -C "$PROD_DIR" show-ref --verify --quiet "refs/heads/$branch"; then
            if ! git -C "$PROD_DIR" branch -d "$branch" >>"$LOG_FILE" 2>&1; then
                log "WARN: abandoned branch is not safely deletable — preserved marker for $cid"
                continue
            fi
        fi
        rm -f "$marker"
        log "abandon reclaim: released clean local attempt $cid ($attempt); remote branch, if any, preserved"
    done <"$reclaim_file"
    rm -f "$ledger_file" "$reclaim_file"
}

PR_OUTCOME="none"
record_pr_outcome() {
    local cid="$1" attempt="$2" branch="$3" rc="$4" elapsed="$5" ahead="$6" decline_note="${7:-}"
    local pr_json state number url merge_sha note
    local -a result_args
    pr_json=$(pr_json_for_branch "$branch" || printf '[]')
    state=$(jq -r '.[0].state // empty' <<<"${pr_json:-[]}" 2>/dev/null || true)
    number=$(jq -r '.[0].number // 0' <<<"${pr_json:-[]}" 2>/dev/null || printf '0')
    url=$(jq -r '.[0].url // empty' <<<"${pr_json:-[]}" 2>/dev/null || true)
    merge_sha=$(jq -r '.[0].mergeCommit.oid // empty' <<<"${pr_json:-[]}" 2>/dev/null || true)
    note="dispatch session rc=$rc elapsed=${elapsed}s; prState=${state:-unknown}"
    # The executor's structured decline reason (from .dispatch-decline.md)
    # rides the ledger note so post-mortems read the ledger, not the 39K-line
    # session transcript.
    [[ -n "$decline_note" ]] && note="$note; decline: $decline_note"
    result_args=(--state-dir "$STATE_DIR" result --id "$cid" --attempt-id "$attempt" \
        --branch "$branch" --rc "$rc" --pr-number "$number" --pr-url "$url" \
        --commit-sha "$merge_sha" --note "$note")
    [[ -n "$ahead" ]] && result_args+=(--ahead "$ahead")
    [[ -n "$state" ]] && result_args+=(--pr-state "$state")
    if ! PR_OUTCOME=$(python3 "$DISPATCH_RPC" "${result_args[@]}" 2>>"$LOG_FILE"); then
        PR_OUTCOME="unknown"
        return 1
    fi
}

main() {
    exec 9>"$LOCK_FILE"
    if ! flock -n 9; then
        log "another dispatch holds the lock — idle"
        record_runtime_status busy "another dispatch holds the lock"
        exit 0
    fi
    mkdir -p "$DISPATCH_DIR"
    local script_dir="$SCRIPT_DIR"
    reconcile_dispatches
    reclaim_abandoned_dispatches
    prune_stale_markers

    # Upgrade non-terminal outcomes first. Newest-first within 14d (mtime), then
    # truncate — sorting AFTER age filter so five stale markers cannot starve
    # newer attempted ones (bot #3609). Also reprobe failed/timeout when the PR
    # later merged (OPEN-at-end sessions used to record failed before attempted).
    if [[ -n "$GH_BIN" ]]; then
        local m mcid mstate mbranch
        while IFS= read -r m; do
            [[ -f "$m" ]] || continue
            if ! grep -qE '"outcome": "(attempted|failed|timeout)"' "$m" 2>/dev/null; then
                continue
            fi
            mcid=$(basename "$m" .json)
            mbranch=$(jq -r '.branch // empty' "$m" 2>/dev/null || true)
            [[ -n "$mbranch" ]] || continue
            mstate=$(cd "$PROD_DIR" && "$GH_BIN" pr list --head "$mbranch" --state merged \
                --json state --jq '.[0].state // ""' 2>/dev/null || true)
            if [[ "$mstate" == "MERGED" ]]; then
                python3 "$script_dir/dispatch_outcome.py" --marker "$m" --rc 0 \
                    --pr-state MERGED --upgrade-only --preserve-mtime >>"$LOG_FILE" 2>&1 || true
                log "reprobe: $mcid → landed (PR merged after session end)"
            fi
        done < <(
            find "$DISPATCH_DIR" -name '*.json' -mtime -14 -printf '%T@ %p\n' 2>/dev/null \
                | sort -rn | head -20 | while read -r _ path; do printf '%s\n' "$path"; done
        )
    fi

    # Daily cap: prefer explicit dispatchedAt (ms) so late reprobe rewrites do
    # not burn today's slots; fall back to mtime for legacy markers.
    local today spent dispatch_timezone cap_usage
    cap_usage=$(dispatch_cap_usage "$DISPATCH_DIR" "$STATE_DIR")
    IFS=$'\t' read -r today spent dispatch_timezone <<<"$cap_usage"
    if (( spent >= DAILY_CAP )); then
        log "daily cap reached ($spent/$DAILY_CAP $dispatch_timezone $today) — idle"
        record_runtime_status cap_reached "$spent/$DAILY_CAP $dispatch_timezone $today"
        exit 0
    fi

    if [[ ! -f "$DISPATCH_EXECUTOR" ]] || ! python3 "$DISPATCH_EXECUTOR" --check >>"$LOG_FILE" 2>&1; then
        log "Codex executor unavailable or not logged in — idle"
        record_runtime_status environment_failed "Codex executor unavailable or not logged in"
        exit 0
    fi

    mkdir -p "$WORKTREE_ROOT"

    # Pick + setup: keep trying until a worktree is ready or the queue is
    # exhausted this tick (hard-capping at 5 left candidate 6+ permanently
    # starved across timer ticks when the head of queue was poison — bot #3615).
    local pick="" cid="" wt="" skip_ids="" attempt_id="" branch="" had_setup_failure=0
    local attempt=0
    while (( attempt < 40 )); do
        attempt=$((attempt + 1))
        # The gateway owns review, delivery, source, and forbidden-surface policy.
        # This client contributes only local marker residue plus setup failures from
        # the current tick as exclusions, then asks for one canonical candidate.
        local -a next_args skipped_ids
        next_args=(--state-dir "$STATE_DIR" next --dispatch-dir "$DISPATCH_DIR" \
            --abandon-after "$SESSION_TIMEOUT")
        IFS=',' read -r -a skipped_ids <<<"$skip_ids"
        local skipped_id
        for skipped_id in "${skipped_ids[@]}"; do
            [[ -n "$skipped_id" ]] && next_args+=(--exclude-id "$skipped_id")
        done
        if ! pick=$(python3 "$DISPATCH_RPC" "${next_args[@]}" 2>>"$LOG_FILE"); then
            log "authoritative dispatch policy unavailable — idle"
            record_runtime_status ledger_failed "candidate selection RPC unavailable"
            exit 0
        fi
        if [[ -z "$pick" ]]; then
            log "no dispatchable candidate (attempt=$attempt skip=${skip_ids:-none}) — idle"
            if [[ "$had_setup_failure" -eq 1 ]]; then
                record_runtime_status setup_failed "candidate setup failed; skipped ${skip_ids:-none}"
            else
                record_runtime_status idle "no dispatchable candidate"
            fi
            exit 0
        fi
        cid=$(printf '%s' "$pick" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
        attempt_id="$cid-$(date +%s)-$$-$attempt"
        branch="dispatch/$attempt_id"
        wt="$WORKTREE_ROOT/dispatch-$attempt_id"

        # Every retry gets a fresh branch and worktree path. Existing paths are
        # evidence of an improbable ID collision, so preserve them and try the
        # next candidate rather than resetting or force-deleting anything.
        if [[ -e "$wt" ]] || git -C "$PROD_DIR" show-ref --verify --quiet "refs/heads/$branch"; then
            log "attempt identity collision for $cid — preserving $wt / $branch"
            had_setup_failure=1
            skip_ids="${skip_ids:+$skip_ids,}$cid"
            pick=""; cid=""; wt=""
            continue
        fi
        if ! git -C "$PROD_DIR" fetch origin main --quiet >>"$LOG_FILE" 2>&1; then
            log "fetch origin/main failed — setup deferred for $cid"
            had_setup_failure=1
            skip_ids="${skip_ids:+$skip_ids,}$cid"
            pick=""; cid=""; wt=""
            continue
        fi
        if ! git -C "$PROD_DIR" worktree add "$wt" -b "$branch" origin/main >>"$LOG_FILE" 2>&1; then
            log "worktree creation failed for $cid — try next"
            had_setup_failure=1
            skip_ids="${skip_ids:+$skip_ids,}$cid"
            pick=""; cid=""; wt=""
            continue
        fi

        if [[ -d "$wt" ]]; then
            break
        fi
        had_setup_failure=1
        skip_ids="${skip_ids:+$skip_ids,}$cid"
        pick=""; cid=""; wt=""
    done

    if [[ -z "$pick" || -z "$cid" || ! -d "$wt" ]]; then
        log "setup exhausted after skips (${skip_ids:-none}) — idle"
        record_runtime_status setup_failed "setup exhausted after skips ${skip_ids:-none}"
        exit 0
    fi

    # Dependency-graph supply, before the session opens: the contract's first
    # step needs an index in THIS worktree.
    seed_codegraph_index "$wt"

    # Prompt composition + dispatch marker live in dispatch_prompt.py: the
    # contract half of the prompt is the externalized meta artifact
    # (meta/dispatch-contract-prompt.md, RSI P5-4 — gateway materializes it
    # from the compiled default), and the marker (written BEFORE the session,
    # so a crashed session must not redispatch forever) carries promptVersion
    # provenance. An unusable artifact defers the dispatch — candidate and
    # daily-cap slot stay unburned until the gateway materializes it.
    local prompt
    if ! prompt=$(printf '%s' "$pick" | python3 "$SCRIPT_DIR/dispatch_prompt.py" \
            --meta-dir "$STATE_DIR/skills/genesis/meta" \
            --marker "$DISPATCH_DIR/$cid.json" \
            --attempt-id "$attempt_id" --branch "$branch" 2>>"$LOG_FILE"); then
        log "dispatch contract artifact unavailable — $cid deferred (no marker burned)"
        release_clean_attempt "$wt" "$branch" || true
        record_runtime_status prompt_failed "dispatch contract artifact unavailable" "$cid"
        exit 0
    fi
    if [[ -z "$prompt" ]]; then
        # dispatch_prompt writes the marker before printing; an empty prompt
        # must not leave a blocking marker behind.
        release_owned_marker "$DISPATCH_DIR/$cid.json" "$attempt_id"
        release_clean_attempt "$wt" "$branch" || true
        log "empty dispatch prompt for $cid — marker released, deferred"
        record_runtime_status prompt_failed "empty dispatch prompt" "$cid"
        exit 0
    fi

    # Fail closed before spending an agent session: a dispatch without its
    # authoritative start event would recreate the result-ledger gap.
    if ! record_event --id "$cid" --phase started --attempt-id "$attempt_id" --branch "$branch" \
            --note "coding dispatch started"; then
        release_owned_marker "$DISPATCH_DIR/$cid.json" "$attempt_id"
        release_clean_attempt "$wt" "$branch" || true
        log "dispatch ledger unavailable — $cid deferred (marker released)"
        record_runtime_status ledger_failed "start event rejected" "$cid"
        exit 0
    fi
    record_runtime_status dispatched "agent session started" "$cid"
    log "dispatching $cid → $wt (Codex CLI, cap $((spent+1))/$DAILY_CAP today)"
    set +e
    local started_at rc
    started_at=$(date +%s)
    printf '%s' "$prompt" | python3 "$DISPATCH_EXECUTOR" \
        --worktree "$wt" --prod-dir "$PROD_DIR" --timeout "$SESSION_TIMEOUT" \
        >>"$LOG_FILE" 2>&1
    rc=$?
    set -e
    local elapsed=$(( $(date +%s) - started_at ))
    log "dispatch $cid finished (rc=$rc, ${elapsed}s)"
    # Structured decline reason: the contract asks the executor to write
    # .dispatch-decline.md when it lands nothing. Capture and remove it so a
    # declined worktree still reads as clean for cleanup below.
    local decline_note=""
    if [[ -f "$wt/.dispatch-decline.md" ]]; then
        decline_note=$(head -c 800 "$wt/.dispatch-decline.md" 2>/dev/null | tr '\n\t' '  ' | sed 's/  */ /g' || true)
        rm -f "$wt/.dispatch-decline.md"
        [[ -n "$decline_note" ]] && log "dispatch $cid decline reason: $decline_note"
    fi
    # Decide normal clean no-op versus failure from the actual branch delta
    # before writing the authoritative terminal lifecycle event.
    local ahead=""
    git -C "$PROD_DIR" fetch origin main --quiet >>"$LOG_FILE" 2>&1 || true
    ahead=$(git -C "$wt" rev-list --count origin/main..HEAD 2>/dev/null || echo "")
    local terminal_recorded=1
    if ! record_pr_outcome "$cid" "$attempt_id" "$branch" "$rc" "$elapsed" "$ahead" "$decline_note"; then
        terminal_recorded=0
        log "WARN: failed to record terminal PR outcome for $cid"
    fi

    # Instant failure (<60s, rc!=0) means the session never really started —
    # binary not logged in, missing
    # deps, etc. Burning the candidate AND a daily-cap slot on that would starve
    # the lane silently: release the marker and the worktree so the same
    # candidate re-dispatches once the environment is fixed.
    if (( rc != 0 && elapsed < 60 )) && [[ "$PR_OUTCOME" == "failed" || "$PR_OUTCOME" == "none" ]]; then
        local ifails
        ifails=$(bump_instant_fails "$cid")
        if (( ifails < INSTANT_FAIL_MAX )); then
            release_owned_marker "$DISPATCH_DIR/$cid.json" "$attempt_id"
            release_clean_attempt "$wt" "$branch" || true
            log "instant failure ${ifails}/${INSTANT_FAIL_MAX} — marker released for $cid (environment problem, retrying)"
            record_runtime_status environment_failed "instant environment failure rc=$rc (${ifails}/${INSTANT_FAIL_MAX})" "$cid"
            exit 0
        fi
        # Streak hit the cap: stop granting the free environment pass. A candidate
        # that instant-fails every tick would otherwise re-dispatch forever,
        # consuming no daily-cap slot and starving the lane silently (observed:
        # one candidate re-dispatched 4x over 4h against a broken executor). Fall
        # through to normal outcome accounting so it records a real failed marker —
        # bounded by the daily cap and visible in the ledger/dashboard.
        log "instant failure ${ifails}/${INSTANT_FAIL_MAX} for $cid — persistent; recording as candidate failure (no longer masked as environment)"
    else
        # A real session ran (>=60s or produced a PR): clear the streak.
        reset_instant_fails "$cid"
    fi

    # Outcome accounting (graduation-ladder evidence: cap raise needs a
    # measured land rate). Gather observable facts — PR state for the dispatch
    # branch, commits ahead of origin/main in the worktree — and let
    # dispatch_outcome.py fold the verdict into the marker. Never fatal.
    local pr_state=""
    if [[ -n "$GH_BIN" ]]; then
        pr_state=$(cd "$PROD_DIR" && "$GH_BIN" pr list --head "$branch" --state all \
            --json state --jq '.[0].state // ""' 2>/dev/null || true)
    fi
    local outcome
    outcome=$(python3 "$script_dir/dispatch_outcome.py" --marker "$DISPATCH_DIR/$cid.json" \
        --rc "$rc" --elapsed "$elapsed" --ahead "$ahead" --pr-state "$pr_state" \
        --authoritative-phase "$PR_OUTCOME" --decline-note "$decline_note" 2>>"$LOG_FILE" || echo "unknown")
    log "dispatch $cid outcome: $outcome (prState=${pr_state:-n/a}, ahead=${ahead:-n/a})"

    if [[ "$terminal_recorded" -eq 0 ]]; then
        record_runtime_status ledger_failed "terminal event rejected" "$cid"
    else
        record_session_status "$cid" "$PR_OUTCOME" "$outcome" "$rc"
    fi

    # Worktree cleanup only when the branch merged or session ended clean with
    # no unpushed work; otherwise keep for inspection.
    if [[ $rc -eq 0 ]] && ! git -C "$wt" status --porcelain 2>/dev/null | grep -q .; then
        release_clean_attempt "$wt" "$branch" || true
    fi
    exit 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
