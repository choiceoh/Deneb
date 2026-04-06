#!/usr/bin/env bash
# Setup RL training environment for Deneb.
#
# Creates a Python virtualenv and installs sglang + Tinker + Atropos.
# Run once on DGX Spark before enabling DENEB_RL_ENABLED=true.
#
# Usage: scripts/rl/setup.sh [--venv-dir DIR]
set -euo pipefail

VENV_DIR="${1:-$HOME/.deneb/rl/venv}"

echo "=== Deneb RL Setup ==="
echo "  venv: $VENV_DIR"

# Create venv if needed.
if [ ! -d "$VENV_DIR" ]; then
    echo "Creating virtualenv..."
    python3 -m venv "$VENV_DIR"
fi

# Activate.
# shellcheck disable=SC1091
source "$VENV_DIR/bin/activate"

# Upgrade pip.
pip install --upgrade pip setuptools wheel

# Install sglang (inference server).
echo "Installing sglang..."
pip install "sglang[all]"

# Install Tinker (RL trainer with IS loss).
echo "Installing tinker-rl..."
pip install tinker-rl

# Install Atropos (trajectory environment server).
echo "Installing atropos..."
pip install atropos-rl

echo ""
echo "=== Setup complete ==="
echo "  Activate: source $VENV_DIR/bin/activate"
echo "  Enable:   export DENEB_RL_ENABLED=true"
echo "  Model:    export DENEB_RL_MODEL=<huggingface-model-path>"
