#!/usr/bin/env python3
"""mail-bench.py — 메일 분석 파이프라인 고정 벤치 (측정 게이트).

메일 분석 경로(stage2 프롬프트·analysis 역할 모델·컨텍스트 주입)를 바꿀 때
"같은 입력을 다시 돌려 점수가 유지/개선될 때만 채택"하는 고정 시드 게이트다.
AutoMem(arXiv:2607.01224) 루프①의 측정 게이트를 메일 파이프라인에 이식한 것
— recall 경로의 recall-metric.sh 와 같은 역할을 메일 경로에서 맡는다.

두 모드:

  trap    함정 메일 6통 × 심은 통찰 18지표 자동 채점 (결정적, LLM 채점 없음).
          각 메일에 "놓치면 실무 손해인 판단"(마진선 적자 판정, 일정 충돌,
          첨부 수치 우선, 범위 밖 요청 감지, 상대 날짜 해석, 구두 합의 리스크)
          이 심어져 있고, 지표는 필수 포함(all)/택일(any)/오답(bad) 문자열 변형
          목록으로 채점한다. ⚠ 자동 채점은 표현 변주를 놓칠 수 있으므로
          경계선 결과는 저장된 출력 원문을 수작업 대조할 것 (2026-07-04 MD/HTML
          실험에서 자동 3건 오판을 수작업 검증으로 뒤집은 전례).

  shadow  실제 수신 메일(IMAP 아카이브 UID 지정)을 두 모델에 동일 입력으로
          나란히 실행해 사람 판독용 파일로 저장. --anchor 를 켜면 헤더에서
          결정적으로 조립한 당사자 앵커 블록(누가 우리 측인가)을 주입한다.

          --reps 를 2 이상으로 주면 TRAP_TOTAL 뒤에 TRAP_STABILITY 가 붙는다:
          지표를 "한 번이라도 통과"(availability)와 "매번 통과"(robustness)로
          나눠 세므로, 점수가 올라간 이유가 실제 개선인지 실행 간 변동인지를
          단일 비율로는 못 하던 방식으로 가른다 (stability() 참조).

사용 예:
  # 게이트: 모델/프롬프트 변경 후 함정 벤치 (A/B)
  python3 scripts/dev/mail-bench.py trap --model glm-5.2 --model-b dsv4-nothink

  # 변동까지 보려면 반복 (TRAP_STABILITY 출력)
  python3 scripts/dev/mail-bench.py trap --model glm-5.2 --reps 3

  # 실메일 섀도런 + 당사자 앵커 실험
  python3 scripts/dev/mail-bench.py shadow --uids 139,151,154,147,132,143 \
      --model glm-5.2 --model-b dsv4-nothink --anchor

전제: wormhole(:18800) 경유 — 토큰은 ~/.wormhole/config.json 또는 $WORMHOLE_TOKEN.
shadow 모드의 IMAP 자격증명은 DENEB_ARCHIVE_IMAP_* env (게이트웨이 systemd 유닛
인라인 값을 폴백으로 읽음). 출력: /tmp/mail-bench-<mode>-<epoch>.{json,txt}.
"""

import argparse
import email
import email.header
import email.utils
import html as htmllib
import imaplib
import json
import os
import re
import subprocess
import time
import urllib.request

# ---------------------------------------------------------------- 프롬프트 (프로덕션 stage2 복제)

SYSTEM = (
    "당신은 이메일 분석 어시스턴트입니다. 이메일 본문, 이전 메일 맥락, 관련 기억을 종합하여 깊이 있는 분석을 제공합니다. "
    "모든 섹션 제목·라벨은 한국어로 쓰세요 ('Primary Analysis', 'Summary', 'Action Items' 같은 영문 라벨 금지). "
    "이모지는 최소화하세요: 기한·금액처럼 놓치면 안 되는 경고에만 ⚠️ 정도를 드물게 쓰고, 섹션 제목이나 항목마다 장식용 이모지(📌✨🔥📊🙂 등)를 붙이지 마세요."
)

DEFAULT_PROMPT = """카카오메일 알림으로 도착한 새 메일을 업무 관점에서 깊이 분석한다.
업무 무관한 광고·뉴스레터·자동 알림이면 길게 분석하지 말고 "참고"로 짧게 끝낸다.

분석은 고정 양식 채우기가 아니라 판단이다. 그래도 다음 관점은 반드시 확인한다.
- 이 메일이 왜 지금 왔는지: 직전 합의, 바뀐 조건, 이어지는 요청을 이전 메일 맥락과 관련 기억으로 연결한다.
- 발신자·수신자·회사·프로젝트 관계: 누가 어떤 입장에서 말하고 있고, 실무/결정 라인이 어떻게 움직이는지 본다.
- 프로젝트 맥락: 현재 어디까지 진행됐고, 이 메일이 그 흐름에서 어떤 의미인지 설명한다.
- 숫자와 조건: 단가·수량·금액·납기·결제기한·마감·승인 조건이 있으면 원문 수치를 보존하고, 이전 맥락과 달라진 값이 있을 때만 비교한다.
- 첨부파일 내용이 주어졌으면 본문보다 첨부 원문 수치를 우선해 반영한다.
- 마지막은 추측이 아닌 구체적인 다음 행동으로 끝낸다.

보고는 먼저 사람이 바로 읽을 수 있는 텍스트로 출력한다. 메일 전체 본문을 그대로 전달하지 않는다.
중요도는 "긴급", "확인 필요", "참고" 중 하나가 드러나게 쓰되, 장식용 이모지는 쓰지 않는다.
기한·금액처럼 놓치면 손해가 큰 경고에만 ⚠️를 드물게 쓸 수 있다.
한국어로 간결하게 쓰고, 근거가 필요한 판단에는 메일 문구나 이전 맥락을 짧게 붙여 사실과 추측을 구분한다."""

# ---------------------------------------------------------------- 함정 코퍼스 (고정 시드 — 수정 금지, 추가만)
# probe: all=[변형리스트...] 전 그룹 등장, any=[...] 하나 이상, bad=[...] 등장 시 실패.

TRAP_MAILS = [
    {
        "name": "m1-renegotiation",
        "email": """제목: [한빛건설] 케이블 공급 조건 관련 협의 요청
보낸사람: 박정우 차장 <jw.park@hanbit-const.co.kr>
날짜: 2026-07-03 (목) 09:20

안녕하세요, 한빛건설 구매팀 박정우입니다.

지난 미팅에서 정리해 주신 조건 잘 받았습니다. 내부 검토 중에 몇 가지 협의드릴 사항이 생겨 연락드립니다.

첫째, 최근 구리 시황이 안정세로 돌아선 점을 반영하여 공급 단가를 미터당 12,000원 수준으로 조정해 주실 수 있는지 검토 부탁드립니다.
둘째, 당사 자금 운용 계획상 선수금 비중을 20%로 낮추고 잔금 비중을 높이는 방안을 제안드립니다.
셋째, 현장 공정이 앞당겨져 1차 납품분을 8월 10일까지 받을 수 있으면 좋겠습니다.

참고로 내부에서 타사 견적도 함께 검토 중인 점 양해 부탁드립니다.""",
        "thread": """- 6월 26일 미팅에서 단가 미터당 12,400원, 선수금 30%(5,300만원), 1차 납품 8월 20일로 최종 합의. 계약서 날인은 다음 주 화요일 예정.
- 상대는 두 차례 12,900원을 고수하다 연간 물량 보장을 조건으로 12,400원을 수용함. 구리값 톤당 5% 이상 상승 시 재협상 조항 포함.""",
        "memory": """- 한빛건설은 직전 두 건의 대금 결제가 각각 9일, 14일 지연된 이력이 있음.
- 우리 쪽 마진 기준: 미터당 12,200원 이하로 내려가면 연간 물량 보장 없이는 적자 전환.""",
        "probes": [
            {"name": "마진선-적자판정", "all": [["12,200", "12200"]], "any": ["적자", "마진 손실", "손익분기 하회", "밑도는", "하회"], "bad": ["적자 전환 위험은 없", "적자 위험은 없"]},
            {"name": "지연이력-선수금연결", "all": [["9일"], ["14일"]], "any": ["선수금", "현금 흐름", "회수"], "bad": []},
            {"name": "합의뒤집기-인지", "all": [], "any": ["재협상", "뒤집", "번복", "합의를 무효", "전면 재검토"], "bad": []},
        ],
    },
    {
        "name": "m2-schedule-conflict",
        "email": """제목: [대한이엔씨] 영덕 프로젝트 계약서 검토 및 회의 일정
보낸사람: 김서연 과장 <sy.kim@daehanenc.co.kr>
날짜: 2026-07-03 (목) 14:05

영덕 부지 건으로 세 가지 부탁드립니다.

1. 계약서 수정안 중 5.2조(지체상금) 조항을 저희 법무 의견대로 수정했습니다. 첨부 검토 후 의견 주세요.
2. 다음 주 화요일인 7월 8일 오후 3시에 화상회의로 계약 조건을 최종 정리했으면 합니다.
3. 회의 전까지 KS 인증서 사본을 보내주시면 내부 품의에 첨부하겠습니다.

인허가 진행 상황도 공유해 주시면 감사하겠습니다.""",
        "thread": """- 영덕 부지 태양광 프로젝트. 인허가 서류 5종 중 3종 확보, 나머지 2종은 군청 회신 대기 중.
- 견적(9억 2천만원 한도)은 7월 11일까지 회신 예정.""",
        "memory": """- 7월 8일(화) 오후 2~5시 영덕 현장 실사 일정이 확정되어 있음(남도에코 동행, 이동 포함 반나절 소요).
- 대한이엔씨 내부 품의는 통상 3영업일 걸림.""",
        "probes": [
            {"name": "일정충돌-포착", "all": [["7월 8일", "7/8"]], "any": ["겹", "충돌", "불가능"], "bad": []},
            {"name": "품의-역산", "all": [["3영업일"]], "any": ["7월 11일", "7/11", "회신 기한", "견적 회신"], "bad": []},
            {"name": "요청3건-분리", "all": [["5.2"], ["KS 인증서", "KS인증서"]], "any": ["화상회의", "회의 일정"], "bad": []},
        ],
    },
    {
        "name": "m3-attachment-priority",
        "email": """제목: [서진전력] 변압기 교체 공사 견적 송부
보낸사람: 오태식 부장 <ts.oh@seojin-power.co.kr>
날짜: 2026-07-03 (목) 11:40

요청하신 변압기 교체 공사 견적을 보내드립니다. 총액은 약 3억원 수준으로 맞췄습니다.
세부 내역은 첨부 견적서를 확인해 주세요. 유효기간 내 발주 여부 회신 부탁드립니다.

[첨부: 견적서_서진전력_변압기.pdf — 추출 내용]
- 공급가액: 298,000,000원
- 부가세: 29,800,000원
- 합계: 327,800,000원
- 견적 유효기간: 2026년 7월 10일까지
- 특기사항: 기초 보강 공사비 별도 (현장 확인 후 산정)""",
        "thread": """- 변압기 노후 교체 건. 6월 중순 현장 점검 완료, 견적 요청은 6월 30일 발송.""",
        "memory": """- 이 공사 예산 승인 한도는 3억 1천만원(부가세 포함)으로 품의가 이미 올라가 있음.""",
        "probes": [
            {"name": "첨부수치-우선(합계)", "all": [["327,800,000", "3억 2,780", "32,780"]], "any": [], "bad": []},
            {"name": "예산초과-감지", "all": [["3억 1천", "310,000,000", "3억1천"]], "any": ["초과", "넘", "부족", "재품의", "증액"], "bad": ["예산 내", "한도 내"]},
            {"name": "유효기간+별도비용", "all": [["7월 10일", "7/10"]], "any": ["기초 보강", "별도", "추가 비용"], "bad": []},
        ],
    },
    {
        "name": "m4-scope-creep",
        "email": """제목: [그린산업] 모니터링 시스템 관련 감사 인사
보낸사람: 한지민 대리 <jm.han@green-ind.co.kr>
날짜: 2026-07-03 (목) 16:20

지난주 설치 작업 깔끔하게 마무리해 주셔서 감사합니다. 현장 반응이 아주 좋습니다.

한 가지, 저희 공장장님께서 옥상 ESS동에도 동일한 모니터링 센서를 설치해 주십사 하시는데,
기존 계약 범위에 포함된 것으로 알고 있어 다음 주 중 방문 일정만 잡아주시면 될 것 같습니다.
일정 회신 부탁드립니다.""",
        "thread": """- 그린산업 공장동 모니터링 시스템 설치 계약(4,800만원). 지난주 설치·검수 완료.""",
        "memory": """- 견적서 특기사항에 'ESS동 센서 설치는 본 견적 범위에서 제외, 별도 견적'이라고 명시되어 있음.""",
        "probes": [
            {"name": "범위외-감지", "all": [["제외", "별도 견적", "별도견적"]], "any": ["범위", "계약 범위", "포함되지 않"], "bad": ["범위에 포함되어 있으므로", "포함된 것이 맞"]},
            {"name": "무상확장-리스크", "all": [], "any": ["추가 비용", "별도 비용", "무상", "추가 견적", "비용 안내"], "bad": []},
            {"name": "관계보존-대응", "all": [], "any": ["정중", "감사 인사", "관계", "부드럽", "우호"], "bad": []},
        ],
    },
    {
        "name": "m5-date-trap",
        "email": """제목: [남도에코] 실사 보고서 초안 회신 요청
보낸사람: 문경호 팀장 <kh.moon@namdo-eco.kr>
날짜: 2026-07-03 (목) 10:05

지난번 협의한 영덕 부지 공동 실사 관련하여, 실사 보고서 초안에 대한 귀사 의견을
다음 주 금요일까지 회신해 주시면 감사하겠습니다. 초안은 첨부드립니다.

참고로 저희 내부 보고는 그 다음 주 월요일에 예정되어 있습니다.""",
        "thread": """- 영덕 부지 공동 실사 건. 7월 8일(화) 오후 현장 실사 예정(공동 참여).""",
        "memory": """- 7월 11일(금)은 대한이엔씨 견적(9억 2천만원 한도) 회신 마감일이기도 함.""",
        "probes": [
            {"name": "기한-계산(다음주금=7/11)", "all": [["7월 11일", "7/11"]], "any": [], "bad": ["7월 4일까지 회신", "이번 주 금요일까지"]},
            {"name": "동일날-마감중첩", "all": [], "any": ["같은 날", "겹", "동시에", "몰려", "중첩", "두 건"], "bad": []},
            {"name": "실사와-순서연결", "all": [["7월 8일", "7/8"]], "any": ["실사 후", "실사 결과", "실사가 끝난"], "bad": []},
        ],
    },
    {
        "name": "m6-quiet-reset",
        "email": """제목: [태화이엔지] 담당자 변경 안내
보낸사람: 총무팀 <admin@taehwa-eng.co.kr>
날짜: 2026-07-03 (목) 13:30

안녕하세요, 태화이엔지 총무팀입니다.

인사 이동으로 귀사 담당이 기존 서동현 과장에서 신임 류가온 과장으로 변경되었음을 안내드립니다.
기존에 협의 중이던 사항들은 신임 담당자와 다시 정리하게 될 예정이오니 양해 부탁드립니다.
류 과장이 다음 주 중 인사 연락을 드릴 예정입니다.""",
        "thread": """- 태화이엔지와 접속반 납품 협의 진행 중. 서동현 과장과 구두로 단가 7% 할인과 9월 납품 일정에 합의(아직 문서화 전).""",
        "memory": """- 서동현 과장과의 구두 합의는 이메일이나 계약서로 남기지 않은 상태.""",
        "probes": [
            {"name": "합의리셋-리스크", "all": [["구두"]], "any": ["무효", "리셋", "다시 협의", "원점", "번복", "잃을", "사라질", "지켜지지 않"], "bad": ["단순 안내", "참고만"]},
            {"name": "문서화-긴급행동", "all": [], "any": ["문서화", "서면", "이메일로 정리", "기록으로 남", "확인 메일"], "bad": []},
            {"name": "합의내용-보존(7%·9월)", "all": [["7%"], ["9월"]], "any": [], "bad": []},
        ],
    },
]

# ---------------------------------------------------------------- 공용: LLM 호출

def wormhole_token():
    tok = os.environ.get("WORMHOLE_TOKEN", "")
    if tok:
        return tok
    try:
        with open(os.path.expanduser("~/.wormhole/config.json"), encoding="utf-8") as f:
            return str(json.load(f).get("token", ""))
    except OSError:
        return ""

def call_llm(base_url, token, model, prompt, max_tokens, timeout):
    body = {
        "model": model,
        "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": prompt}],
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"},
        method="POST",
    )
    start = time.monotonic()
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        payload = json.load(resp)
    ms = int((time.monotonic() - start) * 1000)
    msg = payload["choices"][0]["message"]
    usage = payload.get("usage") or {}
    return (msg.get("content") or ""), ms, usage.get("prompt_tokens"), usage.get("completion_tokens")

def build_prompt(email_text, thread, memory, anchor_block=""):
    parts = [DEFAULT_PROMPT, "\n## 이메일 원문\n" + email_text]
    if anchor_block:
        parts.append("\n\n## 당사자 앵커 (헤더 기반 결정적 조립)\n" + anchor_block)
    parts.append("\n\n## 이전 메일 맥락\n" + thread)
    parts.append("\n\n## 관련 기억\n" + memory)
    return "".join(parts)

# ---------------------------------------------------------------- trap 모드: 자동 채점

def hit_any(text, variants):
    return any(v in text for v in variants)

def score_probes(mail, out):
    detail, hits = [], 0
    for p in mail["probes"]:
        ok = all(hit_any(out, group) for group in p["all"])
        if ok and p["any"] and not hit_any(out, p["any"]):
            ok = False
        if hit_any(out, p.get("bad", [])):
            ok = False
        detail.append((p["name"], ok))
        hits += 1 if ok else 0
    return hits, detail

def stability(results):
    """모델별 지표 단위 availability / robustness (순수 함수).

    reps>1 로 돌리면 같은 지표가 어떤 실행에서는 잡히고 어떤 실행에서는 놓친다.
    TRAP_TOTAL 의 단일 비율은 그 둘을 한 숫자로 눌러버려서, 모델·프롬프트를
    바꿨을 때 "진짜 좋아졌는지"와 "운 좋게 한 번 맞았는지"를 구분하지 못한다.
    그래서 두 끝을 따로 센다:

      availability  reps 중 **한 번이라도** 통과한 지표 비율 (pass@k 유형)
      robustness    reps **전부** 통과한 지표 비율

    둘의 차이(flaky)가 곧 그 모델이 해당 지표에서 흔들리는 폭이다. 다양성만
    늘리는 변경은 availability 만 올리고 robustness 는 못 올린다 — 그래서
    robustness 가 함께 올라야 실제 개선으로 채택한다.

    반환: {model: {"reps", "probes", "available", "robust", "flaky"}}
    """
    seen = {}  # model -> {(mail, probe): [ok, ...]}
    for row in results:
        by_probe = seen.setdefault(row["model"], {})
        for name, ok in row.get("detail") or []:
            by_probe.setdefault((row["mail"], name), []).append(bool(ok))
    out = {}
    for model, by_probe in seen.items():
        available = sum(1 for runs in by_probe.values() if any(runs))
        robust = sum(1 for runs in by_probe.values() if all(runs))
        out[model] = {
            "reps": max((len(runs) for runs in by_probe.values()), default=0),
            "probes": len(by_probe),
            "available": available,
            "robust": robust,
            "flaky": available - robust,
        }
    return out


def print_stability(stats, out=print):
    """reps>1 일 때만 출력 — reps=1 이면 두 값이 TRAP_TOTAL 과 같아 정보가 0이다."""
    for model, s in stats.items():
        if s["reps"] < 2 or not s["probes"]:
            continue
        avail_pct = 100 * s["available"] / s["probes"]
        robust_pct = 100 * s["robust"] / s["probes"]
        out(f"TRAP_STABILITY model={model} reps={s['reps']} probes={s['probes']}"
            f" available={s['available']}({avail_pct:.1f}%)"
            f" robust={s['robust']}({robust_pct:.1f}%)"
            f" flaky={s['flaky']}")


def run_trap(args, token):
    models = [m for m in (args.model, args.model_b) if m]
    results, totals = [], {m: [0, 0, 0, 0] for m in models}  # hits, probes, ms, in_tok
    for mail in TRAP_MAILS:
        anchor = build_anchor_from_text(mail["email"], args) if args.anchor else ""
        prompt = build_prompt(mail["email"], mail["thread"], mail["memory"], anchor)
        for model in models:
            for rep in range(args.reps):
                out, ms, ptok, ctok = call_llm(args.base_url, token, model, prompt,
                                               args.max_tokens, args.timeout)
                hits, detail = score_probes(mail, out)
                t = totals[model]
                t[0] += hits
                t[1] += len(mail["probes"])
                t[2] += ms
                t[3] += ptok or 0
                miss = [n for n, ok in detail if not ok]
                print(f"{mail['name']:<24} {model:<14} rep{rep+1} probes={hits}/{len(detail)}"
                      f" {ms}ms in={ptok}" + (f"  MISS={miss}" if miss else ""))
                results.append({"mail": mail["name"], "model": model, "rep": rep + 1,
                                "hits": hits, "detail": detail, "ms": ms,
                                "in": ptok, "outTok": ctok, "out": out})
    print()
    for model, (h, p, ms, tok) in totals.items():
        n = len(TRAP_MAILS) * args.reps
        print(f"TRAP_TOTAL model={model} probes={h}/{p} pct={100*h//max(p,1)}"
              f" avg_latency_ms={ms//max(n,1)} avg_in_tokens={tok//max(n,1)}")
    print_stability(stability(results))
    return results

# ---------------------------------------------------------------- shadow 모드: 실메일

def imap_env(name):
    v = os.environ.get(name)
    if v:
        return v
    try:  # 게이트웨이 systemd 유닛의 인라인 Environment= 폴백
        out = subprocess.run(["systemctl", "--user", "cat", "deneb-gateway"],
                             capture_output=True, text=True, timeout=10).stdout
        for line in out.splitlines():
            line = line.strip()
            if line.startswith("Environment=" + name + "="):
                return line.split("=", 2)[2].strip().strip('"').strip("'")
    except Exception:
        pass
    return ""

def decode_header(s):
    parts = email.header.decode_header(s or "")
    decoded = [
        (v.decode(e or "utf-8", errors="replace") if isinstance(v, bytes) else v).strip()
        for v, e in parts
    ]
    return " ".join(part for part in decoded if part)

def body_text(msg):
    plain, htm = [], []
    for part in msg.walk():
        ctype = part.get_content_type()
        if ctype not in ("text/plain", "text/html"):
            continue
        payload = part.get_payload(decode=True)
        if payload is None:
            continue
        text = payload.decode(part.get_content_charset() or "utf-8", errors="replace")
        (plain if ctype == "text/plain" else htm).append(text)
    if plain:
        return "\n".join(plain)
    text = "\n".join(htm)
    text = re.sub(r"<(style|script)[^>]*>.*?</\1>", " ", text, flags=re.S | re.I)
    text = re.sub(r"<br\s*/?>|</p>|</div>|</tr>", "\n", text, flags=re.I)
    text = re.sub(r"<[^>]+>", " ", text)
    text = htmllib.unescape(text)
    return re.sub(r"[ \t]+", " ", text)

def fetch_mails(uids, mailbox, body_cap):
    addr = imap_env("DENEB_ARCHIVE_IMAP_ADDR")
    host = addr.split(":")[0] or "127.0.0.1"
    port = int((addr.split(":") + ["1143"])[1] or 1143)
    # 993 = implicit TLS. Anything else on this single-machine deployment is
    # the loopback archive (1143) — plaintext there never leaves the host.
    imap_cls = imaplib.IMAP4_SSL if port == 993 else imaplib.IMAP4
    imap = imap_cls(host, port)
    imap.login(imap_env("DENEB_ARCHIVE_IMAP_USER"), imap_env("DENEB_ARCHIVE_IMAP_PASS"))
    imap.select(mailbox, readonly=True)
    mails = []
    for uid in uids:
        # UID command path: plain fetch() addresses SEQUENCE numbers, which
        # silently benchmarks the wrong mails once the mailbox has expunges.
        typ, data = imap.uid("fetch", str(uid), "(BODY.PEEK[])")
        if typ != "OK" or not data or data == [None]:
            raise SystemExit(f"IMAP UID FETCH {uid} 실패 (typ={typ}) — UID가 존재하는지 확인")
        raw = b"".join(p[1] for p in data if isinstance(p, tuple))
        msg = email.message_from_bytes(raw)
        body = re.sub(r"\n{3,}", "\n\n", body_text(msg).replace("\r", ""))[:body_cap]
        mails.append({
            "uid": str(uid),
            "subject": decode_header(msg.get("Subject")),
            "from": decode_header(msg.get("From")),
            "to": decode_header(msg.get("To")),
            "cc": decode_header(msg.get("Cc")),
            "date": msg.get("Date", ""),
            # 수신/참조 헤더 포함 — 프로덕션 포매터와 입력 동형성 유지 (에스컬레이션·
            # 내부 참조 판단이 수신자 구성에서 나오는 메일이 많다).
            "text": (f"제목: {decode_header(msg.get('Subject'))}\n"
                     f"보낸사람: {decode_header(msg.get('From'))}\n"
                     f"받는사람: {decode_header(msg.get('To'))}\n"
                     f"참조: {decode_header(msg.get('Cc'))}\n"
                     f"날짜: {msg.get('Date','')}\n\n{body}"),
        })
    imap.logout()
    return mails

# ---------------------------------------------------------------- 당사자 앵커 (결정적 조립)

ANCHOR_RULES = (
    "판독 규칙: '우리 측'은 아래 도메인의 조직(탑솔라·남도에코 그룹)이다. 분석은 항상 우리 측 관점에서 쓴다. "
    "보낸사람이 외부면 이 메일은 상대의 발화이고, 보낸사람이 우리 측이면 우리가 보낸 메일의 사본이다. "
    "누가 무엇을 요구했는지는 반드시 이 앵커의 소속과 일치시켜라."
)

def side_label(addr, our_domains, org_map):
    dom = addr.rsplit("@", 1)[-1].lower() if "@" in addr else ""
    org = org_map.get(dom, "")
    if dom in our_domains:
        return f"우리 측({org or dom})"
    return f"외부({org or dom})" if dom else "외부(주소 불명)"

def format_party(field_value, our_domains, org_map):
    out = []
    for name, addr in email.utils.getaddresses([field_value or ""]):
        if not addr.strip():
            continue  # 빈 헤더가 만들어내는 가공의 '외부(주소 불명)' 당사자 차단
        label = side_label(addr, our_domains, org_map)
        disp = decode_header(name).strip() or addr
        out.append(f"{disp} <{addr}> — {label}")
    return out

def build_anchor(mail, our_domains, org_map):
    lines = [f"- 보낸사람: {p}" for p in format_party(mail.get("from", ""), our_domains, org_map)]
    lines += [f"- 받는사람: {p}" for p in format_party(mail.get("to", ""), our_domains, org_map)]
    lines += [f"- 참조: {p}" for p in format_party(mail.get("cc", ""), our_domains, org_map)]
    lines.append("- " + ANCHOR_RULES)
    return "\n".join(lines)

def build_anchor_from_text(email_text, args):
    """trap 코퍼스용: 본문의 '보낸사람:' 줄에서만 조립 (수신자는 우리 측으로 간주)."""
    m = re.search(r"보낸사람:\s*(.+)", email_text)
    frm = m.group(1).strip() if m else ""
    fake = {"from": frm, "to": "우리 담당자 <us@topsolar.kr>", "cc": ""}
    return build_anchor(fake, args.our_domain_set, args.org_map_dict)

def run_shadow(args, token):
    uids = [u.strip() for u in args.uids.split(",") if u.strip()]
    mails = fetch_mails(uids, args.mailbox, args.body_cap)
    for m in mails:
        print(f"fetched #{m['uid']}: {m['subject'][:60]}")
    models = [m for m in (args.model, args.model_b) if m]
    thread = "- (별도 스레드 요약 없음 — 본문에 인용된 이전 메일이 있으면 그것이 맥락)"
    memory = "- (관련 기억 없음)"
    results = []
    for m in mails:
        anchor = build_anchor(m, args.our_domain_set, args.org_map_dict) if args.anchor else ""
        prompt = build_prompt(m["text"], thread, memory, anchor)
        row = {"uid": m["uid"], "subject": m["subject"], "anchor": anchor}
        for model in models:
            try:
                out, ms, ptok, ctok = call_llm(args.base_url, token, model, prompt,
                                               args.max_tokens, args.timeout)
            except Exception as e:  # 실패해도 나머지는 계속
                out, ms, ptok, ctok = f"[ERROR: {e}]", -1, 0, 0
            row[model] = {"out": out, "ms": ms, "in": ptok, "outTok": ctok}
            print(f"#{m['uid']} {model:<14} {ms:>7}ms in={ptok} out={ctok} len={len(out)}")
        results.append(row)
    return results

# ---------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("mode", choices=["trap", "shadow"])
    ap.add_argument("--model", required=True, help="기본 모델 (wormhole 라우팅 이름)")
    ap.add_argument("--model-b", default="", help="비교 모델 (선택, A/B)")
    ap.add_argument("--base-url", default="http://127.0.0.1:18800/v1")
    ap.add_argument("--reps", type=int, default=1, help="trap 반복 횟수")
    # 프로덕션 자율 stage2 기본(1536)과 동일 예산 — 게이트가 프로덕션보다 넉넉한
    # 예산으로 통과하는 착시 방지. 인터랙티브 경로(4096) 재현은 명시 지정.
    ap.add_argument("--max-tokens", type=int, default=1536)
    ap.add_argument("--timeout", type=int, default=300)
    ap.add_argument("--anchor", action="store_true", help="당사자 앵커 블록 주입")
    ap.add_argument("--our-domains", default="topsolar.kr,namdo-eco.kr",
                    help="우리 측 도메인 (콤마 구분; 그룹사 포함 기본)")
    ap.add_argument("--org-map", default="", help="도메인→조직 라벨 JSON 파일 (선택)")
    ap.add_argument("--uids", default="", help="shadow: IMAP UID 콤마 목록")
    ap.add_argument("--mailbox", default="INBOX")
    ap.add_argument("--body-cap", type=int, default=5000)
    args = ap.parse_args()

    args.our_domain_set = {d.strip().lower() for d in args.our_domains.split(",") if d.strip()}
    args.org_map_dict = {}
    if args.org_map:
        with open(args.org_map, encoding="utf-8") as f:
            args.org_map_dict = {k.lower(): v for k, v in json.load(f).items()}

    token = wormhole_token()
    if not token:
        raise SystemExit("WORMHOLE_TOKEN 또는 ~/.wormhole/config.json 이 필요합니다")
    if args.mode == "shadow" and not args.uids:
        raise SystemExit("shadow 모드에는 --uids 가 필요합니다")

    results = run_trap(args, token) if args.mode == "trap" else run_shadow(args, token)

    stamp = f"{int(time.time() * 1000)}-{os.getpid()}"
    base = f"/tmp/mail-bench-{args.mode}-{stamp}"
    with open(base + ".json", "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=1, default=list)
    with open(base + ".txt", "w", encoding="utf-8") as f:
        for r in results:
            if args.mode == "trap":
                f.write(f"{'='*100}\n{r['mail']} [{r['model']}] rep{r['rep']} "
                        f"probes={r['hits']} ({r['ms']}ms)\n{'='*100}\n{r['out']}\n\n")
            else:
                for model in (args.model, args.model_b):
                    if model and model in r:
                        f.write(f"{'='*100}\n#{r['uid']} [{model}] {r['subject']} "
                                f"({r[model]['ms']}ms)\n{'='*100}\n{r[model]['out']}\n\n")
    print(f"\nsaved: {base}.json  {base}.txt")

if __name__ == "__main__":
    main()
