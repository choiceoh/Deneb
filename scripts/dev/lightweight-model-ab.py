#!/usr/bin/env python3
"""
lightweight-model-ab.py — A/B battery for the *lightweight/tiny* model roles.

Compares two candidate models on Deneb's actual local-workhorse duties — the
text-only chores the lightweight/tiny roles perform (no tool calling). Each
task mirrors its production counterpart's CONTRACT (prompt shape, output
format, parser semantics), so a battery pass predicts production behavior:

  compaction  [lightweight] 한국어 대화 → 프로덕션 4-섹션 스켈레톤 요약.
              시스템 프롬프트는 compaction/llm.go의 compactionSystemPrompt 원문이며,
              섹션 준수(4개 헤더)까지 채점한다 — 산문 요약은 프로덕션 소비자가
              기대하는 구조가 아니다.
  extract     [tiny] 한국어 업무 메일 → 고정 스키마 JSON (gmail stage1과 동형).
              json_object 모드 강제: 프로덕션 callLocalLLMJSON(mailanalysis/pipeline.go)은
              formatless 폴백이 없으므로, JSON 모드를 400으로 거부하는 엔드포인트는
              폴백으로 채점하되 케이스 점수 40 하드캡 + `json_mode=rejected` 표기
              (콘텐츠가 좋아도 프로덕션을 깨는 후보를 통과시키지 않는다).
              ※ 오프라인 토이 스키마(보낸사람/의도/금액/기한/요청사항)는 프로덕션
              deal 스키마(isDeal/counterparty/docType/amount/date/dueDate/items/summary,
              mailanalysis/pipeline_extractors.go)와 다르다 — 충실도가 필요하면
              --eval-extract-url로 프로덕션 추출 경로를 태워라 (아래).
  title       [tiny] 대화 스니펫 → 짧은 한국어 명사구 제목 (세션 자동 제목과 동형)
  verdict     [lightweight] ① DONE/CONTINUE 한 단어 (장황함·부정 프로브)
              ② 프로덕션 goal judge 계약: goalJudgeSystem 원문 프롬프트에
              {"done":bool,"reason":str} 단일 라인 JSON을 요구하고,
              goal_task.go parseJudgeVerdict 미러로 done 필드 정오를 채점한다.
  triage      [tiny] 알림 YES/NO 트리아지 (server_http_event_ingest.go
              worthFullJudgment 미러 — max_tokens=4, "NO로 시작하지 않으면 전부
              YES"라는 프로덕션 파서 의미론 그대로. 수다형 tiny 모델은 여기서
              잘려 오판된다 — 그것이 프로덕션 동작이다).

Scoring is DETERMINISTIC (fact checklists, JSON parsing, length/format rules) —
no LLM judge — so the number is reproducible and argues for itself. Latency and
output-token counts ride along because verbosity is wall-clock on these paths
(compaction latency has caused real incidents).

Request shaping mirrors production text-role calls (pilot.CallRoleLLM):
  - system/user 분리 메시지 (단일 user 메시지 아님)
  - 후보별 기본 thinking-off extra body — modelrole.ThinkingOffExtraBody의 3분기
    미러 (단일 진실원: gateway-go/internal/ai/modelrole/thinking.go). 끄려면
    --no-thinking-off, 키가 겹치면 --extra-body-*가 이긴다.
Remaining deliberate gaps vs production: 배터리는 비스트리밍 HTTP(프로덕션은
Stream=true — 콘텐츠 동일, 벽시계는 어차피 전체 생성 포함), 서버측 timeout kwarg
미주입, 오프라인 extract 채점은 ```-펜스에 10점 감점(프로덕션 jsonutil은 펜스를
벗겨 소비하지만 bare JSON이 명시 계약이므로 스타일 감점 유지).

Usage (on the DGX host; both models served behind wormhole):
  python3 scripts/dev/lightweight-model-ab.py \
      --model-a qwen3.6-35b --model-b agents-a1 \
      [--base-url http://127.0.0.1:18800/v1] [--api-key-env WORMHOLE_TOKEN] [--rounds 1]

Optional production-path extract: 게이트웨이가 살아 있으면 extract 코퍼스를
실제 추출 경로(POST /api/eval/extract, kind=deal — 실프롬프트+jsonutil+후처리)로
라우팅해 "소비 결과"(DealInfo)를 채점한다:
  ... --eval-extract-url http://127.0.0.1:18789     # env DENEB_EVAL_EXTRACT_URL 자동 인식
(인증: $DENEB_CLIENT_TOKEN 또는 ~/.deneb/client_token. 미지정 시 오프라인 토이
스키마가 기본 — 인프라 없이 도는 대신 위의 스키마 갭을 감수한다.)

Self-test of the harness + scoring (no server needed):
  python3 scripts/dev/lightweight-model-ab.py --mock

Output: per-task table + one greppable line per model —
  AB_METRIC model=<name> total=<0-100> compaction=.. extract=.. title=.. verdict=.. triage=.. \
      json_mode=<ok|rejected|eval> avg_latency_ms=.. avg_out_tokens=..
역할별 서브버딕트 — 승격 판단은 역할 단위다 (model-roles.md 임무 배치:
extract·title·triage=tiny, compaction·verdict=lightweight):
  AB_VERDICT_TINY winner=<name|tie> margin=<pts>
  AB_VERDICT_LIGHTWEIGHT winner=<name|tie> margin=<pts>
and a final `AB_VERDICT winner=<name|tie> margin=<pts>[ json_mode_rejected=<models>]` line.

Role doctrine: docs/agent-rules/model-roles.md — tool-heavy roles are promoted via
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

# --- Per-candidate request shaping (mirror of modelrole.ThinkingOffExtraBody) ---


def _is_reasoning_model(model: str) -> bool:
    """modelrole.ProfileFor(profile.go)의 reasoning 분류 미러 — 항상 사고 채널을
    여는(끌 수 없는) 모델 패밀리."""
    m = model.lower()
    if "step3" in m or "step-3" in m:
        return True
    if ("qwen3" in m or "qwen36" in m or "qwen35" in m) and "instruct" not in m:
        return True
    return any(k in m for k in ("qwq", "deepseek-r1", "deepseek-reasoner", "gpt-oss"))


def thinking_off_extra_body(model: str):
    """프로덕션 텍스트 콜(pilot.CallRoleLLM)이 자동 적용하는 thinking-off 셰이핑의
    Python 미러. 단일 진실원: gateway-go/internal/ai/modelrole/thinking.go
    (ThinkingOffExtraBody) — 그쪽 분기가 바뀌면 여기도 갱신할 것. 3분기:

      1) dual-mode deepseek-v4 → chat_template_kwargs.thinking=false (템플릿 토글 철자)
      2) 끌 수 없는 사고형(step3/qwq/r1/비-instruct qwen3 등) → None
         (enable_thinking 부착은 thinking-only 템플릿에서 400 위험)
      3) 그 외 비사고형 → chat_template_kwargs.enable_thinking=false (Qwen 계열 철자)

    프로바이더 게이트(modelcaps.ServesVllmBacked)는 생략: 이 배터리의 문서화된
    대상이 wormhole/raw vLLM이라 항상 vLLM-backed로 간주한다. 클라우드 후보처럼
    chat_template_kwargs를 거부하는 엔드포인트는 --no-thinking-off로 끈다.
    """
    m = model.lower()
    if "deepseek-v4" in m or "deepseek_v4" in m:
        return {"chat_template_kwargs": {"thinking": False}}
    if _is_reasoning_model(m):
        return None
    return {"chat_template_kwargs": {"enable_thinking": False}}


# --- OpenAI-compatible client (stdlib only) ---------------------------------


def chat_once(base_url, api_key, model, system, user, max_tokens, timeout, response_format=None, extra_body=None):
    payload_body = {
        "model": model,
        # 프로덕션 텍스트 콜(CallRoleLLM/callLocalLLMJSON)과 동일한 system/user 분리 —
        # 단일 user 메시지로 뭉치면 후보가 프로덕션과 다른 프롬프트 형상으로 측정된다.
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    if response_format:
        payload_body["response_format"] = response_format
    if extra_body:
        # 후보별 thinking-off 셰이핑 + 운영자 --extra-body-* 병합 결과.
        payload_body.update(extra_body)
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


def chat_with_retry(base_url, api_key, model, system, user, max_tokens, timeout, response_format=None, extra_body=None):
    # 레이턴시는 재시도·백오프를 포함한 벽시계 전체 — 플레이키한 모델이 실패 시간을
    # 숨기고 건강한 avg_latency_ms를 보고하면 운영 판단이 왜곡된다.
    start = time.monotonic()
    json_rejected = False

    def attempt():
        nonlocal json_rejected
        try:
            return chat_once(base_url, api_key, model, system, user, max_tokens, timeout, response_format, extra_body)
        except urllib.error.HTTPError as e:
            # guided-decoding 미지원 서버의 response_format 거부 → 형식 없이 1회
            # 폴백하되 반드시 json_rejected로 표면화한다: 프로덕션 gmail stage1
            # (callLocalLLMJSON, mailanalysis/pipeline.go)은 항상 json_object를 보내고
            # formatless 폴백이 없으므로, JSON 모드를 거부하는 엔드포인트는 이
            # 배터리를 무벌점 통과해도 프로덕션에선 매 추출이 실패한다.
            if response_format is not None and e.code == 400:
                json_rejected = True
                print(
                    f"  ! {model}: response_format rejected (400) — falling back without JSON mode (score capped)",
                    file=sys.stderr,
                )
                return chat_once(base_url, api_key, model, system, user, max_tokens, timeout, None, extra_body)
            raise

    def done(content, out_tokens):
        return content, int((time.monotonic() - start) * 1000), out_tokens, json_rejected

    try:
        return done(*attempt())
    except urllib.error.HTTPError as e:
        if e.code not in TRANSIENT_HTTP:
            raise
        print(f"  ! transient HTTP {e.code} for {model} — retrying once", file=sys.stderr)
    except (urllib.error.URLError, TimeoutError, OSError) as e:
        print(f"  ! transient error for {model}: {e} — retrying once", file=sys.stderr)
    time.sleep(2)
    return done(*attempt())


# --- Production-path extract (POST /api/eval/extract) -----------------------


def resolve_client_token(env_name: str) -> str:
    """eval-extract 인증 토큰: env 우선, 없으면 게이트웨이 호스트의
    ~/.deneb/client_token 파일 (clientauth와 동일 소스)."""
    tok = os.environ.get(env_name, "").strip()
    if tok:
        return tok
    try:
        with open(os.path.expanduser("~/.deneb/client_token"), encoding="utf-8") as f:
            return f.read().strip()
    except OSError:
        return ""


def eval_extract_once(eval_url, token, model, mail, timeout):
    """프로덕션 추출 경로 실행 (server_http_eval.go handleEvalExtract, kind=deal):
    실제 프롬프트 + jsonutil 파싱 + 후처리를 통과한 '소비 결과'를 반환한다.
    result가 None이면 "not a deal" 판정."""
    payload = json.dumps({"kind": "deal", "input": mail, "model": model}).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token  # clientauth.Header
    req = urllib.request.Request(
        eval_url.rstrip("/") + "/api/eval/extract", data=payload, headers=headers, method="POST"
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 — local/tailnet gateway only
        body = json.load(resp)
    if not body.get("ok"):
        raise RuntimeError(f"eval extract failed: {body.get('error', 'unknown')}")
    return body.get("result")


def eval_extract_with_retry(eval_url, token, model, mail, timeout):
    start = time.monotonic()
    try:
        result = eval_extract_once(eval_url, token, model, mail, timeout)
    except (urllib.error.URLError, TimeoutError, OSError, RuntimeError) as e:
        print(f"  ! transient eval-extract error: {e} — retrying once", file=sys.stderr)
        time.sleep(2)
        result = eval_extract_once(eval_url, token, model, mail, timeout)
    return result, int((time.monotonic() - start) * 1000)


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
        "- [확실] spark4tb 디스크 82% 사용 — 다음 달 초 정리 필요\n"
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


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model-a", help="first model name as served (e.g. qwen3.6-35b)")
    ap.add_argument("--model-b", help="second model name as served (e.g. agents-a1)")
    ap.add_argument("--base-url", default="http://127.0.0.1:18800/v1", help="OpenAI-compatible base (default: wormhole)")
    ap.add_argument("--base-url-b", default="", help="model-b용 별도 엔드포인트 (기본: --base-url과 동일 — 후보를 wormhole에 태우기 전 raw vLLM으로 직접 잴 때)")
    ap.add_argument("--api-key-env", default="WORMHOLE_TOKEN", help="env var holding the bearer token")
    ap.add_argument("--rounds", type=int, default=1, help="repeat the battery N times per model")
    ap.add_argument("--timeout", type=float, default=120.0, help="per-call timeout seconds")
    ap.add_argument(
        "--extra-body-a", default="", help='model-a 요청 body에 병합할 JSON (예: \'{"chat_template_kwargs":{"enable_thinking":false}}\') — 자동 thinking-off와 키 충돌 시 이 값이 이긴다'
    )
    ap.add_argument("--extra-body-b", default="", help="model-b 요청 body에 병합할 JSON — 사고형 후보의 서빙 옵션 실험 등")
    ap.add_argument(
        "--no-thinking-off", action="store_true",
        help="후보별 기본 thinking-off 셰이핑(modelrole.ThinkingOffExtraBody 미러)을 끈다 — chat_template_kwargs를 거부하는 비-vLLM 엔드포인트용",
    )
    ap.add_argument(
        "--eval-extract-url", default=os.environ.get("DENEB_EVAL_EXTRACT_URL", ""),
        help="게이트웨이 base URL — 지정 시 extract 케이스를 프로덕션 추출 경로(POST /api/eval/extract, kind=deal)로 라우팅. env DENEB_EVAL_EXTRACT_URL로도 지정 가능",
    )
    ap.add_argument(
        "--client-token-env", default="DENEB_CLIENT_TOKEN",
        help="eval-extract 인증 토큰을 담은 env (비었으면 ~/.deneb/client_token 파일 폴백)",
    )
    ap.add_argument("--dump", default="", help="모든 케이스의 모델 출력 원문을 JSON으로 저장할 경로 (정성 리뷰용)")
    ap.add_argument("--mock", action="store_true", help="run the harness self-test against built-in mock models")
    args = ap.parse_args()

    if args.mock:
        return run_mock()
    if not args.model_a or not args.model_b:
        ap.error("--model-a and --model-b are required (or use --mock)")
    api_key = os.environ.get(args.api_key_env, "")
    if not api_key:
        print(f"warning: ${args.api_key_env} is empty — sending without auth", file=sys.stderr)
    extra_a = json.loads(args.extra_body_a) if args.extra_body_a else None
    extra_b = json.loads(args.extra_body_b) if args.extra_body_b else None
    eval_token = resolve_client_token(args.client_token_env) if args.eval_extract_url else ""
    if args.eval_extract_url and not eval_token:
        print("warning: eval-extract token not found — requests may 401", file=sys.stderr)
    dump = [] if args.dump else None
    common = {
        "eval_extract_url": args.eval_extract_url,
        "eval_token": eval_token,
        "thinking_off": not args.no_thinking_off,
    }
    results = [
        run_model(args.base_url, api_key, args.model_a, args.rounds, args.timeout, extra_a, dump, **common),
        run_model(args.base_url_b or args.base_url, api_key, args.model_b, args.rounds, args.timeout, extra_b, dump, **common),
    ]
    print_report(results)
    if args.dump:
        with open(args.dump, "w", encoding="utf-8") as f:
            json.dump(dump, f, ensure_ascii=False, indent=1)
        print(f"outputs dumped: {args.dump}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
