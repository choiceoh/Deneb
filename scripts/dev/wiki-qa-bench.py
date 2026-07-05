#!/usr/bin/env python3
"""wiki-qa-bench — 위키 QA 품질 자(ruler).

Deneb 위키가 실제 업무 질문에 답하는지를 골드셋으로 채점한다. 이후의 모든
위키·회상 개선은 이 점수의 전후 비교로 평가한다 (감이 아니라 자).

골드셋은 실데이터(프로젝트명·금액·인명)를 담으므로 레포에 커밋하지 않는다 —
기본 위치는 운영 호스트의 ~/.deneb/wiki-qa-gold.jsonl. 레코드(JSONL 한 줄):

  {"id": "pipeline-h2", "category": "업무",
   "question": "올해 하반기 착공 파이프라인 총 용량이 얼마지?",
   "gold_paths": ["업무/2026-하반기-착공-파이프라인"],
   "must_contain": ["1,068"], "must_not": []}

모드:
  recall (기본, LLM 0원·~초 단위): miniapp.memory.search 상위 K 결과에
      gold_paths 부분문자열이 드는지 (hit@K). 검색층만 잰다 — 회상 앵커·
      그래프는 answer 모드가 간접 포괄.
  answer (실 LLM 턴, 비용·시간 주의): miniapp.chat.send 실턴을 돌려
      must_contain(숫자는 쉼표/공백 무시 비교)·must_not·레이턴시로 채점.
      각 질문 전 /reset — 벤치 세션 1개만 생성된다. must_contain이 빈
      케이스는 answer 채점에서 skip(~)으로 표시.

출력: 케이스별 ✓/✗/~ 라인 + 머신 파싱용 요약:
  WIKI_QA_RECALL=hit/total (pct%)   WIKI_QA_ANSWER=pass/total (pct%)

예:
  python3 scripts/dev/wiki-qa-bench.py                       # recall 전량
  python3 scripts/dev/wiki-qa-bench.py --mode answer --ids pipeline-h2,kec-dc
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.request


def rpc(gw, token, method, params, timeout):
    body = json.dumps({"id": "qa", "method": method, "params": params}).encode()
    req = urllib.request.Request(
        gw.rstrip("/") + "/api/v1/miniapp/rpc",
        data=body,
        headers={"Content-Type": "application/json", "X-Deneb-Client-Token": token},
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)


def digits_relaxed(s):
    """Strip commas/spaces so '1,068MW' matches needle '1,068' or '1068'."""
    return re.sub(r"[,\s]", "", s)


def contains(text, needle):
    t, n = text.casefold(), needle.casefold()
    if n in t:
        return True
    if any(ch.isdigit() for ch in n):
        return digits_relaxed(n) in digits_relaxed(t)
    return False


def score_recall(case, gw, token, k, timeout):
    res = rpc(gw, token, "miniapp.memory.search", {"query": case["question"], "limit": k}, timeout)
    results = (res.get("payload") or {}).get("results") or []
    paths = [r.get("path", "") for r in results[:k]]
    hit = any(g in p for g in case["gold_paths"] for p in paths)
    return hit, paths


def score_answer(case, gw, token, session, timeout):
    must = case.get("must_contain") or []
    if not must:
        return None, 0, ""  # ungraded case — recall-only
    # Back-to-back sync turns on one session can collide with the previous
    # turn's async teardown (instant "run in progress"-style rejection) — settle
    # between turns and retry once on an instant-empty response.
    text, ms = "", 0
    for attempt in range(2):
        try:
            rpc(gw, token, "miniapp.chat.send", {"sessionKey": session, "message": "/reset"}, timeout)
            time.sleep(1.5)
            t0 = time.time()
            res = rpc(gw, token, "miniapp.chat.send", {"sessionKey": session, "message": case["question"]}, timeout)
            ms = int((time.time() - t0) * 1000)
            text = str((res.get("payload") or {}).get("text") or "")
        except Exception as e:  # noqa: BLE001
            text, ms = f"(error: {e})", 0
        if text and not text.startswith("(error:") and ms >= 500:
            break
        if attempt == 0:
            time.sleep(4)
    ok = all(contains(text, m) for m in must) and not any(
        contains(text, m) for m in (case.get("must_not") or [])
    )
    return ok, ms, text


def main():
    ap = argparse.ArgumentParser(description="Deneb wiki QA bench")
    ap.add_argument("--mode", choices=["recall", "answer", "both"], default="recall")
    ap.add_argument("--gold", default=os.path.expanduser("~/.deneb/wiki-qa-gold.jsonl"))
    ap.add_argument("--gw", default=os.environ.get("DENEB_QA_GW", "http://127.0.0.1:18789"))
    ap.add_argument("--token-file", default=os.path.expanduser("~/.deneb/client_token"))
    ap.add_argument("--k", type=int, default=8, help="recall hit@K (기본 8)")
    ap.add_argument("--ids", default="", help="쉼표로 지정한 케이스만")
    ap.add_argument("--limit", type=int, default=0, help="앞에서 N개만")
    ap.add_argument("--session", default="client:main:wiki-qa-bench")
    ap.add_argument("--timeout", type=int, default=240)
    ap.add_argument("--verbose", action="store_true", help="answer 모드에서 응답 앞부분 출력")
    args = ap.parse_args()

    token = open(args.token_file, encoding="utf-8").read().strip()
    cases = []
    with open(args.gold, encoding="utf-8") as f:
        for ln in f:
            ln = ln.strip()
            if ln and not ln.startswith("#"):
                cases.append(json.loads(ln))
    if args.ids:
        want = {i.strip() for i in args.ids.split(",") if i.strip()}
        cases = [c for c in cases if c["id"] in want]
    if args.limit > 0:
        cases = cases[: args.limit]
    if not cases:
        print("no cases selected", file=sys.stderr)
        return 2

    r_hit = r_tot = a_pass = a_tot = a_skip = 0
    for c in cases:
        marks = []
        if args.mode in ("recall", "both"):
            r_tot += 1
            try:
                hit, paths = score_recall(c, args.gw, token, args.k, args.timeout)
            except Exception as e:  # noqa: BLE001
                hit, paths = False, [f"(error: {e})"]
            r_hit += hit
            marks.append(("recall", "✓" if hit else "✗", "" if hit else " top=" + ";".join(paths[:3])))
        if args.mode in ("answer", "both"):
            try:
                ok, ms, text = score_answer(c, args.gw, token, args.session, args.timeout)
            except Exception as e:  # noqa: BLE001
                ok, ms, text = False, 0, f"(error: {e})"
            if ok is None:
                a_skip += 1
                marks.append(("answer", "~", " (must_contain 없음 — 채점 제외)"))
            else:
                a_tot += 1
                a_pass += ok
                detail = f" {ms}ms"
                if not ok:
                    missing = [m for m in c.get("must_contain") or [] if not contains(text, m)]
                    detail += f" missing={missing}"
                if args.verbose:
                    detail += " | " + text[:160].replace("\n", " ")
                marks.append(("answer", "✓" if ok else "✗", detail))
        mark_str = "  ".join(f"{k}:{v}{d}" for k, v, d in marks)
        print(f"[{c['category']:>3}] {c['id']:<22} {mark_str}")

    print()
    if r_tot:
        print(f"WIKI_QA_RECALL={r_hit}/{r_tot} ({100 * r_hit // r_tot}%)")
    if a_tot:
        print(f"WIKI_QA_ANSWER={a_pass}/{a_tot} ({100 * a_pass // a_tot}%) skipped={a_skip}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
