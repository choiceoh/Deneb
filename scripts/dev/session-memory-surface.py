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
import subprocess
import sys

MEM_DIR = os.path.expanduser("~/.claude/deneb-session-memory")
EPISODES = os.path.join(MEM_DIR, "episodes.jsonl")
CARD = os.path.join(MEM_DIR, "card.md")
DECISIONS = os.path.join(MEM_DIR, "decisions.jsonl")

RECENT_EPISODES = 4
CARD_CAP = 2500  # chars -- keep the always-injected block bounded

# Decisions are selected by path overlap, not recency, so the budget buys
# relevance rather than a changelog. Deliberately under half the card's cap:
# this block rides every session, and the card is still the primary tenant. Two
# slots because the best path match is usually the one that matters and the
# second is the fallback; a truncated body is fine since each entry carries the
# sha to read in full.
DECISION_SLOTS = 2
DECISION_CHARS = 600

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


def working_areas(cwd):
    """Directories this checkout is currently touching.

    Uncommitted work first, then the branch's diff against main. A session that
    has not edited anything yet gets nothing -- surfacing "recent decisions"
    with no relevance signal would just be a changelog.
    """
    def git(*args):
        try:
            out = subprocess.run(
                ["git", "-C", cwd, *args], capture_output=True, text=True, timeout=8
            )
            return out.stdout.splitlines() if out.returncode == 0 else []
        except Exception:
            return []

    files = [f for f in git("status", "--porcelain") if f]
    # Porcelain lines are "XY path"; take the path, and the rename target.
    paths = [ln[3:].split(" -> ")[-1].strip() for ln in files]
    paths += [f for f in git("diff", "--name-only", "origin/main...HEAD") if f]

    areas = set()
    for p in paths:
        if not p:
            continue
        parts = p.split("/")
        if parts[0]:
            areas.add(parts[0])
        if len(parts) >= 2:
            areas.add(os.path.dirname(p))
    return areas


def relevant_decisions(areas, path=DECISIONS):
    """Decisions whose touched areas overlap what this session is editing.

    Scored by the most specific overlap: matching a package directory says far
    more than both sides containing "gateway-go". Ties break toward the newer
    decision, which is the one still in force.
    """
    if not areas:
        return []
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            rows = [json.loads(ln) for ln in f if ln.strip()]
    except Exception:
        return []

    scored = []
    for idx, row in enumerate(rows):
        overlap = areas & set(row.get("areas") or [])
        if not overlap:
            continue
        # Depth of the deepest shared path == specificity of the match.
        depth = max(a.count("/") for a in overlap)
        scored.append((depth, idx, row))
    scored.sort(key=lambda t: (t[0], t[1]), reverse=True)
    return [row for _, _, row in scored[:DECISION_SLOTS]]


def fmt_decision(d):
    # A squash subject already ends with "(#1234)", so only add the number when
    # the subject does not carry it -- otherwise every entry says it twice.
    subject = d.get("subject", "")
    pr = d.get("pr")
    head = f"- **{subject}**"
    if pr and f"#{pr}" not in subject:
        head += f" (#{pr})"
    body = (d.get("rationale") or "").strip()
    truncated = len(body) > DECISION_CHARS
    if truncated:
        body = body[:DECISION_CHARS].rstrip() + " …"
    sha = d.get("sha") or d.get("commit", "")
    lines = [head, f"  `{d.get('commit','')}` · {d.get('date','')}"]
    if body:
        lines += ["", "  " + body.replace("\n", "\n  ")]
    if truncated:
        lines += ["", f"  _전문: `git show {sha}`_"]
    return "\n".join(lines)


def main():
    card = read_card()
    eps = recent_episodes()
    cwd = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
    decisions = relevant_decisions(working_areas(cwd))
    if not card and not eps and not decisions:
        sys.exit(0)  # nothing to surface yet

    sections = ["# 코딩 세션 기억 (자동 표면화)"]
    if card:
        sections.append(card)
    if eps:
        sections.append("## 최근 세션 (자동 기록)")
        sections.append("\n".join(fmt_episode(e) for e in reversed(eps)))
    if decisions:
        sections.append(
            "## 지금 건드리는 코드의 결정 근거 (git 본문에서 채굴)\n"
            "_왜 이렇게 되어 있는지다. 뒤집기 전에 근거부터 읽을 것._"
        )
        sections.append("\n\n".join(fmt_decision(d) for d in decisions))
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
