#!/bin/bash
# ──────────────────────────────────────────────
# bootstrap.sh — 컨테이너/세션 시작 시 자동 복구
#
# 컨테이너 재시작 후 DB 재빌드, sync-db 데몬 재시작을
# 수행합니다.
#
# 사용법:
#   ./bootstrap.sh              # 전체 복구
#   ./bootstrap.sh --quiet      # 에러만 출력
# ──────────────────────────────────────────────

set -euo pipefail

SELF_DIR="$(cd "$(dirname "$0")" && pwd)"

# 공통 유틸리티 로드
source "$SELF_DIR/_lib.sh"
PYTHON="$(_find_python)"

# .env 파일이 있으면 로드
if [[ -f "$SELF_DIR/.env" ]]; then
    set -a; source "$SELF_DIR/.env"; set +a
fi

MD_DIR="${MD_DIR:-$SELF_DIR/projects}"
DB_PATH="${DB_PATH:-$SELF_DIR/projects.db}"
QUIET="${1:-}"

log() {
    [[ "$QUIET" != "--quiet" ]] && echo "[bootstrap] $*" || true
}

err() {
    echo "[bootstrap] ERROR: $*" >&2
}

# ── 1. MD 디렉토리 확인 ──
if [[ ! -d "$MD_DIR" ]]; then
    # 대안 경로 시도
    ALT="$HOME/.openclaw/agents/main/knowledge/projects"
    if [[ -d "$ALT" ]]; then
        MD_DIR="$ALT"
        log "MD 디렉토리 대안 경로 사용: $MD_DIR"
    else
        err "MD 디렉토리 없음: $MD_DIR"
        exit 1
    fi
fi

# ── 2. DB 없으면 자동 재빌드 ──
if [[ ! -f "$DB_PATH" ]]; then
    log "DB 없음 → 자동 재빌드 시작..."
    IMPORTER="$SELF_DIR/project_db_v2.py"
    if [[ -f "$IMPORTER" ]]; then
        "$PYTHON" "$IMPORTER" import "$MD_DIR" --db "$DB_PATH" 2>/dev/null
        if [[ -f "$DB_PATH" ]]; then
            log "DB 재빌드 완료 ($(stat -c%s "$DB_PATH" 2>/dev/null || echo '?') bytes)"
        else
            err "DB 재빌드 실패"
        fi
    else
        err "project_db_v2.py 없음: $IMPORTER"
    fi
else
    log "DB 존재 확인: $DB_PATH"
fi

# ── 3. sync-db 데몬 확인/재시작 ──
PID_FILE="$SELF_DIR/.sync-db.pid"
SYNC_SCRIPT="$SELF_DIR/sync-db.sh"
DAEMON_ALIVE=false

if [[ -f "$PID_FILE" ]]; then
    pid=$(cat "$PID_FILE" 2>/dev/null || echo "")
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        DAEMON_ALIVE=true
        log "sync-db 데몬 작동 중 (PID: $pid)"
    else
        rm -f "$PID_FILE"
    fi
fi

if ! $DAEMON_ALIVE && [[ -f "$SYNC_SCRIPT" ]]; then
    log "sync-db 데몬 시작..."
    export MD_DIR DB_PATH
    bash "$SYNC_SCRIPT" --daemon 2>/dev/null || true
    log "sync-db 데몬 시작됨"
fi

# ── 4. 헬스체크 ──
log "헬스체크 실행..."
"$PYTHON" "$SELF_DIR/vega.py" health 2>/dev/null | "$PYTHON" -c "
import sys, json
try:
    d = json.load(sys.stdin)
    issues = d.get('data', {}).get('issues', [])
    if issues:
        print(f'[bootstrap] 이슈 {len(issues)}건: ' + '; '.join(issues))
    else:
        print('[bootstrap] 시스템 정상')
except:
    print('[bootstrap] 헬스체크 파싱 실패')
" || true

log "부트스트랩 완료"
