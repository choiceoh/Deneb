"""Preflight and reproducibility tests for the isolated BGE CUDA build script."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script, write_executable


class CUDABuildShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.cuda = self.root / "cuda"
        self.venv = self.root / "venv"
        self.log = self.root / "calls.log"
        self.home.mkdir()
        self.bin.mkdir()
        (self.cuda / "bin").mkdir(parents=True)
        (self.venv / "bin").mkdir(parents=True)
        write_executable(self.cuda / "bin/nvcc", """
            #!/usr/bin/env bash
            printf 'nvcc %s\n' "$*" >> "$FAKE_LOG"
            echo 'Cuda compilation tools, release 13.0, V13.0.88'
        """)
        write_executable(self.bin / "nvidia-smi", """
            #!/usr/bin/env bash
            printf 'nvidia-smi %s\n' "$*" >> "$FAKE_LOG"
            echo 'NVIDIA GB10, 12.1'
        """)
        write_executable(self.venv / "bin/pip", """
            #!/usr/bin/env bash
            printf 'pip CUDACXX=%s CMAKE_ARGS=%s args=%s\n' \
              "${CUDACXX:-}" "${CMAKE_ARGS:-}" "$*" >> "$FAKE_LOG"
            exit "${PIP_RC:-0}"
        """)
        write_executable(self.venv / "bin/python3", """
            #!/usr/bin/env bash
            printf 'venv-python %s\n' "$*" >> "$FAKE_LOG"
            cat >/dev/null
            echo "llama_supports_gpu_offload: ${VERIFY_GPU:-1}"
            [[ "${VERIFY_GPU:-1}" == 1 ]]
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "FAKE_LOG": str(self.log),
            "BGE_GPU_VENV": str(self.venv),
            "CUDA_HOME": str(self.cuda),
            "CUDA_ARCH": "121",
            "LLAMA_CPP_VERSION": "0.3.16",
            "VERIFY_GPU": "1",
            "PIP_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def calls(self) -> str:
        return self.log.read_text(encoding="utf-8") if self.log.exists() else ""

    def test_missing_nvcc_fails_before_gpu_probe_or_package_install(self) -> None:
        missing = self.root / "missing-cuda"
        proc = run_script(
            "scripts/deploy/bge-m3-build-cuda.sh",
            env=self.env(CUDA_HOME=str(missing)),
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn(f"ERROR: nvcc not found at {missing}/bin", proc.stdout)
        self.assertEqual(self.calls(), "")

    def test_existing_venv_installs_runtime_then_source_build_with_exact_cuda_flags(self) -> None:
        proc = run_script("scripts/deploy/bge-m3-build-cuda.sh", env=self.env(), timeout=10)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("nvcc --version", calls)
        self.assertIn("nvidia-smi --query-gpu=name,compute_cap --format=csv,noheader", calls)
        self.assertIn("pip CUDACXX= CMAKE_ARGS= args=install --quiet --upgrade pip", calls)
        self.assertIn("args=install --quiet --require-hashes -r ", calls)
        self.assertIn("requirements.lock", calls)
        self.assertNotIn("args=install --quiet fastapi uvicorn pydantic numpy", calls)
        self.assertIn(f"CUDACXX={self.cuda}/bin/nvcc", calls)
        self.assertIn("CMAKE_ARGS=-DGGML_CUDA=on -DCMAKE_CUDA_ARCHITECTURES=121", calls)
        self.assertIn(
            "--no-binary llama-cpp-python llama-cpp-python==0.3.16 "
            "--force-reinstall --no-cache-dir",
            calls,
        )
        self.assertIn("venv-python -", calls)
        self.assertIn(f"GPU runtime ready at {self.venv}", proc.stdout)

    def test_architecture_and_llama_version_overrides_reach_build_command(self) -> None:
        proc = run_script(
            "scripts/deploy/bge-m3-build-cuda.sh",
            env=self.env(CUDA_ARCH="89", LLAMA_CPP_VERSION="0.4.0"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("CMAKE_CUDA_ARCHITECTURES=89", calls)
        self.assertIn("llama-cpp-python==0.4.0", calls)
        self.assertIn("building llama-cpp-python 0.4.0 with CUDA (sm_89)", proc.stdout)

    def test_failed_pip_build_stops_before_gpu_verification(self) -> None:
        proc = run_script(
            "scripts/deploy/bge-m3-build-cuda.sh",
            env=self.env(PIP_RC="6"),
        )
        self.assertEqual(proc.returncode, 6)
        self.assertNotIn("venv-python", self.calls())
        self.assertNotIn("GPU runtime ready", proc.stdout)

    def test_gpu_capability_verification_failure_propagates_nonzero(self) -> None:
        proc = run_script(
            "scripts/deploy/bge-m3-build-cuda.sh",
            env=self.env(VERIFY_GPU="0"),
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("llama_supports_gpu_offload: 0", proc.stdout)
        self.assertNotIn("GPU runtime ready", proc.stdout)


if __name__ == "__main__":
    unittest.main()
