#!/usr/bin/env python3
"""Run an RSI L4 coding prompt through the authenticated Codex CLI."""

from __future__ import annotations

import argparse
import os
import shutil
import signal
import subprocess
import sys
from pathlib import Path


TIMEOUT_EXIT = 124


def resolve_codex(explicit: str = "") -> str | None:
    """Resolve the configured Codex binary without shell evaluation."""
    candidate = explicit.strip()
    if candidate:
        path = Path(candidate).expanduser()
        return str(path) if path.is_file() and os.access(path, os.X_OK) else None
    from_path = shutil.which("codex")
    if from_path:
        return from_path

    # systemd user services commonly inherit a minimal PATH that omits the
    # standard per-user install directory used by the Codex installer.
    user_install = Path.home() / ".local" / "bin" / "codex"
    if user_install.is_file() and os.access(user_install, os.X_OK):
        return str(user_install)
    return None


def build_command(codex: str, worktree: Path, prod_dir: Path) -> list[str]:
    """Build the bounded, non-interactive command used by the dispatcher."""
    return [
        codex,
        "exec",
        "-C",
        str(worktree),
        "-s",
        "workspace-write",
        "--add-dir",
        str(prod_dir / ".git"),
        "-c",
        "sandbox_workspace_write.network_access=true",
        "--ephemeral",
        "--color",
        "never",
        "-",
    ]


def preflight(codex: str, timeout: float = 15) -> bool:
    """Return whether Codex has a usable, non-interactive login."""
    try:
        result = subprocess.run(
            [codex, "login", "status"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return False
    return result.returncode == 0


def run_codex(
    codex: str,
    worktree: Path,
    prod_dir: Path,
    prompt: str,
    timeout: float,
) -> int:
    """Run Codex and terminate its whole process group on timeout."""
    try:
        proc = subprocess.Popen(
            build_command(codex, worktree, prod_dir),
            stdin=subprocess.PIPE,
            start_new_session=True,
        )
    except OSError as exc:
        print(f"failed to start Codex: {exc}", file=sys.stderr)
        return 1

    try:
        proc.communicate(prompt.encode("utf-8"), timeout=timeout)
    except subprocess.TimeoutExpired:
        os.killpg(proc.pid, signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait()
        return TIMEOUT_EXIT
    return proc.returncode or 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check binary and login only")
    parser.add_argument("--worktree")
    parser.add_argument("--prod-dir")
    parser.add_argument("--timeout", type=float, default=7200)
    args = parser.parse_args(argv)

    codex = resolve_codex(os.environ.get("DENEB_DISPATCH_CODEX_BIN", ""))
    if codex is None:
        print("Codex binary is unavailable", file=sys.stderr)
        return 1
    if not preflight(codex):
        print("Codex is not logged in", file=sys.stderr)
        return 1
    if args.check:
        return 0
    if not args.worktree or not args.prod_dir:
        parser.error("--worktree and --prod-dir are required unless --check is used")

    prompt = sys.stdin.read()
    if not prompt.strip():
        print("dispatch prompt is empty", file=sys.stderr)
        return 1
    return run_codex(
        codex,
        Path(args.worktree).resolve(),
        Path(args.prod_dir).resolve(),
        prompt,
        args.timeout,
    )


if __name__ == "__main__":
    raise SystemExit(main())
