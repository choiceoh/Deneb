#!/usr/bin/env python3
"""Relevance sweep of the skill validation corpus (skill_validation_cases.jsonl).

Why: capture attributes a validation case to every skill *consulted* during a
turn, not just the one that actually drove it (run_agent_config.recordRunSkillUsage).
So a session about something unrelated (a mail/project query that merely loaded
`system-health-check`) became that skill's held-out case, demanding tools the skill
can never use and pinning it at an unimprovable score — good on-topic evolutions
then get rejected. The live gate (skilllifecycle.sessionExercisesSkill, PR #3673)
stops NEW contamination; this one-shot re-classifies the EXISTING corpus.

Method (the most accurate available — see the operator discussion):
  - Classify each session-mined case from its OWN stored content (frozen at
    capture), NOT a transcript re-load: the case id (e.g. "session-client:main")
    is a rolling/shared key, so the live transcript no longer maps to the
    historical session, whereas the stored replay froze that session's tool calls.
  - Feed the skill's real SKILL.md `description` so the judge knows the skill's
    purpose (name alone is weaker).
  - Prune only on UNANIMOUS off-topic across a strong primary + two corroborators
    (one same-family, one independent-family — orthogonal blind spots). Any
    disagreement / error / unparsed output → KEEP (conservative: never delete on
    uncertainty; false-negatives are the safe direction).

Safety: DRY-RUN by default (prints the per-skill plan). --apply writes an atomic
backup (`<file>.bak-sweep-<ts>`) + an audit log (`<file>.sweep-audit-<ts>.jsonl`)
before rewriting, so every prune is reversible and attributable. The corpus is
read fresh (jsonlstore.Load) by the gateway, so a prune takes effect on the next
evolve cycle with no restart.

Usage:
  python3 scripts/audit/skill_corpus_relevance_sweep.py                 # dry-run
  python3 scripts/audit/skill_corpus_relevance_sweep.py --skill morning-letter
  python3 scripts/audit/skill_corpus_relevance_sweep.py --apply         # prune

Needs the wormhole model endpoint (127.0.0.1:18800, token ~/.wormhole/config.json).
"""
from __future__ import annotations

import argparse
import glob
import json
import os
import re
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor

DATA_DEFAULT = os.path.expanduser("~/.deneb/data/skill_validation_cases.jsonl")
WH_URL = "http://127.0.0.1:18800/v1/chat/completions"
WH_CFG = os.path.expanduser("~/.wormhole/config.json")

# Prune only when the primary AND every corroborator agree the case is off-topic.
PRIMARY = "deepseek-v4-pro-api"  # strongest judge
CORROBORATORS = ["deepseek-v4-flash", "qwen3.6-35b-a3b"]  # same-family + independent-family
TRANSCRIPT_BUDGET = 600  # per fixture excerpt; a relevance call is classification, not a full read
WORKERS = 6

_SYSTEM = (
    "You decide whether a chat session was a GENUINE example of using a named agent skill. "
    "An agent lists/consults many skills each turn; only some actually guide the work. "
    'Answer strict JSON {"uses_skill": true|false} and nothing else. '
    "true only if the session's actual task is what the skill is for; "
    "false if the skill was merely consulted while the real work was about something else."
)


def load_descriptions() -> dict[str, str]:
    """Map skill name -> SKILL.md description (bundled dir wins over managed)."""
    out: dict[str, str] = {}
    for root in (os.path.expanduser("~/deneb/skills"), os.path.expanduser("~/.deneb/skills")):
        for path in glob.glob(root + "/**/SKILL.md", recursive=True):
            try:
                txt = open(path, encoding="utf-8", errors="replace").read()
            except OSError:
                continue
            name = re.search(r"(?m)^name:\s*(.+)$", txt)
            desc = re.search(r"(?m)^description:\s*(.+)$", txt)
            if name:
                key = name.group(1).strip().strip("\"'")
                out.setdefault(key, (desc.group(1).strip().strip("\"'") if desc else "")[:300])
    return out


def wormhole_token() -> str:
    cfg = json.load(open(WH_CFG))
    return cfg.get("token") or cfg.get("api_key") or ""


def classify(token: str, skill: str, desc: str, content: str, model: str) -> tuple[bool, bool]:
    """Return (uses_skill, parsed). parsed=False => caller keeps (fail-open)."""
    user = (
        f"Skill: {skill}\nSkill purpose: {desc or '(no description)'}\n\n"
        f"What the session actually did (frozen from the stored case):\n{content}"
    )
    body = json.dumps({
        "model": model,
        "messages": [{"role": "system", "content": _SYSTEM}, {"role": "user", "content": user}],
        "max_tokens": 400,
        "temperature": 0,
    }).encode()
    req = urllib.request.Request(
        WH_URL, data=body,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=40) as resp:
            raw = json.load(resp)["choices"][0]["message"]["content"]
    except Exception:
        return True, False
    i, j = raw.find("{"), raw.rfind("}")
    if i >= 0 and j > i:
        raw = raw[i:j + 1]
    try:
        val = json.loads(raw)
        if isinstance(val.get("uses_skill"), bool):
            return val["uses_skill"], True
    except Exception:
        pass
    return True, False


def case_content(rec: dict) -> tuple[str, str]:
    """(topic-hint, classifier-content). Empty content => not classifiable => keep."""
    rep = rec.get("replay") or {}
    tools = rep.get("requiredTools") or []
    parts: list[str] = []
    topic = ""
    for call in (rep.get("expectedToolCalls") or []):
        inc = call.get("inputIncludes") or []
        if inc:
            if not topic and len(inc) > 1:
                topic = str(inc[1])[:60]
            parts.append(f"- {call.get('name')}({', '.join(map(str, inc))[:120]})")
        fixture = call.get("fixtureOutput")
        if fixture and len(parts) <= 2:
            parts.append("  excerpt: " + str(fixture)[:TRANSCRIPT_BUDGET].replace("\n", " "))
    if rep.get("input"):
        parts.insert(0, f"user asked: {str(rep['input'])[:200]}")
    if not parts and not tools:
        return topic, ""
    return topic, f"tools: {tools}\n" + "\n".join(parts[:6])


def unanimously_offtopic(token: str, skill: str, desc: str, content: str) -> bool:
    """True only if the primary AND every corroborator call it off-topic."""
    uses, parsed = classify(token, skill, desc, content, PRIMARY)
    if not (parsed and not uses):
        return False
    for model in CORROBORATORS:
        u2, p2 = classify(token, skill, desc, content, model)
        if not (p2 and not u2):
            return False
    return True


def main() -> int:
    ap = argparse.ArgumentParser(description="Relevance sweep of the skill validation corpus")
    ap.add_argument("--apply", action="store_true", help="prune off-topic cases (default: dry-run)")
    ap.add_argument("--skill", default="", help="limit to one skill name")
    ap.add_argument("--limit", type=int, default=0, help="cap classified cases (0 = all)")
    ap.add_argument("--data", default=DATA_DEFAULT, help="corpus path")
    args = ap.parse_args()

    token = wormhole_token()
    descs = load_descriptions()
    lines = open(args.data, encoding="utf-8", errors="replace").read().splitlines()
    recs = []
    for idx, ln in enumerate(lines):
        rec = None
        if ln.strip():
            try:
                rec = json.loads(ln)
            except Exception:
                rec = None
        recs.append((idx, ln, rec))

    cand = []
    for idx, _ln, rec in recs:
        if not rec:
            continue
        skill = rec.get("skillName") or rec.get("skill") or ""
        src = rec.get("source") or ""
        if not skill or src.startswith("adversarial"):
            continue  # adversarial-coverage cases are authored, not session-mined
        if args.skill and skill != args.skill:
            continue
        if skill not in descs:
            continue  # no description -> cannot classify accurately -> keep
        topic, content = case_content(rec)
        if not content:
            continue  # nothing to classify against -> keep
        cand.append((idx, skill, descs[skill], topic, content))
        if args.limit and len(cand) >= args.limit:
            break

    print(f"corpus lines={len(lines)}  classifiable candidates={len(cand)}  "
          f"primary={PRIMARY}  corroborators={CORROBORATORS}", flush=True)

    def work(item):
        idx, skill, desc, topic, content = item
        return idx, skill, topic, unanimously_offtopic(token, skill, desc, content)

    t0 = time.time()
    prune_idx: set[int] = set()
    by_skill: dict[str, dict] = {}
    with ThreadPoolExecutor(max_workers=WORKERS) as ex:
        for done, (idx, skill, topic, off) in enumerate(ex.map(work, cand), 1):
            entry = by_skill.setdefault(skill, {"prune": [], "keep": 0})
            if off:
                prune_idx.add(idx)
                entry["prune"].append(topic)
            else:
                entry["keep"] += 1
            if done % 40 == 0:
                print(f"  ...{done}/{len(cand)}  ({time.time() - t0:.0f}s)", flush=True)

    print("\n=== PLAN (per skill: prune / keep) ===")
    for skill in sorted(by_skill, key=lambda k: -len(by_skill[k]["prune"])):
        entry = by_skill[skill]
        if not entry["prune"]:
            continue
        print(f"  {skill}: prune {len(entry['prune']):3d} · keep {entry['keep']:3d}")
        for topic in entry["prune"][:4]:
            print(f"        off-topic e.g.: {topic!r}")
    print(f"\nTOTAL to prune: {len(prune_idx)} of {len(cand)} classified  (kept: {len(cand) - len(prune_idx)})")

    if not args.apply:
        print("\nDRY-RUN — no changes. Re-run with --apply to prune (backup + audit taken automatically).")
        return 0

    ts = int(time.time())
    backup = f"{args.data}.bak-sweep-{ts}"
    with open(backup, "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines) + ("\n" if lines else ""))
    kept = [ln for idx, ln, _rec in recs if idx not in prune_idx]
    tmp = args.data + ".sweep.tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write("\n".join(kept) + ("\n" if kept else ""))
    os.replace(tmp, args.data)
    with open(f"{args.data}.sweep-audit-{ts}.jsonl", "w", encoding="utf-8") as fh:
        for idx in sorted(prune_idx):
            _, ln, rec = recs[idx]
            fh.write(json.dumps({
                "line": idx,
                "skill": (rec or {}).get("skillName") or (rec or {}).get("skill"),
                "source": (rec or {}).get("source"),
            }, ensure_ascii=False) + "\n")
    print(f"\nAPPLIED — pruned {len(prune_idx)}; kept {len(kept)} lines; backup={backup}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
