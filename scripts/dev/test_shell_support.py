"""Shared isolation helpers for tests that execute repository shell scripts."""

from __future__ import annotations

import os
import subprocess
import textwrap
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]


def write_executable(path: Path, body: str) -> Path:
    """Write a dedented executable fixture and return its path."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(textwrap.dedent(body).lstrip("\n"), encoding="utf-8")
    path.chmod(0o755)
    return path


def isolated_env(home: Path, fake_bin: Path | None = None, **values: str) -> dict[str, str]:
    """Build a deterministic environment while retaining standard Unix tools."""
    path = os.environ.get("PATH", "")
    if fake_bin is not None:
        path = f"{fake_bin}{os.pathsep}{path}"
    env = {
        "HOME": str(home),
        "PATH": path,
        "LANG": "C.UTF-8",
        "LC_ALL": "C.UTF-8",
    }
    env.update(values)
    return env


def run_script(
    relative_path: str,
    *args: str,
    env: dict[str, str] | None = None,
    cwd: Path | None = None,
    input_text: str | None = None,
    timeout: float = 15,
) -> subprocess.CompletedProcess[str]:
    """Execute one checked-in script without raising on its exit status."""
    return subprocess.run(
        [str(REPO_ROOT / relative_path), *args],
        cwd=str(cwd or REPO_ROOT),
        env=env,
        input=input_text,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )


def extract_heredoc(path: Path, opener: str, terminator: str) -> str:
    """Return the executable body between an exact shell heredoc pair."""
    lines = path.read_text(encoding="utf-8").splitlines()
    try:
        start = lines.index(opener) + 1
        end = lines.index(terminator, start)
    except ValueError as exc:
        raise AssertionError(f"missing heredoc contract in {path}") from exc
    return "\n".join(lines[start:end]) + "\n"
