#!/usr/bin/env bash
# coding-dispatch.sh — RSI L4 실행 레인: 자기교정 큐의 소스 후보를 코딩
# 에이전트(Claude Code 헤드리스)에 자동 배차한다.
#
# 오퍼레이터 승인(2026-07-12, memory: source-self-edit-authorization): dev
# 트리에서만 편집, 전체 게이트 그린일 때만 랜딩·핫스왑. 이 스크립트는 그
# 계약의 배차원이다:
#   1. ~/.deneb/data/self_correction_candidates.jsonl에서 미배차 후보
#      (scope=code, 증거 기반 Source, status=accepted 우선 → proposed) 1건을 고른다.
#   2. 프로덕션 클론의 워크트리(~/deneb-agent-worktrees/<id>)를 만들고
#   3. Claude Code를 -p(헤드리스)로 실행 — CLAUDE.md 게이트 규약이 세션에
#      그대로 적용되고, 프롬프트가 랜딩까지 지시한다 (체크 그린 시 pr.sh land).
#   4. 배차 마커(~/.deneb/data/coding_dispatch/<id>.json)로 재배차를 막고
#      일일 배차 상한으로 토큰 예산을 지킨다. 마커 파일 존재만으로는 영구
#      스킵하지 않는다 — landed/attempted만 차단, declined/failed/timeout은
#      재시도, outcome 없는 마커는 세션 타임아웃 경과 후 포기(abandoned)로
#      본다 (dispatch_outcome.blocks_redispatch). abandoned 마커·앞선 0의
#      스테일 워크트리는 틱 시작 시 회수해 재배차가 origin/main에서 다시
#      시작되게 한다 (live 2026-07-13: ee440d82 29h outcome-less + 80 behind).
#   5. 기존 워크트리는 origin/main에 동기화(ahead==0일 때 reset --hard).
#      declined/failed/timeout 재시도는 이전 tip을 버리고 origin/main에서
#      다시 깐다 (bot #3614: 워크트리만 지우고 브랜치 tip이 남는 스테일 재시도).
#      셋업 실패 시 같은 틱에서 다음 후보로 넘어간다 (헤드오브큐 독성 방지).
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
DAILY_CAP="${DENEB_DISPATCH_DAILY_CAP:-2}"
SESSION_TIMEOUT="${DENEB_DISPATCH_TIMEOUT_SEC:-7200}"
# Claude Code binary: newest installed ccd-cli unless overridden.
CLAUDE_BIN="${DENEB_DISPATCH_CLAUDE_BIN:-}"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DISPATCH_RPC="$SCRIPT_DIR/self_correction_dispatch.py"

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
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
        elif [[ "$state" == "OPEN" ]]; then
            record_event --id "$cid" --phase pr_opened --attempt-id "$attempt" --branch "$branch" \
                --pr-number "$number" --pr-url "$url" --note "reconciled open PR" \
                || log "WARN: failed to reconcile open dispatch $cid"
        fi
    done
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
        exit 0
    fi
    mkdir -p "$DISPATCH_DIR"
    local script_dir="$SCRIPT_DIR"
    reconcile_dispatches

    # Reclaim abandoned outcome-less markers (age >= SESSION_TIMEOUT) and their
    # worktrees/branches. Pick already unblocks them, but leaving the marker +
    # 80-commit-behind tree around confuses operators and can strand a dirty
    # or stale dir until that cid is picked (live ee440d82, 2026-07-13).
    python3 - "$DISPATCH_DIR" "$SESSION_TIMEOUT" "$WORKTREE_ROOT" "$PROD_DIR" <<'PY' >>"$LOG_FILE" 2>&1
import json, os, sys, time, subprocess
dispatch_dir, abandon_after, wt_root, prod = sys.argv[1:5]
abandon_after = int(abandon_after)
now = time.time()

def run(argv):
    subprocess.run(argv, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

for name in sorted(os.listdir(dispatch_dir)):
    if not name.endswith(".json"):
        continue
    path = os.path.join(dispatch_dir, name)
    try:
        age = now - os.path.getmtime(path)
        with open(path, encoding="utf-8", errors="replace") as f:
            rec = json.load(f)
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        continue
    if not isinstance(rec, dict):
        continue
    outcome = rec.get("outcome")
    if isinstance(outcome, str) and outcome.strip():
        continue
    if age < abandon_after:
        continue
    cid = name[:-5]
    wt = os.path.join(wt_root, f"dispatch-{cid}")
    branch = f"dispatch/{cid}"
    try:
        os.remove(path)
    except OSError as e:
        print(f"abandon reclaim: marker delete failed for {cid}: {e}", flush=True)
        continue
    if os.path.isdir(wt):
        run(["git", "-C", prod, "worktree", "remove", "--force", wt])
        if os.path.isdir(wt):
            run(["rm", "-rf", wt])
    run(["git", "-C", prod, "branch", "-D", branch])
    print(
        f"abandon reclaim: released {cid} "
        f"(outcome-less age {int(age)}s ≥ {abandon_after}s)",
        flush=True,
    )
PY

    # Upgrade non-terminal outcomes first. Newest-first within 14d (mtime), then
    # truncate — sorting AFTER age filter so five stale markers cannot starve
    # newer attempted ones (bot #3609). Also reprobe failed/timeout when the PR
    # later merged (OPEN-at-end sessions used to record failed before attempted).
    if command -v gh >/dev/null 2>&1; then
        local m mcid mstate
        while IFS= read -r m; do
            [[ -f "$m" ]] || continue
            if ! grep -qE '"outcome": "(attempted|failed|timeout)"' "$m" 2>/dev/null; then
                continue
            fi
            mcid=$(basename "$m" .json)
            mstate=$(cd "$PROD_DIR" && gh pr list --head "dispatch/$mcid" --state merged \
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
    ts = None
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            rec = json.load(f)
        if isinstance(rec, dict) and isinstance(rec.get("dispatchedAt"), (int, float)):
            ts = int(rec["dispatchedAt"])
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        pass
    if ts is None:
        try:
            ts = int(os.path.getmtime(path) * 1000)
        except OSError:
            continue
    if start <= ts < end:
        n += 1
print(n)
PY
)
    if (( spent >= DAILY_CAP )); then
        log "daily cap reached ($spent/$DAILY_CAP UTC $today) — idle"
        exit 0
    fi

    if [[ ! -f "$QUEUE_FILE" ]]; then
        log "queue file missing — idle"
        exit 0
    fi

    local claude_bin
    if ! claude_bin=$(resolve_claude); then
        log "no Claude Code binary available — idle"
        exit 0
    fi

    mkdir -p "$WORKTREE_ROOT"

    # Pick + setup: keep trying until a worktree is ready or the queue is
    # exhausted this tick (hard-capping at 5 left candidate 6+ permanently
    # starved across timer ticks when the head of queue was poison — bot #3615).
    local pick="" cid="" wt="" skip_ids=""
    local attempt=0
    while (( attempt < 40 )); do
        attempt=$((attempt + 1))
        # Newest undispatched proposed/accepted code candidate with an evidence-
        # bearing source. Marker skip: dispatch_outcome.blocks_redispatch.
        # Optional argv[5]=comma-separated ids to skip this tick after setup fail.
        pick=$(python3 - "$QUEUE_FILE" "$DISPATCH_DIR" "$script_dir" "$SESSION_TIMEOUT" "$skip_ids" <<'PYEOF'
import json, os, sys
queue, dispatch_dir, script_dir = sys.argv[1], sys.argv[2], sys.argv[3]
abandon_after = int(sys.argv[4])
skip = {s for s in (sys.argv[5] if len(sys.argv) > 5 else "").split(",") if s}
sys.path.insert(0, script_dir)
import dispatch_outcome
cand, status = {}, {}
for line in open(queue, errors="replace"):
    line = line.strip()
    if not line:
        continue
    try:
        rec = json.loads(line)
    except json.JSONDecodeError:
        continue
    rid = rec.get("id") or ""
    if not rid:
        continue
    if rec.get("type") == "self_correction_candidate":
        cand[rid] = rec
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
    if not (src.startswith("evolve-tool-gap") or src.startswith("self-harness")
            or src.startswith("health-finding") or src.startswith("tool-quality")):
        continue
    if dispatch_outcome.blocks_redispatch(
            os.path.join(dispatch_dir, rid + ".json"),
            abandon_after_sec=abandon_after):
        continue
    out = dict(rec)
    out["status"] = status.get(rid, rec.get("status") or "proposed")
    print(json.dumps(out, ensure_ascii=False))
    break
PYEOF
        )
        if [[ -z "$pick" ]]; then
            log "no dispatchable candidate (attempt=$attempt skip=${skip_ids:-none}) — idle"
            exit 0
        fi
        cid=$(printf '%s' "$pick" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
        wt="$WORKTREE_ROOT/dispatch-$cid"
        local branch="dispatch/$cid"

        # Retryable prior outcomes must not resume from a stale tip left behind
        # after clean declined cleanup (bot #3614). Wipe worktree+branch and
        # recreate from origin/main below.
        local retry_refresh=0
        if [[ -f "$DISPATCH_DIR/$cid.json" ]] \
            && grep -qE '"outcome"[[:space:]]*:[[:space:]]*"(declined|failed|timeout)"' \
                "$DISPATCH_DIR/$cid.json" 2>/dev/null; then
            retry_refresh=1
        fi
        if [[ "$retry_refresh" -eq 1 ]]; then
            if [[ -d "$wt" ]]; then
                log "retryable outcome on $cid — discarding prior worktree/branch for origin/main refresh"
                git -C "$PROD_DIR" worktree remove --force "$wt" >>"$LOG_FILE" 2>&1 || true
                rm -rf "$wt"
            fi
            if git -C "$PROD_DIR" show-ref --verify --quiet "refs/heads/$branch"; then
                git -C "$PROD_DIR" branch -D "$branch" >>"$LOG_FILE" 2>&1 || true
            fi
        fi

        # Unregistered leftover dir → wipe only after worktree list SUCCEEDS
        # (bot #3614: a bad PROD_DIR made list fail and rm -rf real trees).
        if [[ -d "$wt" ]]; then
            local wt_list
            if ! wt_list=$(git -C "$PROD_DIR" worktree list --porcelain 2>/dev/null); then
                log "git worktree list failed for $PROD_DIR — setup deferred for $cid"
                skip_ids="${skip_ids:+$skip_ids,}$cid"
                pick=""; cid=""; wt=""
                continue
            fi
            if ! printf '%s\n' "$wt_list" | grep -Fxq "worktree $wt"; then
                log "stale worktree dir (not registered) — removing $wt"
                rm -rf "$wt"
            fi
        fi

        # Sync registered worktrees to origin/main when clean+ahead==0. Dirty
        # trees from failed/timeout sessions are preserved for inspection and
        # skipped this tick (bot #3615: reset --hard wiped uncommitted work).
        # Note: retry_refresh already wiped above, so this path is first-attempt
        # / in-flight / abandoned-outcome markers only.
        if [[ -d "$wt" ]]; then
            if ! git -C "$PROD_DIR" fetch origin main --quiet >>"$LOG_FILE" 2>&1; then
                log "fetch origin/main failed — setup deferred for $cid"
                skip_ids="${skip_ids:+$skip_ids,}$cid"
                pick=""; cid=""; wt=""
                continue
            fi
            local ahead_existing dirty
            ahead_existing=$(git -C "$wt" rev-list --count origin/main..HEAD 2>/dev/null || echo 0)
            dirty=$(git -C "$wt" status --porcelain 2>/dev/null || true)
            if [[ -n "$dirty" && "$ahead_existing" == "0" ]]; then
                log "dirty worktree $cid with no commits ahead — preserving, try next"
                skip_ids="${skip_ids:+$skip_ids,}$cid"
                pick=""; cid=""; wt=""
                continue
            fi
            if [[ "$ahead_existing" == "0" ]]; then
                if git -C "$wt" reset --hard origin/main >>"$LOG_FILE" 2>&1; then
                    log "synced existing worktree $cid to origin/main"
                else
                    log "worktree sync failed for $cid — recreating"
                    git -C "$PROD_DIR" worktree remove --force "$wt" >>"$LOG_FILE" 2>&1 || true
                    rm -rf "$wt"
                fi
            else
                log "reusing worktree $cid with $ahead_existing commit(s) ahead of origin/main"
            fi
        fi

        if [[ ! -d "$wt" ]]; then
            # Orphan-branch recovery. On attach of an existing branch with no
            # commits ahead, refresh to origin/main so retries do not land from
            # a stale tip (bot #3614). Also: if show-ref misses a branch that
            # still blocks `worktree add -b` (ghost ref / race), delete and
            # recreate (live 2026-07-13: 4c2c454a).
            if git -C "$PROD_DIR" show-ref --verify --quiet "refs/heads/$branch"; then
                if ! git -C "$PROD_DIR" worktree add "$wt" "$branch" >>"$LOG_FILE" 2>&1; then
                    log "stale dispatch branch $branch — recreating worktree"
                    git -C "$PROD_DIR" branch -D "$branch" >>"$LOG_FILE" 2>&1 || true
                    if ! git -C "$PROD_DIR" worktree add "$wt" -b "$branch" origin/main >>"$LOG_FILE" 2>&1; then
                        log "worktree creation failed for $cid after branch reset — try next"
                        skip_ids="${skip_ids:+$skip_ids,}$cid"
                        pick=""; cid=""; wt=""
                        continue
                    fi
                else
                    git -C "$PROD_DIR" fetch origin main --quiet >>"$LOG_FILE" 2>&1 || true
                    local ahead_attach
                    ahead_attach=$(git -C "$wt" rev-list --count origin/main..HEAD 2>/dev/null || echo 0)
                    if [[ "$ahead_attach" == "0" ]]; then
                        git -C "$wt" reset --hard origin/main >>"$LOG_FILE" 2>&1 || true
                    fi
                fi
            elif ! git -C "$PROD_DIR" worktree add "$wt" -b "$branch" origin/main >>"$LOG_FILE" 2>&1; then
                # Ghost branch: show-ref false but add -b still conflicts.
                if git -C "$PROD_DIR" branch -D "$branch" >>"$LOG_FILE" 2>&1; then
                    log "ghost dispatch branch $branch — deleted, recreating"
                    if git -C "$PROD_DIR" worktree add "$wt" -b "$branch" origin/main >>"$LOG_FILE" 2>&1; then
                        :
                    else
                        log "worktree creation failed for $cid after ghost cleanup — try next"
                        skip_ids="${skip_ids:+$skip_ids,}$cid"
                        pick=""; cid=""; wt=""
                        continue
                    fi
                else
                    log "worktree creation failed for $cid — try next"
                    skip_ids="${skip_ids:+$skip_ids,}$cid"
                    pick=""; cid=""; wt=""
                    continue
                fi
            fi
        fi

        if [[ -d "$wt" ]]; then
            break
        fi
        skip_ids="${skip_ids:+$skip_ids,}$cid"
        pick=""; cid=""; wt=""
    done

    if [[ -z "$pick" || -z "$cid" || ! -d "$wt" ]]; then
        log "setup exhausted after skips (${skip_ids:-none}) — idle"
        exit 0
    fi

    # Prompt composition + dispatch marker live in dispatch_prompt.py: the
    # contract half of the prompt is the externalized meta artifact
    # (meta/dispatch-contract-prompt.md, RSI P5-4 — gateway materializes it
    # from the compiled default), and the marker (written BEFORE the session,
    # so a crashed session must not redispatch forever) carries promptVersion
    # provenance. An unusable artifact defers the dispatch — candidate and
    # daily-cap slot stay unburned until the gateway materializes it.
    local prompt attempt_id branch
    branch="dispatch/$cid"
    attempt_id="$cid-$(date +%s)-$$"
    if ! prompt=$(printf '%s' "$pick" | python3 "$SCRIPT_DIR/dispatch_prompt.py" \
            --meta-dir "$STATE_DIR/skills/genesis/meta" \
            --marker "$DISPATCH_DIR/$cid.json" \
            --attempt-id "$attempt_id" --branch "$branch" 2>>"$LOG_FILE"); then
        log "dispatch contract artifact unavailable — $cid deferred (no marker burned)"
        exit 0
    fi
    if [[ -z "$prompt" ]]; then
        # dispatch_prompt writes the marker before printing; an empty prompt
        # must not leave a blocking marker behind.
        rm -f "$DISPATCH_DIR/$cid.json"
        log "empty dispatch prompt for $cid — marker released, deferred"
        exit 0
    fi

    # Fail closed before spending an agent session: a dispatch without its
    # authoritative start event would recreate the result-ledger gap.
    if ! record_event --id "$cid" --phase started --attempt-id "$attempt_id" --branch "$branch" \
            --note "coding dispatch started"; then
        rm -f "$DISPATCH_DIR/$cid.json"
        log "dispatch ledger unavailable — $cid deferred (marker released)"
        exit 0
    fi
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
    if ! record_pr_outcome "$cid" "$attempt_id" "$branch" "$rc" "$elapsed"; then
        log "WARN: failed to record terminal PR outcome for $cid"
    fi

    # Instant failure (<60s, rc!=0) means the session never really started —
    # binary not logged in ("Not logged in", observed live 2026-07-12), missing
    # deps, etc. Burning the candidate AND a daily-cap slot on that would starve
    # the lane silently: release the marker and the worktree so the same
    # candidate re-dispatches once the environment is fixed.
    if (( rc != 0 && elapsed < 60 )) && [[ "$PR_OUTCOME" == "failed" || "$PR_OUTCOME" == "none" ]]; then
        rm -f "$DISPATCH_DIR/$cid.json"
        git -C "$PROD_DIR" worktree remove --force "$wt" >>"$LOG_FILE" 2>&1 || true
        log "instant failure — marker released for $cid (environment problem, not the candidate)"
        exit 0
    fi

    # Outcome accounting (graduation-ladder evidence: cap raise needs a
    # measured land rate). Gather observable facts — PR state for the dispatch
    # branch, commits ahead of origin/main in the worktree — and let
    # dispatch_outcome.py fold the verdict into the marker. Never fatal.
    local pr_state="" ahead=""
    git -C "$PROD_DIR" fetch origin main --quiet >>"$LOG_FILE" 2>&1 || true
    if command -v gh >/dev/null 2>&1; then
        pr_state=$(cd "$PROD_DIR" && gh pr list --head "dispatch/$cid" --state all \
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
        git -C "$PROD_DIR" worktree remove --force "$wt" >>"$LOG_FILE" 2>&1 || true
    fi
    exit 0
}

main "$@"
