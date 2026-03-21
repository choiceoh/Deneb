#!/bin/bash
# ──────────────────────────────────────────────
# setup.sh — Vega 프로젝트 검색엔진 설치
#
# 수행 작업:
#   1. Python 및 필수 파일 확인
#   2. .md 디렉토리 탐색/설정
#   3. .md → SQLite 초기 빌드
#   4. 파일 감시 데몬 시작 (선택)
#   5. 클로 MCP 연동 안내
#
# 사용법:
#   ./setup.sh /path/to/md/files       # .md 파일 디렉토리 지정
#   ./setup.sh                          # 대화형 설정
# ──────────────────────────────────────────────

set -euo pipefail

# ── 색상 ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}ℹ️  $*${NC}"; }
ok()    { echo -e "${GREEN}✅ $*${NC}"; }
warn()  { echo -e "${YELLOW}⚠️  $*${NC}"; }
err()   { echo -e "${RED}❌ $*${NC}"; }

SELF_DIR="$(cd "$(dirname "$0")" && pwd)"

# 공통 유틸리티 로드
source "$SELF_DIR/_lib.sh"
PYTHON="$(_find_python)"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " Vega 프로젝트 검색엔진 — 설치"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── 1. Python 확인 ──
if ! command -v "$PYTHON" &>/dev/null; then
    err "Python을 찾을 수 없습니다. python3 또는 python을 설치하세요."
    exit 1
fi
PYTHON_VER=$("$PYTHON" --version 2>&1)
ok "Python: $PYTHON ($PYTHON_VER)"

# ── 2. .md 디렉토리 확인 ──

MD_DIR="${1:-}"

if [[ -z "$MD_DIR" ]]; then
    # 기본 위치 후보 (OpenClaw 버전 무관 glob 포함)
    CANDIDATES=(
        "$SELF_DIR/projects"
        "$HOME/.openclaw/agents/main/knowledge/projects"
        "$HOME/projects"
    )
    # OpenClaw 버전별 경로 glob
    for d in "$HOME"/.openclaw/*/agents/main/knowledge/projects; do
        [[ -d "$d" ]] && CANDIDATES+=("$d")
    done

    for candidate in "${CANDIDATES[@]}"; do
        if [[ -d "$candidate" ]]; then
            count=$(find "$candidate" -name "*.md" 2>/dev/null | wc -l)
            if [[ $count -gt 0 ]]; then
                MD_DIR="$candidate"
                info "자동 감지: $MD_DIR ($count개 .md 파일)"
                break
            fi
        fi
    done
fi

if [[ -z "$MD_DIR" ]]; then
    echo "프로젝트 .md 파일이 있는 디렉토리 경로를 입력하세요:"
    read -r MD_DIR
fi

if [[ ! -d "$MD_DIR" ]]; then
    err "디렉토리 없음: $MD_DIR"
    exit 1
fi

MD_COUNT=$(find "$MD_DIR" -name "*.md" | wc -l)
ok ".md 디렉토리: $MD_DIR ($MD_COUNT개 파일)"

# ── 3. 필수 파일 확인 ──

REQUIRED_FILES=(
    "$SELF_DIR/_lib.sh"
    "$SELF_DIR/config.py"
    "$SELF_DIR/vega.py"
    "$SELF_DIR/project_db_v2.py"
    "$SELF_DIR/addons/__init__.py"
    "$SELF_DIR/router.py"
    "$SELF_DIR/mail_to_md.py"
    "$SELF_DIR/md_editor.py"
    "$SELF_DIR/vega-wrapper.sh"
    "$SELF_DIR/sync-db.sh"
)

missing=0
for f in "${REQUIRED_FILES[@]}"; do
    if [[ ! -f "$f" ]]; then
        err "필수 파일 없음: $(basename "$f")"
        missing=1
    fi
done
[[ $missing -eq 1 ]] && exit 1
ok "필수 파일 확인 완료 (${#REQUIRED_FILES[@]}개)"

# ── 4. 실행 권한 부여 ──

chmod +x "$SELF_DIR/vega-wrapper.sh"
chmod +x "$SELF_DIR/sync-db.sh"
[[ -f "$SELF_DIR/bootstrap.sh" ]] && chmod +x "$SELF_DIR/bootstrap.sh"
[[ -f "$SELF_DIR/search-router.sh" ]] && chmod +x "$SELF_DIR/search-router.sh"
ok "실행 권한 설정"

# ── 5. 환경 설정 파일 생성 ──

ENV_FILE="$SELF_DIR/.env"
cat > "$ENV_FILE" <<EOF
# Vega 프로젝트 검색엔진 설정
# 생성: $(date -Iseconds 2>/dev/null || date)

# .md 파일 디렉토리
MD_DIR=$MD_DIR

# SQLite DB 경로
DB_PATH=$SELF_DIR/projects.db

# 스크립트 디렉토리
SCRIPT_DIR=$SELF_DIR
EOF

ok "환경 설정: $ENV_FILE"

# ── 6. 초기 SQLite 빌드 ──

info "SQLite 초기 빌드 시작..."
export MD_DIR DB_PATH="$SELF_DIR/projects.db"
bash "$SELF_DIR/sync-db.sh" --once

if [[ -f "$SELF_DIR/projects.db" ]]; then
    DB_SIZE=$(stat -c%s "$SELF_DIR/projects.db" 2>/dev/null || stat -f%z "$SELF_DIR/projects.db" 2>/dev/null || echo "?")
    ok "DB 빌드 완료 (${DB_SIZE} bytes)"
else
    err "DB 빌드 실패"
    exit 1
fi

# ── 7. 파일 감시 데몬 시작 ──

echo ""
info "파일 감시 데몬을 시작할까요? (y/N)"
read -r START_DAEMON

if [[ "${START_DAEMON:-n}" =~ ^[yY] ]]; then
    export MD_DIR DB_PATH="$SELF_DIR/projects.db"
    bash "$SELF_DIR/sync-db.sh" --daemon
    ok "파일 감시 데몬 시작됨"
else
    info "수동 시작: ./sync-db.sh --daemon"
fi

# ── 8. 설치 요약 ──

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " 설치 완료"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo " 핵심 파일:"
echo "   config.py           설정 (경로, DB, 모델)"
echo "   vega.py             CLI/MCP 메인 엔트리포인트"
echo "   vega-wrapper.sh     MCP 래퍼 (stdin JSON 지원)"
echo "   project_db_v2.py    .md → SQLite 변환기"
echo "   addons/             대시보드/교차분석/연락처 등"
echo "   router.py           통합 검색 라우터 (로컬 모델+SQLite)"
echo "   mail_to_md.py       메일 → .md 자동 삽입"
echo "   md_editor.py        .md 필드 편집 + DB 동기화"
echo "   sync-db.sh          파일 감시 데몬"
echo "   projects.db         SQLite DB (자동 재빌드 가능)"
echo ""
echo " 테스트:"
echo "   $PYTHON vega.py list"
echo "   $PYTHON vega.py dashboard"
echo "   $PYTHON vega.py search \"비금도\""
echo "   $PYTHON vega.py health"
echo ""
echo " 동기화:"
echo "   ./sync-db.sh --once      수동 1회 동기화"
echo "   ./sync-db.sh --daemon    백그라운드 감시 시작"
echo "   ./sync-db.sh --stop      감시 중지"
echo ""
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── 9. 클로 MCP 연동 안내 ──

cat <<'GUIDE'
 ┌──────────────────────────────────────────────────┐
 │  클로 MCP 연동 방법                               │
 ├──────────────────────────────────────────────────┤
 │                                                  │
 │  mcp-vega.json을 MCP config에 복사:              │
 │    cp mcp-vega.json ~/.openclaw/mcp/             │
 │                                                  │
 │  또는 claude_desktop_config.json에 추가:          │
 │    "vega": {                                     │
 │      "command": "bash",                          │
 │      "args": ["/path/to/vega-wrapper.sh"]        │
 │    }                                             │
 │                                                  │
 │  → 클로가 자연어 질문에 자동으로 도구 호출         │
 │  → search, dashboard, contacts, pipeline,        │
 │    cross, weekly, mail-append, update, person,   │
 │    urgent, add-action, timeline 등 전체 지원      │
 │                                                  │
 └──────────────────────────────────────────────────┘
GUIDE
