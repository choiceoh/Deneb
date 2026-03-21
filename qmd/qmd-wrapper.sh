#!/bin/bash
# QMD 2.0 wrapper — Vega 프로젝트용 환경 세팅
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$SCRIPT_DIR/config}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$SCRIPT_DIR/cache}"

# 한국어 프로젝트 — Qwen3-Embedding 사용 (CJK 지원)
if [ -z "$QMD_EMBED_MODEL" ]; then
  export QMD_EMBED_MODEL="hf:Qwen/Qwen3-Embedding-8B-GGUF/Qwen3-Embedding-8B-Q4_K_M.gguf"
fi

exec qmd "$@"
