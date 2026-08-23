#!/usr/bin/env python3
"""rpcmap → CodeGraph synthetic edges.

CodeGraph can't follow string-keyed indirection (RPC method names, tool names,
event names → their handlers) — the static-analysis frontier rpcmap.py fills
deterministically. This injector materializes rpcmap's answers INSIDE the
CodeGraph index as synthetic nodes+edges, so the agent's default search
(`codegraph callers/impact <handler>`) shows the dispatch surface without a
separate rpcmap lookup:

    codegraph callers peopleList
      ├─ rpc:miniapp.people.list        ← this injector
      └─ (real static callers…)

Idempotent: every run wipes rows it owns (nodes id LIKE 'rpcmap:%', edges
provenance='rpcmap') and re-inserts from a fresh `rpcmap --list`. Designed to
run after `codegraph sync`/`index` (which rebuild only real rows) — wired into
zcode-codegraph-sync.sh so the index stays enriched as code changes.
"""

from __future__ import annotations

import re
import sqlite3
import subprocess
import sys
from pathlib import Path

ENTRY_RE = re.compile(r"^\s*\[(?P<kind>rpc|tool|event)\]\s+(?P<name>\S+)")
HANDLER_RE = re.compile(r"^\s*→\s+(?P<handler>\S+)\s+\((?P<path>[^):]+):(?P<line>\d+)\)")


def rpcmap_entries(repo: Path) -> list[tuple[str, str, str, str, int]]:
    out = subprocess.run(
        [sys.executable, str(repo / "scripts/dev/rpcmap.py"), "--list"],
        capture_output=True, text=True, cwd=repo,
    )
    if out.returncode != 0:
        raise SystemExit(f"rpcmap --list failed: {out.stderr.strip()[:200]}")
    entries = []
    kind = name = None
    for line in out.stdout.splitlines():
        m = ENTRY_RE.match(line)
        if m:
            kind, name = m.group("kind"), m.group("name")
            continue
        h = HANDLER_RE.match(line)
        if h and kind and name:
            entries.append((kind, name, h.group("handler"), h.group("path"), int(h.group("line"))))
            kind = name = None
    return entries


def sync(repo: Path) -> tuple[int, int]:
    db = repo / ".codegraph" / "codegraph.db"
    if not db.exists():
        raise SystemExit("no .codegraph/codegraph.db — run `codegraph init` first")
    entries = rpcmap_entries(repo)
    con = sqlite3.connect(db)
    cur = con.cursor()
    cur.execute("DELETE FROM edges WHERE provenance='rpcmap'")
    cur.execute("DELETE FROM nodes WHERE id LIKE 'rpcmap:%'")

    linked = skipped = 0
    for kind, name, handler, path, line in entries:
        row = cur.execute(
            "SELECT id FROM nodes WHERE name=? AND file_path=? AND kind IN ('function','method') LIMIT 1",
            (handler, path),
        ).fetchone()
        if row is None and handler != "func":
            # rpcmap's path can lag file splits (calendar.go → calendar_proposals.go).
            # A globally UNIQUE name match is still deterministic; ambiguity or an
            # anonymous handler ("func") stays skipped — never guess.
            cands = cur.execute(
                "SELECT id FROM nodes WHERE name=? AND kind IN ('function','method') LIMIT 2",
                (handler,),
            ).fetchall()
            if len(cands) == 1:
                row = cands[0]
        if row is None:
            skipped += 1
            continue
        vid = f"rpcmap:{kind}:{name}"
        cur.execute(
            "INSERT OR REPLACE INTO nodes (id, kind, name, qualified_name, file_path, language,"
            " start_line, end_line, start_column, end_column, docstring, signature, updated_at)"
            " VALUES (?,?,?,?,?,?,?,?,?,?,?,?, strftime('%s','now'))",
            (
                vid, f"{kind}-name", name, name, path, "wire",
                line, line, 0, 0,
                f"string-keyed dispatch ({kind}) resolved by rpcmap — synthetic node",
                None,
            ),
        )
        cur.execute(
            "INSERT INTO edges (source, target, kind, metadata, line, col, provenance)"
            " VALUES (?,?,?,?,?,?,?)",
            (vid, row[0], "calls", None, line, 0, "rpcmap"),
        )
        linked += 1
    con.commit()
    con.close()
    return linked, skipped


def main() -> int:
    repo = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parents[2]
    linked, skipped = sync(repo)
    print(f"rpcmap→codegraph: {linked} edges injected, {skipped} skipped (handler not in index)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
