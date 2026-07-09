#!/usr/bin/env python3
"""PreToolUse nudge: steer symbol-name text searches to CodeGraph.

Reads the Claude Code PreToolUse payload on stdin (Grep / Bash). When an agent
greps a BARE IDENTIFIER that CodeGraph's index knows as an actual definition
(exact name match), block that one call with a pointer to the structural tools
(the codegraph_explore MCP tool, or `codegraph callers/impact` on the CLI). The
retry — or any genuine text search (TODO, log strings, regex) — passes silently.

Discriminator: `codegraph query <pat> --json` must return a node whose `name`
equals the pattern exactly. Common words (TODO, error, handler) either miss or
match a differently-cased symbol, so they are never blocked.

Blocks once per (session, pattern): the immediate retry passes via the marker.
Fail-open by design: codegraph missing, index unbuilt, parse error, timeout →
exit 0 (never break searching). Enforcement half of CLAUDE.md's CodeGraph line.
"""

import hashlib
import json
import os
import re
import shlex
import subprocess
import sys
import tempfile

IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{2,}$")
GREP_TOKENS = {"grep", "egrep", "fgrep", "rg", "ripgrep"}


def bare_identifier(pat):
    """A single code-identifier-shaped token (len>=3), no regex/space/path."""
    return bool(pat) and bool(IDENT.match(pat))


def pattern_from_bash(command):
    """Best-effort: extract the pattern from a `grep/rg PATTERN` command. Returns
    "" when the command is not a simple grep/rg, invokes codegraph, or can't be
    parsed — all of which mean "not ours to gate"."""
    if "codegraph" in command:
        return ""  # never gate an explicit codegraph call
    try:
        tokens = shlex.split(command)
    except ValueError:
        return ""
    for i, tok in enumerate(tokens):
        if os.path.basename(tok) in GREP_TOKENS:
            for arg in tokens[i + 1:]:
                if arg.startswith("-"):
                    continue
                if arg in ("|", "&&", "||", ";"):
                    break
                return arg
            return ""
    return ""


def is_defined_symbol(pat, root):
    """True iff CodeGraph's index has a definition whose name == pat (exact)."""
    try:
        out = subprocess.run(
            ["codegraph", "query", pat, "--json"],
            capture_output=True, text=True, timeout=6, cwd=root,
        )
    except (OSError, subprocess.SubprocessError):
        return False  # codegraph absent / crashed → fail open
    if out.returncode != 0 or not out.stdout:
        return False
    try:
        data = json.loads(out.stdout)
    except ValueError:
        return False
    rows = data if isinstance(data, list) else (data.get("results") or data.get("nodes") or [])
    for row in rows:
        node = row.get("node") if isinstance(row, dict) else None
        name = (node or row or {}).get("name") if isinstance(row, dict) else None
        if name == pat:
            return True
    return False


def main():
    payload = json.load(sys.stdin)
    tool = payload.get("tool_name") or ""
    ti = payload.get("tool_input") or {}

    if tool == "Grep":
        pat = (ti.get("pattern") or "").strip()
    elif tool == "Bash":
        pat = pattern_from_bash(ti.get("command") or "")
    else:
        return 0

    if not bare_identifier(pat):
        return 0  # regex, multi-word, or empty → a real text search

    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    root = os.path.realpath(root)

    if not is_defined_symbol(pat, root):
        return 0  # not a known symbol → let the text search run

    session = re.sub(r"[^A-Za-z0-9_-]", "_", str(payload.get("session_id") or "nosession"))
    seen_dir = os.path.join(tempfile.gettempdir(), f"codegraph-nudge-{session}")
    os.makedirs(seen_dir, exist_ok=True)
    marker = os.path.join(seen_dir, hashlib.sha1(pat.encode()).hexdigest())
    if os.path.exists(marker):
        return 0
    open(marker, "w").close()

    msg = [
        f"[codegraph] '{pat}'는 코드 심볼입니다 (CodeGraph 인덱스에 정의 존재). 관계·구조 질문이면 텍스트 grep보다:",
        f"  · MCP 툴 codegraph_explore \"{pat} 관련 질문\"  (호출자·호출대상·블래스트 반경을 한 번에)",
        f"  · 또는 CLI: codegraph callers {pat}  ·  codegraph impact {pat}  ·  codegraph node {pat}",
        "텍스트 문자열 검색이 목적이면 같은 검색을 그대로 재실행하세요 (세션당 심볼별 1회만 안내).",
    ]
    print("\n".join(msg), file=sys.stderr)
    return 2  # block this one call; retry passes via the marker


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — a nudge must never break searching
        sys.exit(0)
