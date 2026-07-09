#!/usr/bin/env python3
"""rpcmap — resolve deneb's string-keyed indirection that CodeGraph can't.

CodeGraph's AST graph connects symbol→symbol call edges, but NOT the string KEY
of a registration to its handler value:
  - RPC method:   `"miniapp.people.list": peopleList(deps)`  (map literal in a
                  handler package's `*Methods()` function)
  - Chat tool:    `ToolDef{ Name: "wiki", ... Fn: tools.ToolWiki(...) }`

So "what handles miniapp.people.list?" dead-ends in CodeGraph at the neighborhood.
This scans the gateway source for those two patterns (deterministic, grep-only —
no index, no codegraph needed to run) and answers the missing edge. Feed the
resolved handler to `codegraph node <handler>` for full source + callers/callees.

Usage:
  rpcmap miniapp.people.list        method/tool name → handler + file:line
  rpcmap wiki                       (prefix/substring match works too)
  rpcmap --handler peopleList       reverse: what this handler serves
  rpcmap --list [--json]            dump the whole map
  rpcmap --root DIR                 scan a repo other than the cwd
"""

import argparse
import json
import os
import re
import sys

# RPC method → handler: `"a.b.c": handlerName` inside *Methods() map literals.
RPC_RE = re.compile(r'"([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)"\s*:\s*([A-Za-z_]\w*)')
# Tool name → handler: ToolDef{ Name: "x", ... Fn: pkg.Handler(...) }.
# `[^}]*?` keeps Name↔Fn within one struct literal (no `}` between them).
TOOL_RE = re.compile(r'Name:\s*"([^"]+)"[^}]*?Fn:\s*(?:\w+\.)?([A-Za-z_]\w*)', re.DOTALL)

# Where deneb registers things (scoped to cut false positives from unrelated
# dotted-string maps elsewhere in the tree).
RPC_DIRS = ["gateway-go/internal/runtime"]
TOOL_DIRS = ["gateway-go/internal/pipeline/chat/toolreg"]


def _go_files(root, subdirs):
    for sub in subdirs:
        base = os.path.join(root, sub)
        for dirpath, _dirs, files in os.walk(base):
            for f in files:
                if f.endswith(".go") and not f.endswith("_test.go"):
                    yield os.path.join(dirpath, f)


def build_map(root):
    """Return {kind: [(name, handler, relpath, line)]} for 'rpc' and 'tool'."""
    out = {"rpc": [], "tool": []}
    for path in _go_files(root, RPC_DIRS):
        try:
            with open(path, encoding="utf-8") as fh:
                for i, line in enumerate(fh, 1):
                    m = RPC_RE.search(line)
                    if m:
                        out["rpc"].append((m.group(1), m.group(2),
                                           os.path.relpath(path, root), i))
        except OSError:
            continue
    for path in _go_files(root, TOOL_DIRS):
        try:
            text = open(path, encoding="utf-8").read()
        except OSError:
            continue
        rel = os.path.relpath(path, root)
        for m in TOOL_RE.finditer(text):
            line = text[:m.start()].count("\n") + 1
            out["tool"].append((m.group(1), m.group(2), rel, line))
    # Stable de-dup + sort.
    for k in out:
        out[k] = sorted(set(out[k]))
    return out


def _fmt(kind, rows):
    return "\n".join(
        f"  [{kind}] {name}\n"
        f"        → {handler}   ({rel}:{ln})\n"
        f"          codegraph node {handler}"
        for name, handler, rel, ln in rows
    )


def main():
    ap = argparse.ArgumentParser(add_help=True)
    ap.add_argument("query", nargs="?", help="method/tool name (substring ok)")
    ap.add_argument("--handler", help="reverse lookup: what a handler serves")
    ap.add_argument("--list", action="store_true", help="dump everything")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--root", default=os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd())
    args = ap.parse_args()
    root = os.path.realpath(args.root)

    m = build_map(root)
    allrows = [("rpc", *r) for r in m["rpc"]] + [("tool", *r) for r in m["tool"]]

    if args.handler:
        hits = [r for r in allrows if r[2] == args.handler]
    elif args.query:
        q = args.query.lower()
        hits = [r for r in allrows if q in r[1].lower() or q == r[2].lower()]
        exact = [r for r in hits if r[1].lower() == q]
        if exact:
            hits = exact  # prefer exact name match over substring
    elif args.list:
        hits = allrows
    else:
        print(f"rpcmap: {len(m['rpc'])} RPC methods, {len(m['tool'])} tools indexed. "
              "Give a name, --handler NAME, or --list.", file=sys.stderr)
        return 2

    if args.json:
        print(json.dumps([
            {"kind": k, "name": n, "handler": h, "file": f, "line": ln}
            for (k, n, h, f, ln) in hits
        ], ensure_ascii=False, indent=2))
        return 0 if hits else 1

    if not hits:
        print(f"no match. ({len(m['rpc'])} RPC methods, {len(m['tool'])} tools indexed)",
              file=sys.stderr)
        return 1
    by_kind = {"rpc": [r[1:] for r in hits if r[0] == "rpc"],
               "tool": [r[1:] for r in hits if r[0] == "tool"]}
    for kind, rows in by_kind.items():
        if rows:
            print(_fmt(kind, rows))
    return 0


if __name__ == "__main__":
    sys.exit(main())
