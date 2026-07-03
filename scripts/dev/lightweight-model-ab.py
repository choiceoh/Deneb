#!/usr/bin/env python3
"""
lightweight-model-ab.py — A/B battery for the *lightweight/tiny* model roles.

Compares two candidate models on Deneb's actual local-workhorse duties — the
text-only chores the lightweight/tiny roles perform (no tool calling):

  compaction  한국어 대화를 사실 보존 요약으로 압축 (컴팩션 청크 요약과 동형)
  extract     한국어 업무 메일 → 고정 스키마 JSON 추출 (gmail stage1과 동형)
  title       대화 스니펫 → 짧은 한국어 명사구 제목 (세션 자동 제목과 동형)
  verdict     "DONE/CONTINUE 한 단어만" 바운드 판정 (goal judge와 동형 — 장황함 프로브)

Scoring is DETERMINISTIC (fact checklists, JSON parsing, length/format rules) —
no LLM judge — so the number is reproducible and argues for itself. Latency and
output-token counts ride along because verbosity is wall-clock on these paths
(compaction latency has caused real incidents).

Usage (on the DGX host; both models served behind wormhole):
  python3 scripts/dev/lightweight-model-ab.py \
      --model-a qwen3.6-35b --model-b agents-a1 \
      [--base-url http://127.0.0.1:18800/v1] [--api-key-env WORMHOLE_TOKEN] [--rounds 1]

Self-test of the harness + scoring (no server needed):
  python3 scripts/dev/lightweight-model-ab.py --mock

Output: per-task table + one greppable line per model —
  AB_METRIC model=<name> total=<0-100> compaction=.. extract=.. title=.. verdict=.. avg_latency_ms=.. avg_out_tokens=..
and a final `AB_VERDICT winner=<name|tie> margin=<pts>` line.

Role doctrine: .claude/rules/model-roles.md — tool-heavy roles are promoted via
SparkFleet run_tool_eval; THIS script is the counterpart for the text roles.
"""

import argparse
import http.server
import json
import os
import re
import sys
import threading
import time
import urllib.error
import urllib.request

# --- Task battery (fixed corpus, planted ground truth) ---------------------

# Each fact is an any-of variant list: retention counts if ANY variant appears
# (models legitimately normalize "5,300만원" → "5300만원").
COMPACTION_CASES = [
    {
        "name": "compaction-deal",
        "dialogue": (
            "사용자: 어제 한빛건설 미팅 어떻게 됐지?\n"
            "비서: 한빛건설 발주분 케이블 단가를 미터당 12,400원으로 최종 합의했습니다. "
            "납품은 8월 20일까지 1차분 60%, 잔여분은 9월 10일까지입니다. "
            "선수금은 계약금액의 30%인 5,300만원이고 계약서 날인은 다음 주 화요일입니다.\n"
            "사용자: 담당자 누구였지?\n"
            "비서: 구매팀 박정우 차장입니다. 결재라인이 바뀌어서 최종 승인은 이제 강민석 상무가 합니다. "
            "품질 서류는 KS 인증서와 시험성적서 2부를 요구했고, 물류는 진영로지스틱스로 지정했습니다.\n"
            "사용자: 리스크는?\n"
            "비서: 구리값이 톤당 5% 더 오르면 단가 재협상 조항이 발동됩니다. 그 경우 마진이 2%p 줄어듭니다."
        ),
        "facts": [
            ["12,400원", "12400원"],
            ["8월 20일"],
            ["9월 10일"],
            ["5,300만원", "5300만원"],
            ["박정우"],
            ["강민석"],
            ["재협상"],
        ],
    },
    {
        "name": "compaction-ops",
        "dialogue": (
            "사용자: 서버 점검 결과 정리해줘.\n"
            "비서: 게이트웨이는 포트 18789에서 정상이고, 평균 응답은 1.8초입니다. "
            "디스크는 spark4tb 노드가 82% 찼고 30일 보존 정책 기준으로 다음 달 초 정리가 필요합니다. "
            "백업은 매일 자정에 돌았고 실패는 6월 28일 한 번, 원인은 ssh 타임아웃이었습니다.\n"
            "사용자: 조치는?\n"
            "비서: 재시도 간격을 30초에서 90초로 늘렸고, 실패 시 알림을 Error 레벨로 올렸습니다. "
            "다음 점검은 7월 15일로 잡아뒀습니다."
        ),
        "facts": [
            ["18789"],
            ["82%", "82 %"],
            ["6월 28일"],
            ["ssh 타임아웃", "ssh타임아웃", "SSH 타임아웃"],
            ["90초"],
            ["7월 15일"],
        ],
    },
]

EXTRACT_CASES = [
    {
        "name": "extract-quote",
        "mail": (
            "제목: [태양광] 영덕 부지 모듈 견적 회신 요청\n"
            "보낸사람: 김서연 과장 <sy.kim@daehanenc.co.kr>\n\n"
            "안녕하세요, 대한이엔씨 김서연입니다.\n"
            "영덕 부지 건으로 550W 모듈 1,800장 견적을 요청드립니다. "
            "예산은 부가세 별도 9억 2천만원 한도이며, 7월 11일(금)까지 회신 부탁드립니다. "
            "가능하시면 KS 인증서 사본과 납기 계획서도 함께 보내주세요."
        ),
        "truth": {
            "금액": [["9억 2천만원", "9억2천만원", "920,000,000", "9.2억"]],
            "기한": [["7월 11일", "7/11"]],
            "요청사항": [["견적"], ["KS 인증서", "KS인증서"], ["납기 계획서", "납기계획서"]],
        },
    },
    {
        "name": "extract-payment",
        "mail": (
            "제목: 잔금 입금 일정 안내\n"
            "보낸사람: 정해준 부장 <hj.jung@sunvalley.kr>\n\n"
            "선밸리에너지 정해준입니다. 계약 잔금 1억 4,500만원은 8월 5일 입금 예정입니다. "
            "세금계산서는 입금일 기준으로 발행 부탁드리고, 통장 사본을 이번 주 안에 보내주시면 됩니다."
        ),
        "truth": {
            "금액": [["1억 4,500만원", "1억4500만원", "1억 4500만원", "145,000,000"]],
            "기한": [["8월 5일", "8/5"]],
            "요청사항": [["세금계산서"], ["통장 사본", "통장사본"]],
        },
    },
]

TITLE_CASES = [
    {
        "name": "title-deal",
        "snippet": (
            "사용자: 한빛건설 케이블 단가 합의된 걸로 계약서 초안 잡아줘. 선수금 조항이랑 "
            "구리값 재협상 조항 꼭 넣고.\n비서: 초안을 작성하겠습니다. 날인 일정은 다음 주 화요일로 잡습니다."
        ),
    },
    {
        "name": "title-ops",
        "snippet": (
            "사용자: 백업 실패 알림이 왜 안 왔는지 봐줘.\n"
            "비서: 6월 28일 실패는 Warn 레벨로만 기록되어 알림이 발송되지 않았습니다. Error로 올리겠습니다."
        ),
    },
]

VERDICT_CASES = [
    {
        "name": "verdict-done",
        "context": (
            "목표: 주간보고 PDF를 생성해 메일로 발송한다.\n"
            "진행 기록: PDF 생성 완료(4페이지). 수신자 3명에게 발송 완료, 반송 없음. 발송 로그 확인됨."
        ),
        "answer": "DONE",
    },
    {
        "name": "verdict-continue",
        "context": (
            "목표: 영덕 부지 인허가 서류 5종을 모두 수집한다.\n"
            "진행 기록: 3종 확보(개발행위허가·환경검토·소유권 등본). 나머지 2종은 군청 회신 대기 중."
        ),
        "answer": "CONTINUE",
    },
]

# --- Prompts (mirror the real duties' shape: bounded, format-strict) -------


def compaction_prompt(dialogue: str) -> str:
    return (
        "아래 대화를 나중에 참조할 사실 보존 요약으로 압축하세요. "
        "결정·금액·날짜·이름·수치를 반드시 유지하고, 5문장 이내의 한국어 산문으로만 답하세요. "
        "머리말·목록·설명 없이 요약 본문만 출력합니다.\n\n" + dialogue
    )


def extract_prompt(mail: str) -> str:
    return (
        "아래 메일에서 정보를 추출해 JSON만 출력하세요. 다른 텍스트·코드펜스 금지.\n"
        '스키마: {"보낸사람": str, "의도": str, "금액": [str], "기한": [str], "요청사항": [str]}\n'
        "해당 없음은 빈 배열.\n\n" + mail
    )


def title_prompt(snippet: str) -> str:
    return (
        "아래 대화의 제목을 한국어 명사구로 15자 이내, 따옴표·마침표 없이 정확히 한 줄만 출력하세요.\n\n"
        + snippet
    )


def verdict_prompt(context: str) -> str:
    return (
        context + "\n\n위 목표가 달성되었으면 DONE, 아니면 CONTINUE — 둘 중 한 단어만 출력하세요."
    )


# --- Deterministic scoring (0-100 per task) --------------------------------


def _any_variant(text: str, variants) -> bool:
    squashed = re.sub(r"\s+", " ", text)
    return any(v in squashed for v in variants)


def hangul_ratio(text: str) -> float:
    letters = [c for c in text if c.isalpha()]
    if not letters:
        return 0.0
    return sum(1 for c in letters if "가" <= c <= "힣") / len(letters)


def score_compaction(case, out: str) -> float:
    facts = case["facts"]
    kept = sum(1 for f in facts if _any_variant(out, f))
    fact_score = kept / len(facts)
    ratio = len(out) / max(1, len(case["dialogue"]))
    brevity = 1.0 if ratio <= 0.35 else max(0.0, (1.0 - ratio) / 0.65)
    korean = 1.0 if hangul_ratio(out) >= 0.3 else 0.0
    return 100 * (0.6 * fact_score + 0.25 * brevity + 0.15 * korean)


def _strip_fence(text: str) -> str:
    t = text.strip()
    if t.startswith("```"):
        t = re.sub(r"^```[a-zA-Z]*\n?", "", t)
        t = re.sub(r"\n?```$", "", t.strip())
    return t.strip()


def score_extract(case, out: str) -> float:
    raw = out.strip()
    fenced = raw != _strip_fence(raw)
    try:
        obj = json.loads(_strip_fence(raw))
    except (json.JSONDecodeError, ValueError):
        return 0.0
    if not isinstance(obj, dict):
        return 0.0
    # parse 40 (fence-wrapped costs 10: the real pipeline wants bare JSON)
    score = 30.0 if fenced else 40.0
    keys = {"보낸사람", "의도", "금액", "기한", "요청사항"}
    score += 20.0 * (len(keys & set(obj.keys())) / len(keys))
    # ground-truth field hits: 40
    blob = json.dumps(obj, ensure_ascii=False)
    checks = [v for groups in case["truth"].values() for v in groups]
    hit = sum(1 for variants in checks if _any_variant(blob, variants))
    score += 40.0 * (hit / len(checks))
    return score


def score_title(_case, out: str) -> float:
    t = out.strip()
    score = 0.0
    if t and "\n" not in t:
        score += 25
    if 0 < len(t) <= 20:  # 15자 목표, 20자까지 관용
        score += 25
    if t and not re.search(r'["\'`.!?」』]$|^["\'`「『]', t) and "#" not in t:
        score += 25
    if hangul_ratio(t) >= 0.3:
        score += 25
    return score


def score_verdict(case, out: str) -> float:
    t = out.strip().strip(".")
    if t == case["answer"]:
        return 100.0
    # 정답을 포함하되 말이 붙은 경우 — 바운드 판정 소비자는 한 단어를 기대한다
    if case["answer"] in t and ("DONE" in t) != ("CONTINUE" in t):
        return 40.0
    return 0.0


TASKS = [
    ("compaction", COMPACTION_CASES, lambda c: compaction_prompt(c["dialogue"]), score_compaction, 400),
    ("extract", EXTRACT_CASES, lambda c: extract_prompt(c["mail"]), score_extract, 300),
    ("title", TITLE_CASES, lambda c: title_prompt(c["snippet"]), score_title, 50),
    ("verdict", VERDICT_CASES, lambda c: verdict_prompt(c["context"]), score_verdict, 20),
]

# --- OpenAI-compatible client (stdlib only) ---------------------------------


def chat_once(base_url: str, api_key: str, model: str, prompt: str, max_tokens: int, timeout: float):
    body = json.dumps(
        {
            "model": model,
            "messages": [{"role": "user", "content": prompt}],
            "temperature": 0,
            "max_tokens": max_tokens,
        }
    ).encode("utf-8")
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        data=body,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {api_key}"},
        method="POST",
    )
    start = time.monotonic()
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 — local/tailnet endpoints only
        payload = json.load(resp)
    latency_ms = int((time.monotonic() - start) * 1000)
    content = payload["choices"][0]["message"]["content"] or ""
    usage = payload.get("usage") or {}
    out_tokens = usage.get("completion_tokens") or max(1, len(content) // 3)
    return content, latency_ms, out_tokens


def chat_with_retry(base_url, api_key, model, prompt, max_tokens, timeout):
    try:
        return chat_once(base_url, api_key, model, prompt, max_tokens, timeout)
    except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError, OSError) as e:
        print(f"  ! transient error for {model}: {e} — retrying once", file=sys.stderr)
        time.sleep(2)
        return chat_once(base_url, api_key, model, prompt, max_tokens, timeout)


# --- Runner ------------------------------------------------------------------


def run_model(base_url, api_key, model, rounds, timeout):
    per_task = {}
    latencies, out_tokens_all = [], []
    for task_name, cases, mk_prompt, scorer, max_tokens in TASKS:
        scores = []
        for _ in range(rounds):
            for case in cases:
                out, ms, otoks = chat_with_retry(base_url, api_key, model, mk_prompt(case), max_tokens, timeout)
                s = scorer(case, out)
                scores.append(s)
                latencies.append(ms)
                out_tokens_all.append(otoks)
                print(f"  {model:<24} {case['name']:<20} score={s:5.1f} latency={ms}ms out_tokens={otoks}")
        per_task[task_name] = sum(scores) / len(scores)
    total = sum(per_task.values()) / len(per_task)
    return {
        "model": model,
        "total": total,
        "per_task": per_task,
        "avg_latency_ms": int(sum(latencies) / len(latencies)),
        "avg_out_tokens": int(sum(out_tokens_all) / len(out_tokens_all)),
    }


def print_report(results):
    print()
    header = f"{'task':<12}" + "".join(f"{r['model']:>26}" for r in results)
    print(header)
    for task_name, _, _, _, _ in TASKS:
        print(f"{task_name:<12}" + "".join(f"{r['per_task'][task_name]:>26.1f}" for r in results))
    print(f"{'TOTAL':<12}" + "".join(f"{r['total']:>26.1f}" for r in results))
    print(f"{'avg ms':<12}" + "".join(f"{r['avg_latency_ms']:>26}" for r in results))
    print(f"{'avg tokens':<12}" + "".join(f"{r['avg_out_tokens']:>26}" for r in results))
    print()
    for r in results:
        tasks = " ".join(f"{k}={v:.1f}" for k, v in r["per_task"].items())
        print(
            f"AB_METRIC model={r['model']} total={r['total']:.1f} {tasks} "
            f"avg_latency_ms={r['avg_latency_ms']} avg_out_tokens={r['avg_out_tokens']}"
        )
    a, b = results
    margin = abs(a["total"] - b["total"])
    winner = "tie" if margin < 3.0 else (a["model"] if a["total"] > b["total"] else b["model"])
    print(f"AB_VERDICT winner={winner} margin={margin:.1f}")
    return winner


# --- Mock self-test ----------------------------------------------------------
# Two fake models: "mock-good" answers every task compliantly; "mock-verbose"
# behaves like an over-planning agent model (long prose, fenced JSON, chatty
# verdicts). The harness must score good > verbose — proving the scoring can
# actually tell the two behaviors apart before anyone trusts it on real models.

MOCK_GOOD = {
    "compaction": (
        "한빛건설 케이블 단가는 미터당 12,400원으로 합의했고 1차 납품은 8월 20일, 잔여분은 9월 10일까지다. "
        "선수금은 5,300만원이며 담당은 박정우 차장, 최종 승인은 강민석 상무다. "
        "구리값 5% 이상 상승 시 단가 재협상 조항이 발동된다. "
        "서버는 18789 포트 정상, spark4tb 디스크 82%, 6월 28일 백업 실패 원인은 ssh 타임아웃이며 재시도 간격을 90초로 늘렸고 다음 점검은 7월 15일이다."
    ),
    "extract": (
        '{"보낸사람": "김서연", "의도": "견적 요청", "금액": ["9억 2천만원", "1억 4,500만원"], '
        '"기한": ["7월 11일", "8월 5일"], "요청사항": ["견적", "KS 인증서", "납기 계획서", "세금계산서", "통장 사본"]}'
    ),
    "title": "한빛건설 계약 초안",
    "verdict-done": "DONE",
    "verdict-continue": "CONTINUE",
}

MOCK_VERBOSE = {
    "compaction": (
        "이 과제를 해결하기 위해 먼저 대화를 단계별로 분석하겠습니다. 1단계: 주요 주제 식별. 2단계: 세부 사실 정리. "
        "회의에서는 여러 주제가 논의되었습니다. 단가와 납기에 대한 논의가 있었고 담당자 관련 사항도 언급되었습니다. "
        "추가로 리스크 요인에 대한 검토가 필요해 보입니다. 요약하자면 전반적으로 계약 관련 진행 상황이 공유되었습니다. "
        "다음 단계로는 계약서 검토를 제안드립니다. 더 자세한 분석이 필요하시면 말씀해 주세요. "
        "이상으로 대화 내용을 폭넓게 살펴보았습니다. 참고로 위 내용은 제공된 대화만을 근거로 하였습니다."
    ),
    "extract": '```json\n{"보낸사람": "김서연", "의도": "견적 요청", "금액": [], "기한": []}\n```\n추출을 완료했습니다.',
    "title": '"한빛건설과의 케이블 단가 계약 초안 작성 요청 건에 대한 대화입니다."',
    "verdict-done": "분석 결과, 목표가 달성된 것으로 판단되므로 DONE입니다.",
    "verdict-continue": "아직 서류 2종이 남아 있으므로 CONTINUE가 적절해 보입니다. 추가 조치를 제안드립니다.",
}


class _MockHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802 — http.server API
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        prompt = body["messages"][0]["content"]
        table = MOCK_GOOD if body["model"] == "mock-good" else MOCK_VERBOSE
        if "요약으로 압축" in prompt:
            content = table["compaction"]
        elif "JSON만 출력" in prompt:
            content = table["extract"]
        elif "제목" in prompt:
            content = table["title"]
        elif "DONE" in prompt:
            content = table["verdict-done"] if "발송 완료" in prompt else table["verdict-continue"]
        else:
            content = "?"
        resp = json.dumps(
            {"choices": [{"message": {"content": content}}], "usage": {"completion_tokens": len(content) // 3}}
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(resp)))
        self.end_headers()
        self.wfile.write(resp)

    def log_message(self, *args):  # silence request logging
        pass


def run_mock() -> int:
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _MockHandler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{server.server_address[1]}/v1"
    try:
        results = [run_model(base, "mock", m, 1, 30) for m in ("mock-good", "mock-verbose")]
        winner = print_report(results)
        good, verbose = results
        ok = winner == "mock-good" and good["total"] >= 85 and verbose["total"] <= 60
        print(f"MOCK_SELFTEST {'PASS' if ok else 'FAIL'} good={good['total']:.1f} verbose={verbose['total']:.1f}")
        return 0 if ok else 1
    finally:
        server.shutdown()


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model-a", help="first model name as served (e.g. qwen3.6-35b)")
    ap.add_argument("--model-b", help="second model name as served (e.g. agents-a1)")
    ap.add_argument("--base-url", default="http://127.0.0.1:18800/v1", help="OpenAI-compatible base (default: wormhole)")
    ap.add_argument("--api-key-env", default="WORMHOLE_TOKEN", help="env var holding the bearer token")
    ap.add_argument("--rounds", type=int, default=1, help="repeat the battery N times per model")
    ap.add_argument("--timeout", type=float, default=120.0, help="per-call timeout seconds")
    ap.add_argument("--mock", action="store_true", help="run the harness self-test against built-in mock models")
    args = ap.parse_args()

    if args.mock:
        return run_mock()
    if not args.model_a or not args.model_b:
        ap.error("--model-a and --model-b are required (or use --mock)")
    api_key = os.environ.get(args.api_key_env, "")
    if not api_key:
        print(f"warning: ${args.api_key_env} is empty — sending without auth", file=sys.stderr)
    results = [run_model(args.base_url, api_key, m, args.rounds, args.timeout) for m in (args.model_a, args.model_b)]
    print_report(results)
    return 0


if __name__ == "__main__":
    sys.exit(main())
