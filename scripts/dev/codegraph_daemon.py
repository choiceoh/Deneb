#!/usr/bin/env python3
"""Evict a CodeGraph daemon that predates the installed CLI.

`codegraph upgrade` leaves the previous daemon listening on
`.codegraph/daemon.sock`. New CLI/MCP clients keep talking to it, so explore
ranking and rpc-name nodes stay on the old engine until something kills the
process. Serve wrappers call this immediately before `codegraph serve`.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path


def cli_version(codegraph_bin: str | None = None) -> str:
    binary = codegraph_bin or shutil.which("codegraph") or os.path.expanduser("~/.local/bin/codegraph")
    if not binary or not os.path.exists(binary):
        return ""
    try:
        out = subprocess.run(
            [binary, "--version"],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError):
        return ""
    return (out.stdout or out.stderr or "").strip().lstrip("v")


def _pid_command(pid: int) -> str:
    try:
        out = subprocess.run(
            ["ps", "-p", str(pid), "-o", "args="],
            capture_output=True,
            text=True,
            timeout=2,
        )
    except (OSError, subprocess.SubprocessError):
        return ""
    return (out.stdout or "").strip()


def evict_stale_daemon(root: str, *, expected: str = "", kill=os.kill, pid_command=_pid_command) -> str:
    """Return absent | fresh | evicted | skipped. Never raises."""
    root_path = Path(root)
    pidfile = root_path / ".codegraph" / "daemon.pid"
    if not pidfile.is_file():
        return "absent"
    try:
        meta = json.loads(pidfile.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return "skipped"
    running = str(meta.get("version") or "").strip().lstrip("v")
    want = (expected or cli_version()).strip().lstrip("v")
    if not running or not want or running == want:
        return "fresh" if running == want and want else "skipped"
    try:
        pid = int(meta.get("pid"))
    except (TypeError, ValueError):
        return "skipped"
    cmd = pid_command(pid)
    if cmd and "codegraph" not in cmd:
        return "skipped"
    if cmd:
        try:
            kill(pid, 15)
        except OSError:
            pass
    sock = root_path / ".codegraph" / "daemon.sock"
    for stale in (pidfile, sock):
        try:
            stale.unlink()
        except OSError:
            pass
    return "evicted"


def main(argv: list[str] | None = None) -> int:
    args = argv if argv is not None else sys.argv[1:]
    if len(args) < 2 or args[0] != "evict":
        print("usage: codegraph_daemon.py evict <root>", file=sys.stderr)
        return 2
    print(evict_stale_daemon(args[1]))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — serve wrappers must never fail closed
        sys.exit(0)
