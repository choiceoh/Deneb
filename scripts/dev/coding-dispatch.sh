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
#      일일 배차 상한으로 토큰 예산을 지킨다.
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

log() {
    printf '%s  %s\n' "$(date -Iseconds)" "$*" >> "$LOG_FILE"
}

resolve_claude() {
    if [[ -n "$CLAUDE_BIN" && -x "$CLAUDE_BIN" ]]; then
        printf '%s' "$CLAUDE_BIN"
        return 0
    fi
    local newest
    newest=$(ls -1 "$HOME/.claude/remote/ccd-cli/" 2>/dev/null | sort -V | tail -1 || true)
    if [[ -n "$newest" && -x "$HOME/.claude/remote/ccd-cli/$newest" ]]; then
        printf '%s' "$HOME/.claude/remote/ccd-cli/$newest"
        return 0
    fi
    command -v claude 2>/dev/null || return 1
}

main() {
    exec 9>"$LOCK_FILE"
    if ! flock -n 9; then
        exit 0 # a dispatch session is running
    fi
    mkdir -p "$DISPATCH_DIR"
    local script_dir
    script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

    # Upgrade non-terminal outcomes first: a session that pushed a PR but died
    # before landing records "attempted" — if that PR merged later, the land
    # rate (graduation-ladder evidence) would permanently undercount. Bounded
    # reprobe: newest few attempted markers within 14d, one gh probe each.
    if command -v gh >/dev/null 2>&1; then
        local m mcid mstate
        for m in $(grep -l '"outcome": "attempted"' "$DISPATCH_DIR"/*.json 2>/dev/null | head -5); do
            [[ -n $(find "$m" -mtime -14 2>/dev/null) ]] || continue
            mcid=$(basename "$m" .json)
            mstate=$(cd "$PROD_DIR" && gh pr list --head "dispatch/$mcid" --state merged \
                --json state --jq '.[0].state // ""' 2>/dev/null || true)
            if [[ "$mstate" == "MERGED" ]]; then
                python3 "$script_dir/dispatch_outcome.py" --marker "$m" --rc 0 \
                    --pr-state MERGED --upgrade-only >>"$LOG_FILE" 2>&1 || true
                log "reprobe: $mcid attempted → landed (PR merged after session end)"
            fi
        done
    fi

    # Daily cap: markers created today.
    local today spent
    today=$(date +%Y-%m-%d)
    spent=$(find "$DISPATCH_DIR" -name "*.json" -newermt "$today" 2>/dev/null | wc -l)
    if (( spent >= DAILY_CAP )); then
        exit 0
    fi

    [[ -f "$QUEUE_FILE" ]] || exit 0

    # Newest undispatched proposed code candidate with an evidence-bearing
    # source. jq-free: python3 is guaranteed on this host (deploy scripts use it).
    local pick
    pick=$(python3 - "$QUEUE_FILE" "$DISPATCH_DIR" <<'PYEOF'
import json, os, sys
queue, dispatch_dir = sys.argv[1], sys.argv[2]
# A self_correction_review row is a STATUS DELTA ({id,status,...}), not a full
# record — merge its status onto the candidate rather than replacing it, or the
# candidate's scope/source/title get wiped and nothing ever matches.
cand = {}    # id -> full candidate record
status = {}  # id -> latest status
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
# Dispatch order: review-endorsed (accepted) candidates first — the heartbeat
# review lane actively accepts queue candidates it cannot implement itself
# ("코딩 에이전트 후속", observed live 2026-07-12) — then unreviewed proposed,
# newest within each tier. rejected/superseded/applied never dispatch.
def pick_order(kv):
    rid, rec = kv
    st = status.get(rid, rec.get("status") or "proposed")
    return (0 if st == "accepted" else 1, -(rec.get("createdAt") or 0))
for rid, rec in sorted(cand.items(), key=pick_order):
    if status.get(rid, rec.get("status") or "proposed") not in ("proposed", "accepted"):
        continue
    if rec.get("scope") != "code":
        continue
    src = rec.get("source") or ""
    # health-finding graduated 2026-07-12: first mined batch (7) reviewed clean
    # (findings reproduce at HEAD, deterministic evidence, remediation directions
    # actionable) — roadmap P5 graduation ladder. runtime-error stays staged.
    # tool-quality graduated 2026-07-13 by DIRECT OPERATOR DIRECTIVE ("노브를
    # 켜버려") ahead of the usual reviewed-batch gate: its candidates are
    # description/schema clarifications and per-tool latency (:desc / :latency),
    # both narrow and gated by the same land-time stack (make check + live-test +
    # CI). The tool-quality-dryrun workflow previews what it would file before any
    # real mine. deadcode-finding stays staged.
    if not (src.startswith("evolve-tool-gap") or src.startswith("self-harness")
            or src.startswith("health-finding") or src.startswith("tool-quality")):
        continue
    if os.path.exists(os.path.join(dispatch_dir, rid + ".json")):
        continue
    print(json.dumps(rec, ensure_ascii=False))
    break
PYEOF
    )
    [[ -n "$pick" ]] || exit 0

    local cid
    cid=$(printf '%s' "$pick" | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
    local claude_bin
    if ! claude_bin=$(resolve_claude); then
        log "no Claude Code binary available; leaving candidate $cid queued"
        exit 0
    fi

    local wt="$WORKTREE_ROOT/dispatch-$cid"
    mkdir -p "$WORKTREE_ROOT"
    if [[ ! -d "$wt" ]]; then
        if ! git -C "$PROD_DIR" worktree add "$wt" -b "dispatch/$cid" origin/main >>"$LOG_FILE" 2>&1; then
            log "worktree creation failed for $cid"
            exit 0
        fi
    fi

    # Prompt composition + dispatch marker live in dispatch_prompt.py: the
    # contract half of the prompt is the externalized meta artifact
    # (meta/dispatch-contract-prompt.md, RSI P5-4 — gateway materializes it
    # from the compiled default), and the marker (written BEFORE the session,
    # so a crashed session must not redispatch forever) carries promptVersion
    # provenance. An unusable artifact defers the dispatch — candidate and
    # daily-cap slot stay unburned until the gateway materializes it.
    local prompt
    if ! prompt=$(printf '%s' "$pick" | python3 "$script_dir/dispatch_prompt.py" \
            --meta-dir "$STATE_DIR/skills/genesis/meta" \
            --marker "$DISPATCH_DIR/$cid.json" 2>>"$LOG_FILE"); then
        log "dispatch contract artifact unavailable — $cid deferred (no marker burned)"
        exit 0
    fi
    if [[ -z "$prompt" ]]; then
        log "empty dispatch prompt for $cid — deferred"
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

    # Instant failure (<60s, rc!=0) means the session never really started —
    # binary not logged in ("Not logged in", observed live 2026-07-12), missing
    # deps, etc. Burning the candidate AND a daily-cap slot on that would starve
    # the lane silently: release the marker and the worktree so the same
    # candidate re-dispatches once the environment is fixed.
    if (( rc != 0 && elapsed < 60 )); then
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
