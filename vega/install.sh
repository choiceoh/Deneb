#!/bin/bash
set -e
echo "=== Vega 설치 ==="
INSTALL_DIR="${1:-$HOME/.vega}"

# 디렉토리 생성
mkdir -p "$INSTALL_DIR/vega"
mkdir -p "$INSTALL_DIR/models"
mkdir -p "$INSTALL_DIR/projects"
mkdir -p "$INSTALL_DIR/bin"

# 스크립트 위치 기반 소스 탐색
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Vega 소스 복사
if [ -d "$SCRIPT_DIR" ]; then
  # Python 소스 + 패키지 디렉토리 복사
  cp "$SCRIPT_DIR"/*.py "$INSTALL_DIR/vega/" 2>/dev/null || true
  for subdir in commands addons db ml search mail editor; do
    if [ -d "$SCRIPT_DIR/$subdir" ]; then
      cp -r "$SCRIPT_DIR/$subdir" "$INSTALL_DIR/vega/" 2>/dev/null || true
    fi
  done
  echo "✅ Vega 소스 복사됨"
fi

# 기존 DB 복사 (있으면)
if [ -f "$SCRIPT_DIR/projects.db" ]; then
  cp "$SCRIPT_DIR/projects.db" "$INSTALL_DIR/vega/projects.db"
  echo "✅ 프로젝트 DB 복사됨"
fi

# 임베딩 모델 안내
if [ ! -f "$INSTALL_DIR/models/qwen3-embedding-8b-q4_k_m.gguf" ] && \
   [ ! -f "$HOME/.vega/models/qwen3-embedding-8b-q4_k_m.gguf" ]; then
  echo ""
  echo "📥 임베딩 모델 필요:"
  echo "   huggingface-cli download Qwen/Qwen3-Embedding-8B-GGUF qwen3-embedding-8b-q4_k_m.gguf --local-dir $INSTALL_DIR/models/"
fi

echo ""
echo "=== 설치 완료 ==="
echo "  Vega:     $INSTALL_DIR/vega/"
echo "  Models:   $INSTALL_DIR/models/"
echo "  Projects: $INSTALL_DIR/projects/"
echo ""
echo "사용법 (.bashrc에 추가):"
echo "  export VEGA_HOME=$INSTALL_DIR"
echo "  export DB_PATH=\$VEGA_HOME/vega/projects.db"
echo "  export MD_DIR=\$VEGA_HOME/projects"
echo "  export VEGA_MODELS_DIR=\$VEGA_HOME/models"
echo "  alias vega=\"python3 \$VEGA_HOME/vega/vega.py\""
