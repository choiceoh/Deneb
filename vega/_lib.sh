#!/bin/bash
# ──────────────────────────────────────────────
# _lib.sh — Vega 셸 스크립트 공통 유틸리티
#
# 사용법: source "$(dirname "$0")/_lib.sh"
# ──────────────────────────────────────────────

# Python 실행 파일 탐색
_find_python() {
    if command -v python3 &>/dev/null; then echo python3
    elif command -v python &>/dev/null; then echo python
    else echo python3
    fi
}
