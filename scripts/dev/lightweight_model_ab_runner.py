"""Battery execution, reporting, and the built-in mock verifier."""

import http.server
import json
import threading

from lightweight_model_ab_battery import (
    COMPACTION_CASES,
    EXTRACT_CASES,
    JSON_REJECT_CAP,
    TASKS,
    TITLE_CASES,
    TRIAGE_CASES,
    VERDICT_CASES,
    score_compaction,
    score_extract,
    score_extract_eval,
    score_title,
    score_triage,
    score_verdict,
)
from lightweight_model_ab_transport import (
    chat_with_retry,
    eval_extract_with_retry,
    thinking_off_extra_body,
)

# --- Runner ------------------------------------------------------------------


def run_model(base_url, api_key, model, rounds, timeout, extra_body=None, dump=None,
              eval_extract_url="", eval_token="", thinking_off=True):
    # 후보별 기본 thinking-off 셰이핑: 프로덕션 CallRoleLLM처럼 모델별 off-kwargs를
    # 깔고, 운영자 --extra-body-*가 키 충돌 시 이긴다 (shapedExtra와 같은 병합 순서).
    shaped = thinking_off_extra_body(model) if thinking_off else None
    if shaped or extra_body:
        merged = dict(shaped or {})
        merged.update(extra_body or {})
        extra_body = merged or None
    print(f"AB_SHAPE model={model} thinking_off={json.dumps((shaped or {}).get('chat_template_kwargs'))}")

    per_task = {}
    latencies, out_tokens_all = [], []
    # extract 태스크의 JSON 모드 상태: ok(수용) / rejected(400 거부 — 프로덕션 파손
    # 신호) / eval(프로덕션 경로 위임). AB_METRIC/AB_VERDICT에 표면화된다.
    json_mode = "ok"
    for task_name, _role, cases, mk_prompt, scorer, task_max_tokens, response_format in TASKS:
        scores = []
        for _ in range(rounds):
            for case in cases:
                if task_name == "extract" and eval_extract_url:
                    result, ms = eval_extract_with_retry(eval_extract_url, eval_token, model, case["mail"], timeout)
                    out = json.dumps(result, ensure_ascii=False)
                    s = score_extract_eval(case, result)
                    otoks = max(1, len(out) // 3)  # 엔드포인트가 usage를 안 주므로 추정치
                    label = case["name"] + "(eval)"
                    json_mode = "eval"
                else:
                    system, user = mk_prompt(case)
                    max_tokens = case.get("max_tokens", task_max_tokens)
                    out, ms, otoks, rejected = chat_with_retry(
                        base_url, api_key, model, system, user, max_tokens, timeout, response_format, extra_body
                    )
                    s = scorer(case, out)
                    label = case["name"]
                    if rejected:
                        # H3: JSON 모드 거부는 콘텐츠가 좋아도 승격 불가 신호 — 하드캡.
                        s = min(s, JSON_REJECT_CAP)
                        json_mode = "rejected"
                        label += "(json-rejected)"
                scores.append(s)
                latencies.append(ms)
                out_tokens_all.append(otoks)
                print(f"  {model:<24} {label:<26} score={s:5.1f} latency={ms}ms out_tokens={otoks}")
                if dump is not None:
                    # 판정 근거를 눈으로 확인하는 정성 리뷰용 — 점수는 규칙 통과 여부만
                    # 말해주고, 문장 품질·누락 사실은 원문을 읽어야 보인다.
                    dump.append({"model": model, "case": label, "score": s, "latency_ms": ms, "output": out})
        per_task[task_name] = sum(scores) / len(scores)
    total = sum(per_task.values()) / len(per_task)
    return {
        "model": model,
        "total": total,
        "per_task": per_task,
        "json_mode": json_mode,
        "avg_latency_ms": int(sum(latencies) / len(latencies)),
        "avg_out_tokens": int(sum(out_tokens_all) / len(out_tokens_all)),
    }


def _winner(results, score_of):
    a, b = results
    sa, sb = score_of(a), score_of(b)
    margin = abs(sa - sb)
    winner = "tie" if margin < 3.0 else (a["model"] if sa > sb else b["model"])
    return winner, margin


def print_report(results):
    print()
    header = f"{'task':<12}" + "".join(f"{r['model']:>26}" for r in results)
    print(header)
    for task_name, _role, _c, _p, _s, _m, _f in TASKS:
        print(f"{task_name:<12}" + "".join(f"{r['per_task'][task_name]:>26.1f}" for r in results))
    print(f"{'TOTAL':<12}" + "".join(f"{r['total']:>26.1f}" for r in results))
    print(f"{'avg ms':<12}" + "".join(f"{r['avg_latency_ms']:>26}" for r in results))
    print(f"{'avg tokens':<12}" + "".join(f"{r['avg_out_tokens']:>26}" for r in results))
    print()
    for r in results:
        tasks = " ".join(f"{k}={v:.1f}" for k, v in r["per_task"].items())
        print(
            f"AB_METRIC model={r['model']} total={r['total']:.1f} {tasks} json_mode={r['json_mode']} "
            f"avg_latency_ms={r['avg_latency_ms']} avg_out_tokens={r['avg_out_tokens']}"
        )
    # 역할별 서브버딕트: 두 역할은 다른 임무(model-roles.md)라 평균 하나로 합치면
    # 한쪽 역할에서 프로덕션을 깨는 후보가 다른 쪽 점수로 가려진다.
    role_tasks = {}
    for task_name, role, _c, _p, _s, _m, _f in TASKS:
        role_tasks.setdefault(role, []).append(task_name)
    winners = {}
    for role in ("tiny", "lightweight"):
        names = role_tasks[role]
        w, margin = _winner(results, lambda r, names=names: sum(r["per_task"][n] for n in names) / len(names))
        winners[role] = w
        print(f"AB_VERDICT_{role.upper()} winner={w} margin={margin:.1f} tasks={'+'.join(names)}")
    w, margin = _winner(results, lambda r: r["total"])
    winners["overall"] = w
    rejected = ",".join(r["model"] for r in results if r["json_mode"] == "rejected")
    suffix = f" json_mode_rejected={rejected}" if rejected else ""
    print(f"AB_VERDICT winner={w} margin={margin:.1f}{suffix}")
    return winners


# --- Mock self-test ----------------------------------------------------------
# Two fake models: "mock-good" answers every task compliantly; "mock-verbose"
# behaves like an over-planning agent model (long prose, fenced JSON, chatty
# verdicts). The harness must score good > verbose — proving the scoring can
# actually tell the two behaviors apart before anyone trusts it on real models.
# A third fake, "mock-json-reject", answers like mock-good but 400-rejects
# response_format — proving the H3 cap keeps a JSON-mode-rejecting endpoint
# from riding good content to a pass.

MOCK_GOOD = {
    # 케이스별로 정확한(그 케이스의 사실만 담은) 모범 답 — 교차 오염/과잉 추출 벌점이
    # 모범생에게는 발동하지 않고 위반자만 잡는다는 것까지 셀프테스트가 검증한다.
    # 컴팩션은 프로덕션 4-섹션 스켈레톤 그대로.
    "compaction-deal": (
        "### 핵심 사실 (Facts)\n"
        "- [확실] 한빛건설 케이블 단가: 미터당 12,400원 최종 합의\n"
        "- [확실] 납품: 1차분 60%는 8월 20일, 잔여분은 9월 10일까지\n"
        "- [확실] 선수금: 계약금액의 30%인 5,300만원\n"
        "- [확실] 담당: 구매팀 박정우 차장, 최종 승인 강민석 상무\n"
        "- [확실] 구리값 톤당 5% 이상 상승 시 단가 재협상 조항 발동\n\n"
        "### 열린 루프 (Open Loops)\n"
        "- [진행중] 인감증명서·사용인감계 준비 (총무팀, 내일까지)\n"
        "- [대기] 계약서 날인: 다음 주 화요일, 상대 본사 3층 회의실\n\n"
        "### 불확실한 메모 (Uncertain Notes)\n"
        "- [추정] 모기업 감사로 결재가 하루 이틀 늦어질 수 있음\n\n"
        "### 도구 결과 (Tool Outcomes)\n"
        "없음"
    ),
    "compaction-ops": (
        "### 핵심 사실 (Facts)\n"
        "- [확실] 게이트웨이: 포트 18789 정상, 평균 응답 1.8초, 오류율 0.2%\n"
        "- [확실] srv2 디스크 82% 사용 — 다음 달 초 정리 필요\n"
        "- [확실] 6월 28일 백업 1회 실패, 원인은 ssh 타임아웃 (데이터 유실 없음)\n"
        "- [확실] 조치: 재시도 간격 30초→90초, 실패 알림 Error 레벨 상향\n\n"
        "### 열린 루프 (Open Loops)\n"
        "- [대기] 7월 15일 다음 점검 — 디스크 정리 결과·재시도 성공률 확인\n\n"
        "### 불확실한 메모 (Uncertain Notes)\n"
        "없음\n\n"
        "### 도구 결과 (Tool Outcomes)\n"
        "없음"
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
    "judge-done": '{"done": true, "reason": "회의록 3건 위키 저장과 경로 확인까지 완료"}',
    "judge-continue": '{"done": false, "reason": "서류 2종이 아직 군청 회신 대기"}',
    "triage-yes": "YES",
    "triage-no": "NO",
}

MOCK_VERBOSE = {
    # 에이전트형 실패 모드 총집합: 계획 서두·산문(스켈레톤 무시)+교차 사실 혼입(compaction),
    # 코드펜스+사족+타입 위반+과잉 추출(extract), 따옴표 장문(title), 판정 사족(verdict),
    # 펜스+산문 JSON(judge), 수다 서두로 프로덕션 파서가 오판하는 트리아지(triage).
    "compaction": (
        "이 과제를 해결하기 위해 먼저 대화를 단계별로 분석하겠습니다. 1단계: 주요 주제 식별. 2단계: 세부 사실 정리. "
        "회의에서는 여러 주제가 논의되었습니다. 단가와 납기에 대한 논의가 있었고 담당자 관련 사항도 언급되었습니다. "
        "참고로 지난 점검에서 srv2 디스크와 18789 포트 상태도 함께 확인된 바 있습니다. "
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
    "judge-done": (
        "판단 과정을 먼저 설명드리겠습니다. 응답에 저장 완료가 명시되어 있습니다.\n"
        '```json\n{"done": true, "reason": "저장 완료가 확인됨"}\n```\n추가 검증이 필요하시면 말씀해 주세요.'
    ),
    "judge-continue": (
        "아직 서류가 남아 있는 상태로 보입니다.\n"
        '```json\n{"done": false, "reason": "서류 2종 미확보"}\n```'
    ),
    "triage-yes": "업무상 중요한 연락으로 판단되어 YES입니다",
    "triage-no": "광고성 프로모션 알림이므로 알릴 필요가 없습니다. 답: NO",
}


def _mock_reply(model: str, system: str, user: str) -> str:
    good = model in ("mock-good", "mock-json-reject")
    if "핵심 사실" in system:  # 컴팩션 스켈레톤 시스템 프롬프트
        if good:
            return MOCK_GOOD["compaction-deal"] if "한빛건설" in user else MOCK_GOOD["compaction-ops"]
        return MOCK_VERBOSE["compaction"]
    if "JSON만 출력" in system:
        if good:
            return MOCK_GOOD["extract-quote"] if "영덕" in user else MOCK_GOOD["extract-payment"]
        return MOCK_VERBOSE["extract"]
    if "제목" in system:
        if good:
            return MOCK_GOOD["title-deal"] if "케이블" in user else MOCK_GOOD["title-ops"]
        return MOCK_VERBOSE["title"]
    if "strict judge" in system:
        key = "judge-done" if "위키에 저장" in user else "judge-continue"
        return MOCK_GOOD[key] if good else MOCK_VERBOSE[key]
    if "알림 분류기" in system:
        key = "triage-yes" if "합의서" in user else "triage-no"
        return MOCK_GOOD[key] if good else MOCK_VERBOSE[key]
    if "DONE" in system:
        key = "verdict-done" if "발송 완료" in user else "verdict-continue"
        return MOCK_GOOD[key] if good else MOCK_VERBOSE[key]
    return "?"


class _MockHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802 — http.server API
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        # H3 재현: mock-json-reject는 response_format을 400으로 거부한다
        # (guided-decoding 미지원 서버) — 폴백+하드캡 경로의 e2e 검증용.
        if body.get("response_format") and body.get("model") == "mock-json-reject":
            err = json.dumps({"error": "response_format is not supported"}).encode("utf-8")
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(err)))
            self.end_headers()
            self.wfile.write(err)
            return
        msgs = body["messages"]
        system = next((m["content"] for m in msgs if m.get("role") == "system"), "")
        user = next((m["content"] for m in msgs if m.get("role") == "user"), "")
        content = _mock_reply(body["model"], system, user)
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


def _selftest_scoring_edges() -> bool:
    """서버 없이 스코어러의 신설 캡·규칙을 직접 검증한다 — 채점기가 조용히
    풀리는 회귀를 mock 비교보다 먼저 잡는 정밀 프로브."""
    checks = []
    # M32: 사실 0건 보존 요약은 형식·한국어 점수로 연명 불가 — 0점.
    zero_fact = (
        "### 핵심 사실 (Facts)\n- [확실] 회의가 있었다\n\n### 열린 루프 (Open Loops)\n없음\n\n"
        "### 불확실한 메모 (Uncertain Notes)\n없음\n\n### 도구 결과 (Tool Outcomes)\n없음"
    )
    checks.append(("zero-fact summary scores 0", score_compaction(COMPACTION_CASES[0], zero_fact) == 0.0))
    # M34: 스켈레톤 미준수(산문)는 같은 사실 보존이어도 스켈레톤 준수보다 낮다.
    prose = "한빛건설 단가 12,400원 합의, 납품 8월 20일과 9월 10일, 선수금 5,300만원, 담당 박정우, 승인 강민석, 재협상 조항 있음."
    skel = MOCK_GOOD["compaction-deal"]
    checks.append(("skeleton beats same-facts prose", score_compaction(COMPACTION_CASES[0], skel) > score_compaction(COMPACTION_CASES[0], prose)))
    # M37: 전부 빈 배열의 well-formed JSON은 파싱+스키마 점수로 연명 불가 (≤10).
    empty = '{"보낸사람": "", "의도": "", "금액": [], "기한": [], "요청사항": []}'
    checks.append(("all-empty extract capped <=10", score_extract(EXTRACT_CASES[0], empty) <= 10.0))
    # M35: 값이 엉뚱한 키에 있으면(금액↔기한 스왑) 진실 매칭이 깎인다.
    swapped = '{"보낸사람": "김서연", "의도": "견적 요청", "금액": ["7월 11일"], "기한": ["9억 2천만원"], "요청사항": []}'
    straight = '{"보낸사람": "김서연", "의도": "견적 요청", "금액": ["9억 2천만원"], "기한": ["7월 11일"], "요청사항": []}'
    checks.append(("swapped 금액/기한 scores below straight", score_extract(EXTRACT_CASES[0], swapped) < score_extract(EXTRACT_CASES[0], straight)))
    # LOW: 부정된 판정("아직 DONE이 아닙니다"/"NOT DONE")은 부분 점수 없이 0.
    done_case = VERDICT_CASES[0]
    checks.append(("negated DONE scores 0", score_verdict(done_case, "아직 DONE이 아닙니다") == 0.0))
    checks.append(("NOT DONE scores 0", score_verdict(done_case, "NOT DONE") == 0.0))
    checks.append(("affirmative wordy DONE keeps partial 40", score_verdict(done_case, "목표가 달성되어 DONE입니다") == 40.0))
    # LOW: 제목 중간 마침표도 감점, 숫자 사이 소수점은 예외.
    mid = score_title(TITLE_CASES[0], "한빛. 케이블 단가")
    dec = score_title(TITLE_CASES[0], "케이블 12.4원 단가")
    clean = score_title(TITLE_CASES[0], "한빛 케이블 단가")
    checks.append(("mid-title period penalized", mid < clean))
    checks.append(("decimal point not penalized", dec == clean))
    # M30: 프로덕션 judge 계약 — 단일 라인 bare JSON 100, done 오답/비JSON 0.
    jd = next(c for c in VERDICT_CASES if c["name"] == "judge-done")
    checks.append(("bare single-line judge JSON = 100", score_verdict(jd, '{"done": true, "reason": "완료 확인"}') == 100.0))
    checks.append(("wrong done field = 0", score_verdict(jd, '{"done": false, "reason": "완료 확인"}') == 0.0))
    checks.append(("non-JSON judge output = 0", score_verdict(jd, "DONE") == 0.0))
    # 트리아지: 프로덕션 파서 미러 — NO로 시작하지 않는 수다는 YES로 오판된다.
    tn = next(c for c in TRIAGE_CASES if c["answer"] == "NO")
    ty = next(c for c in TRIAGE_CASES if c["answer"] == "YES")
    checks.append(("triage exact YES = 100", score_triage(ty, "YES") == 100.0))
    checks.append(("triage chatty wrong-YES on NO case = 0", score_triage(tn, "중요해 보이므로 알림 대상입니다") == 0.0))
    ok = True
    for name, passed in checks:
        print(f"SELFTEST {'ok' if passed else 'FAIL'} — {name}")
        ok &= passed
    return ok


def run_mock() -> int:
    edges_ok = _selftest_scoring_edges()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _MockHandler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{server.server_address[1]}/v1"
    try:
        results = [run_model(base, "mock", m, 1, 30) for m in ("mock-good", "mock-verbose")]
        winners = print_report(results)
        good, verbose = results
        # H3 e2e: JSON 모드 거부 엔드포인트는 좋은 폴백 콘텐츠로도 extract 캡을 못 넘는다.
        rej = run_model(base, "mock", "mock-json-reject", 1, 30)
        reject_ok = rej["json_mode"] == "rejected" and rej["per_task"]["extract"] <= JSON_REJECT_CAP
        print(
            f"MOCK_JSONREJECT {'ok' if reject_ok else 'FAIL'} — extract={rej['per_task']['extract']:.1f} "
            f"(cap {JSON_REJECT_CAP:.0f}) json_mode={rej['json_mode']}"
        )
        ok = (
            edges_ok
            and reject_ok
            and winners["overall"] == "mock-good"
            and winners["tiny"] == "mock-good"
            and winners["lightweight"] == "mock-good"
            and good["total"] >= 85
            and verbose["total"] <= 60
        )
        print(f"MOCK_SELFTEST {'PASS' if ok else 'FAIL'} good={good['total']:.1f} verbose={verbose['total']:.1f}")
        return 0 if ok else 1
    finally:
        server.shutdown()
