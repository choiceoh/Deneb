#!/usr/bin/env python3
"""SessionStart hook: surface the compact session-memory card + recent episodes.

Fires when a Claude Code session starts, resumes, or recovers from compaction.
Emits the always-on "memory card" (a short, agent-maintained digest of recent
decisions / open threads / hard-won lessons) plus the last few auto-captured
session skeletons, as additionalContext. The agent thus begins each session
already knowing what recent sessions did -- instead of the operator re-explaining.

Bounded on purpose: an always-injected block must stay small or it bloats every
session (the same context-bloat we removed from the runtime heartbeat). The card
is capped; episodes are limited to the most recent few. Running on the 'compact'
source is a feature -- it re-injects memory after a compaction wipe.

Fail-safe contract: ANY error exits 0 with empty output (no context added, the
session is unaffected).
"""

import json
import os
import sys

MEM_DIR = os.path.expanduser("~/.claude/deneb-session-memory")
EPISODES = os.path.join(MEM_DIR, "episodes.jsonl")
CARD = os.path.join(MEM_DIR, "card.md")

RECENT_EPISODES = 4
CARD_CAP = 2500  # chars -- keep the always-injected block bounded

# The semantic layer is compressed by the in-loop agent, not a remote model.
# This nudge keeps that discipline alive: it is surfaced every session so the
# agent updates the digest when a durable decision / lesson / self-correction
# arises, without which the card would go stale.
FOOTER = (
    "_세션에서 새 결정·교훈·자기교정이 나오면 이 카드를 갱신하세요 "
    "(파일: ~/.claude/deneb-session-memory/card.md)._"
)


def read_card():
    try:
        with open(CARD, "r", encoding="utf-8", errors="replace") as f:
            return f.read().strip()[:CARD_CAP]
    except Exception:
        return ""


def recent_episodes():
    try:
        with open(EPISODES, "r", encoding="utf-8", errors="replace") as f:
            lines = [ln for ln in f if ln.strip()]
    except Exception:
        return []
    out = []
    for line in lines[-RECENT_EPISODES:]:
        try:
            out.append(json.loads(line))
        except Exception:
            continue
    return out


def fmt_episode(ep):
    date = ep.get("date", "")
    branch = ep.get("branch", "")
    topic = ep.get("topic", "")
    commits = ep.get("commits", [])
    header = f"- {date} · `{branch}`"
    if topic:
        header += f' · "{topic}"'
    if commits:
        header += "\n" + "\n".join(f"    · {c}" for c in commits[:5])
    return header


def main():
    card = read_card()
    eps = recent_episodes()
    if not card and not eps:
        sys.exit(0)  # nothing to surface yet

    sections = ["# 코딩 세션 기억 (자동 표면화)"]
    if card:
        sections.append(card)
    if eps:
        sections.append("## 최근 세션 (자동 기록)")
        sections.append("\n".join(fmt_episode(e) for e in reversed(eps)))
    sections.append(FOOTER)
    context = "\n\n".join(sections)

    out = {
        "hookSpecificOutput": {
            "hookEventName": "SessionStart",
            "additionalContext": context,
        }
    }
    print(json.dumps(out, ensure_ascii=False))
    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.exit(0)
