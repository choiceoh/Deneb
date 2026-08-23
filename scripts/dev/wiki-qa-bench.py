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
      must_contain(숫자는 쉼표/공백 무시 비교)·must_not∪stale_values·레이턴시로 채점.
      각 질문 전 /reset — 벤치 세션 1개만 생성된다. 정답·금지값이 모두 빈
      케이스는 answer 채점에서 skip(~)으로 표시.

출력: 케이스별 ✓/✗/~ 라인 + 머신 파싱용 요약:
  WIKI_QA_RECALL=hit/total   WIKI_QA_ANSWER=pass/total   WIKI_QA_STALE=leak/checked

예:
  python3 scripts/dev/wiki-qa-bench.py                       # recall 전량
  python3 scripts/dev/wiki-qa-bench.py --mode answer --ids pipeline-h2,kec-dc
  python3 scripts/dev/wiki-qa-bench.py --mode answer --repeat 3 \
    --json-out /tmp/candidate.json --baseline-json /tmp/baseline.json --require-zero-stale
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.request
from pathlib import Path


def rpc(gw, token, method, params, timeout):
    body = json.dumps({"id": "qa", "method": method, "params": params}).encode()
    req = urllib.request.Request(
        gw.rstrip("/") + "/api/v1/miniapp/rpc",
        data=body,
        headers={"Content-Type": "application/json", "X-Deneb-Client-Token": token},
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        res = json.load(resp)
    # RPC error frames come back as HTTP 200 ({ok:false,...} or bare {error}) —
    # surface them as exceptions so infra failures read as '(error: ...)'
    # instead of silently scoring as content misses.
    if isinstance(res, dict) and (res.get("ok") is False or ("error" in res and "payload" not in res)):
        raise RuntimeError(f"rpc {method}: {str(res.get('error') or res)[:200]}")
    return res


def digits_relaxed(s):
    """Strip commas/spaces so '1,068MW' matches needle '1,068' or '1068'."""
    return re.sub(r"[,\s]", "", s)


def digits_bounded(t, n):
    """Substring hit only at digit boundaries: '1,068' must not pass on '11,068'."""
    i = t.find(n)
    while i != -1:
        pre = t[i - 1] if i > 0 else ""
        post = t[i + len(n)] if i + len(n) < len(t) else ""
        if not pre.isdigit() and not post.isdigit():
            return True
        i = t.find(n, i + 1)
    return False


def contains(text, needle):
    # "a|b" = any-of alternation: one gold needle accepts phrasing variants
    # ("6/2|6월 2일") without loosening what counts as correct.
    text = str(text)
    for alt in str(needle).split("|"):
        alt = alt.strip()
        if not alt:
            continue
        t, n = text.casefold(), alt.casefold()
        if any(ch.isdigit() for ch in n):
            if digits_bounded(digits_relaxed(t), digits_relaxed(n)):
                return True
            continue
        if n in t:
            return True
    return False


def case_values(case, key):
    """Return one case list as stable, non-empty strings."""
    values = []
    seen = set()
    for value in case.get(key) or []:
        value = str(value).strip()
        if not value or value in seen:
            continue
        seen.add(value)
        values.append(value)
    return values


def forbidden_values(case):
    """Return the stable union used for stale and negative answer gating."""
    merged = {"values": [*case_values(case, "must_not"), *case_values(case, "stale_values")]}
    return case_values(merged, "values")


def path_hit(gold, p):
    """gold matches p only from a path-segment start; the match may continue
    within the segment ('비금도-154kv' → '비금도-154kv-케이블…') but must not
    start mid-name, so '영덕' never hits '남영덕/…'. Gold authors disambiguate
    sibling prefixes ('영덕' vs '영덕-구') by writing the longer form."""
    p = p.removesuffix(".md")
    g = str(gold).removesuffix(".md")
    if not g:
        return False
    i = p.find(g)
    while i != -1:
        if i == 0 or p[i - 1] == "/":
            return True
        i = p.find(g, i + 1)
    return False


def score_recall(case, gw, token, k, timeout):
    res = rpc(gw, token, "miniapp.memory.search", {"query": case["question"], "limit": k}, timeout)
    results = (res.get("payload") or {}).get("results") or []
    paths = [r.get("path", "") for r in results[:k]]
    hit = any(path_hit(g, p) for g in case["gold_paths"] for p in paths)
    return hit, paths


def score_answer(case, gw, token, session, timeout):
    must = case.get("must_contain") or []
    forbidden = forbidden_values(case)
    if not must and not forbidden:
        return None, 0, ""  # ungraded case — recall-only
    # Back-to-back sync turns on one session can collide with the previous
    # turn's async teardown (instant "run in progress"-style rejection) — settle
    # between turns and retry once on an empty/error response only (a fast
    # VALID answer must not trigger a costly duplicate turn).
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
        if text and not text.startswith("(error:"):
            break
        if attempt == 0:
            time.sleep(4)
    ok = all(contains(text, m) for m in must) and not any(contains(text, m) for m in forbidden)
    return ok, ms, text


def percentage(passed, total):
    return 100.0 * passed / total if total else None


def baseline_regressions(report, baseline, max_regression_pp):
    """Return aggregate regressions against a prior JSON run."""
    regressions = []
    current_summary = report.get("summary") or {}
    baseline_summary = baseline.get("summary") or {}
    for metric in ("recall", "answer", "current_value"):
        current = current_summary.get(metric) or {}
        prior = baseline_summary.get(metric) or {}
        denominator = "total" if metric in ("recall", "answer") else "checked"
        if not current.get(denominator) or not prior.get(denominator):
            continue
        current_pct = float(current.get("pct") or 0)
        prior_pct = float(prior.get("pct") or 0)
        if current_pct + max_regression_pp < prior_pct:
            regressions.append(
                f"{metric} {current_pct:.2f}% < baseline {prior_pct:.2f}% "
                f"(허용 {max_regression_pp:.2f}pp)"
            )

    for metric in ("stale", "stale_answer", "forget_leak"):
        current = current_summary.get(metric) or {}
        prior = baseline_summary.get(metric) or {}
        if not current.get("checked") or not prior.get("checked"):
            continue
        current_pct = float(current.get("pct") or 0)
        prior_pct = float(prior.get("pct") or 0)
        if current_pct > prior_pct + max_regression_pp:
            regressions.append(
                f"{metric} {current_pct:.2f}% > baseline {prior_pct:.2f}% "
                f"(허용 {max_regression_pp:.2f}pp)"
            )

    baseline_cases = {case.get("id"): case for case in baseline.get("cases") or []}
    for current_case in report.get("cases") or []:
        prior_case = baseline_cases.get(current_case.get("id"))
        if not prior_case:
            continue
        current_runs = ((current_case.get("answer") or {}).get("runs") or [])
        prior_runs = ((prior_case.get("answer") or {}).get("runs") or [])
        if len(current_runs) < 3 or len(prior_runs) < 3:
            continue
        current_passed = sum(bool(run.get("passed")) for run in current_runs)
        prior_passed = sum(bool(run.get("passed")) for run in prior_runs)
        if current_passed == 0 and 3 * prior_passed >= 2 * len(prior_runs):
            regressions.append(
                f"case {current_case.get('id')} persistent answer regression "
                f"{prior_passed}/{len(prior_runs)} -> 0/{len(current_runs)}"
            )
    return regressions


def main():
    ap = argparse.ArgumentParser(description="Deneb wiki QA bench")
    ap.add_argument("--mode", choices=["recall", "answer", "both"], default="recall")
    ap.add_argument("--gold", default=os.path.expanduser("~/.deneb/wiki-qa-gold.jsonl"))
    # Gateway/token defaults follow the live-test harness when it is active
    # (iterate.sh --metric path exports these), else production on this host.
    default_gw = (
        os.environ.get("DENEB_QA_GW")
        or os.environ.get("DENEB_LIVETEST_GW_URL")
        or "http://127.0.0.1:18789"
    )
    state_dir = os.environ.get("DENEB_LIVETEST_STATE_DIR")
    default_token = (
        os.path.join(state_dir, "client_token") if state_dir else os.path.expanduser("~/.deneb/client_token")
    )
    ap.add_argument("--gw", default=default_gw)
    ap.add_argument("--token-file", default=default_token)
    ap.add_argument("--k", type=int, default=8, help="recall hit@K (기본 8)")
    ap.add_argument("--ids", default="", help="쉼표로 지정한 케이스만")
    ap.add_argument("--limit", type=int, default=0, help="앞에서 N개만")
    ap.add_argument("--session", default="client:main:wiki-qa-bench")
    ap.add_argument("--timeout", type=int, default=240)
    ap.add_argument("--repeat", type=int, default=1, help="answer 케이스 반복 횟수 (기본 1)")
    ap.add_argument("--json-out", help="원문 응답을 제외한 실행 결과 JSON 저장 경로")
    ap.add_argument("--baseline-json", help="비열화 여부를 비교할 이전 --json-out 결과")
    ap.add_argument(
        "--max-regression-pp",
        type=float,
        default=1.0,
        help="baseline 대비 허용할 최대 하락폭 percentage-point (기본 1.0)",
    )
    ap.add_argument(
        "--require-zero-stale",
        action="store_true",
        help="must_not 또는 stale_values가 답변에 한 번이라도 나오면 exit 1",
    )
    ap.add_argument("--verbose", action="store_true", help="answer 모드에서 응답 앞부분 출력")
    args = ap.parse_args()
    if args.repeat < 1:
        ap.error("--repeat must be at least 1")
    if args.max_regression_pp < 0:
        ap.error("--max-regression-pp must be non-negative")
    if args.require_zero_stale and args.mode == "recall":
        ap.error("--require-zero-stale requires --mode answer or both")

    baseline = None
    if args.baseline_json:
        try:
            baseline = json.loads(Path(args.baseline_json).read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as e:
            print(f"baseline JSON 읽기 실패: {e}", file=sys.stderr)
            return 2
        if not isinstance(baseline, dict) or not isinstance(baseline.get("summary"), dict):
            print("baseline JSON 형식 오류: summary object가 필요합니다", file=sys.stderr)
            return 2

    with open(args.token_file, encoding="utf-8") as tf:
        token = tf.read().strip()
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
    stale_leaks = stale_checked = 0
    current_hits = current_checked = 0
    stale_answer_leaks = stale_answer_checked = 0
    forget_leaks = forget_checked = 0
    by_diff = {}  # "mode/difficulty" -> [pass, total]
    case_reports = []
    for c in cases:
        diff = c.get("difficulty", "easy")
        marks = []
        case_report = {
            "id": c["id"],
            "category": c.get("category", ""),
            "difficulty": diff,
        }
        if args.mode in ("recall", "both"):
            if not c.get("gold_paths"):
                # Negative-existence cases have no right page — recall N/A.
                marks.append(("recall", "~", " (gold_paths 없음)"))
                case_report["recall"] = {"graded": False}
            else:
                r_tot += 1
                try:
                    hit, paths = score_recall(c, args.gw, token, args.k, args.timeout)
                except Exception as e:  # noqa: BLE001
                    hit, paths = False, [f"(error: {e})"]
                r_hit += hit
                d = by_diff.setdefault("recall/" + diff, [0, 0])
                d[0] += hit
                d[1] += 1
                marks.append(("recall", "✓" if hit else "✗", "" if hit else " top=" + ";".join(paths[:3])))
                case_report["recall"] = {"graded": True, "passed": bool(hit)}
        if args.mode in ("answer", "both"):
            must = case_values(c, "must_contain")
            stale = case_values(c, "stale_values")
            forbidden = forbidden_values(c)
            is_forget = str(c.get("op_type") or "").casefold() == "forget"
            if not must and not forbidden:
                a_skip += 1
                marks.append(("answer", "~", " (must_contain 없음 — 채점 제외)"))
                case_report["answer"] = {"graded": False, "runs": []}
            else:
                runs = []
                texts = []
                for repeat_index in range(args.repeat):
                    try:
                        ok, ms, text = score_answer(c, args.gw, token, args.session, args.timeout)
                    except Exception as e:  # noqa: BLE001
                        ok, ms, text = False, 0, f"(error: {e})"
                    missing = [m for m in must if not contains(text, m)]
                    forbidden_hits = [m for m in forbidden if contains(text, m)]
                    current_hit = bool(must) and not missing
                    stale_answer_hit = any(contains(text, value) for value in stale)
                    forget_leak_hit = is_forget and bool(forbidden_hits)
                    a_tot += 1
                    a_pass += bool(ok)
                    d = by_diff.setdefault("answer/" + diff, [0, 0])
                    d[0] += bool(ok)
                    d[1] += 1
                    if forbidden:
                        stale_checked += 1
                        stale_leaks += bool(forbidden_hits)
                    if must:
                        current_checked += 1
                        current_hits += current_hit
                    if stale:
                        stale_answer_checked += 1
                        stale_answer_leaks += stale_answer_hit
                    if is_forget and forbidden:
                        forget_checked += 1
                        forget_leaks += forget_leak_hit
                    runs.append({
                        "repeat": repeat_index + 1,
                        "passed": bool(ok),
                        "latency_ms": ms,
                        "current_value_hit": current_hit,
                        "stale_answer_hit": stale_answer_hit,
                        "forget_leak_hit": forget_leak_hit,
                        "missing_count": len(missing),
                        "forbidden_hit_count": len(forbidden_hits),
                    })
                    texts.append(text)

                passed_runs = sum(run["passed"] for run in runs)
                all_ok = passed_runs == len(runs)
                if args.repeat == 1:
                    run = runs[0]
                    detail = f" {run['latency_ms']}ms"
                    if missing:
                        detail += f" missing={missing}"
                    if forbidden_hits:
                        detail += f" forbidden={forbidden_hits}"
                    if args.verbose:
                        detail += " | " + texts[0][:160].replace("\n", " ")
                else:
                    latencies = sorted(run["latency_ms"] for run in runs)
                    p95_index = max(0, (95 * len(latencies) + 99) // 100 - 1)
                    detail = f" {passed_runs}/{len(runs)} p95={latencies[p95_index]}ms"
                    leaked = sorted({value for text in texts for value in forbidden if contains(text, value)})
                    if leaked:
                        detail += f" forbidden={leaked}"
                    if args.verbose:
                        detail += " | " + texts[-1][:160].replace("\n", " ")
                marks.append(("answer", "✓" if all_ok else "✗", detail))
                case_report["answer"] = {"graded": True, "passed": all_ok, "runs": runs}
        mark_str = "  ".join(f"{kind}:{mark}{detail}" for kind, mark, detail in marks)
        print(f"[{c['category']:>3}] {c['id']:<22} {mark_str}")
        case_reports.append(case_report)

    print()
    if r_tot:
        print(f"WIKI_QA_RECALL={r_hit}/{r_tot} ({100 * r_hit // r_tot}%)")
    if a_tot:
        print(f"WIKI_QA_ANSWER={a_pass}/{a_tot} ({100 * a_pass // a_tot}%) skipped={a_skip}")
    if stale_checked:
        print(f"WIKI_QA_STALE={stale_leaks}/{stale_checked} ({100 * stale_leaks // stale_checked}%)")
    if current_checked:
        print(
            f"CURRENT_VALUE_RATE={current_hits}/{current_checked} "
            f"({100 * current_hits // current_checked}%)"
        )
    if stale_answer_checked:
        print(
            f"STALE_ANSWER_RATE={stale_answer_leaks}/{stale_answer_checked} "
            f"({100 * stale_answer_leaks // stale_answer_checked}%)"
        )
    if forget_checked:
        print(
            f"FORGET_LEAK_RATE={forget_leaks}/{forget_checked} "
            f"({100 * forget_leaks // forget_checked}%)"
        )
    for key in sorted(by_diff):
        hits, total = by_diff[key]
        print(f"  {key}: {hits}/{total} ({100 * hits // total}%)")
    # iterate.sh --metric CMD parses exactly this key for the optimization loop.
    if a_tot:
        print(f"metric_value={100 * a_pass // a_tot}")
    elif r_tot:
        print(f"metric_value={100 * r_hit // r_tot}")

    report = {
        "schema_version": 1,
        "mode": args.mode,
        "repeat": args.repeat,
        "summary": {
            "recall": {"passed": r_hit, "total": r_tot, "pct": percentage(r_hit, r_tot)},
            "answer": {
                "passed": a_pass,
                "total": a_tot,
                "pct": percentage(a_pass, a_tot),
                "skipped": a_skip,
            },
            "stale": {
                "leaks": stale_leaks,
                "checked": stale_checked,
                "pct": percentage(stale_leaks, stale_checked),
            },
            "current_value": {
                "hits": current_hits,
                "checked": current_checked,
                "pct": percentage(current_hits, current_checked),
            },
            "stale_answer": {
                "leaks": stale_answer_leaks,
                "checked": stale_answer_checked,
                "pct": percentage(stale_answer_leaks, stale_answer_checked),
            },
            "forget_leak": {
                "leaks": forget_leaks,
                "checked": forget_checked,
                "pct": percentage(forget_leaks, forget_checked),
            },
        },
        "cases": case_reports,
    }
    if args.json_out:
        try:
            Path(args.json_out).write_text(
                json.dumps(report, ensure_ascii=False, indent=2) + "\n",
                encoding="utf-8",
            )
        except OSError as e:
            print(f"JSON 결과 저장 실패: {e}", file=sys.stderr)
            return 2

    gate_failures = []
    if args.require_zero_stale:
        if not stale_checked:
            gate_failures.append("stale/forbidden 채점 케이스 0건")
        elif stale_leaks:
            gate_failures.append(f"stale/forbidden leak {stale_leaks}건 (요구값 0)")
    if baseline is not None:
        gate_failures.extend(baseline_regressions(report, baseline, args.max_regression_pp))
    for failure in gate_failures:
        print(f"CUTOVER_GATE_FAIL: {failure}", file=sys.stderr)
    return 1 if gate_failures else 0


if __name__ == "__main__":
    sys.exit(main())
