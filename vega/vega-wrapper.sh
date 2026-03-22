#!/bin/bash
# ──────────────────────────────────────────────
# vega-wrapper.sh — MCP용 래퍼
#
# MCP config에 등록:
#   "command": "/path/to/vega-wrapper.sh"
#
# 호출 방식:
#   vega-wrapper.sh query "비금도 ZTT 케이블"
#   vega-wrapper.sh search "환경공단"
#   vega-wrapper.sh vsearch "인버터 연결 방식"
#
# 추가 명령:
#   vega-wrapper.sh cross
#   vega-wrapper.sh dashboard
#   vega-wrapper.sh contacts "Christina"
#   vega-wrapper.sh pipeline
#   vega-wrapper.sh weekly
#
# JSON 출력만 stdout으로, 나머지는 stderr로.
# ──────────────────────────────────────────────

set -euo pipefail

SELF_DIR="$(cd "$(dirname "$0")" && pwd)"
VEGA_CLI="$SELF_DIR/vega.py"

# 공통 유틸리티 로드
source "$SELF_DIR/_lib.sh"
PYTHON="$(_find_python)"

# .env 파일이 있으면 로드
if [[ -f "$SELF_DIR/.env" ]]; then
    set -a; source "$SELF_DIR/.env" 2>/dev/null; set +a
fi

# DB 없으면 자동 재빌드 (vega.py 내부에서도 하지만 이중 안전장치)
DB_PATH="${DB_PATH:-$SELF_DIR/projects.db}"
if [[ ! -f "$DB_PATH" ]]; then
    MD_DIR="${MD_DIR:-$SELF_DIR/projects}"
    IMPORTER="$SELF_DIR/project_db_v2.py"
    if [[ -f "$IMPORTER" && -d "$MD_DIR" ]]; then
        "$PYTHON" "$IMPORTER" import "$MD_DIR" --db "$DB_PATH" 2>/dev/null || true
    fi
fi

# 서브명령이 없으면 도움말
if [[ $# -eq 0 ]]; then
    echo '{"status":"error","data":{"error":"서브명령 필요. 예: query, search, cross, dashboard, pipeline, contacts, weekly"}}'
    exit 1
fi

SUBCMD="$1"
shift

# MCP 도구 호출: 인수 없고 stdin에 JSON이 있으면 인수로 전달
# mail-append, update, add-action 등 JSON 파라미터를 받는 모든 명령에 적용
if [[ $# -eq 0 && ! -t 0 ]]; then
    JSON_INPUT=$(cat)
    if [[ -n "$JSON_INPUT" ]]; then
        exec "$PYTHON" "$VEGA_CLI" "$SUBCMD" "$JSON_INPUT"
    fi
fi

# vega.py로 전달 (JSON 기본 출력)
exec "$PYTHON" "$VEGA_CLI" "$SUBCMD" "$@"
