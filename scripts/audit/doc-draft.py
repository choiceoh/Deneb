#!/usr/bin/env python3
"""doc-draft.py — local-model draft generator for agent-facing subsystem docs.

The stolen-and-fixed idea from LangChain's OpenWiki: keep an agent-facing wiki of
the codebase so agents understand a subsystem before touching it. But instead of
a cloud SaaS (LangChain/LangSmith/OpenRouter) that auto-commits verbose prose, we
draft with Deneb's OWN model plumbing (the wormhole — one URL, one token, fronts
glm-5.2 and the local GPU models), GROUND the draft in real source + the call
graph so it can't hallucinate, and write a *.draft.md an agent then curates down
to the terse docs/agent-rules bar before it lands. No new dependency, no drift-
prone daily action, curation stays human.

Two modes:
  doc-draft.py --list-gaps
      Deterministic. Rank gateway-go/internal packages by weight and flag the
      heavy ones with no module CLAUDE.md and no matching agent-rule — i.e. the
      subsystems most worth a narrative doc.

  doc-draft.py --target domain/skills/genesis --name self-improvement
      Gather grounding (go doc + file map + `codegraph explore`) for the target
      package(s), draft a docs/agent-rules-style page with the model, and write
      docs/agent-rules/<name>.draft.md for curation.

The model call reads the wormhole token from ~/.wormhole/config.json at RUNTIME
(same source the gateway uses) — no credential is stored in this repo. Stdlib only.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.request

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "dev"))
from model_role import role_model  # noqa: E402  (path set above)

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
GATEWAY = os.path.join(REPO, "gateway-go")
INTERNAL = os.path.join(GATEWAY, "internal")
RULES_DIR = os.path.join(REPO, "docs", "agent-rules")
WORMHOLE_CFG = os.path.expanduser("~/.wormhole/config.json")
WORMHOLE_URL = "http://127.0.0.1:18800/v1/chat/completions"

CONTEXT_CHAR_BUDGET = 42_000   # per grounding section is capped below this total
# Ask for a ROLE, not a model name: names churn with every fleet swap and a dead
# pin does not fail, it silently routes elsewhere (see scripts/dev/model_role.py).
# Drafting agent-facing docs is coding-tier work; --model still overrides for
# A/B runs (comparing models by name is that flag's whole purpose).
DEFAULT_ROLE = "coding"
FALLBACK_MODEL = "glm-5.2"
DEFAULT_EXEMPLAR = "hub-wiring.md"


# --- model transport --------------------------------------------------------- #
def wormhole_token() -> str:
    with open(WORMHOLE_CFG, encoding="utf-8") as fh:
        cfg = json.load(fh)
    return os.path.expandvars(cfg["token"])


def call_model(model: str, system: str, user: str, max_tokens: int = 8000) -> str:
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "system", "content": system},
                     {"role": "user", "content": user}],
        "max_tokens": max_tokens,       # generous: glm reasons even with thinking off,
        "stream": False,                # so a small budget yields an empty reply
    }).encode()
    req = urllib.request.Request(
        WORMHOLE_URL, data=payload,
        headers={"Content-Type": "application/json",
                 "Authorization": f"Bearer {wormhole_token()}"})
    with urllib.request.urlopen(req, timeout=300) as r:
        resp = json.load(r)
    return resp["choices"][0]["message"]["content"]


# --- grounding gathering ----------------------------------------------------- #
def _run(cmd, cwd=None, timeout=60) -> str:
    try:
        out = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout)
        return out.stdout
    except (OSError, subprocess.TimeoutExpired) as exc:
        return f"(command failed: {exc})"


def _pkg_dirs(target: str) -> list[str]:
    """target is a path under internal/ (e.g. domain/skills/genesis); return it
    and every subdirectory that holds non-test Go files."""
    root = os.path.join(INTERNAL, target)
    dirs = []
    for base, _, files in os.walk(root):
        if any(f.endswith(".go") and not f.endswith("_test.go") for f in files):
            dirs.append(os.path.relpath(base, INTERNAL))
    return sorted(dirs)


def _go_doc(pkgrel: str, cap_lines: int = 320) -> str:
    out = _run(["go", "doc", "-all", "./internal/" + pkgrel], cwd=GATEWAY, timeout=90)
    lines = out.splitlines()
    if len(lines) > cap_lines:
        lines = lines[:cap_lines] + [f"... (+{len(lines) - cap_lines} more lines of exported API)"]
    return "\n".join(lines)


def _file_map(target: str) -> str:
    rows = []
    for base, _, files in os.walk(os.path.join(INTERNAL, target)):
        for f in sorted(files):
            if f.endswith(".go") and not f.endswith("_test.go"):
                p = os.path.join(base, f)
                with open(p, errors="ignore") as fh:
                    loc = sum(1 for _ in fh)
                rows.append((loc, os.path.relpath(p, REPO)))
    rows.sort(reverse=True)
    return "\n".join(f"  {loc:>5} LOC  {path}" for loc, path in rows[:40])


def _codegraph(name: str, target: str) -> str:
    env = dict(os.environ, PATH=os.path.expanduser("~/.local/bin") + ":" + os.environ.get("PATH", ""))
    try:
        out = subprocess.run(
            ["codegraph", "explore", f"{name} subsystem: entry points and end-to-end flow ({target})"],
            cwd=REPO, capture_output=True, text=True, timeout=120, env=env)
    except (OSError, subprocess.TimeoutExpired):
        return "(codegraph unavailable)"
    txt = out.stdout or out.stderr
    return txt[:12_000] if txt else "(codegraph unavailable)"


def gather_context(name: str, target: str) -> str:
    dirs = _pkg_dirs(target)
    parts = [f"# FILE MAP ({target})\n{_file_map(target)}"]
    budget = CONTEXT_CHAR_BUDGET - len(parts[0])
    for d in dirs:
        section = f"\n\n# EXPORTED API + DOC COMMENTS — internal/{d}\n{_go_doc(d)}"
        if budget - len(section) < 6000:   # leave room for the call graph
            parts.append(f"\n\n(+{len(dirs)} packages; remaining API omitted for budget)")
            break
        parts.append(section)
        budget -= len(section)
    parts.append(f"\n\n# CALL GRAPH (codegraph explore)\n{_codegraph(name, target)}")
    return "".join(parts)


# --- prompt ------------------------------------------------------------------ #
def build_prompt(name: str, target: str, context: str, exemplar_text: str):
    system = (
        "You write terse, high-signal, agent-facing subsystem docs for the Deneb "
        "codebase, in the exact style of docs/agent-rules/*.md. Your reader is a "
        "coding agent that will edit this subsystem and needs the end-to-end flow, "
        "the key types/files, and the gotchas — fast.\n\n"
        "HARD RULES:\n"
        "1. Ground EVERY claim in the provided source/API/call-graph. Never invent "
        "a function, field, file, or flow that is not in the context. If something "
        "is unclear from the context, omit it — do not guess.\n"
        "2. Cite real anchors as `path/to/file.go` or `file.go:func` drawn from the "
        "context (the file map gives exact repo paths).\n"
        "3. Be terse: ~80-150 lines total. Tables for phase/state maps. No filler, "
        "no marketing, no restating the obvious. Match the exemplar's density.\n"
        "4. English-first technical prose; Korean only where it adds real nuance "
        "(matching existing agent-rules).\n"
        "5. Output ONLY the markdown document, starting with a YAML frontmatter "
        "block (`---` / `description:` / `globs:` list of the real source paths / "
        "`---`) then a `# Title`. No preamble, no ``` fences around the whole doc.\n\n"
        "Suggested sections (drop any the context can't support): Title + 1-line "
        "purpose; Entry points (who calls in, from where); End-to-end flow "
        "(numbered or table); Key types/files; State/lifecycle if any; Gotchas & "
        "invariants; Where to look next / related rules."
    )
    user = (
        f"Draft the agent-rule doc for the **{name}** subsystem "
        f"(package tree: internal/{target}).\n\n"
        f"=== STYLE EXEMPLAR (docs/agent-rules/{DEFAULT_EXEMPLAR} — match this shape/density) ===\n"
        f"{exemplar_text}\n\n"
        f"=== GROUNDING CONTEXT (the ONLY source of truth — do not go beyond it) ===\n"
        f"{context}\n\n"
        f"Now write the doc for `{name}`."
    )
    return system, user


def strip_fences(text: str) -> str:
    t = text.strip()
    if t.startswith("```"):
        t = re.sub(r"^```[a-zA-Z]*\n", "", t)
        t = re.sub(r"\n```\s*$", "", t)
    return t.strip() + "\n"


# --- gap finder (deterministic) ---------------------------------------------- #
def _rule_globs() -> list[str]:
    globs = []
    for f in sorted(os.listdir(RULES_DIR)):
        if not f.endswith(".md"):
            continue
        with open(os.path.join(RULES_DIR, f), errors="ignore") as fh:
            head = fh.read(1500)
        globs += re.findall(r'internal/[\w/*.-]+', head)
    return globs


def list_gaps(top: int = 20) -> None:
    rule_globs = _rule_globs()
    rows = []
    for base, _, files in os.walk(INTERNAL):
        gofiles = [f for f in files if f.endswith(".go") and not f.endswith("_test.go")]
        if not gofiles:
            continue
        rel = os.path.relpath(base, INTERNAL)
        if rel.count("/") > 2:            # roll leaf pkgs up to subsystem granularity
            continue
        loc = 0
        for filename in gofiles:
            with open(os.path.join(base, filename), errors="ignore") as fh:
                loc += sum(1 for _ in fh)
        has_claude = os.path.exists(os.path.join(base, "CLAUDE.md"))
        covered_by_rule = any(rel in g or g.split("*")[0].rstrip("/").endswith(rel) for g in rule_globs)
        rows.append((loc, rel, len(gofiles), has_claude, covered_by_rule))
    rows.sort(reverse=True)
    print(f"{'LOC':>6} {'files':>5}  {'CLAUDE':>6} {'rule':>4}  package")
    print("─" * 66)
    for loc, rel, nf, hc, cr in rows[:top]:
        flag = "" if (hc or cr) else "  ← GAP (heavy, no narrative doc)"
        print(f"{loc:>6} {nf:>5}  {'✓' if hc else '·':>6} {'✓' if cr else '·':>4}  {rel}{flag}")
    print("\nGAP = heavy subsystem with neither a module CLAUDE.md nor an agent-rule glob.")
    print("Draft one with:  scripts/audit/doc-draft.py --target <pkg> --name <slug>")


# --- main -------------------------------------------------------------------- #
def main() -> int:
    ap = argparse.ArgumentParser(description="Draft agent-facing subsystem docs with Deneb's own model.")
    ap.add_argument("--list-gaps", action="store_true", help="rank undocumented heavy subsystems")
    ap.add_argument("--target", help="package path under internal/ (e.g. domain/skills/genesis)")
    ap.add_argument("--name", help="doc slug / subsystem name (e.g. self-improvement)")
    default_model = role_model(DEFAULT_ROLE, FALLBACK_MODEL)
    ap.add_argument(
        "--model",
        default=default_model,
        help=f"wormhole model (default: the {DEFAULT_ROLE} role, currently {default_model})",
    )
    ap.add_argument("--exemplar", default=DEFAULT_EXEMPLAR, help="agent-rule to match for style")
    ap.add_argument("--out", help="output path (default docs/agent-rules/<name>.draft.md)")
    args = ap.parse_args()

    if args.list_gaps:
        list_gaps()
        return 0
    if not args.target or not args.name:
        ap.error("either --list-gaps, or --target and --name")

    if not os.path.isdir(os.path.join(INTERNAL, args.target)):
        print(f"doc-draft: no such package: internal/{args.target}", file=sys.stderr)
        return 2

    print(f"doc-draft: gathering grounding for internal/{args.target} …", file=sys.stderr)
    context = gather_context(args.name, args.target)
    with open(os.path.join(RULES_DIR, args.exemplar), errors="ignore") as fh:
        exemplar = fh.read()
    system, user = build_prompt(args.name, args.target, context, exemplar)
    print(f"doc-draft: {len(context)} chars grounding → drafting with {args.model} "
          f"(this can take ~30-90s) …", file=sys.stderr)
    try:
        draft = strip_fences(call_model(args.model, system, user))
    except Exception as exc:
        print(f"doc-draft: model call failed: {exc}", file=sys.stderr)
        return 1

    out = args.out or os.path.join(RULES_DIR, f"{args.name}.draft.md")
    with open(out, "w") as fh:
        fh.write(draft)
    print(f"\ndoc-draft: wrote {os.path.relpath(out, REPO)} ({draft.count(chr(10)) + 1} lines)")
    print("  → DRAFT. An agent must curate it to the agent-rules bar (verify every")
    print("    anchor against real code, cut filler) before renaming off '.draft' and")
    print("    wiring it into CLAUDE.md's rule index + the rules-gate hook.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
