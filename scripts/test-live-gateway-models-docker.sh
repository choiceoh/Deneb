#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_NAME="${DENEB_IMAGE:-${CLAWDBOT_IMAGE:-deneb:local}}"
LIVE_IMAGE_NAME="${DENEB_LIVE_IMAGE:-${CLAWDBOT_LIVE_IMAGE:-${IMAGE_NAME}-live}}"
CONFIG_DIR="${DENEB_CONFIG_DIR:-${CLAWDBOT_CONFIG_DIR:-$HOME/.deneb}}"
WORKSPACE_DIR="${DENEB_WORKSPACE_DIR:-${CLAWDBOT_WORKSPACE_DIR:-$HOME/.deneb/workspace}}"
PROFILE_FILE="${DENEB_PROFILE_FILE:-${CLAWDBOT_PROFILE_FILE:-$HOME/.profile}}"

PROFILE_MOUNT=()
if [[ -f "$PROFILE_FILE" ]]; then
  PROFILE_MOUNT=(-v "$PROFILE_FILE":/home/node/.profile:ro)
fi

EXTERNAL_AUTH_MOUNTS=()
for auth_dir in .claude .codex .minimax .qwen; do
  host_path="$HOME/$auth_dir"
  if [[ -d "$host_path" ]]; then
    EXTERNAL_AUTH_MOUNTS+=(-v "$host_path":/host-auth/"$auth_dir":ro)
  fi
done

read -r -d '' LIVE_TEST_CMD <<'EOF' || true
set -euo pipefail
[ -f "$HOME/.profile" ] && source "$HOME/.profile" || true
for auth_dir in .claude .codex .minimax .qwen; do
  if [ -d "/host-auth/$auth_dir" ]; then
    mkdir -p "$HOME/$auth_dir"
    cp -R "/host-auth/$auth_dir/." "$HOME/$auth_dir"
    chmod -R u+rwX "$HOME/$auth_dir" || true
  fi
done
tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT
tar -C /src \
  --exclude=.git \
  --exclude=node_modules \
  --exclude=dist \
  --exclude=ui/dist \
  --exclude=ui/node_modules \
  -cf - . | tar -C "$tmp_dir" -xf -
ln -s /app/node_modules "$tmp_dir/node_modules"
ln -s /app/dist "$tmp_dir/dist"
cd "$tmp_dir"
pnpm test:live
EOF

echo "==> Build live-test image: $LIVE_IMAGE_NAME (target=build)"
docker build --target build -t "$LIVE_IMAGE_NAME" -f "$ROOT_DIR/Dockerfile" "$ROOT_DIR"

echo "==> Run gateway live model tests (profile keys)"
docker run --rm -t \
  --entrypoint bash \
  -e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 \
  -e HOME=/home/node \
  -e NODE_OPTIONS=--disable-warning=ExperimentalWarning \
  -e DENEB_LIVE_TEST=1 \
  -e DENEB_LIVE_GATEWAY_MODELS="${DENEB_LIVE_GATEWAY_MODELS:-${CLAWDBOT_LIVE_GATEWAY_MODELS:-modern}}" \
  -e DENEB_LIVE_GATEWAY_PROVIDERS="${DENEB_LIVE_GATEWAY_PROVIDERS:-${CLAWDBOT_LIVE_GATEWAY_PROVIDERS:-}}" \
  -e DENEB_LIVE_GATEWAY_MAX_MODELS="${DENEB_LIVE_GATEWAY_MAX_MODELS:-${CLAWDBOT_LIVE_GATEWAY_MAX_MODELS:-24}}" \
  -e DENEB_LIVE_GATEWAY_MODEL_TIMEOUT_MS="${DENEB_LIVE_GATEWAY_MODEL_TIMEOUT_MS:-${CLAWDBOT_LIVE_GATEWAY_MODEL_TIMEOUT_MS:-}}" \
  -v "$ROOT_DIR":/src:ro \
  -v "$CONFIG_DIR":/home/node/.deneb \
  -v "$WORKSPACE_DIR":/home/node/.deneb/workspace \
  "${EXTERNAL_AUTH_MOUNTS[@]}" \
  "${PROFILE_MOUNT[@]}" \
  "$LIVE_IMAGE_NAME" \
  -lc "$LIVE_TEST_CMD"
