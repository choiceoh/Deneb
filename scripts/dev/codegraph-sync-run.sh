#!/usr/bin/env bash
# Shared CodeGraph refresh: sync → rpcmap inject → optional semantic index.
# Caller backgrounds this; the script itself runs the steps in order.
# Fail-open: any step missing a binary/index is a no-op.
set -uo pipefail

ROOT="${1:-}"
[[ -n "$ROOT" && -d "$ROOT/.codegraph" ]] || exit 0
ROOT=$(cd "$ROOT" && pwd) || exit 0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

CG=""
for c in codegraph \
         "${HOME}/.local/bin/codegraph" \
         "${HOME}/.npm-global/bin/codegraph"; do
  if command -v "$c" >/dev/null 2>&1; then
    CG="$c"
    break
  fi
done
[[ -n "$CG" ]] || exit 0

cd "$ROOT" || exit 0
bash -lc "$CG sync" >/dev/null 2>&1 || true
python3 "$HERE/rpcmap_codegraph_sync.py" "$ROOT" >/dev/null 2>&1 || true

SEM="$ROOT/.codegraph/semantic-code.json"
STAMP="$ROOT/.codegraph/.semantic-sync-stamp"
if [[ -f "$SEM" && -d "$ROOT/gateway-go" ]]; then
  now=$(date +%s)
  last=0
  [[ -f "$STAMP" ]] && last=$(cat "$STAMP" 2>/dev/null || echo 0)
  if (( now - last >= 120 )) && \
     curl -sf -m 1 "${DENEB_EMBEDDING_URL:-http://127.0.0.1:8002}/health" >/dev/null 2>&1; then
    echo "$now" >"$STAMP"
    (cd "$ROOT/gateway-go" && bash -lc "go run ./cmd/codesearch index") >/dev/null 2>&1 || true
  fi
fi
exit 0
