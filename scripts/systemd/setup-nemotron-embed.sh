#!/usr/bin/env bash
# Install the Nemotron NVFP4 embedding stack on the production host:
# eugr spark-vllm container backend (8003) + stdlib adapter (8002).
#
# Idempotent. The BGE unit stays untouched — the gateway flips between sidecars
# with the DENEB_EMBEDDING_URL drop-in (rollback = remove the drop-in; the BGE
# semantic cache is fingerprint-keyed and survives intact).
#
# Prereqs on the host (one-time, operator-space): docker with the nvidia
# runtime and the image ghcr.io/spark-arena/dgx-vllm-eugr-nightly:latest
# (prebuilt sm_121a wheels — pip vLLM JIT-compiles fp4 kernels on GB10 and
# trips earlyoom, which is why there is no venv here).
#
# Usage (from ~/deneb on main):
#   scripts/systemd/setup-nemotron-embed.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

cd "$REPO_DIR"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "ERROR: setup-nemotron-embed must be run from the production main checkout." >&2
  exit 1
fi

if ! docker image inspect ghcr.io/spark-arena/dgx-vllm-eugr-nightly:latest >/dev/null 2>&1; then
  echo "ERROR: eugr image missing — docker pull ghcr.io/spark-arena/dgx-vllm-eugr-nightly:latest" >&2
  exit 1
fi

mkdir -p "$USER_SYSTEMD_DIR" "$HOME/.deneb/nemotron-hf-cache"
install -m 0644 "$SCRIPT_DIR/nemotron-vllm.service" "$USER_SYSTEMD_DIR/nemotron-vllm.service"
install -m 0644 "$SCRIPT_DIR/nemotron-embed.service" "$USER_SYSTEMD_DIR/nemotron-embed.service"
systemctl --user daemon-reload
systemctl --user enable --now nemotron-vllm.service
systemctl --user enable --now nemotron-embed.service

echo "Nemotron NVFP4 embed stack installed (backend :8003, adapter :8002)."
echo "Cutover : deneb-gateway drop-in with DENEB_EMBEDDING_URL=http://127.0.0.1:8002"
echo "          (+ DENEB_WIKI_SEM_FLOOR=0.30), then restart the gateway (kill -TERM)."
echo "Rollback: remove the drop-in, restart the gateway — the BGE cache is intact."
echo "Health  : curl -s http://127.0.0.1:8002/health"
