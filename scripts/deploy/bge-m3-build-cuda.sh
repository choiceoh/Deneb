#!/usr/bin/env bash
# bge-m3-build-cuda.sh — reproducibly build the GPU (CUDA) runtime for the
# BGE-M3 embedding server (bge-m3-server.py) on the DGX Spark GB10.
#
# WHY THIS EXISTS: the stock llama-cpp-python wheel is CPU-only
# (llama_supports_gpu_offload() == False), which pinned BGE-M3 embedding to the
# CPU (~34ms per query, ~200ms for a 3-query recall turn). Rebuilding the wheel
# against the on-box CUDA 13 toolkit for GB10 (compute capability 12.1 / sm_121)
# offloads all layers to the GPU and cuts that to ~10ms / ~27ms — an ~7-8x
# speedup on the dominant wiki/file recall cost, with the SAME Q5_K_M GGUF so
# cached vectors stay valid (no re-embed). vLLM keeps ~56GB of the 122GB unified
# memory; the embedder adds ~1GB, well within headroom.
#
# The build lives in an ISOLATED venv so the stock CPU wheel in ~/.local stays
# intact as an instant rollback (point the systemd unit back at /usr/bin/python3
# with --gpu-layers 0). Idempotent: re-running reuses the venv and reinstalls.
#
# After a successful build, point the bge-m3 systemd user unit at this venv:
#   ExecStart=<VENV>/bin/python3 <repo>/scripts/deploy/bge-m3-server.py \
#             --port 8001 --host 127.0.0.1 --gpu-layers 99
#   systemctl --user daemon-reload && systemctl --user restart bge-m3.service
set -euo pipefail

VENV="${BGE_GPU_VENV:-$HOME/.deneb/bge-gpu-venv}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME_REQUIREMENTS="${BGE_RUNTIME_REQUIREMENTS:-$SCRIPT_DIR/../../requirements.lock}"
LLAMA_CPP_VERSION="${LLAMA_CPP_VERSION:-0.3.16}"
# GB10 = Blackwell, compute capability 12.1 → CMAKE_CUDA_ARCHITECTURES=121.
# Override for a different GPU: `nvidia-smi --query-gpu=compute_cap --format=csv,noheader`
# (e.g. "8.9" → 89). Defaults here target this fleet's srv4.
CUDA_ARCH="${CUDA_ARCH:-121}"
CUDA_HOME="${CUDA_HOME:-/usr/local/cuda}"

echo "── preflight"
command -v "$CUDA_HOME/bin/nvcc" >/dev/null || { echo "ERROR: nvcc not found at $CUDA_HOME/bin (install the CUDA toolkit)"; exit 1; }
"$CUDA_HOME/bin/nvcc" --version | tail -1
nvidia-smi --query-gpu=name,compute_cap --format=csv,noheader | head -1

echo "── venv @ $VENV"
[ -d "$VENV" ] || python3 -m venv "$VENV"
"$VENV/bin/pip" install --quiet --upgrade pip
# Runtime deps for bge-m3-server.py are fully pinned, including transitive
# wheels and hashes. llama-cpp-python remains the target-specific source build
# below because its CUDA architecture cannot be represented by a portable lock.
[ -f "$RUNTIME_REQUIREMENTS" ] || { echo "ERROR: runtime lock not found: $RUNTIME_REQUIREMENTS"; exit 1; }
"$VENV/bin/pip" install --quiet --require-hashes -r "$RUNTIME_REQUIREMENTS"

echo "── building llama-cpp-python $LLAMA_CPP_VERSION with CUDA (sm_$CUDA_ARCH) — this compiles the ggml-cuda kernels, ~15-40min on aarch64"
export PATH="$CUDA_HOME/bin:$PATH"
export CUDACXX="$CUDA_HOME/bin/nvcc"
export CMAKE_ARGS="-DGGML_CUDA=on -DCMAKE_CUDA_ARCHITECTURES=$CUDA_ARCH"
"$VENV/bin/pip" install --no-binary llama-cpp-python "llama-cpp-python==$LLAMA_CPP_VERSION" \
  --force-reinstall --no-cache-dir

echo "── verify GPU offload"
"$VENV/bin/python3" - <<'PY'
import sys
import llama_cpp
ok = llama_cpp.llama_supports_gpu_offload()
print("llama_supports_gpu_offload:", ok)
sys.exit(0 if ok else 1)
PY

echo "✅ GPU runtime ready at $VENV — repoint the bge-m3 unit (see header) and restart."
