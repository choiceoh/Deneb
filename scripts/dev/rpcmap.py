#!/usr/bin/env python3
"""rpcmap — resolve deneb's string-keyed indirection that CodeGraph can't.

CodeGraph's AST graph connects symbol→symbol call edges, but NOT the string KEY
of a registration to its handler value:
  - RPC method:   `"miniapp.people.list": peopleList(deps)`  (map literal in a
                  handler package's `*Methods()` function)
  - Chat tool:    `ToolDef{ Name: "wiki", ... Fn: tools.ToolWiki(...) }`
  - Event:        `broadcast("chat.delivery_failed", ChatDeliveryFailedEvent{...})`
                  (event-name string → event struct type, consumed by SSE subscribers)

So "what handles miniapp.people.list?" dead-ends in CodeGraph at the neighborhood.
This scans the gateway source for these patterns (deterministic, grep-only —
no index, no codegraph needed to run) and answers the missing edge. Feed the
resolved handler to `codegraph node <handler>` for full source + callers/callees.

Usage:
  rpcmap miniapp.people.list        method/tool/event name → handler + file:line
  rpcmap wiki                       (prefix/substring match works too)
  rpcmap chat.delivery_failed       (event names also indexed)
  rpcmap --handler peopleList       reverse: what this handler serves
  rpcmap --list [--json]            dump the whole map
  rpcmap --root DIR                 scan a repo other than the cwd
"""

import argparse
import json
import os
import re
import sys

# RPC method → handler, two registration forms found in the gateway:
#   map literal:      `"a.b.c": handlerName`         (in *Methods() returns)
#   map assignment:   `m["a.b.c"] = handlerName`     (builder-style registration)
RPC_RE = re.compile(r'"([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)"\s*:\s*([A-Za-z_]\w*)')
RPC_ASSIGN_RE = re.compile(r'\[\s*"([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)"\s*\]\s*=\s*([A-Za-z_]\w*)')
# Tool name → handler: ToolDef{ Name: "x", ... Fn: pkg.Handler(...) }.
# `[^}]*?` keeps Name↔Fn within one struct literal (no `}` between them).
TOOL_RE = re.compile(r'Name:\s*"([^"]+)"[^}]*?Fn:\s*(?:\w+\.)?([A-Za-z_]\w*)', re.DOTALL)
# Event name → event type: broadcast("event.name", EventType{...}) or
# broadcast("event.name", map[string]any{...}).  The struct form resolves to a
# named type that codegraph can trace; the inline-map form is recorded as-is.
EVENT_RE = re.compile(r'broadcast\(\s*"([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)"\s*,\s*([A-Za-z_]\w*)')

# Where deneb registers things (scoped to cut false positives from unrelated
# dotted-string maps elsewhere in the tree).
RPC_DIRS = ["gateway-go/internal/runtime"]
# chat root (not just chat/toolreg/) — some ToolDefs live in chat/toolreg_core.go.
# TOOL_RE is specific enough (Name+Fn in a ToolDef) that the wider walk is clean.
TOOL_DIRS = ["gateway-go/internal/pipeline/chat"]
# Events are broadcast from both the chat pipeline and the runtime layer.
EVENT_DIRS = ["gateway-go/internal/pipeline/chat", "gateway-go/internal/runtime"]

# Runtime-assembled method names static extraction can't see literally.
# Some families are built via `methodsWithPrefix(deps, "PREFIX.")` (e.g.
# observatory.*) — genuinely computed, so a miss there is a real static limit.
COMPUTED_HINT = ("일부 메서드명은 런타임 합성이라 안 잡힐 수 있음 "
                 "(observatory.* 등 methodsWithPrefix). base 네임스페이스나 "
                 "codegraph_explore로 확인.")


def _go_files(root, subdirs):
    seen_dirs = set()
    for sub in subdirs:
        base = os.path.join(root, sub)
        if base in seen_dirs:
            continue
        seen_dirs.add(base)
        for dirpath, _dirs, files in os.walk(base):
            for f in files:
                if f.endswith(".go") and not f.endswith("_test.go"):
                    yield os.path.join(dirpath, f)


def build_map(root):
    """Return {kind: [(name, handler, relpath, line)]} for 'rpc', 'tool', 'event'."""
    out = {"rpc": [], "tool": [], "event": []}
    for path in _go_files(root, RPC_DIRS):
        try:
            with open(path, encoding="utf-8") as fh:
                for i, line in enumerate(fh, 1):
                    for m in (RPC_RE.search(line), RPC_ASSIGN_RE.search(line)):
                        if m:
                            out["rpc"].append((m.group(1), m.group(2),
                                               os.path.relpath(path, root), i))
        except OSError:
            continue
    for path in _go_files(root, TOOL_DIRS):
        try:
            with open(path, encoding="utf-8") as fh:
                text = fh.read()
        except OSError:
            continue
        rel = os.path.relpath(path, root)
        for m in TOOL_RE.finditer(text):
            line = text[:m.start()].count("\n") + 1
            out["tool"].append((m.group(1), m.group(2), rel, line))
    for path in _go_files(root, EVENT_DIRS):
        try:
            with open(path, encoding="utf-8") as fh:
                for i, line in enumerate(fh, 1):
                    m = EVENT_RE.search(line)
                    if m:
                        out["event"].append((m.group(1), m.group(2),
                                             os.path.relpath(path, root), i))
        except OSError:
            continue
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
    allrows = ([("rpc", *r) for r in m["rpc"]]
               + [("tool", *r) for r in m["tool"]]
               + [("event", *r) for r in m["event"]])

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
        print(f"rpcmap: {len(m['rpc'])} RPC methods, {len(m['tool'])} tools, "
              f"{len(m['event'])} events indexed. "
              "Give a name, --handler NAME, or --list.", file=sys.stderr)
        return 2

    if args.json:
        print(json.dumps([
            {"kind": k, "name": n, "handler": h, "file": f, "line": ln}
            for (k, n, h, f, ln) in hits
        ], ensure_ascii=False, indent=2))
        return 0 if hits else 1

    if not hits:
        print(f"no match. ({len(m['rpc'])} RPC methods, {len(m['tool'])} tools, "
              f"{len(m['event'])} events indexed)\n{COMPUTED_HINT}",
              file=sys.stderr)
        return 1
    by_kind = {"rpc": [r[1:] for r in hits if r[0] == "rpc"],
               "tool": [r[1:] for r in hits if r[0] == "tool"],
               "event": [r[1:] for r in hits if r[0] == "event"]}
    for kind, rows in by_kind.items():
        if rows:
            print(_fmt(kind, rows))
    return 0


if __name__ == "__main__":
    sys.exit(main())
