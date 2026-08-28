#!/usr/bin/env python3
"""Qualitative depth probe: how well does a model *think* about work?

Tool-call benchmarks score execution; they say almost nothing about analytical
depth, and the two rank nearly inversely (docs/research/, model comparison
2026-08-26: glm-5.3 was first on depth and fourth on tool-eval). This probe is
the other half.

Each scenario carries contradictory or unstated signals on purpose. A shallow
answer anchors on the loudest fact and elaborates around it; a deep answer
crosses the signals, names what the contradiction implies, and reframes the
question when the stated one is the wrong question.

Steering matters: an unsteered style comparison misjudges, because models
differ mostly in default verbosity. This sends the real gateway persona
(prompt/system_prompt_params.go DefaultPersona plus the Communication,
Attitude, Action and Execution sections), minus the card-authoring contract
that only applies to a live client.

Usage:
    depth_probe.py --base-url http://HOST:8000/v1 --model NAME --out DIR
    depth_probe.py ... --scenarios D1,D3      # subset
"""

import argparse
import json
import pathlib
import sys
import urllib.request

# Mirrors DefaultPersona + the steering sections of BuildSystemPrompt. Keep in
# sync when the persona changes; the point is to measure the model as the
# product actually prompts it.
SYSTEM = """You are Nev — a personal assistant running inside Deneb. Deneb is a single-user AI agent platform on DGX Spark.

## Role
You are one chief-of-staff agent; never split the analyst and assistant personas. Combine **business analysis** (synthesize mail, project, people, and deal context into why it matters now, risks, and deadlines) with **executive assistance** (calendar and meeting preparation plus imminent reminders that say what is due when). A strong answer carries both the analyst's why and the assistant's when in one response; never divide them into separate replies or tabs.

## Communication
Respond directly and substantively to the user's current message. Never evade with phrases such as '완료된 작업입니다' or '진행할 내용 없습니다'.
Lead with the answer, then explain. Be direct and practical.
Match the user's tone and formality naturally. Always respond in Korean.
Avoid filler such as "좋은 질문이네요!" or "기꺼이 도와드리겠습니다". Earn trust through results.
Match length to complexity: simple question → 1-3 sentences; analysis or explanation → structured answer; work report → result plus next step.

## Attitude
Say when you see a better approach; you do not need to agree with everything.
Call out inefficient or awkward choices and maintain your own point of view.

## Action Principles
Check before asking: read files, understand context, connect prior information, and search when useful. Try to resolve the task yourself and ask only when genuinely necessary.

## Execution Bias
When the user requests work, start in the same turn. Never stop after making a plan or saying '하겠습니다'.
For multi-step work, begin immediately and provide concise progress updates."""

# Each scenario embeds a contradiction the shallow reading misses. The "tests"
# note is for the human reader of the side-by-side, not sent to the model.
SCENARIOS = [
    {
        "id": "D1",
        "title": "모순 신호 교차",
        "tests": "가장 날카로운 모순(반품 ↔ '분위기 좋았다')을 교차시키는가, 아니면 가장 큰 숫자에 닻을 내리는가",
        "prompt": """다음은 오늘까지 들어온 신호들이다. 상황을 판단해줘.

- 어제 17:40 — 대성전기에서 인버터 12대 반품 접수. 사유란은 "현장 사정".
- 오늘 09:12 — 대성전기 구매팀장이 "3분기 단가 재검토 요청" 메일. 본문 두 줄, 근거 없음.
- 오늘 11:30 — 우리 영업 김 과장 주간보고: "대성 미팅 분위기 좋았음, 4분기 물량 확대 논의".
- 지난주 — 경쟁사 태산이 대성에 견적 제출했다는 이야기를 협력업체에서 전해 들음.
- 대성과의 연간 계약 갱신은 6주 뒤. 아직 갱신 미팅 일정이 안 잡혔다.""",
    },
    {
        "id": "D2",
        "title": "문제 재규정",
        "tests": "'협업 안 하는 사람' 문제를 개인 성향이 아니라 제도(KPI·보상) 문제로 재규정하는가",
        "prompt": """생산팀장이 품질팀과 계속 부딪힌다. 품질팀이 요청한 공정 데이터를 세 번 연속 늦게 줬고, 지난달엔 아예 무시했다. 생산팀장은 우리 회사 최고 수율을 내는 사람이고 본인 팀 관리도 잘한다. 품질팀장은 "협조가 안 된다"고 나한테 두 번 말했다.

생산팀장은 내년 임원 후보군에 올라 있다. 어떻게 봐야 하나?""",
    },
    {
        "id": "D3",
        "title": "가설 경쟁과 검증 설계",
        "tests": "가설을 나열만 하는가, 아니면 가설별 예측을 미리 적고 크기까지 검증하는가",
        "prompt": """지난달 우리 O&M 서비스 지표다.

- 고객 문의 건수: 전월 대비 34% 감소
- 계약 해지율: 2.1% → 3.4%
- 현장 출동 건수: 변동 없음
- 앱 평점: 4.2 → 4.1 (리뷰 수는 절반으로 감소)
- 지난달 15일에 고객용 앱을 대규모 업데이트했다

무슨 일이 일어난 건지 판단해줘.""",
    },
    {
        "id": "D4",
        "title": "숨은 조항의 함의",
        "tests": "독소조항을 찾아 나열하는 데 그치는가, 상대가 왜 그 조항을 넣었는지까지 읽는가",
        "prompt": """신규 EPC 계약서 초안에서 상대측이 넣은 조항들이다.

- 하자보수 기간 10년 (업계 관행 5년)
- 준공 지연 시 지체상금 일 0.3%, 상한 없음
- 발주처가 "합리적 사유"로 공정 변경을 지시할 수 있고, 그에 따른 비용은 상호 협의
- 대금 지급은 발주처의 PF 인출 시점 기준
- 분쟁은 발주처 소재지 법원 전속관할

우리는 이 발주처와 처음 거래한다. 어떻게 대응할까?""",
    },
    {
        "id": "D5",
        "title": "제약 하 우선순위",
        "tests": "네 개를 다 하는 절충안으로 도피하는가, 무엇을 포기할지 명시하는가",
        "prompt": """다음 달에 셋 중 하나만 할 수 있다. 엔지니어는 4명, 다른 일은 이미 꽉 차 있다.

1. 대성전기 신규 현장 3곳 시공 (매출 8억, 하지만 우리 주력 아닌 지붕형)
2. 기존 현장 40곳의 모니터링 시스템 교체 (매출 0, 현재 시스템 벤더가 내년 지원 종료)
3. 공공 입찰 2건 준비 (기대매출 20억, 낙찰 확률 30%, 준비에 엔지니어 2명 3주)

뭘 해야 하나?""",
    },
]


def ask(base_url, model, prompt, max_tokens, timeout, effort=None):
    body = {
        "model": model,
        "temperature": 0.7,
        "max_tokens": max_tokens,
        "messages": [
            {"role": "system", "content": SYSTEM},
            {"role": "user", "content": prompt},
        ],
    }
    # Reasoning tiers are per-model: Qwen3.8-Flash-Next takes xhigh (its
    # default), medium and low, and rejects "high" outright. Left unset, a
    # model on its highest tier can spend the entire token budget thinking and
    # return an empty answer — always read finish_reason before believing a
    # short reply is the model's considered output.
    if effort:
        body["reasoning_effort"] = effort
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        json.dumps(body).encode(),
        {"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=timeout) as r:
        d = json.load(r)
    m = d["choices"][0]["message"]
    return {
        "content": (m.get("content") or "").strip(),
        # vLLM exposes the thinking trace under different keys per model family;
        # take whichever is present rather than assuming one.
        "reasoning": (m.get("reasoning") or m.get("reasoning_content") or ""),
        "finish_reason": d["choices"][0].get("finish_reason"),
        "usage": d.get("usage", {}),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", required=True)
    ap.add_argument("--model", required=True)
    ap.add_argument("--out", required=True, help="directory for answers.json")
    ap.add_argument("--label", default=None, help="name in the output (default: model)")
    ap.add_argument("--scenarios", default=None, help="comma-separated ids, default all")
    ap.add_argument("--max-tokens", type=int, default=6000)
    ap.add_argument("--timeout", type=int, default=900)
    ap.add_argument("--effort", default=None,
                    help="reasoning_effort; omit for the model default")
    a = ap.parse_args()

    want = set(a.scenarios.split(",")) if a.scenarios else None
    picked = [s for s in SCENARIOS if not want or s["id"] in want]
    out = pathlib.Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    label = a.label or a.model

    results = []
    for s in picked:
        try:
            r = ask(a.base_url, a.model, s["prompt"], a.max_tokens, a.timeout, a.effort)
        except Exception as e:  # a dead scenario must not lose the others
            r = {"content": "", "reasoning": "", "error": str(e)}
            print(f"{s['id']}: FAILED {e}", file=sys.stderr)
        else:
            print(
                f"{s['id']}: 답변 {len(r['content'])}자 · 추론 {len(r['reasoning'])}자 "
                f"· {r['finish_reason']}",
                file=sys.stderr,
            )
        results.append({**{k: s[k] for k in ("id", "title", "tests", "prompt")}, **r})

    (out / "answers.json").write_text(
        json.dumps({"label": label, "model": a.model, "results": results},
                   ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    print(f"→ {out / 'answers.json'}", file=sys.stderr)


if __name__ == "__main__":
    main()
