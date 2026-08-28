#!/usr/bin/env python3
"""Run an RSI L4 coding prompt through the authenticated Codex CLI.

Sessions are persisted (no ``--ephemeral``) so the cross-harness behavior
miner (scripts/audit/harness_behavior_miner.py) can observe the fleet's main
Codex consumer. After each run the rollout files whose recorded session cwd
lies under the dispatch worktree root are moved out of ``$CODEX_HOME/sessions``
into a dedicated archive, keeping the operator's interactive Codex session
list clean and giving the archive its own retention window. The archive nests
a ``.codex/sessions`` suffix because numbat classifies artifacts by vendor
directory layout ("Preserve vendor directory layouts when scanning copied or
mounted artifacts") — a rollout outside such a path parses to zero sessions.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path


TIMEOUT_EXIT = 124

SESSION_ARCHIVE_SUBDIR = Path("data") / "coding_dispatch_sessions" / ".codex" / "sessions"
DEFAULT_SESSION_RETENTION_DAYS = 90


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
        "--color",
        "never",
        "-",
    ]


def rollout_session_cwd(path: Path) -> str | None:
    """Recorded session cwd from a rollout's first-line session_meta, or None.

    Codex 0.144.x writes ``{"type": "session_meta", "payload": {"cwd": ...}}``;
    the pre-0.144 schema carried cwd at the top level. Anything unreadable or
    schema-less returns None and the file is left untouched.
    """
    try:
        with path.open(encoding="utf-8") as fh:
            meta = json.loads(fh.readline())
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        return None
    if not isinstance(meta, dict):
        return None
    payload = meta.get("payload")
    if isinstance(payload, dict) and isinstance(payload.get("cwd"), str):
        return payload["cwd"]
    cwd = meta.get("cwd")
    return cwd if isinstance(cwd, str) else None


def archive_session_rollouts(
    codex_home: Path,
    dispatch_root: Path,
    archive_dir: Path,
    retention_days: int = DEFAULT_SESSION_RETENTION_DAYS,
    now_s: float | None = None,
) -> tuple[int, int]:
    """Move dispatch-session rollouts into the archive and prune old ones.

    A rollout belongs to the dispatch lane iff its recorded session cwd is at
    or under ``dispatch_root`` (the worktree root), so operator sessions are
    never touched and orphans from a crashed prior run are swept on the next
    dispatch. Archived files older than ``retention_days`` (mtime) are deleted;
    0 disables pruning. Never raises — mining persistence must not affect the
    dispatch result. Returns (archived, pruned).
    """
    archived = pruned = 0
    for path in sorted(codex_home.glob("sessions/**/rollout-*.jsonl")):
        cwd = rollout_session_cwd(path)
        if cwd is None:
            continue
        try:
            if not Path(cwd).is_relative_to(dispatch_root):
                continue
        except ValueError:
            continue
        try:
            archive_dir.mkdir(parents=True, exist_ok=True)
            shutil.move(str(path), str(archive_dir / path.name))
            archived += 1
        except (OSError, shutil.Error) as exc:
            print(f"rollout archive failed for {path}: {exc}", file=sys.stderr)
    if retention_days > 0 and archive_dir.is_dir():
        cutoff = (time.time() if now_s is None else now_s) - retention_days * 86400
        for path in sorted(archive_dir.glob("rollout-*.jsonl")):
            try:
                if path.stat().st_mtime < cutoff:
                    path.unlink()
                    pruned += 1
            except OSError:
                continue
    return archived, pruned


def session_retention_days() -> int:
    raw = os.environ.get("DENEB_DISPATCH_SESSION_RETENTION_DAYS", "").strip()
    try:
        return int(raw) if raw else DEFAULT_SESSION_RETENTION_DAYS
    except ValueError:
        return DEFAULT_SESSION_RETENTION_DAYS


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
    worktree = Path(args.worktree).resolve()
    rc = run_codex(codex, worktree, Path(args.prod_dir).resolve(), prompt, args.timeout)
    # Archive on every exit path (timeout included — rollouts are written
    # incrementally, so a killed session still leaves a minable partial file).
    state_dir = Path(os.environ.get("DENEB_STATE_DIR") or Path.home() / ".deneb").expanduser()
    codex_home = Path(os.environ.get("CODEX_HOME") or Path.home() / ".codex").expanduser()
    archived, pruned = archive_session_rollouts(
        codex_home,
        worktree.parent,
        state_dir / SESSION_ARCHIVE_SUBDIR,
        retention_days=session_retention_days(),
    )
    if archived or pruned:
        print(f"session rollouts: archived {archived}, pruned {pruned}", file=sys.stderr)
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
