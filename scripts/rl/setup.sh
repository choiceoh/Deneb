#!/bin/bash
# Install sglang + Tinker-Atropos in a managed Python venv.
# Run once on DGX Spark setup.
#
# Prerequisites:
#   - Python 3.10+ with venv support
#   - CUDA toolkit (for GPU inference/training)
#   - pip
#
# Usage:
#   ./scripts/rl/setup.sh [VENV_DIR]
#   Default: ~/.deneb/rl/venv
set -euo pipefail

VENV_DIR="${1:-${HOME}/.deneb/rl/venv}"

echo "[rl-setup] Creating venv: ${VENV_DIR}"
python3 -m venv "${VENV_DIR}"
# shellcheck disable=SC1091
source "${VENV_DIR}/bin/activate"

echo "[rl-setup] Upgrading pip"
pip install --upgrade pip wheel setuptools

echo "[rl-setup] Installing sglang (inference server)"
pip install "sglang[all]"

echo "[rl-setup] Installing Tinker (LoRA trainer)"
pip install git+https://github.com/NousResearch/Tinker.git

echo "[rl-setup] Installing Atropos (trajectory API)"
pip install git+https://github.com/NousResearch/Atropos.git

echo "[rl-setup] Installing PyTorch + PEFT (for adapter conversion)"
pip install torch peft transformers

echo "[rl-setup] Verifying installation"
python3 -c "import sglang; print(f'  sglang {sglang.__version__}')" 2>/dev/null || echo "  sglang: FAILED"
python3 -c "import tinker; print('  tinker: OK')" 2>/dev/null || echo "  tinker: FAILED"
python3 -c "import atropos; print('  atropos: OK')" 2>/dev/null || echo "  atropos: FAILED"
python3 -c "import peft; print(f'  peft {peft.__version__}')" 2>/dev/null || echo "  peft: FAILED"

echo ""
echo "[rl-setup] Done. Venv: ${VENV_DIR}"
echo "[rl-setup] Add to deneb config: rl.venvDir = \"${VENV_DIR}\""
