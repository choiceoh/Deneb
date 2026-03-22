#!/bin/bash
# ──────────────────────────────────────────────
# sync-db.sh — .md 파일 변경 감지 → SQLite 자동 재빌드
#
# 사용법:
#   ./sync-db.sh                    # 포그라운드 실행
#   ./sync-db.sh --daemon           # 백그라운드 데몬
#   ./sync-db.sh --once             # 1회 동기화 후 종료
#   ./sync-db.sh --stop             # 데몬 중지
#
# 환경변수:
#   MD_DIR     .md 파일 디렉토리 (기본: ./projects)
#   DB_PATH    SQLite DB 경로 (기본: ./projects.db)
#   SCRIPT_DIR project_db_v2.py 위치 (기본: 스크립트와 같은 디렉토리)
# ──────────────────────────────────────────────

set -euo pipefail

SELF_DIR="$(cd "$(dirname "$0")" && pwd)"

# 공통 유틸리티 로드
source "$SELF_DIR/_lib.sh"
PYTHON="$(_find_python)"

MD_DIR="${MD_DIR:-$SELF_DIR/projects}"
DB_PATH="${DB_PATH:-$SELF_DIR/projects.db}"
IMPORTER="$SELF_DIR/project_db_v2.py"
PID_FILE="$SELF_DIR/.sync-db.pid"
LOG_FILE="$SELF_DIR/sync-db.log"

# ── 유틸 ──

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

rebuild() {
    log "🔄 SQLite 재빌드 시작 ($MD_DIR → $DB_PATH)"
    # Atomic rebuild: 임시 파일에 빌드 → 성공 시 rename
    TEMP_DB="${DB_PATH}.tmp.$$"
    trap 'rm -f "$TEMP_DB"' EXIT
    "$PYTHON" "$IMPORTER" import "$MD_DIR" --db "$TEMP_DB" 2>&1 | tee -a "$LOG_FILE"
    if [[ ${PIPESTATUS[0]} -eq 0 && -f "$TEMP_DB" ]]; then
        mv -f "$TEMP_DB" "$DB_PATH"
        log "✅ 재빌드 완료 ($(stat -c%s "$DB_PATH" 2>/dev/null || echo '?') bytes)"
    else
        rm -f "$TEMP_DB"
        log "❌ 재빌드 실패, 기존 DB 유지"
        return 1
    fi

    # 자동 changelog (addons.py가 있으면)
    ADDONS="$SELF_DIR/addons.py"
    if [[ -f "$ADDONS" ]]; then
        log "📋 변경 리포트 생성 중..."
        "$PYTHON" "$ADDONS" changelog --db "$DB_PATH" 2>&1 | tee -a "$LOG_FILE"
    fi
}

incremental_rebuild() {
    log "🔄 증분 업데이트 시작 ($MD_DIR → $DB_PATH)"
    "$PYTHON" "$IMPORTER" import "$MD_DIR" --db "$DB_PATH" --incremental 2>&1 | tee -a "$LOG_FILE"
    if [[ $? -eq 0 ]]; then
        log "✅ 증분 업데이트 완료"
    else
        log "⚠️ 증분 업데이트 실패, 전체 재빌드 시도"
        rebuild
    fi

    ADDONS="$SELF_DIR/addons.py"
    if [[ -f "$ADDONS" ]]; then
        log "📋 변경 리포트 생성 중..."
        "$PYTHON" "$ADDONS" changelog --db "$DB_PATH" 2>&1 | tee -a "$LOG_FILE"
    fi
}

# ── 1회 동기화 ──

if [[ "${1:-}" == "--once" ]]; then
    rebuild
    exit 0
fi

# ── 데몬 중지 ──

if [[ "${1:-}" == "--stop" ]]; then
    if [[ -f "$PID_FILE" ]]; then
        pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
            rm -f "$PID_FILE"
            echo "sync-db 데몬 중지 (PID: $pid)"
        else
            rm -f "$PID_FILE"
            echo "PID $pid 이미 종료됨"
        fi
    else
        echo "실행 중인 데몬 없음"
    fi
    exit 0
fi

# ── 데몬 모드 ──

if [[ "${1:-}" == "--daemon" ]]; then
    nohup "$0" >> "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"
    echo "sync-db 데몬 시작 (PID: $!, 로그: $LOG_FILE)"
    exit 0
fi

# ── 디렉토리 확인 ──

if [[ ! -d "$MD_DIR" ]]; then
    echo "❌ MD_DIR이 없습니다: $MD_DIR"
    echo "   MD_DIR 환경변수를 설정하거나 $SELF_DIR/projects 디렉토리를 생성하세요."
    exit 1
fi

# ── 초기 빌드 ──

if [[ ! -f "$DB_PATH" ]]; then
    log "📦 초기 빌드 (DB 파일 없음)"
    rebuild
fi

# ── 파일 감시 루프 ──

log "👀 파일 감시 시작: $MD_DIR (*.md 변경 감지)"

# inotifywait 사용 가능하면 사용, 아니면 폴링
if command -v inotifywait &>/dev/null; then
    log "   모드: inotifywait (실시간)"
    while true; do
        # .md 파일의 생성/수정/삭제/이동 감시
        inotifywait -q -r -e modify,create,delete,move \
            --include '\.md$' \
            "$MD_DIR" 2>/dev/null || true
        
        # 연속 변경 대비 1초 대기 (debounce)
        sleep 1
        incremental_rebuild
    done
else
    log "   모드: 폴링 (5초 간격, inotifywait 미설치)"
    log "   ℹ️  apt install inotify-tools 로 실시간 감지 가능"
    
    # 마지막 빌드 시각 기록
    last_hash=""
    
    while true; do
        sleep 5
        # .md 파일들의 수정시각 해시
        current_hash=$(find "$MD_DIR" -name "*.md" -exec stat -c%Y {} + 2>/dev/null | sort | md5sum | cut -d' ' -f1)
        
        if [[ "$current_hash" != "$last_hash" && -n "$current_hash" ]]; then
            last_hash="$current_hash"
            incremental_rebuild
        fi
    done
fi
