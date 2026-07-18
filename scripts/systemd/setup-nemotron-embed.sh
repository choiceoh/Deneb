#!/usr/bin/env bash
# Install the Nemotron embedding sidecar on the production host.
#
# Idempotent: creates the venv/HF cache only when missing, then (re)installs the
# unit and restarts it. The BGE unit stays untouched — the gateway flips between
# sidecars with the DENEB_EMBEDDING_URL drop-in (rollback = remove the drop-in).
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-nemotron-embed.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"
VENV="$HOME/.deneb/nemotron-venv"
HF_CACHE="$HOME/.deneb/nemotron-hf-cache"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-nemotron-embed must be run from the production main checkout." >&2
  exit 1
fi

if [[ ! -x "$VENV/bin/python3" ]]; then
  echo "==> creating venv at $VENV"
  python3 -m venv "$VENV"
  "$VENV/bin/pip" install --upgrade pip >/dev/null
  "$VENV/bin/pip" install "sentence-transformers>=5" fastapi uvicorn "torch==2.13.*" numpy
fi

# Seed the HF model cache from the eval environment when available (hardlink
# clone — same filesystem, no duplicate 2.2G download).
if [[ ! -d "$HF_CACHE" && -d "$HOME/nemotron-eval/hf-cache" ]]; then
  echo "==> seeding HF cache from ~/nemotron-eval/hf-cache"
  cp -al "$HOME/nemotron-eval/hf-cache" "$HF_CACHE"
fi

mkdir -p "$USER_SYSTEMD_DIR"
install -m 0644 "$SCRIPT_DIR/nemotron-embed.service" "$USER_SYSTEMD_DIR/nemotron-embed.service"
systemctl --user daemon-reload
systemctl --user enable --now nemotron-embed.service

echo "Nemotron embed sidecar installed (port 8002)."
echo "Cutover : add DENEB_EMBEDDING_URL=http://127.0.0.1:8002 (+ DENEB_WIKI_SEM_FLOOR=0.30)"
echo "          to the deneb-gateway drop-in, then restart the gateway (kill -TERM)."
echo "Rollback: remove the drop-in, restart the gateway — the BGE cache is intact."
echo "Health  : curl -s http://127.0.0.1:8002/health"
