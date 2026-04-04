#!/usr/bin/env bash
# Deneb Git 대시보드 실행 스크립트.
# Usage:
#   scripts/git-ui.sh              # 로컬 전용 (포트 8099)
#   scripts/git-ui.sh 9000         # 포트 지정
#   scripts/git-ui.sh --remote     # Tailscale 원격 접속 허용
#   scripts/git-ui.sh --remote 9000
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$REPO_ROOT/tools/git-ui/git-ui"

BIND="127.0.0.1"
PORT="8099"

for arg in "$@"; do
  case "$arg" in
    --remote) BIND="0.0.0.0" ;;
    *)        PORT="$arg" ;;
  esac
done

# Build if needed
if [[ ! -f "$BINARY" ]] || [[ "$REPO_ROOT/tools/git-ui/main.go" -nt "$BINARY" ]] || [[ "$REPO_ROOT/tools/git-ui/static/index.html" -nt "$BINARY" ]]; then
  echo "빌드 중..."
  (cd "$REPO_ROOT/tools/git-ui" && go build -o git-ui .)
fi

exec "$BINARY" -bind "$BIND" -port "$PORT" -repo "$REPO_ROOT"
