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
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from codegraph_worktree_index import ensure_index  # noqa: E402

IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{2,}$")
GREP_TOKENS = {"grep", "egrep", "fgrep", "rg", "ripgrep"}


def codegraph_bin():
    """Resolve the codegraph binary even when the hook env has a minimal PATH
    (login profile not sourced) — ~/.local/bin isn't always on PATH. Same spots
    as codegraph-serve.sh. Returns None if not found (→ fail open, no nudge)."""
    for c in ("codegraph",
              os.path.expanduser("~/.local/bin/codegraph"),
              os.path.expanduser("~/.npm-global/bin/codegraph")):
        p = shutil.which(c)
        if p:
            return p
    return None


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
    if codegraph_bin() is None:
        return False  # codegraph not installed → fail open, no nudge
    try:
        # Run via a LOGIN shell: codegraph is a node script and a minimal hook
        # PATH lacks both codegraph and node; the profile restores both.
        out = subprocess.run(
            ["bash", "-lc", f"codegraph query {shlex.quote(pat)} --json"],
            capture_output=True, text=True, timeout=8, cwd=root,
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


# A dotted RPC method / tool name like "miniapp.people.list". rpcmap.py is the
# deterministic resolver; rpcmap_codegraph_sync.py also injects rpc-name nodes
# so `codegraph node miniapp.people.list` works after a sync.
DOTTED_METHOD = re.compile(r"^[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+$")


def resolves_via_rpcmap(pat, root):
    """True iff rpcmap.py maps this dotted name to an RPC/tool handler. rpcmap is
    the false-positive filter — 'package.json' etc. return no match."""
    script = os.path.join(root, "scripts", "dev", "rpcmap.py")
    if not os.path.isfile(script):
        return False
    try:
        out = subprocess.run(
            [sys.executable, script, pat, "--json", "--root", root],
            capture_output=True, text=True, timeout=6,
        )
    except (OSError, subprocess.SubprocessError):
        return False
    return out.returncode == 0 and out.stdout.strip() not in ("", "[]")


def main():
    payload = json.load(sys.stdin)

    # Self-heal a mid-session worktree's index (see codegraph-remind.py) so the
    # symbol grep we're about to gate — and the codegraph search it steers to —
    # hit THIS worktree's code, not the parent checkout's stale index.
    ensure_index(payload.get("cwd") or os.getcwd())

    tool = payload.get("tool_name") or ""
    ti = payload.get("tool_input") or {}

    if tool == "Grep":
        pat = (ti.get("pattern") or "").strip()
    elif tool == "Bash":
        pat = pattern_from_bash(ti.get("command") or "")
    else:
        return 0

    root = os.environ.get("CLAUDE_PROJECT_DIR") or payload.get("cwd") or os.getcwd()
    root = os.path.realpath(root)

    if bare_identifier(pat):
        if not is_defined_symbol(pat, root):
            return 0  # not a known symbol → let the text search run
        msg = [
            f"[codegraph] '{pat}'는 코드 심볼입니다 (CodeGraph 인덱스에 정의 존재). 텍스트 grep보다:",
            f"  · MCP codegraph_node / CLI: codegraph node {pat}   (정의·멤버·트레일 — 단일 심볼 기본)",
            f"  · codegraph callers {pat}  ·  codegraph impact {pat}",
            "  · 영역 조사만 explore (단일 심볼 explore는 Hub/유사명 노이즈 가능)",
            "텍스트 문자열 검색이 목적이면 같은 검색을 그대로 재실행하세요 (세션당 1회만 안내).",
        ]
    elif DOTTED_METHOD.match(pat) and resolves_via_rpcmap(pat, root):
        msg = [
            f"[codegraph] '{pat}'는 RPC 메서드/툴 이름입니다. 어느 핸들러가 처리하는지는 grep보다:",
            f"  · MCP/CLI: codegraph node {pat}   (rpc-name 합성 노드 → 핸들러)",
            f"  · scripts/dev/rpcmap.py {pat}   → 핸들러 + 파일:라인 (그다음 codegraph node <핸들러>)",
            "텍스트 문자열 검색이 목적이면 같은 검색을 그대로 재실행하세요 (세션당 1회만 안내).",
        ]
    else:
        return 0  # regex / plain text / unknown name → a real text search

    session = re.sub(r"[^A-Za-z0-9_-]", "_", str(payload.get("session_id") or "nosession"))
    seen_dir = os.path.join(tempfile.gettempdir(), f"codegraph-nudge-{session}")
    os.makedirs(seen_dir, exist_ok=True)
    marker = os.path.join(seen_dir, hashlib.sha1(pat.encode()).hexdigest())
    if os.path.exists(marker):
        return 0
    open(marker, "w").close()

    print("\n".join(msg), file=sys.stderr)
    return 2  # block this one call; retry passes via the marker


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — a nudge must never break searching
        sys.exit(0)
