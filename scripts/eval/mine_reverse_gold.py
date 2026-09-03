#!/usr/bin/env python3
"""Derive REVERSE-direction recall gold from an existing (direct) gold set.

Every hand-written gold question is direct: it names the project and asks for a
detail recorded on its page ("강진 신다산 EPC 계약서 법무검토 결과 뭐였지?" →
하자보수). The reverse direction — name the detail, ask which project carries it
("EPC 법무검토에서 하자보수 쟁점이 나온 현장이 어디였지?") — is the direction a
project-major wiki is NOT indexed for, and no gold set has ever carried it. This
miner produces it, so `recall-bench`'s direction split has something to score.

The transformation is deliberately minimal: a reverse case keeps the SAME
`gold_paths` and `must_contain` as the direct case it came from, and changes only
the question and the `direction` label. Both questions must retrieve the same
page, so the direct/reverse comparison is matched by construction — the answer
space cannot differ between the two arms, which is the confound that makes the
published reversal-curse comparisons hard to read.

Only the rewrite is a model call. Everything that decides whether a candidate is
KEPT is pure and pinned by tests (`reverse_case`, `reject_reason`), because a
miner that silently admits a bad case corrupts the metric it exists to feed.

Guards, in the order they fire:

  * subject must be quoted from the direct question — a model that invents the
    subject has not identified it, and the leak guard below would then be
    checking a string that was never in play;
  * no subject leak — if the rewrite still names the project, it is not a
    reverse question, it is a paraphrase, and it would score as direct;
  * detail must survive — the rewrite has to ask FROM the recorded answer, so
    some of the detail token must remain in it;
  * must actually differ from the direct question.

Cases are emitted, never auto-trusted: like `mine_analysis_gold.py`, spot-check a
sample before pointing a baseline at the output.

Usage:
  scripts/eval/mine_reverse_gold.py --gold ~/.deneb/wiki-qa-gold.jsonl \\
      --model glm-5.2 [--limit 40] [--out ~/.deneb/wiki-qa-gold-reverse.jsonl]

Without --out the set is written to stdout, so a dry run costs nothing.
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

WORMHOLE_URL = "http://127.0.0.1:18800/v1/chat/completions"
WORMHOLE_CFG = os.path.expanduser("~/.wormhole/config.json")
RETRY_ATTEMPTS = 4
RETRY_BACKOFF_S = 5

SYSTEM = (
    "당신은 한국어 사내 위키의 검색 평가셋을 만드는 도구다. "
    "지시한 JSON 스키마만 출력하고 설명이나 코드펜스를 붙이지 않는다."
)

PROMPT = """아래는 사내 위키 검색 평가용 질문이다. 이 질문은 **정방향**이다:
프로젝트(현장)를 이름으로 지목하고 그 프로젝트에 기록된 세부사항을 묻는다.

이것을 **역방향** 질문으로 바꿔라. 역방향이란 세부사항을 단서로 주고
"그게 어느 프로젝트/현장이냐"를 묻는 형태다.

원 질문: {question}
그 질문의 정답에 해당하는 세부 토큰: {detail}

규칙:
- 역방향 질문에는 프로젝트/현장 이름이 **절대 들어가면 안 된다**. 그 이름을 맞히는 게 질문의 목적이다.
- 세부 토큰의 내용은 질문 안에 단서로 남겨라.
- 실무자가 실제로 물어볼 법한 자연스러운 한국어 한 문장으로 쓴다.
- 단서가 너무 흔해서 여러 현장이 답이 될 것 같으면 reverse 를 빈 문자열로 두어라.

JSON 으로만 답한다:
{{"subject": "<원 질문에서 프로젝트/현장을 가리키는 부분을 그대로 복사>",
  "reverse": "<역방향 질문 또는 빈 문자열>"}}"""


# --- model transport --------------------------------------------------------- #
def wormhole_token() -> str:
    with open(WORMHOLE_CFG, encoding="utf-8") as fh:
        return os.path.expandvars(json.load(fh)["token"])


def should_retry(exc: BaseException) -> bool:
    """Transport faults and upstream 5xx are worth waiting out; a 4xx is not."""
    if isinstance(exc, urllib.error.HTTPError):
        return exc.code >= 500 or exc.code == 429
    return isinstance(exc, (urllib.error.URLError, OSError, ValueError))


def call_model(model: str, prompt: str, max_tokens: int = 700, timeout: int = 180,
               attempts: int = RETRY_ATTEMPTS, sleep=time.sleep) -> str:
    """One chat completion through the wormhole, retried through a restart."""
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "system", "content": SYSTEM},
                     {"role": "user", "content": prompt}],
        "max_tokens": max_tokens, "stream": False,
    }).encode()
    for attempt in range(max(1, attempts)):
        try:
            req = urllib.request.Request(
                WORMHOLE_URL, data=payload,
                headers={"Content-Type": "application/json",
                         "Authorization": f"Bearer {wormhole_token()}"})
            with urllib.request.urlopen(req, timeout=timeout) as r:
                resp = json.load(r)
            return (resp["choices"][0]["message"].get("content") or "").strip()
        except Exception as exc:  # noqa: BLE001 - re-raised below
            if not should_retry(exc) or attempt == attempts - 1:
                raise
            sleep(RETRY_BACKOFF_S * (2 ** attempt))
    raise RuntimeError("unreachable")  # keeps the contract explicit


# --- protocol + guards (pure) ------------------------------------------------- #
FENCE_RE = re.compile(r"^\s*```(?:json)?\s*|\s*```\s*$", re.MULTILINE)


def parse_reply(raw: str) -> dict:
    """Pull the JSON object out of a model reply, tolerating a code fence.

    Returns {} for anything unparseable rather than raising: one malformed
    reply must cost its own case, not the whole run.
    """
    text = FENCE_RE.sub("", raw or "").strip()
    if not text:
        return {}
    start, end = text.find("{"), text.rfind("}")
    if start == -1 or end <= start:
        return {}
    try:
        obj = json.loads(text[start:end + 1])
    except ValueError:
        return {}
    return obj if isinstance(obj, dict) else {}


def normalize(s: str) -> str:
    """Fold whitespace so a leak check is not defeated by spacing alone."""
    return re.sub(r"\s+", "", s or "")


def detail_tokens(must_contain: list) -> list:
    """Answer tokens a case accepts. '|' separates alternatives in gold."""
    out = []
    for entry in must_contain or []:
        out.extend(part.strip() for part in str(entry).split("|") if part.strip())
    return out


def reject_reason(question: str, subject: str, reverse: str, must_contain: list):
    """Why this candidate must not become a gold case, or None to keep it.

    Order matters: an unquoted subject is checked first because every later
    guard is only meaningful once we know the subject is the real one.
    """
    subject, reverse = (subject or "").strip(), (reverse or "").strip()
    if not reverse:
        return "model declined (clue too generic)"
    if not subject:
        return "no subject identified"
    if normalize(subject) not in normalize(question):
        return "subject not quoted from the direct question"
    if normalize(subject) in normalize(reverse):
        return "subject leaks into the reverse question"
    if normalize(reverse) == normalize(question):
        return "reverse question is identical to the direct one"
    tokens = detail_tokens(must_contain)
    if tokens and not any(normalize(t) in normalize(reverse) for t in tokens):
        return "detail clue dropped from the reverse question"
    return None


def reverse_case(case: dict, reverse_question: str) -> dict:
    """Build the reverse twin: same target, same answer tokens, new question.

    Keeping gold_paths and must_contain identical is the point — it makes the
    two directions a matched pair rather than two different questions.
    """
    twin = dict(case)
    twin["id"] = f"{case['id']}-rev"
    twin["question"] = reverse_question.strip()
    twin["direction"] = "reverse"
    return twin


def direct_case(case: dict) -> dict:
    """The original, explicitly labeled so both arms are counted."""
    labeled = dict(case)
    labeled["direction"] = "direct"
    return labeled


def load_gold(path: str) -> list:
    """Parse gold JSONL, skipping the '#' header/divider lines the sets carry."""
    out = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            try:
                obj = json.loads(line)
            except ValueError:
                continue
            if isinstance(obj, dict) and obj.get("id") and obj.get("question"):
                out.append(obj)
    return out


# --- run --------------------------------------------------------------------- #
def mine(cases: list, model: str, ask=call_model, log=sys.stderr.write) -> tuple:
    """Return (emitted cases, rejection reasons) for the given direct cases."""
    emitted, rejects = [], []
    for case in cases:
        emitted.append(direct_case(case))
        detail = " / ".join(detail_tokens(case.get("must_contain"))) or "(없음)"
        prompt = PROMPT.format(question=case["question"], detail=detail)
        try:
            reply = parse_reply(ask(model, prompt))
        except Exception as exc:  # noqa: BLE001 - one case must not kill the run
            rejects.append((case["id"], f"model call failed: {exc}"))
            continue
        reason = reject_reason(case["question"], reply.get("subject", ""),
                               reply.get("reverse", ""), case.get("must_contain"))
        if reason:
            rejects.append((case["id"], reason))
            continue
        emitted.append(reverse_case(case, reply["reverse"]))
        log(f"  + {case['id']}-rev  {reply['reverse']}\n")
    return emitted, rejects


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--gold", required=True, help="direct 골드 JSONL 경로")
    ap.add_argument("--model", default="glm-5.2", help="재작성 모델 (웜홀이 서빙 중이어야 함)")
    ap.add_argument("--limit", type=int, default=0, help="처음 N건만 (0=전부)")
    ap.add_argument("--out", default="", help="출력 JSONL (미지정 시 stdout)")
    a = ap.parse_args(argv)

    cases = load_gold(a.gold)
    if a.limit > 0:
        cases = cases[:a.limit]
    if not cases:
        sys.stderr.write(f"no gold cases in {a.gold}\n")
        return 1
    sys.stderr.write(f"== {len(cases)} direct cases → reverse via {a.model}\n")

    emitted, rejects = mine(cases, a.model)

    lines = [json.dumps(c, ensure_ascii=False) for c in emitted]
    body = "\n".join(lines) + "\n"
    if a.out:
        with open(a.out, "w", encoding="utf-8") as fh:
            fh.write("# 방향 축 골드 — scripts/eval/mine_reverse_gold.py 생성. "
                     "채택 전 표본 검수 필요.\n")
            fh.write(body)
        sys.stderr.write(f"wrote {a.out}\n")
    else:
        sys.stdout.write(body)

    reverse_n = sum(1 for c in emitted if c.get("direction") == "reverse")
    sys.stderr.write(f"\nMINE_REVERSE direct={len(cases)} reverse={reverse_n} "
                     f"rejected={len(rejects)}\n")
    by_reason = {}
    for _, reason in rejects:
        by_reason[reason] = by_reason.get(reason, 0) + 1
    for reason, n in sorted(by_reason.items(), key=lambda kv: -kv[1]):
        sys.stderr.write(f"  {n:>3}  {reason}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
