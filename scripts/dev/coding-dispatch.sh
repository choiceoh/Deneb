#!/usr/bin/env bash
# coding-dispatch.sh — RSI L4 실행 레인: 자기교정 큐의 소스 후보를 코딩
# 에이전트(Claude Code 헤드리스)에 자동 배차한다.
#
# 오퍼레이터 승인(2026-07-12, memory: source-self-edit-authorization): dev
# 트리에서만 편집, 전체 게이트 그린일 때만 랜딩·핫스왑. 이 스크립트는 그
# 계약의 배차원이다:
#   1. ~/.deneb/data/self_correction_candidates.jsonl에서 미배차 후보
#      (scope=code, 증거 기반 Source, status=accepted 우선 → proposed) 1건을 고른다.
#   2. 프로덕션 클론의 시도별 워크트리(~/deneb-agent-worktrees/<attempt-id>)를 만들고
#   3. Claude Code를 -p(헤드리스)로 실행 — CLAUDE.md 게이트 규약이 세션에
#      그대로 적용되고, 프롬프트가 랜딩까지 지시한다 (체크 그린 시 pr.sh land).
#   4. 배차 마커(~/.deneb/data/coding_dispatch/<id>.json)로 재배차를 막고
#      일일 배차 상한으로 토큰 예산을 지킨다. 마커 파일 존재만으로는 영구
#      스킵하지 않는다 — landed/attempted만 차단, declined/failed/timeout은
#      재시도, outcome 없는 마커는 세션 타임아웃 경과 후 포기(abandoned)로
#      본다 (dispatch_outcome.blocks_redispatch). abandoned 마커는 authoritative
#      lifecycle·GitHub·git 사실을 함께 확인한 뒤에만 회수한다.
#   5. 매 시도는 고유 branch/worktree로 최신 origin/main에서 시작한다. dirty,
#      ahead, remote 보존이 필요한 시도는 강제 삭제하지 않으며, 안전하게 회수할
#      수 없는 경우 그대로 남긴다. 셋업 실패 시 같은 틱에서 다음 후보로 넘어간다
#      (헤드오브큐 독성 방지).
#
# 안전:
# - 항상 exit 0 (systemd 타이머 컨벤션). flock 단일 인스턴스 + 세션 타임아웃.
# - 수용 게이트 회로·보안 CODEOWNERS는 record-time에 이미 forbidden이라
#   큐에 존재하지 않는다(genesis/surfaces.go) — 여기서 재검사하지 않는다.
# - 프로드 트리는 읽기 전용(워크트리 분기만); 편집은 워크트리에서.
set -euo pipefail

STATE_DIR="${DENEB_STATE_DIR:-$HOME/.deneb}"
QUEUE_FILE="$STATE_DIR/data/self_correction_candidates.jsonl"
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
# Claude Code binary: newest installed ccd-cli unless overridden.
CLAUDE_BIN="${DENEB_DISPATCH_CLAUDE_BIN:-}"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DISPATCH_RPC="$SCRIPT_DIR/self_correction_dispatch.py"
DISPATCH_RECLAIM="$SCRIPT_DIR/dispatch_reclaim.py"
DISPATCH_OUTCOME="$SCRIPT_DIR/dispatch_outcome.py"
DISPATCH_STATUS_WRITER="$SCRIPT_DIR/coding_dispatch_status.py"
DISPATCH_STATUS_FILE="$STATE_DIR/data/coding_dispatch_status.json"

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

record_runtime_status() {
    local result="$1" detail="${2:-}" candidate="${3:-}"
    if [[ -f "$DISPATCH_STATUS_WRITER" ]]; then
        python3 "$DISPATCH_STATUS_WRITER" --state-file "$DISPATCH_STATUS_FILE" \
            --result "$result" --detail "$detail" --candidate "$candidate" \
            >>"$LOG_FILE" 2>&1 || log "WARN: failed to persist dispatch runtime status ($result)"
    fi
}

resolve_claude() {
    if [[ -n "$CLAUDE_BIN" && -x "$CLAUDE_BIN" ]]; then
        printf '%s' "$CLAUDE_BIN"
        return 0
    fi
    local newest candidate
    newest=$(
        for candidate in "$HOME/.claude/remote/ccd-cli/"*; do
            [[ -x "$candidate" ]] && basename "$candidate"
        done | sort -V | tail -1
    )
    if [[ -n "$newest" && -x "$HOME/.claude/remote/ccd-cli/$newest" ]]; then
        printf '%s' "$HOME/.claude/remote/ccd-cli/$newest"
        return 0
    fi
    command -v claude 2>/dev/null || return 1
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
    command -v gh >/dev/null 2>&1 || return 1
    command -v jq >/dev/null 2>&1 || return 1
    ( cd "$PROD_DIR" && gh pr list --head "$branch" --state all --limit 1 \
        --json number,url,state,mergeCommit 2>/dev/null )
}

# A session can exit while GitHub checks are still finishing. Reconcile every
# durable marker before selecting new work so an open PR that later merges does
# not remain stuck at pr_opened forever. Tracker-side idempotency makes repeats
# cheap and keeps this safe across timer restarts.
reconcile_dispatches() {
    local marker cid attempt branch pr_json state number url merge_sha
    [[ -x "$DISPATCH_RPC" || -f "$DISPATCH_RPC" ]] || return 0
    for marker in "$DISPATCH_DIR"/*.json; do
        [[ -f "$marker" ]] || continue
        command -v jq >/dev/null 2>&1 || return 0
        cid=$(jq -r '.id // empty' "$marker")
        attempt=$(jq -r '.attemptId // empty' "$marker")
        branch=$(jq -r '.branch // empty' "$marker")
        [[ -n "$cid" && -n "$attempt" && -n "$branch" ]] || continue # legacy marker
        pr_json=$(pr_json_for_branch "$branch" || true)
        [[ -n "$pr_json" ]] || continue
        state=$(jq -r '.[0].state // empty' <<<"$pr_json")
        number=$(jq -r '.[0].number // 0' <<<"$pr_json")
        url=$(jq -r '.[0].url // empty' <<<"$pr_json")
        merge_sha=$(jq -r '.[0].mergeCommit.oid // empty' <<<"$pr_json")
        if [[ "$state" == "MERGED" && -n "$merge_sha" ]]; then
            record_event --id "$cid" --phase merged --attempt-id "$attempt" --branch "$branch" \
                --pr-number "$number" --pr-url "$url" --commit-sha "$merge_sha" \
                --note "reconciled merged PR" || log "WARN: failed to reconcile merged dispatch $cid"
            python3 "$DISPATCH_OUTCOME" --marker "$marker" --rc 0 --pr-state MERGED \
                --upgrade-only --preserve-mtime >>"$LOG_FILE" 2>&1 || true
        elif [[ "$state" == "OPEN" ]]; then
            record_event --id "$cid" --phase pr_opened --attempt-id "$attempt" --branch "$branch" \
                --pr-number "$number" --pr-url "$url" --note "reconciled open PR" \
                || log "WARN: failed to reconcile open dispatch $cid"
            python3 "$DISPATCH_OUTCOME" --marker "$marker" --rc 0 --ahead 0 --pr-state OPEN \
                --preserve-mtime >>"$LOG_FILE" 2>&1 || true
        fi
    done
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
    local cid="$1" attempt="$2" branch="$3" rc="$4" elapsed="$5"
    local pr_json state number url merge_sha
    pr_json=$(pr_json_for_branch "$branch" || true)
    state=$(jq -r '.[0].state // empty' <<<"${pr_json:-[]}" 2>/dev/null || true)
    number=$(jq -r '.[0].number // 0' <<<"${pr_json:-[]}" 2>/dev/null || printf '0')
    url=$(jq -r '.[0].url // empty' <<<"${pr_json:-[]}" 2>/dev/null || true)
    merge_sha=$(jq -r '.[0].mergeCommit.oid // empty' <<<"${pr_json:-[]}" 2>/dev/null || true)
    case "$state" in
        MERGED)
            PR_OUTCOME="merged"
            record_event --id "$cid" --phase merged --attempt-id "$attempt" --branch "$branch" \
                --pr-number "$number" --pr-url "$url" --commit-sha "$merge_sha" \
                --note "dispatch session rc=$rc elapsed=${elapsed}s; PR merged"
            ;;
        OPEN)
            PR_OUTCOME="open"
            record_event --id "$cid" --phase pr_opened --attempt-id "$attempt" --branch "$branch" \
                --pr-number "$number" --pr-url "$url" \
                --note "dispatch session rc=$rc elapsed=${elapsed}s; PR open"
            ;;
        *)
            PR_OUTCOME="failed"
            record_event --id "$cid" --phase failed --attempt-id "$attempt" --branch "$branch" \
                --note "dispatch session rc=$rc elapsed=${elapsed}s; no merged/open PR"
            ;;
    esac
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

    # Upgrade non-terminal outcomes first. Newest-first within 14d (mtime), then
    # truncate — sorting AFTER age filter so five stale markers cannot starve
    # newer attempted ones (bot #3609). Also reprobe failed/timeout when the PR
    # later merged (OPEN-at-end sessions used to record failed before attempted).
    if command -v gh >/dev/null 2>&1; then
        local m mcid mstate mbranch
        while IFS= read -r m; do
            [[ -f "$m" ]] || continue
            if ! grep -qE '"outcome": "(attempted|failed|timeout)"' "$m" 2>/dev/null; then
                continue
            fi
            mcid=$(basename "$m" .json)
            mbranch=$(jq -r '.branch // empty' "$m" 2>/dev/null || true)
            [[ -n "$mbranch" ]] || continue
            mstate=$(cd "$PROD_DIR" && gh pr list --head "$mbranch" --state merged \
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
    local today spent
    today=$(date -u +%Y-%m-%d)
    spent=$(python3 - "$DISPATCH_DIR" "$today" <<'PY'
import json, os, sys, time
from datetime import datetime, timezone
dispatch_dir, today = sys.argv[1], sys.argv[2]
day = datetime.strptime(today, "%Y-%m-%d").replace(tzinfo=timezone.utc)
start = int(day.timestamp() * 1000)
end = start + 86400000
n = 0
try:
    names = os.listdir(dispatch_dir)
except OSError:
    print(0); raise SystemExit
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
print(n)
PY
)
    if (( spent >= DAILY_CAP )); then
        log "daily cap reached ($spent/$DAILY_CAP UTC $today) — idle"
        record_runtime_status cap_reached "$spent/$DAILY_CAP UTC $today"
        exit 0
    fi

    if [[ ! -f "$QUEUE_FILE" ]]; then
        log "queue file missing — idle"
        record_runtime_status queue_missing "$QUEUE_FILE"
        exit 0
    fi

    local claude_bin
    if ! claude_bin=$(resolve_claude); then
        log "no Claude Code binary available — idle"
        record_runtime_status environment_failed "no Claude Code binary available"
        exit 0
    fi

    mkdir -p "$WORKTREE_ROOT"

    # Pick + setup: keep trying until a worktree is ready or the queue is
    # exhausted this tick (hard-capping at 5 left candidate 6+ permanently
    # starved across timer ticks when the head of queue was poison — bot #3615).
    local pick="" cid="" wt="" skip_ids="" attempt_id="" branch="" ledger_phase="" had_setup_failure=0
    local attempt=0
    while (( attempt < 40 )); do
        attempt=$((attempt + 1))
        # Newest undispatched proposed/accepted code candidate with an evidence-
        # bearing source. Marker skip: dispatch_outcome.blocks_redispatch.
        # Optional argv[5]=comma-separated ids to skip this tick after setup fail.
        pick=$(python3 - "$QUEUE_FILE" "$DISPATCH_DIR" "$script_dir" "$SESSION_TIMEOUT" "$skip_ids" <<'PYEOF'
import json, os, re, sys
queue, dispatch_dir, script_dir = sys.argv[1], sys.argv[2], sys.argv[3]
abandon_after = int(sys.argv[4])
skip = {s for s in (sys.argv[5] if len(sys.argv) > 5 else "").split(",") if s}
sys.path.insert(0, script_dir)
import dispatch_outcome
import dispatch_prompt
# Executed graduation-ladder unlocks admit staged sources at runtime (rows
# keyed "source:<prefix>" in ~/.deneb/data/graduation_state.json — the same
# file genesis rsiSourceDispatchable and rsi_status.py read, so the three
# allowlists cannot drift).
graduated_sources = []
try:
    _grows = json.load(open(os.path.expanduser("~/.deneb/data/graduation_state.json"))).get("rows") or {}
    graduated_sources = [k[len("source:"):] for k, v in _grows.items()
                         if k.startswith("source:") and (v or {}).get("unlocked")]
except Exception:
    pass
cand, status, dispatch_phase = {}, {}, {}
for line in open(queue, errors="replace"):
    line = line.strip()
    if not line:
        continue
    try:
        rec = json.loads(line)
    except json.JSONDecodeError:
        continue
    rid = rec.get("id") or ""
    if not isinstance(rid, str) or not re.fullmatch(r"[A-Za-z0-9._-]+", rid):
        continue
    if rec.get("type") == "self_correction_candidate":
        cand[rid] = rec
    if rec.get("type") == "self_correction_dispatch":
        dispatch_phase[rid] = rec.get("dispatchPhase") or ""
    if rec.get("status"):
        status[rid] = rec["status"]
def pick_order(kv):
    rid, rec = kv
    st = status.get(rid, rec.get("status") or "proposed")
    return (0 if st == "accepted" else 1, -(rec.get("createdAt") or 0))
for rid, rec in sorted(cand.items(), key=pick_order):
    if rid in skip:
        continue
    if status.get(rid, rec.get("status") or "proposed") not in ("proposed", "accepted"):
        continue
    if rec.get("scope") != "code":
        continue
    src = rec.get("source") or ""
    # Namespace match must be separator-aware: bare startswith let any caller
    # self-select auto-dispatch by prefixing a graduated namespace
    # ("health-finding-x") — RSI code eval M7.
    def ns_match(val, ns):
        return val == ns or val.startswith(ns + ":")
    allowed = ("evolve-tool-gap", "self-harness", "health-finding", "tool-quality")
    if not (any(ns_match(src, ns) for ns in allowed) or any(ns_match(src, g) for g in graduated_sources)):
        continue
    # A candidate whose prose names acceptance machinery must NOT be
    # auto-dispatched — but it also must not starve the queue by being
    # re-picked every tick only to DEFER at prompt composition (the prompt-side
    # guard is defense-in-depth, not the selection gate). Skip it here so
    # lower-priority safe candidates still run; it stays queued for operator
    # review (Codex review of RSI eval C2).
    if dispatch_prompt.forbidden_surface_mentions(rec):
        continue
    phase = dispatch_phase.get(rid, "")
    if phase in ("started", "pr_opened", "merged", "deployed", "watch_passed"):
        continue
    marker_path = os.path.join(dispatch_dir, rid + ".json")
    if os.path.isfile(marker_path) and not phase:
        try:
            marker = json.load(open(marker_path, errors="replace"))
        except (OSError, json.JSONDecodeError, TypeError, ValueError):
            continue
        if isinstance(marker, dict) and marker.get("attemptId"):
            continue
    if dispatch_outcome.blocks_redispatch(
            marker_path,
            abandon_after_sec=abandon_after,
            authoritative_phase=phase):
        continue
    out = dict(rec)
    out["status"] = status.get(rid, rec.get("status") or "proposed")
    out["_dispatchPhase"] = phase
    print(json.dumps(out, ensure_ascii=False))
    break
PYEOF
        )
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
        ledger_phase=$(printf '%s' "$pick" | python3 -c "import json,sys;print(json.load(sys.stdin).get('_dispatchPhase',''))")
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
    log "dispatching $cid → $wt (claude $(basename "$claude_bin"), cap $((spent+1))/$DAILY_CAP today)"
    set +e
    local started_at rc
    started_at=$(date +%s)
    ( cd "$wt" && timeout "$SESSION_TIMEOUT" "$claude_bin" -p "$prompt" \
        --permission-mode acceptEdits >>"$LOG_FILE" 2>&1 )
    rc=$?
    set -e
    local elapsed=$(( $(date +%s) - started_at ))
    log "dispatch $cid finished (rc=$rc, ${elapsed}s)"
    local terminal_recorded=1
    if ! record_pr_outcome "$cid" "$attempt_id" "$branch" "$rc" "$elapsed"; then
        terminal_recorded=0
        log "WARN: failed to record terminal PR outcome for $cid"
    fi

    if [[ "$terminal_recorded" -eq 0 ]]; then
        record_runtime_status ledger_failed "terminal event rejected" "$cid"
    else
        case "$PR_OUTCOME" in
            merged) record_runtime_status merged "PR merged" "$cid" ;;
            open) record_runtime_status pr_opened "PR open" "$cid" ;;
            *) record_runtime_status session_failed "session rc=$rc; no open or merged PR" "$cid" ;;
        esac
    fi

    # Instant failure (<60s, rc!=0) means the session never really started —
    # binary not logged in ("Not logged in", observed live 2026-07-12), missing
    # deps, etc. Burning the candidate AND a daily-cap slot on that would starve
    # the lane silently: release the marker and the worktree so the same
    # candidate re-dispatches once the environment is fixed.
    if (( rc != 0 && elapsed < 60 )) && [[ "$PR_OUTCOME" == "failed" || "$PR_OUTCOME" == "none" ]]; then
        release_owned_marker "$DISPATCH_DIR/$cid.json" "$attempt_id"
        release_clean_attempt "$wt" "$branch" || true
        log "instant failure — marker released for $cid (environment problem, not the candidate)"
        record_runtime_status session_failed "instant environment failure rc=$rc" "$cid"
        exit 0
    fi

    # Outcome accounting (graduation-ladder evidence: cap raise needs a
    # measured land rate). Gather observable facts — PR state for the dispatch
    # branch, commits ahead of origin/main in the worktree — and let
    # dispatch_outcome.py fold the verdict into the marker. Never fatal.
    local pr_state="" ahead=""
    git -C "$PROD_DIR" fetch origin main --quiet >>"$LOG_FILE" 2>&1 || true
    if command -v gh >/dev/null 2>&1; then
        pr_state=$(cd "$PROD_DIR" && gh pr list --head "$branch" --state all \
            --json state --jq '.[0].state // ""' 2>/dev/null || true)
    fi
    ahead=$(git -C "$wt" rev-list --count origin/main..HEAD 2>/dev/null || echo "")
    local outcome
    outcome=$(python3 "$script_dir/dispatch_outcome.py" --marker "$DISPATCH_DIR/$cid.json" \
        --rc "$rc" --elapsed "$elapsed" --ahead "$ahead" --pr-state "$pr_state" 2>>"$LOG_FILE" || echo "unknown")
    log "dispatch $cid outcome: $outcome (prState=${pr_state:-n/a}, ahead=${ahead:-n/a})"

    # Worktree cleanup only when the branch merged or session ended clean with
    # no unpushed work; otherwise keep for inspection.
    if [[ $rc -eq 0 ]] && ! git -C "$wt" status --porcelain 2>/dev/null | grep -q .; then
        release_clean_attempt "$wt" "$branch" || true
    fi
    exit 0
}

main "$@"
