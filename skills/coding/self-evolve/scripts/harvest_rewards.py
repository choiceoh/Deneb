#!/usr/bin/env python3
"""harvest_rewards.py — S2 of the self-evolve loop (the ATDP reward layer).

Deneb session JSONL already carries o/h/a/y/m (user msgs, thinking, tool calls,
tool results, usage). The one ATDP field it captures NOWHERE is r — the reward.
This scans sessions and extracts late-bound reward signals, credit-assigned back
to the action that earned them (a user correction at turn T attaches to the
assistant action at T-1). Output turns the logs into learnable trajectories and
answers "what has the agent been corrected on / what fails".

Precision-first (self-evolve's own rule): better to miss a weak signal than to
flag a normal turn. Tune the patterns against real output, not intuition.

Usage:
  harvest_rewards.py SESSION.jsonl [more.jsonl ...] [--json]
  ls ~/.deneb/agents/main/sessions/*.jsonl | tail -30 | xargs harvest_rewards.py
"""
import json, re, sys
from collections import Counter

# A user turn that negates/redirects the PRIOR assistant action = negative
# reward on that action. Regex-based for precision (plain "아니" substring
# over-fires on 아니면/아닌가). Strong, deliberate markers only.
USER_CORRECTION = re.compile(
    r"아니[야고라,\s]|아니라|틀렸|틀린|잘못(?!된 게 없)|정정|그게\s*아니|그건\s*아니"
    r"|가\s*아니[고라]|이상한데|이상해|오판|왜\s*안\s*[됐돼]|안\s*됐|헛|다시\s*해"
    r"|\bwrong\b|\bincorrect\b|that'?s not|no,\s")
# The agent correcting ITSELF (rare, high-signal — e.g. this session's GLM-OCR /
# MTP reversals). These are the gold reward events for self-evolution.
SELF_CORRECTION = re.compile(
    r"제가\s*틀렸|제\s*오[류판]|오판이|잘못\s*(봤|판단|짚)|당신이\s*맞|지적이\s*옳"
    r"|제가\s*성급|정정합니|미검증|재현\s*안|\bi was wrong\b|my earlier|correction:")
# Execution failure in a toolResult = weak negative reward on the tool call.
# ★ Text-scanning the result content is a false-positive trap: tool results
# routinely carry code/docs that MENTION "Error:"/"cannot"/"panic:" without any
# failure (validated on real sessions — 11/11 text hits were false). So we trust
# ONLY the structured isErr flag. Lower recall, near-zero false positives.


def parts_text(msg):
    return "\n".join(
        p["text"] for p in (msg.get("content") or [])
        if isinstance(p, dict) and p.get("type") == "text" and p.get("text"))


def harvest(path):
    msgs = []
    for line in open(path, encoding="utf-8", errors="replace"):
        line = line.strip()
        if not line:
            continue
        try:
            r = json.loads(line)
        except Exception:
            continue
        if r.get("type") == "message":
            msgs.append(r.get("message") or {})

    recs, last_assistant = [], None
    for i, m in enumerate(msgs):
        role, body = m.get("role"), parts_text(m)
        if role == "assistant":
            last_assistant = i
            if SELF_CORRECTION.search(body):
                recs.append({"turn": i, "signal": "self_correction", "polarity": -1,
                             "evidence": body[:180]})
        elif role == "user":
            hit = USER_CORRECTION.search(body)
            # a correction is short OR negates early — long new tasks aren't corrections
            if hit and (len(body) < 400 or body.find(hit.group(0)) < 60):
                recs.append({"turn": i, "attaches_to": last_assistant,
                             "signal": "user_correction", "polarity": -1,
                             "evidence": body[:180]})
        elif role == "toolResult":
            is_err = any(isinstance(p, dict) and (p.get("isErr") or p.get("is_error"))
                         for p in (m.get("content") or []))
            if is_err:
                recs.append({"turn": i, "attaches_to": last_assistant,
                             "signal": "tool_failure", "polarity": -1,
                             "evidence": body[:140]})
    return recs


def main():
    paths = [a for a in sys.argv[1:] if not a.startswith("--")]
    as_json = "--json" in sys.argv
    out = {}
    for p in paths:
        try:
            recs = harvest(p)
        except Exception as e:
            print(f"skip {p}: {e}", file=sys.stderr)
            continue
        if recs:
            out[p] = recs
    if as_json:
        print(json.dumps(out, ensure_ascii=False, indent=1))
        return
    tot = Counter(r["signal"] for recs in out.values() for r in recs)
    print("=== reward signals harvested ===")
    for sig, n in tot.most_common():
        print(f"  {sig:16} {n}")
    print(f"  {'sessions':16} {len(out)}")
    print("\n=== evidence (credit-assigned to prior action) ===")
    shown = 0
    for p, recs in out.items():
        sid = p.split("/")[-1][:8]
        for r in recs:
            if shown >= 15:
                break
            ev = r["evidence"].replace("\n", " ")[:120]
            print(f"  [{sid} t{r['turn']:>3} {r['signal']}] {ev}")
            shown += 1
        if shown >= 15:
            break


if __name__ == "__main__":
    main()
