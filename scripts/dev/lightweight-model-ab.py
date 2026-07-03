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
        # 실전 컴팩션 청크에 가까운 길이(~1.3K자) — 짧은 입력이면 하드캡(ratio>0.7)이
        # 모범 요약까지 잡는다 (실측: 초기 짧은 코퍼스에서 두 모델 모두 30점 동률).
        "dialogue": (
            "사용자: 어제 한빛건설 미팅 어떻게 됐지?\n"
            "비서: 한빛건설 발주분 케이블 단가를 미터당 12,400원으로 최종 합의했습니다. "
            "지난 두 차례 협상에서 상대가 12,900원을 고수했는데, 연간 물량 보장을 조건으로 500원을 내렸습니다. "
            "납품은 8월 20일까지 1차분 60%, 잔여분은 9월 10일까지입니다. "
            "선수금은 계약금액의 30%인 5,300만원이고 계약서 날인은 다음 주 화요일입니다.\n"
            "사용자: 날인 전에 우리가 준비할 건?\n"
            "비서: 법무 검토는 어제 끝났고 수정 요청 두 건이 반영됐습니다. 하나는 지체상금 상한을 계약금액의 10%로 "
            "묶는 것, 다른 하나는 불가항력 조항에 원자재 수급 차질을 명시하는 것입니다. 인감증명서와 사용인감계는 "
            "총무팀이 내일까지 준비하고, 날인 장소는 상대 본사 3층 회의실로 잡혔습니다.\n"
            "사용자: 담당자 누구였지?\n"
            "비서: 구매팀 박정우 차장입니다. 결재라인이 바뀌어서 최종 승인은 이제 강민석 상무가 합니다. "
            "박 차장은 회신이 빠른 편이라 실무 협의는 메일로 충분하고, 상무 보고용으로는 한 장짜리 요약을 "
            "선호한다고 합니다. 품질 서류는 KS 인증서와 시험성적서 2부를 요구했고, 물류는 진영로지스틱스로 "
            "지정했습니다. 진영 쪽 배차는 납품 사흘 전에 확정해야 창고 상차 일정이 맞습니다.\n"
            "사용자: 리스크는?\n"
            "비서: 구리값이 톤당 5% 더 오르면 단가 재협상 조항이 발동됩니다. 그 경우 마진이 2%p 줄어듭니다. "
            "환율은 이번 분기 계약분에는 영향이 없고, 다음 분기 원자재 발주부터 반영됩니다. 상대 자금 사정은 "
            "특이사항이 없지만, 모기업 감사가 이달 말이라 결재가 하루 이틀 늦어질 수 있다는 언질이 있었습니다."
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
        # Facts that belong to the OTHER case — appearing here means the model
        # contaminated the summary with unrelated context (hallucination probe).
        "foreign": [["18789"], ["spark4tb"], ["ssh 타임아웃", "ssh타임아웃"], ["7월 15일"]],
    },
    {
        "name": "compaction-ops",
        "dialogue": (
            "사용자: 서버 점검 결과 정리해줘.\n"
            "비서: 게이트웨이는 포트 18789에서 정상이고, 평균 응답은 1.8초입니다. 최근 일주일 오류율은 0.2%로 "
            "평시 범위이고, 피크는 아침 브리핑 생성 시간대에 몰립니다. "
            "디스크는 spark4tb 노드가 82% 찼고 30일 보존 정책 기준으로 다음 달 초 정리가 필요합니다. "
            "오래된 전사 파일이 용량의 절반 이상이라 정리 효과는 클 것으로 봅니다. "
            "백업은 매일 자정에 돌았고 실패는 6월 28일 한 번, 원인은 ssh 타임아웃이었습니다. "
            "그날 새벽 네트워크 순단이 있었고 같은 시간대에 모니터링 핑도 두 번 유실됐습니다.\n"
            "사용자: 그 실패 때 데이터는 괜찮았어?\n"
            "비서: 네, 다음 날 백업이 정상적으로 돌아 공백은 하루뿐이고, 그 하루치도 원본이 남아 있어 유실은 없습니다. "
            "복원 리허설은 지난달에 해뒀고 샘플 복원까지 12분 걸렸습니다.\n"
            "사용자: 조치는?\n"
            "비서: 재시도 간격을 30초에서 90초로 늘렸고, 실패 시 알림을 Error 레벨로 올렸습니다. "
            "알림이 Warn에 묻혀 아침까지 아무도 모를 뻔한 게 이번 건의 교훈이라, 같은 패턴의 다른 작업 두 개도 "
            "레벨을 함께 손봤습니다. 다음 점검은 7월 15일로 잡아뒀습니다. 그때 디스크 정리 결과와 재시도 "
            "성공률을 같이 확인하겠습니다."
        ),
        "facts": [
            ["18789"],
            ["82%", "82 %"],
            ["6월 28일"],
            ["ssh 타임아웃", "ssh타임아웃", "SSH 타임아웃"],
            ["90초"],
            ["7월 15일"],
        ],
        "foreign": [["12,400원", "12400원"], ["한빛건설"], ["박정우"], ["5,300만원", "5300만원"]],
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
        # Values from the OTHER mail — presence means over-extraction (each hit
        # deducts; a model that dumps every value it has ever seen must not pass).
        "foreign": [["1억 4,500만원", "1억4500만원", "1억 4500만원"], ["8월 5일"], ["세금계산서"], ["통장 사본", "통장사본"]],
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
        "foreign": [["9억 2천만원", "9억2천만원"], ["7월 11일"], ["KS 인증서", "KS인증서"], ["납기 계획서", "납기계획서"]],
    },
]

TITLE_CASES = [
    {
        "name": "title-deal",
        "snippet": (
            "사용자: 한빛건설 케이블 단가 합의된 걸로 계약서 초안 잡아줘. 선수금 조항이랑 "
            "구리값 재협상 조항 꼭 넣고.\n비서: 초안을 작성하겠습니다. 날인 일정은 다음 주 화요일로 잡습니다."
        ),
        # Relevance anchor: a useful title must name the subject, not just be a
        # short Korean phrase — any one of these must appear.
        "keywords": ["한빛", "계약", "케이블", "단가"],
    },
    {
        "name": "title-ops",
        "snippet": (
            "사용자: 백업 실패 알림이 왜 안 왔는지 봐줘.\n"
            "비서: 6월 28일 실패는 Warn 레벨로만 기록되어 알림이 발송되지 않았습니다. Error로 올리겠습니다."
        ),
        "keywords": ["백업", "알림", "실패"],
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
    score = 100 * (0.6 * fact_score + 0.25 * brevity + 0.15 * korean)
    # 압축 실패는 하드 캡: 원문의 70%를 넘겨 출력하면 사실을 다 담아도 "요약"이 아니다
    # (통짜 에코가 부분 점수로 살아남던 헛점 봉쇄).
    if ratio > 0.7:
        score = min(score, 30.0)
    # 환각 혼입 벌점: 다른 케이스의 사실이 이 요약에 나타나면 건당 -15 — 복원된
    # 컨텍스트를 오염시키는 모델이 승격되면 안 된다.
    foreign = sum(1 for f in case.get("foreign", []) if _any_variant(out, f))
    return max(0.0, score - 15.0 * foreign)


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
    # 스키마 준수는 키 존재 + 타입까지 — 리스트 필드에 문자열을 넣는 출력이
    # 통과하면 stage1 소비자(배열 순회)가 깨진다.
    want_types = {"보낸사람": str, "의도": str, "금액": list, "기한": list, "요청사항": list}
    ok_keys = sum(1 for k, t in want_types.items() if isinstance(obj.get(k), t))
    score += 20.0 * (ok_keys / len(want_types))
    # ground-truth field hits: 40
    blob = json.dumps(obj, ensure_ascii=False)
    checks = [v for groups in case["truth"].values() for v in groups]
    hit = sum(1 for variants in checks if _any_variant(blob, variants))
    score += 40.0 * (hit / len(checks))
    # 과잉 추출 벌점: 이 메일에 없는(다른 메일의) 값이 섞여 나오면 건당 -15 —
    # 정답을 다 맞혀도 오염된 필드는 승격 불가.
    foreign = sum(1 for variants in case.get("foreign", []) if _any_variant(blob, variants))
    return max(0.0, score - 15.0 * foreign)


def score_title(case, out: str) -> float:
    t = out.strip()
    score = 0.0
    if t and "\n" not in t:
        score += 20
    # 프롬프트는 15자 요구 — 15자 만점, 20자까진 절반 (글자수 감각 관용은 부분 점수로만)
    if 0 < len(t) <= 15:
        score += 20
    elif len(t) <= 20:
        score += 10
    # 금지 문자는 위치 불문 (따옴표·마크다운 전역, 마침표류는 끝)
    if t and not re.search(r'["\'`「」『』#*]|[.!?…]$', t):
        score += 20
    if hangul_ratio(t) >= 0.3:
        score += 15
    # 관련성: 주제를 지칭해야 제목이다 — "업무 정리" 같은 만능 제목 차단.
    if any(k in t for k in case.get("keywords", [])):
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


# (name, cases, prompt-builder, scorer, max_tokens, response_format)
# extract는 프로덕션 gmail stage1과 같은 JSON 모드로 호출해 guided-decoding 호환까지
# 함께 측정한다 (서버가 400으로 거부하면 형식 없이 1회 폴백 — 로그에 표기).
TASKS = [
    # 1024 = 프로덕션 컴팩션의 per-chunk 출력 캡과 동일 (사고형 모델도 완주 기회를 갖되,
    # 장황함은 brevity 점수·avg_out_tokens·레이턴시로 정직하게 드러난다).
    ("compaction", COMPACTION_CASES, lambda c: compaction_prompt(c["dialogue"]), score_compaction, 1024, None),
    ("extract", EXTRACT_CASES, lambda c: extract_prompt(c["mail"]), score_extract, 300, {"type": "json_object"}),
    ("title", TITLE_CASES, lambda c: title_prompt(c["snippet"]), score_title, 50, None),
    ("verdict", VERDICT_CASES, lambda c: verdict_prompt(c["context"]), score_verdict, 20, None),
]

# --- OpenAI-compatible client (stdlib only) ---------------------------------


def chat_once(base_url: str, api_key: str, model: str, prompt: str, max_tokens: int, timeout: float, response_format=None):
    payload_body = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    if response_format:
        payload_body["response_format"] = response_format
    headers = {"Content-Type": "application/json"}
    if api_key:  # 빈 Bearer 헤더는 무인증과 다르게 취급하는 서버가 있다 — 아예 생략
        headers["Authorization"] = f"Bearer {api_key}"
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        data=json.dumps(payload_body).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 — local/tailnet endpoints only
        payload = json.load(resp)
    content = payload["choices"][0]["message"]["content"] or ""
    usage = payload.get("usage") or {}
    out_tokens = usage.get("completion_tokens") or max(1, len(content) // 3)
    return content, out_tokens


# 재시도할 가치가 있는 상태만 — 4xx(400/401/404 등)는 재시도해도 같은 답이고
# 서버에 이중 부하만 준다.
TRANSIENT_HTTP = {408, 429, 500, 502, 503, 504}


def chat_with_retry(base_url, api_key, model, prompt, max_tokens, timeout, response_format=None):
    # 레이턴시는 재시도·백오프를 포함한 벽시계 전체 — 플레이키한 모델이 실패 시간을
    # 숨기고 건강한 avg_latency_ms를 보고하면 운영 판단이 왜곡된다.
    start = time.monotonic()

    def done(content, out_tokens):
        return content, int((time.monotonic() - start) * 1000), out_tokens

    try:
        return done(*chat_once(base_url, api_key, model, prompt, max_tokens, timeout, response_format))
    except urllib.error.HTTPError as e:
        # guided-decoding 미지원 서버의 response_format 거부 → 형식 없이 1회 폴백
        if response_format is not None and e.code == 400:
            print(f"  ! {model}: response_format rejected (400) — falling back without JSON mode", file=sys.stderr)
            return done(*chat_once(base_url, api_key, model, prompt, max_tokens, timeout, None))
        if e.code not in TRANSIENT_HTTP:
            raise
        print(f"  ! transient HTTP {e.code} for {model} — retrying once", file=sys.stderr)
    except (urllib.error.URLError, TimeoutError, OSError) as e:
        print(f"  ! transient error for {model}: {e} — retrying once", file=sys.stderr)
    time.sleep(2)
    return done(*chat_once(base_url, api_key, model, prompt, max_tokens, timeout, response_format))


# --- Runner ------------------------------------------------------------------


def run_model(base_url, api_key, model, rounds, timeout):
    per_task = {}
    latencies, out_tokens_all = [], []
    for task_name, cases, mk_prompt, scorer, max_tokens, response_format in TASKS:
        scores = []
        for _ in range(rounds):
            for case in cases:
                out, ms, otoks = chat_with_retry(
                    base_url, api_key, model, mk_prompt(case), max_tokens, timeout, response_format
                )
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
    for task_name, _, _, _, _, _ in TASKS:
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
    # 케이스별로 정확한(그 케이스의 사실만 담은) 모범 답 — 교차 오염/과잉 추출 벌점이
    # 모범생에게는 발동하지 않고 위반자만 잡는다는 것까지 셀프테스트가 검증한다.
    "compaction-deal": (
        "한빛건설 케이블 단가는 미터당 12,400원으로 합의했고 1차 납품은 8월 20일, 잔여분은 9월 10일까지다. "
        "선수금은 5,300만원, 담당은 박정우 차장이고 최종 승인은 강민석 상무다. "
        "구리값 5% 이상 상승 시 단가 재협상 조항이 발동된다."
    ),
    "compaction-ops": (
        "게이트웨이는 18789 포트에서 정상이고 spark4tb 디스크는 82% 사용 중이다. "
        "6월 28일 백업 실패 원인은 ssh 타임아웃으로, 재시도 간격을 90초로 늘리고 알림을 Error로 올렸다. "
        "다음 점검은 7월 15일이다."
    ),
    "extract-quote": (
        '{"보낸사람": "김서연", "의도": "견적 요청", "금액": ["9억 2천만원"], '
        '"기한": ["7월 11일"], "요청사항": ["견적", "KS 인증서", "납기 계획서"]}'
    ),
    "extract-payment": (
        '{"보낸사람": "정해준", "의도": "잔금 입금 안내", "금액": ["1억 4,500만원"], '
        '"기한": ["8월 5일"], "요청사항": ["세금계산서", "통장 사본"]}'
    ),
    "title-deal": "한빛건설 계약 초안",
    "title-ops": "백업 알림 누락 점검",
    "verdict-done": "DONE",
    "verdict-continue": "CONTINUE",
}

MOCK_VERBOSE = {
    # 에이전트형 실패 모드 총집합: 계획 서두·통짜 재진술 + 교차 사실 혼입(compaction),
    # 코드펜스+사족+타입 위반+과잉 추출(extract), 따옴표 장문(title), 판정 사족(verdict).
    "compaction": (
        "이 과제를 해결하기 위해 먼저 대화를 단계별로 분석하겠습니다. 1단계: 주요 주제 식별. 2단계: 세부 사실 정리. "
        "회의에서는 여러 주제가 논의되었습니다. 단가와 납기에 대한 논의가 있었고 담당자 관련 사항도 언급되었습니다. "
        "참고로 지난 점검에서 spark4tb 디스크와 18789 포트 상태도 함께 확인된 바 있습니다. "
        "추가로 리스크 요인에 대한 검토가 필요해 보입니다. 요약하자면 전반적으로 계약 관련 진행 상황이 공유되었습니다. "
        "다음 단계로는 계약서 검토를 제안드립니다. 더 자세한 분석이 필요하시면 말씀해 주세요."
    ),
    "extract": (
        '```json\n{"보낸사람": "김서연", "의도": "견적 요청", "금액": "9억 2천만원과 1억 4,500만원", '
        '"기한": ["7월 11일", "8월 5일"], "요청사항": ["견적", "세금계산서", "통장 사본"]}\n```'
    ),
    "title": '"한빛건설과의 케이블 단가 계약 초안 작성 요청 건에 대한 대화입니다."',
    "verdict-done": "분석 결과, 목표가 달성된 것으로 판단되므로 DONE입니다.",
    "verdict-continue": "아직 서류 2종이 남아 있으므로 CONTINUE가 적절해 보입니다. 추가 조치를 제안드립니다.",
}


def _mock_reply(model: str, prompt: str) -> str:
    good = model == "mock-good"
    if "요약으로 압축" in prompt:
        if good:
            return MOCK_GOOD["compaction-deal"] if "한빛건설" in prompt else MOCK_GOOD["compaction-ops"]
        return MOCK_VERBOSE["compaction"]
    if "JSON만 출력" in prompt:
        if good:
            return MOCK_GOOD["extract-quote"] if "영덕" in prompt else MOCK_GOOD["extract-payment"]
        return MOCK_VERBOSE["extract"]
    if "제목" in prompt:
        if good:
            return MOCK_GOOD["title-deal"] if "케이블" in prompt else MOCK_GOOD["title-ops"]
        return MOCK_VERBOSE["title"]
    if "DONE" in prompt:
        key = "verdict-done" if "발송 완료" in prompt else "verdict-continue"
        return MOCK_GOOD[key] if good else MOCK_VERBOSE[key]
    return "?"


class _MockHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802 — http.server API
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        content = _mock_reply(body["model"], body["messages"][0]["content"])
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
