#!/usr/bin/env python3
"""Estimate the cite detector's MISS RATE (under-count correction factor).

The end-of-turn citation pass is deliberately conservative — a false positive
would grant unearned utility credit — so its recall (of true uses) is unknown
and the utility scores it feeds are biased low by an unmeasured factor. This
probe measures that factor:

  1. Join recall-utility ledger inject lines (wiki/.recall-hits.jsonl) with
     the transcript answer that followed them (same session, next assistant
     message after the inject timestamp).
  2. Sample N (exposure page, answer) pairs and ask a judge whether the
     answer actually used the page's content.
  3. Compare judge-YES pairs against recorded cite events for the same
     (session, path): judge-yes without a cite line == a miss.

Entry point: main() — read-only over the ledger and transcripts; judge calls
go through the wormhole anthropic path (model k3 by default). Prints per-pair
verdicts and the resulting correction factor:
  true_uses ≈ recorded_cites × factor.

Usage / verification:
  python3 scripts/audit/recall_cite_recall_probe.py [--n 30] [--days 14]
  # env: WORMHOLE_URL (default http://127.0.0.1:18800), JUDGE_MODEL (k3),
  #      DENEB_WIKI_DIR (default ~/.deneb/wiki — point at a COPY when the
  #      run must not touch production files; this script only reads).
Invariant: never writes to the ledger, wiki, or transcripts.
"""

import argparse
import json
import os
import re
import time
import urllib.request

WIKI_DIR = os.path.expanduser(os.environ.get("DENEB_WIKI_DIR", "~/.deneb/wiki"))
TRANSCRIPTS = os.path.expanduser("~/.deneb/transcripts")
WH = os.environ.get("WORMHOLE_URL", "http://127.0.0.1:18800")
JUDGE_MODEL = os.environ.get("JUDGE_MODEL", "k3")


def wormhole_token():
    cfg = json.load(open(os.path.expanduser("~/.wormhole/config.json")))
    return os.path.expandvars(cfg["token"])


def judge(token, page_path, page_head, answer):
    prompt = (
        "Wiki page: %s\nPage content (head):\n%s\n\nAssistant answer:\n%s\n\n"
        "Did the answer USE information from this page (facts, figures, names, "
        "recommendations drawn from it)? Merely being on the same broad topic "
        "does not count. Reply with exactly one word: YES or NO." % (page_path, page_head, answer)
    )
    body = {"model": JUDGE_MODEL, "max_tokens": 2048, "messages": [{"role": "user", "content": prompt}]}
    data = json.dumps(body).encode()
    for attempt in range(3):
        try:
            req = urllib.request.Request(
                WH + "/v1/messages", data=data,
                headers={"Content-Type": "application/json", "Authorization": "Bearer " + token})
            with urllib.request.urlopen(req, timeout=180) as r:
                resp = json.load(r)
            text = " ".join(c.get("text", "") for c in resp.get("content", []) if c.get("type") == "text").upper()
            if "YES" in text:
                return "yes"
            if "NO" in text:
                return "no"
        except Exception:
            time.sleep(3 * (attempt + 1))
    return "error"


def answer_after(session, at_ms):
    path = os.path.join(TRANSCRIPTS, session + ".jsonl")
    if not os.path.exists(path):
        return ""
    for line in open(path, errors="ignore"):
        try:
            msg = json.loads(line)
        except Exception:
            continue
        ts = msg.get("timestamp") or msg.get("ts") or 0
        if msg.get("role") != "assistant" or ts < at_ms:
            continue
        raw = msg.get("textContent") or msg.get("content", "")
        if isinstance(raw, list):  # content blocks: keep the text parts
            raw = " ".join(b.get("text", "") for b in raw if isinstance(b, dict) and b.get("type") == "text")
        text = raw if isinstance(raw, str) else json.dumps(raw, ensure_ascii=False)
        text = re.sub(r"\s+", " ", text)[:4000]
        if text.strip():
            return text  # first assistant message after the inject = the turn's answer
    return ""


def page_head(path, chars=1500):
    try:
        return open(os.path.join(WIKI_DIR, path), errors="ignore").read()[:chars]
    except Exception:
        return ""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=30)
    ap.add_argument("--days", type=int, default=14)
    args = ap.parse_args()

    ledger = os.path.join(WIKI_DIR, ".recall-hits.jsonl")
    cutoff = (time.time() - args.days * 86400) * 1000
    injects, cites = [], set()
    for line in open(ledger, errors="ignore"):
        try:
            h = json.loads(line)
        except Exception:
            continue
        if h.get("at", 0) < cutoff:
            continue
        sess = h.get("session", "")
        ev = h.get("event", "inject") or "inject"
        if ev == "inject" and sess.startswith("client:"):
            injects.append(h)
        elif ev == "cite":
            cites.add((sess, h.get("path", "")))

    injects.sort(key=lambda h: -h.get("at", 0))  # newest first, one per (session, path)
    seen, sample = set(), []
    for h in injects:
        key = (h.get("session", ""), h.get("path", ""))
        if key in seen:
            continue
        seen.add(key)
        sample.append(h)
        if len(sample) >= args.n:
            break

    token = wormhole_token()
    used_yes = miss = judged = 0
    for h in sample:
        sess, path_ = h.get("session", ""), h.get("path", "")
        answer = answer_after(sess, h.get("at", 0))
        head = page_head(path_)
        if not answer or not head:
            print(f"skip  {path_} ({sess}) — 답변/페이지 확보 실패")
            continue
        verdict = judge(token, path_, head, answer)
        if verdict == "error":
            print(f"error {path_} ({sess})")
            continue
        judged += 1
        cited = (sess, path_) in cites
        if verdict == "yes":
            used_yes += 1
            if not cited:
                miss += 1
        print(f"{verdict:3s}  cite기록={'Y' if cited else 'N'}  {path_} ({sess})")

    print("\n== cite 미탐율 프로브 ==")
    print(f"판정 {judged}쌍 · 실사용(judge YES) {used_yes} · 그중 cite 미기록 {miss}")
    if used_yes:
        recall = (used_yes - miss) / used_yes
        if recall > 0:
            print(f"cite 검출 리콜 ≈ {recall:.0%} → 효용 보정계수 ≈ {1 / recall:.2f}x")
        else:
            print("cite 검출 리콜 0 — 표본 확대 필요")


if __name__ == "__main__":
    main()
