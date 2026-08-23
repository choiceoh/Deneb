#!/usr/bin/env python3
"""Resolve the CodeGraph serve root for every coding agent.

Cursor already pins `active-root`; ZCode/Claude/Codex/Trae historically
served `pwd` (often the main checkout) while edits landed in a linked
worktree. One resolver, same rules:

  1) explicit env (CURSOR_WORKTREE / DENEB_AGENT_ROOT / …)
  2) session-scoped active-root.<sid>
  3) cwd when it is a usable checkout
  4) that agent's global active-root
  5) never ~/deneb — fall back to ~/deneb-dev

Agents do not steal each other's newest worktree (concurrent sessions).
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
from pathlib import Path

SAFE_ID = re.compile(r"[^A-Za-z0-9._-]+")

AGENT_BASES = {
    "cursor": (".cursor/worktrees/Deneb",),
    "zcode": (".zcode/worktrees/Deneb",),
    "claude": (".claude/worktrees/Deneb",),
    "codex": (".codex/worktrees/Deneb", ".codex/worktrees"),
    "trae": (".trae/worktrees/Deneb",),
}

ENV_ROOTS = (
    "CURSOR_WORKTREE",
    "DENEB_AGENT_ROOT",
    "ZCODE_WORKTREE",
    "CLAUDE_WORKTREE",
    "CODEX_WORKTREE",
    "TRAE_WORKTREE",
)

SESSION_ENV = {
    "cursor": ("CURSOR_SESSION_ID",),
    "zcode": ("ZCODE_SESSION_ID", "CLAUDE_SESSION_ID"),
    "claude": ("CLAUDE_SESSION_ID",),
    "codex": ("CODEX_THREAD_ID", "CODEX_SESSION_ID"),
    "trae": ("TRAE_SESSION_ID",),
}


def detect_agent(env: dict[str, str] | None = None) -> str:
    env = env if env is not None else os.environ
    forced = (env.get("CODEGRAPH_AGENT") or "").strip().lower()
    if forced in AGENT_BASES:
        return forced
    if env.get("CURSOR_SESSION_ID") or env.get("CURSOR_WORKTREE"):
        return "cursor"
    if env.get("ZCODE_PROJECT_DIR") or env.get("ZCODE_SESSION_ID") or env.get("ZCODE_WORKTREE"):
        return "zcode"
    if env.get("CODEX_THREAD_ID") or env.get("CODEX_SESSION_ID") or env.get("CODEX_WORKTREE"):
        return "codex"
    if env.get("TRAE_SESSION_ID") or env.get("TRAE_WORKTREE"):
        return "trae"
    if env.get("CLAUDE_SESSION_ID") or env.get("CLAUDE_WORKTREE"):
        return "claude"
    return "auto"


def sanitize_sid(sid: str) -> str:
    return SAFE_ID.sub("_", sid).strip("_")[:80]


def _home(home: Path | None = None) -> Path:
    return Path(home) if home is not None else Path.home()


def prod_checkout(home: Path | None = None) -> Path:
    return _home(home) / "deneb"


def dev_checkout(home: Path | None = None) -> Path:
    return _home(home) / "deneb-dev"


def is_prod_checkout(candidate: str, home: Path | None = None) -> bool:
    prod = prod_checkout(home)
    if not candidate or not os.path.isdir(candidate) or not prod.is_dir():
        return False
    try:
        return Path(candidate).resolve() == prod.resolve()
    except OSError:
        return False


def _git_rev_parse(root: str, flag: str) -> str:
    try:
        out = subprocess.run(
            ["git", "-C", root, "rev-parse", flag],
            capture_output=True,
            text=True,
            timeout=3,
        )
    except (OSError, subprocess.SubprocessError):
        return ""
    if out.returncode != 0:
        return ""
    raw = out.stdout.strip()
    if not raw:
        return ""
    path = raw if os.path.isabs(raw) else os.path.normpath(os.path.join(root, raw))
    try:
        return str(Path(path).resolve())
    except OSError:
        return ""


def git_common_dir(root: str) -> str:
    return _git_rev_parse(root, "--git-common-dir")


def is_linked_worktree(root: str) -> bool:
    git_dir = _git_rev_parse(root, "--git-dir")
    common = git_common_dir(root)
    return bool(git_dir and common and git_dir != common)


def usable_root(candidate: str, anchor: str, home: Path | None = None) -> bool:
    if not candidate or not os.path.isdir(candidate):
        return False
    if is_prod_checkout(candidate, home):
        return False
    cand_git, anchor_git = git_common_dir(candidate), git_common_dir(anchor)
    if cand_git and anchor_git:
        return cand_git == anchor_git
    return True


def agent_bases(agent: str, home: Path | None = None) -> list[Path]:
    home_path = _home(home)
    names = AGENT_BASES.get(agent, ())
    if agent == "auto":
        names = tuple(p for paths in AGENT_BASES.values() for p in paths)
    return [home_path / rel for rel in names]


def session_id_for(agent: str, env: dict[str, str] | None = None) -> str:
    env = env if env is not None else os.environ
    for key in SESSION_ENV.get(agent, ()):
        value = (env.get(key) or "").strip()
        if value:
            return sanitize_sid(value)
    return ""


def read_active_root(base: Path, sid: str = "") -> str:
    if sid:
        pin = base / f"active-root.{sid}"
        if pin.is_file():
            try:
                return pin.read_text(encoding="utf-8").splitlines()[0].strip()
            except OSError:
                pass
    fallback = base / "active-root"
    if fallback.is_file():
        try:
            return fallback.read_text(encoding="utf-8").splitlines()[0].strip()
        except OSError:
            return ""
    return ""


def write_active_root(agent: str, path: str, sid: str = "", home: Path | None = None) -> list[str]:
    """Pin a worktree for this agent. Session file first, then global fallback."""
    written: list[str] = []
    bases = agent_bases(agent, home)
    if not bases:
        return written
    base = bases[0]
    try:
        base.mkdir(parents=True, exist_ok=True)
    except OSError:
        return written
    if sid:
        pin = base / f"active-root.{sanitize_sid(sid)}"
        _atomic_write(pin, path)
        written.append(str(pin))
    global_pin = base / "active-root"
    _atomic_write(global_pin, path)
    written.append(str(global_pin))
    return written


def _atomic_write(path: Path, text: str) -> None:
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_text(text.rstrip() + "\n", encoding="utf-8")
    tmp.replace(path)


def _env_candidates(env: dict[str, str]) -> list[str]:
    out: list[str] = []
    for key in ENV_ROOTS:
        value = (env.get(key) or "").strip()
        if value:
            out.append(value)
    return out


def pick_root(
    fallback: str,
    *,
    agent: str = "auto",
    env: dict[str, str] | None = None,
    home: Path | None = None,
) -> str:
    env = env if env is not None else dict(os.environ)
    home_path = _home(home)
    if not fallback:
        fallback = os.getcwd()
    try:
        fallback = str(Path(fallback).resolve()) if os.path.isdir(fallback) else fallback
    except OSError:
        pass
    anchor = fallback
    resolved_agent = agent if agent != "auto" else detect_agent(env)
    sid = session_id_for(resolved_agent, env)

    for candidate in _env_candidates(env):
        if usable_root(candidate, anchor, home_path):
            return str(Path(candidate).resolve())

    # Unknown agent: do not steal another tool's active-root.
    if resolved_agent == "auto":
        if os.path.isdir(fallback) and not is_prod_checkout(fallback, home_path):
            return fallback
        dev = dev_checkout(home_path)
        if is_prod_checkout(fallback, home_path) and dev.is_dir():
            return str(dev.resolve())
        return fallback

    for base in agent_bases(resolved_agent, home_path):
        if not sid:
            continue
        session_dir = base / sid
        if usable_root(str(session_dir), anchor, home_path):
            return str(session_dir.resolve())
        pinned = read_active_root(base, sid)
        if usable_root(pinned, anchor, home_path):
            return str(Path(pinned).resolve())

    # Already inside a linked worktree: stay there. Global active-root is the
    # last writer and would steal another chat's tree.
    if is_linked_worktree(fallback) and not is_prod_checkout(fallback, home_path):
        return fallback

    # Global pin before a main-checkout cwd: MCP often starts from the primary
    # tree with no session id, while SessionStart already wrote active-root.
    for base in agent_bases(resolved_agent, home_path):
        pinned = read_active_root(base, "")
        if usable_root(pinned, anchor, home_path):
            return str(Path(pinned).resolve())

    if os.path.isdir(fallback) and not is_prod_checkout(fallback, home_path):
        return fallback

    dev = dev_checkout(home_path)
    if is_prod_checkout(fallback, home_path) and dev.is_dir():
        return str(dev.resolve())
    return fallback


def main(argv: list[str] | None = None) -> int:
    args = list(argv if argv is not None else sys.argv[1:])
    if args and args[0] == "write-active":
        agent = "auto"
        path = ""
        sid = ""
        i = 1
        while i < len(args):
            if args[i] == "--agent" and i + 1 < len(args):
                agent = args[i + 1]
                i += 2
                continue
            if args[i] == "--path" and i + 1 < len(args):
                path = args[i + 1]
                i += 2
                continue
            if args[i] == "--sid" and i + 1 < len(args):
                sid = args[i + 1]
                i += 2
                continue
            i += 1
        if not path or agent == "auto":
            print("usage: codegraph_root.py write-active --agent NAME --path DIR [--sid ID]", file=sys.stderr)
            return 2
        write_active_root(agent, path, sid)
        return 0

    agent = "auto"
    fallback = os.environ.get("CURSOR_CODEGRAPH_FALLBACK") or os.getcwd()
    i = 0
    while i < len(args):
        if args[i] in ("--print-root",):
            i += 1
            continue
        if args[i] == "--agent" and i + 1 < len(args):
            agent = args[i + 1]
            i += 2
            continue
        if args[i] == "--fallback" and i + 1 < len(args):
            fallback = args[i + 1]
            i += 2
            continue
        if os.path.isdir(args[i]):
            fallback = args[i]
        i += 1
    print(pick_root(fallback, agent=agent))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — serve wrappers must never fail closed
        print(os.getcwd())
        sys.exit(0)
