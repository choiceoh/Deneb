"""Fixed task corpus, prompts, and deterministic scorers for lightweight-model-ab."""

import json
import re

# --- Task battery (fixed corpus, planted ground truth) ---------------------

# Each fact is an any-of variant list: retention counts if ANY variant appears
# (models legitimately normalize "5,300만원" → "5300만원").
COMPACTION_CASES = [
    {
        "name": "compaction-deal",
        # 실전 컴팩션 청크에 가까운 길이(~1.3K자) — 짧은 입력이면 하드캡(ratio 초과)이
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
        "foreign": [["18789"], ["srv2"], ["ssh 타임아웃", "ssh타임아웃"], ["7월 15일"]],
    },
    {
        "name": "compaction-ops",
        "dialogue": (
            "사용자: 서버 점검 결과 정리해줘.\n"
            "비서: 게이트웨이는 포트 18789에서 정상이고, 평균 응답은 1.8초입니다. 최근 일주일 오류율은 0.2%로 "
            "평시 범위이고, 피크는 아침 브리핑 생성 시간대에 몰립니다. "
            "디스크는 srv2 노드가 82% 찼고 30일 보존 정책 기준으로 다음 달 초 정리가 필요합니다. "
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
        # 키별 진실값: 값이 "맞는 키"에 있어야 득점한다 (금액↔기한 스왑 차단).
        # 보낸사람/의도에도 진실값을 심어 타입만 맞는 아무 값이나 통과하지 못하게 한다.
        "truth": {
            "보낸사람": [["김서연"]],
            "의도": [["견적"]],
            "금액": [["9억 2천만원", "9억2천만원", "920,000,000", "9.2억"]],
            "기한": [["7월 11일", "7/11"]],
            "요청사항": [["견적"], ["KS 인증서", "KS인증서"], ["납기 계획서", "납기계획서"]],
        },
        # Values from the OTHER mail — presence means over-extraction (each hit
        # deducts; a model that dumps every value it has ever seen must not pass).
        "foreign": [["1억 4,500만원", "1억4500만원", "1억 4500만원"], ["8월 5일"], ["세금계산서"], ["통장 사본", "통장사본"]],
        # --eval-extract-url 모드용: 프로덕션 deal 추출 결과(DealInfo)에 대한 진실값.
        "deal_truth": {
            "counterparty": ["대한이엔씨", "대한 이엔씨", "daehanenc"],
            "amount": ["9억 2천만원", "9억2천만원", "920,000,000", "9.2억"],
            "due": ["7월 11일", "7/11", "07-11", "7-11"],
            "req": ["견적", "KS", "납기"],
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
            "보낸사람": [["정해준"]],
            "의도": [["잔금", "입금"]],
            "금액": [["1억 4,500만원", "1억4500만원", "1억 4500만원", "145,000,000"]],
            "기한": [["8월 5일", "8/5"]],
            "요청사항": [["세금계산서"], ["통장 사본", "통장사본"]],
        },
        "foreign": [["9억 2천만원", "9억2천만원"], ["7월 11일"], ["KS 인증서", "KS인증서"], ["납기 계획서", "납기계획서"]],
        "deal_truth": {
            "counterparty": ["선밸리", "sunvalley"],
            "amount": ["1억 4,500만원", "1억4500만원", "1억 4500만원", "145,000,000", "1.45억"],
            "due": ["8월 5일", "8/5", "08-05", "8-5"],
            "req": ["세금계산서", "통장"],
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

# verdict 태스크는 두 계약을 함께 잰다:
#   kind="word" — DONE/CONTINUE 한 단어 (장황함·부정 프로브; 바운드 판정의 최소형)
#   kind="json" — 프로덕션 goal judge 계약 (goal_task.go): goalJudgeSystem 원문 +
#                 {"done":bool,"reason":str} 단일 라인 JSON, parseJudgeVerdict 미러 채점.
VERDICT_CASES = [
    {
        "name": "verdict-done",
        "kind": "word",
        "context": (
            "목표: 주간보고 PDF를 생성해 메일로 발송한다.\n"
            "진행 기록: PDF 생성 완료(4페이지). 수신자 3명에게 발송 완료, 반송 없음. 발송 로그 확인됨."
        ),
        "answer": "DONE",
    },
    {
        "name": "verdict-continue",
        "kind": "word",
        "context": (
            "목표: 영덕 부지 인허가 서류 5종을 모두 수집한다.\n"
            "진행 기록: 3종 확보(개발행위허가·환경검토·소유권 등본). 나머지 2종은 군청 회신 대기 중."
        ),
        "answer": "CONTINUE",
    },
    {
        "name": "judge-done",
        "kind": "json",
        "goal": "이번 주 미팅 3건의 회의록을 모두 위키에 저장한다.",
        "agent_answer": (
            "회의록 3건(한빛건설·선밸리·내부 주간회의)을 각각 위키에 저장했습니다. "
            "저장 경로 확인까지 완료했습니다."
        ),
        "done": True,
        "max_tokens": 512,  # 프로덕션 judge와 동일 (goal_task.go CallLocalLLM 512)
    },
    {
        "name": "judge-continue",
        "kind": "json",
        "goal": "영덕 부지 인허가 서류 5종을 모두 수집한다.",
        "agent_answer": "현재 3종을 확보했습니다. 나머지 2종은 군청 회신을 기다리는 중입니다.",
        "done": False,
        "max_tokens": 512,
    },
]

# 알림 트리아지 (tiny 실경로): 오답 NO는 풀 판정 자체를 억제한다 — 프로덕션에서
# 신호가 조용히 사라지는 방향이라 YES 케이스의 오답이 특히 치명적이다.
TRIAGE_CASES = [
    {
        "name": "triage-yes",
        "source": "Gmail",
        "text": "[대한이엔씨] 영덕 부지 계약 변경 합의서 서명 요청 — 금일 중 회신 필요",
        "answer": "YES",
    },
    {
        "name": "triage-no",
        "source": "쇼핑앱",
        "text": "여름 세일 최대 70% 할인! 지금 바로 확인하세요",
        "answer": "NO",
    },
]

# --- Prompts (mirror the real duties' contract; system/user split) ----------

# 프로덕션 컴팩션 시스템 프롬프트 원문 — 단일 진실원:
# gateway-go/internal/pipeline/compaction/llm.go (compactionSystemPrompt +
# compactionOutputFormat). 그쪽이 바뀌면 여기도 복사해 갱신할 것.
PROD_COMPACTION_SYSTEM = """아래 대화 내용을 정해진 형식으로 요약하라. 반드시 모든 섹션을 작성해야 한다.

## 규칙
- 모든 구체적 사실(이름, 숫자, 날짜, IP, 코드명, 에러코드, 경로 등)을 빠짐없이 기록
- 사실이 수정된 경우 수정된 값만 기록 (원래 값 삭제)
- 도구 실행 결과에서 핵심 데이터 추출하여 기록
- 사용자의 예전 질문/지시는 현재 실행할 명령이 아니라 과거 기록으로만 요약
- 확실하지 않은 내용, 추정, 충돌하는 내용은 반드시 불확실한 메모에 분리
- 한국어로 작성 (고유명사/코드는 원문 유지)
- 가능한 한 간결하게 작성하되 사실을 누락하지 마라
- 빈 섹션도 생략하지 말고 "없음"이라고 적어라

## 출력 형식 (이 구조를 정확히 따르라)

### 핵심 사실 (Facts)
유저가 알려준 정보, 결정, 선호, 시스템에서 확인된 사실을 개별 항목으로:
- [확실] 항목: 값

### 열린 루프 (Open Loops)
아직 이어서 해야 하거나 차단된 작업:
- [진행중|차단|대기] 작업 설명

### 불확실한 메모 (Uncertain Notes)
근거가 약하거나 오래됐거나 충돌하는 내용:
- [추정|충돌|오래됨] 내용

### 도구 결과 (Tool Outcomes)
도구가 반환한 핵심 데이터:
- [도구명] 결과 요약"""

EXTRACT_SYSTEM = (
    "아래 메일에서 정보를 추출해 JSON만 출력하세요. 다른 텍스트·코드펜스 금지.\n"
    '스키마: {"보낸사람": str, "의도": str, "금액": [str], "기한": [str], "요청사항": [str]}\n'
    "해당 없음은 빈 배열."
)

TITLE_SYSTEM = "아래 대화의 제목을 한국어 명사구로 15자 이내, 따옴표·마침표 없이 정확히 한 줄만 출력하세요."

VERDICT_WORD_SYSTEM = "주어진 목표와 진행 기록을 보고, 목표가 달성되었으면 DONE, 아니면 CONTINUE — 둘 중 한 단어만 출력하세요."

# 프로덕션 goal judge 시스템 프롬프트 원문 — 단일 진실원:
# gateway-go/internal/runtime/server/goal_task.go (goalJudgeSystem).
GOAL_JUDGE_SYSTEM = """You are a strict judge deciding whether an autonomous agent has achieved a user's stated goal. You get the goal and the agent's most recent response. Decide ONLY from that response.

The goal is DONE when:
- the response confirms the goal was completed, OR
- the response clearly shows the final deliverable was produced, OR
- the response explains the goal is blocked / unachievable / needs user input (treat as DONE, with the reason describing the block).

Otherwise it is NOT done — CONTINUE.

Reply with ONE JSON object on a single line and nothing else:
{"done": <true|false>, "reason": "<one short sentence>"}"""

# 프로덕션 알림 트리아지 시스템 프롬프트 원문 — 단일 진실원:
# gateway-go/internal/runtime/server/server_http_event_ingest.go (worthFullJudgment).
TRIAGE_SYSTEM = (
    "당신은 스마트폰 알림 분류기다. 사용자에게 즉시 알릴 가치가 있는 업무·일정·금전·중요 연락이면 YES, "
    "광고·프로모션·스팸·인증번호(OTP)·결제 영수증·배송/마케팅 알림·일상적 시스템/앱 알림이면 NO. YES 또는 NO 한 단어만 답하라."
)


def compaction_prompt(case):
    return PROD_COMPACTION_SYSTEM, case["dialogue"]


def extract_prompt(case):
    return EXTRACT_SYSTEM, case["mail"]


def title_prompt(case):
    return TITLE_SYSTEM, case["snippet"]


def verdict_prompt(case):
    if case.get("kind") == "json":
        # 프로덕션 goalJudgeUserTmpl 미러 (goal_task.go).
        user = (
            "Goal:\n" + case["goal"] + "\n\nAgent's most recent response:\n"
            + case["agent_answer"] + "\n\nIs the goal satisfied?"
        )
        return GOAL_JUDGE_SYSTEM, user
    return VERDICT_WORD_SYSTEM, case["context"]


def triage_prompt(case):
    # 프로덕션 worthFullJudgment의 user 조립 미러.
    return TRIAGE_SYSTEM, "앱: " + case["source"] + "\n알림 내용:\n" + case["text"]


# --- Deterministic scoring (0-100 per task) --------------------------------


def _any_variant(text: str, variants) -> bool:
    squashed = re.sub(r"\s+", " ", text)
    return any(v in squashed for v in variants)


def hangul_ratio(text: str) -> float:
    letters = [c for c in text if c.isalpha()]
    if not letters:
        return 0.0
    return sum(1 for c in letters if "가" <= c <= "힣") / len(letters)


# 프로덕션 스켈레톤의 4개 섹션 헤더 (compaction/llm.go compactionOutputFormat).
COMPACTION_SECTIONS = ["핵심 사실", "열린 루프", "불확실한 메모", "도구 결과"]


def score_compaction(case, out: str) -> float:
    facts = case["facts"]
    kept = sum(1 for f in facts if _any_variant(out, f))
    # 사실 0건 보존은 무조건 0점 — 형식·간결·한국어 점수로 "쓸모없는 요약"이
    # 40점대에 연명하던 헛점 봉쇄 (요약의 존재 이유가 사실 보존이다).
    if kept == 0:
        return 0.0
    fact_score = kept / len(facts)
    # 프로덕션 4-섹션 스켈레톤 준수 (섹션 헤더 존재 비율) — 산문만 내는 모델은
    # 프로덕션 컴팩션 소비자(요약 fence 구조)와 계약이 안 맞는다.
    skeleton = sum(1 for s in COMPACTION_SECTIONS if s in out) / len(COMPACTION_SECTIONS)
    ratio = len(out) / max(1, len(case["dialogue"]))
    # 스켈레톤은 산문 5문장보다 길어질 수밖에 없어 관용 한도를 0.6으로 둔다.
    brevity = 1.0 if ratio <= 0.6 else max(0.0, (1.0 - ratio) / 0.4)
    korean = 1.0 if hangul_ratio(out) >= 0.3 else 0.0
    score = 100 * (0.5 * fact_score + 0.25 * skeleton + 0.15 * brevity + 0.10 * korean)
    # 압축 실패는 하드 캡: 원문의 85%를 넘겨 출력하면 사실을 다 담아도 "요약"이 아니다
    # (통짜 에코가 부분 점수로 살아남던 헛점 봉쇄).
    if ratio > 0.85:
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
    # ground-truth hits: 40 — 키별 매칭. 값이 "맞는 키"에 있어야 득점한다
    # (금액↔기한 스왑처럼 전체 blob 검색이면 만점이던 오배치 차단).
    hit, checks = 0, 0
    for key, groups in case["truth"].items():
        field_blob = json.dumps(obj.get(key), ensure_ascii=False)
        for variants in groups:
            checks += 1
            if _any_variant(field_blob, variants):
                hit += 1
    score += 40.0 * (hit / max(1, checks))
    # 진실값 0건 적중이면 파싱+스키마 점수로 연명 불가 (≤10) — 전부 빈 배열의
    # well-formed 객체가 60점을 받아 총점을 패딩하던 헛점 봉쇄.
    if hit == 0:
        score = min(score, 10.0)
    # 과잉 추출 벌점: 이 메일에 없는(다른 메일의) 값이 섞여 나오면 건당 -15 —
    # 정답을 다 맞혀도 오염된 필드는 승격 불가.
    blob = json.dumps(obj, ensure_ascii=False)
    foreign = sum(1 for variants in case.get("foreign", []) if _any_variant(blob, variants))
    return max(0.0, score - 15.0 * foreign)


def score_extract_eval(case, result) -> float:
    """--eval-extract-url 모드: 프로덕션 추출 경로의 '소비 결과'(DealInfo) 채점.

    DealInfo는 json 태그가 없어 Go 필드명 그대로 마샬된다 (Counterparty/DocType/
    Amount/Date/DueDate/Items/Summary — mailanalysis/pipeline_extractors.go).
    None = "not a deal" 판정 — 이 코퍼스는 둘 다 거래 메일이라 실패다.
    """
    if not isinstance(result, dict):
        return 0.0
    dt = case["deal_truth"]

    def field(*names):
        return json.dumps([result.get(n) for n in names], ensure_ascii=False)

    score = 0.0
    if _any_variant(field("Counterparty"), dt["counterparty"]):
        score += 35.0
    if _any_variant(field("Amount"), dt["amount"]):
        score += 25.0
    if _any_variant(field("DueDate", "Date"), dt["due"]):
        score += 20.0
    if _any_variant(field("Items", "Summary"), dt["req"]):
        score += 10.0
    if isinstance(result.get("DocType"), str) and result["DocType"].strip():
        score += 10.0
    blob = json.dumps(result, ensure_ascii=False)
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
    # 금지 문자는 위치 불문 — 마침표류도 제목 중간이면 실격 ("백업 점검. 조치 필요"류).
    # 예외: 숫자 사이의 소수점(12.4원)은 문장부호가 아니다.
    t_nodec = re.sub(r"(?<=\d)\.(?=\d)", "", t)
    if t and not re.search(r'["\'`「」『』#*]', t) and not re.search(r"[.!?…]", t_nodec):
        score += 20
    if hangul_ratio(t) >= 0.3:
        score += 15
    # 관련성: 주제를 지칭해야 제목이다 — "업무 정리" 같은 만능 제목 차단.
    if any(k in t for k in case.get("keywords", [])):
        score += 25
    return score


# 정답 토큰 주변의 부정 표지 — "아직 DONE이 아닙니다"가 done 케이스에서 부분
# 점수를 받으면 판정이 정반대로 소비된다.
NEGATION_RE = re.compile(r"아니|않|아직|안 |못 |not\b", re.IGNORECASE)


def _negated_near(text: str, token: str, window: int = 8) -> bool:
    for m in re.finditer(re.escape(token), text):
        before = text[max(0, m.start() - window):m.start()]
        after = text[m.end():m.end() + window]
        if NEGATION_RE.search(before) or NEGATION_RE.search(after):
            return True
    return False


def score_verdict_word(case, out: str) -> float:
    t = out.strip().strip(".")
    if t == case["answer"]:
        return 100.0
    # 정답을 포함하되 말이 붙은 경우 — 바운드 판정 소비자는 한 단어를 기대한다.
    # 단, 정답 토큰이 부정된 채 포함되면("NOT DONE"/"DONE이 아닙니다") 의미가
    # 정반대이므로 부분 점수 없이 0.
    if case["answer"] in t and ("DONE" in t) != ("CONTINUE" in t):
        if _negated_near(t, case["answer"]):
            return 0.0
        return 40.0
    return 0.0


def _parse_judge_verdict(out: str):
    """goal_task.go parseJudgeVerdict 미러: 첫 '{'~마지막 '}'를 추출(펜스·산문 관용),
    done은 bool 또는 "true"/"false" 문자열을 허용. (done, reason, ok) 반환."""
    s = out.strip()
    i, j = s.find("{"), s.rfind("}")
    inner = s[i:j + 1] if (i >= 0 and j > i) else s
    try:
        v = json.loads(inner)
    except (json.JSONDecodeError, ValueError):
        return False, "", False
    if not isinstance(v, dict):
        return False, "", False
    d = v.get("done")
    reason = v.get("reason") if isinstance(v.get("reason"), str) else ""
    if isinstance(d, bool):
        return d, reason, True
    if isinstance(d, str):
        return d.strip().lower() == "true", reason, True
    return False, "", False


def score_verdict_json(case, out: str) -> float:
    """프로덕션 goal judge 계약 채점: done 필드 정오가 전부의 기반 — 오답/해석 불가는
    0 (프로덕션에서 오답 done은 목표 조기 종료/무한 계속, 해석 불가는 parse-failure
    카운트로 이어진다). 형식 보너스는 지시("단일 라인, JSON만") 준수를 잰다."""
    done, reason, ok = _parse_judge_verdict(out)
    if not ok or done != case["done"]:
        return 0.0
    score = 60.0
    if reason.strip():
        score += 15.0
    s = out.strip()
    if s.startswith("{") and s.endswith("}"):
        score += 15.0  # bare — JSON 외 산문·펜스 없음
    if "\n" not in s:
        score += 10.0  # 단일 라인
    return score


def score_verdict(case, out: str) -> float:
    if case.get("kind") == "json":
        return score_verdict_json(case, out)
    return score_verdict_word(case, out)


def score_triage(case, out: str) -> float:
    # 프로덕션 파서 미러 (worthFullJudgment): 대문자화 후 "NO"로 시작하면 NO,
    # 그 외 전부 YES. 수다형 서두는 곧 YES 오판 — 그대로 채점된다.
    t = out.strip()
    parsed = "NO" if t.upper().startswith("NO") else "YES"
    if parsed != case["answer"]:
        return 0.0
    return 100.0 if t.upper().rstrip(".") == case["answer"] else 70.0


# (name, role, cases, prompt-builder, scorer, max_tokens, response_format)
# role은 model-roles.md의 임무 배치: 승격 판단은 역할 단위(sub-verdict)로 한다.
# extract는 프로덕션 gmail stage1과 같은 JSON 모드로 호출해 guided-decoding 호환까지
# 함께 측정한다 (400 거부 시 형식 없이 1회 폴백하되 케이스 점수 40 하드캡 — 프로덕션
# callLocalLLMJSON은 formatless 폴백이 없어 거부 엔드포인트는 프로덕션을 깬다).
# 케이스별 "max_tokens" 키가 태스크 기본값을 덮는다 (judge JSON 케이스 = 512).
TASKS = [
    # 1024 = 프로덕션 컴팩션의 per-chunk 출력 캡과 동일 (사고형 모델도 완주 기회를 갖되,
    # 장황함은 brevity 점수·avg_out_tokens·레이턴시로 정직하게 드러난다).
    ("compaction", "lightweight", COMPACTION_CASES, compaction_prompt, score_compaction, 1024, None),
    ("extract", "tiny", EXTRACT_CASES, extract_prompt, score_extract, 300, {"type": "json_object"}),
    ("title", "tiny", TITLE_CASES, title_prompt, score_title, 50, None),
    ("verdict", "lightweight", VERDICT_CASES, verdict_prompt, score_verdict, 20, None),
    # 4 = 프로덕션 worthFullJudgment의 max_tokens 그대로 — 수다·사고가 잘리는 것까지
    # 프로덕션 조건이다.
    ("triage", "tiny", TRIAGE_CASES, triage_prompt, score_triage, 4, None),
]

# H3: JSON 모드 거부 시 해당 케이스 점수 하드캡.
JSON_REJECT_CAP = 40.0
