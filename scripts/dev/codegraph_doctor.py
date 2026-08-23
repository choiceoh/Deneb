#!/usr/bin/env python3
"""Report CodeGraph health for every wired coding tool.

Fail-open: prints findings and exits 0 unless `--strict`. Used as a
session-start / upgrade check, not a hook gate.
"""

from __future__ import annotations

import json
import os
import sqlite3
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from codegraph_daemon import cli_version  # noqa: E402
from codegraph_mcp_restore import is_upgrade_clobber  # noqa: E402
from codegraph_root import is_prod_checkout, pick_root  # noqa: E402

MIN_VERSION = "1.5.0"


def _ver_tuple(value: str) -> tuple[int, ...]:
    parts = []
    for chunk in value.lstrip("v").split("."):
        if not chunk.isdigit():
            break
        parts.append(int(chunk))
    return tuple(parts) or (0,)


def check_cli() -> dict:
    version = cli_version()
    ok = bool(version) and _ver_tuple(version) >= _ver_tuple(MIN_VERSION)
    return {"id": "cli", "ok": ok, "detail": version or "missing"}


def check_root(repo: str) -> dict:
    root = pick_root(repo, agent="auto")
    ok = os.path.isdir(root) and not is_prod_checkout(root)
    return {"id": "root", "ok": ok, "detail": root}


def check_index(repo: str) -> dict:
    db = Path(repo) / ".codegraph" / "codegraph.db"
    if not db.is_file():
        return {"id": "index", "ok": False, "detail": "no .codegraph/codegraph.db"}
    try:
        con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
        nodes = con.execute("SELECT COUNT(*) FROM nodes").fetchone()[0]
        rpc = con.execute("SELECT COUNT(*) FROM nodes WHERE id LIKE 'rpcmap:%'").fetchone()[0]
        con.close()
    except sqlite3.Error as exc:
        return {"id": "index", "ok": False, "detail": str(exc)}
    return {"id": "index", "ok": nodes > 0, "detail": f"nodes={nodes} rpcmap={rpc}"}


def check_mcp(repo: str) -> dict:
    root = Path(repo)
    files = (
        root / ".cursor" / "mcp.json",
        root / ".mcp.json",
        root / ".zcode" / "config.json",
        root / ".trae" / "mcp.json",
        root / ".vscode" / "mcp.json",
    )
    clobbered = []
    missing = []
    for path in files:
        if not path.is_file():
            missing.append(path.name)
            continue
        try:
            cfg = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            clobbered.append(str(path))
            continue
        if is_upgrade_clobber(cfg):
            clobbered.append(str(path))
    toml = root / ".codex" / "config.toml"
    if toml.is_file():
        text = toml.read_text(encoding="utf-8")
        if "command = \"codegraph\"" in text and "serve" in text:
            clobbered.append(str(toml))
    elif not toml.is_file():
        missing.append("config.toml")
    ok = not clobbered
    detail = "ok" if ok else "clobber:" + ",".join(clobbered)
    if missing:
        detail += f" missing={','.join(missing)}"
    return {"id": "mcp", "ok": ok, "detail": detail}


def diagnose(repo: str) -> list[dict]:
    return [check_cli(), check_root(repo), check_index(repo), check_mcp(repo)]


def main(argv: list[str] | None = None) -> int:
    args = list(argv if argv is not None else sys.argv[1:])
    strict = "--strict" in args
    root = os.getcwd()
    if "--root" in args:
        i = args.index("--root")
        if i + 1 < len(args):
            root = args[i + 1]
    findings = diagnose(root)
    for item in findings:
        mark = "ok" if item["ok"] else "FAIL"
        print(f"{mark:4} {item['id']}: {item['detail']}")
    if strict and any(not item["ok"] for item in findings):
        return 1
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — convenience CLI, never break a session
        sys.exit(0)
