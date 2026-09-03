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
      --model glm-5.2 --wiki <wiki-copy-dir> \\
      [--limit 40] [--out ~/.deneb/wiki-qa-gold-reverse.jsonl]

--wiki is what makes the filtering real rather than a model's opinion: it counts
how many page subtrees actually carry the clue, refuses a case whose clue is
missing from its own target, and hands the rewrite an excerpt so it can ADD a
distinguishing detail instead of giving up on a common one. Point it at a COPY.

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
{kind}을(를) 이름으로 지목하고 거기에 기록된 세부사항을 묻는다.

이것을 **역방향** 질문으로 바꿔라. 역방향이란 세부사항을 단서로 주고
"그게 어느 {kind}이냐"를 묻는 형태다.

원 질문: {question}
그 질문의 정답에 해당하는 세부 토큰: {detail}

{ambiguity}
규칙:
- 역방향 질문에는 {kind} 이름이 **절대 들어가면 안 된다**. 그 이름을 맞히는 게 질문의 목적이다.
- 세부 토큰의 내용은 질문 안에 단서로 남겨라.
- 실무자가 실제로 물어볼 법한 자연스러운 한국어 한 문장으로 쓴다.
- 묻는 대상은 반드시 {kind} 이다. 다른 종류로 바꿔 묻지 마라.
{padding_rule}{excerpt}
JSON 으로만 답한다:
{{"subject": "<원 질문에서 {kind}을(를) 가리키는 부분을 그대로 복사>",
  "reverse": "<역방향 질문 또는 빈 문자열>"}}"""

AMBIGUOUS_NOTE = ("주의: 이 세부 토큰은 위키에서 {n}곳에 나타난다 — 단서 하나만으로는 "
                  "답이 하나로 좁혀지지 않는다.\n")
UNIQUE_NOTE = "참고: 이 세부 토큰은 위키에서 이 {kind} 한 곳에만 나타난다.\n"

# Padding is the failure mode this measurement exists to prevent. A reverse
# question stuffed with four extra distinctive tokens retrieves EASILY — BM25
# has more to match on — so an over-specified reverse arm reports a deficit that
# is smaller than the real one, which is the opposite of what the split is for.
# The corpus count decides whether an extra clue is even permitted.
PAD_FORBIDDEN = ("- 단서가 이미 답을 하나로 좁힌다. 다른 정보를 **절대 보태지 마라** — "
                 "질문을 길게 만들면 검색이 쉬워져 측정이 망가진다.\n"
                 "- 원 질문과 비슷한 길이의 한 문장으로 쓴다.\n")
PAD_ALLOWED = ("- 단서 하나로는 부족하니 이 {kind}을(를) 특정할 단서를 아래 발췌에서 "
               "**딱 하나만** 고른다 (설비 용량·상대 회사·날짜·금액 중 가장 짧은 것). "
               "{kind} 이름 자체는 안 된다.\n"
               "- 둘 이상 보태지 마라 — 길어질수록 검색이 쉬워져 측정이 망가진다.\n"
               "- 발췌를 봐도 특정할 단서가 없을 때만 reverse 를 빈 문자열로 둔다.\n")

PAD_NO_SOURCE = ("- 보탤 발췌가 없으니 원 질문의 정보만으로 자연스럽게 뒤집어 쓴다.\n"
                 "- 새 사실을 지어내지 마라. 뒤집을 수 없을 때만 reverse 를 빈 문자열로 둔다.\n")

# How much longer than its direct twin a reverse question may be. Calibrated on
# the 2026-09-03 ungoverned run, where Korean's density made a first guess of +70
# admit the very example that motivated the guard: one added clue measured +18
# chars ("154kV 케이블을 ZTT에서 공급받은 신안군 태양광 현장이…"), four measured
# +66, and a pure rephrase of a pinned clue fits inside +30.
MAX_GROWTH_PINNED = 30   # clue already unique — rephrasing slack only
MAX_GROWTH_PADDED = 40   # room for exactly one added clue, not four


def growth_reject(question: str, reverse: str, reach: int):
    """Reject a reverse question that grew past what its clue budget allows."""
    budget = MAX_GROWTH_PADDED if reach > 1 else MAX_GROWTH_PINNED
    grew = len((reverse or "").strip()) - len((question or "").strip())
    if grew > budget:
        return f"over-specified (+{grew} chars, budget +{budget})"
    return None

# What the reverse question should ask FOR. The gold sets target three page
# families and only one of them is a site: asking "which 현장?" about 업무/BEP
# (a fund profile) or 인물/에코프로-담당자 (a contact card) produces a question
# with no valid answer, which then scores as a retrieval failure forever.
PAGE_KINDS = {"프로젝트": "현장", "인물": "인물", "업무": "문서"}


def page_kind(gold_paths: list) -> str:
    """The noun the reverse question must ask for, from the gold target family.

    Falls back to 프로젝트's noun: paths that carry no family prefix are vendor
    or site fragments in practice, which are sites.
    """
    for path in gold_paths or []:
        head = str(path).split("/")[0]
        if head in PAGE_KINDS:
            return PAGE_KINDS[head]
    return PAGE_KINDS["프로젝트"]


# --- corpus grounding (pure) -------------------------------------------------- #
# The paper's pipeline filters candidate questions by searching for alternative
# answers and discarding the ambiguous ones. The wiki is small enough to do that
# exactly rather than by sampling.
#
# Two things were wrong in the first attempt and both mattered. Folding pages
# into a fixed two-segment "subtree" mangled the nested targets — gold aims at
# 프로젝트/거래/현대에너지솔루션 while the fold produced 프로젝트/거래, so the
# match failed and 37 sound cases were condemned as dead gold against a
# measured truth of 3. And the answer space is not "folders on disk": it is the
# set of targets the benchmark actually scores, which is the gold set's own
# distinct gold_paths. Counting in that space is what makes "how many answers
# could this question have" mean anything.
def path_hit(gold: str, relpath: str) -> bool:
    """recall-bench's gold-path rule: match only at a path-segment start.

    Without it "영덕" would claim "남영덕/…" and the ambiguity count would
    collapse toward 1 — hiding exactly the cases the count exists to flag.
    """
    g = str(gold or "").rstrip("/")
    p = str(relpath or "").replace("\\", "/")
    if not g:
        return False
    if p.endswith(".md"):
        p = p[:-3]
    start = 0
    while True:
        idx = p.find(g, start)
        if idx == -1:
            return False
        if idx == 0 or p[idx - 1] == "/":
            return True
        start = idx + 1


def pages_for(pages: dict, gold_paths: list) -> list:
    """Every page file the given gold target covers."""
    return [rel for rel in (pages or {})
            if any(path_hit(g, rel) for g in gold_paths or [])]


def pages_with(pages: dict, tokens: list) -> list:
    """Page files whose text carries ANY clue token (gold '|' is OR)."""
    return [rel for rel, text in (pages or {}).items()
            if any(t and t in text for t in tokens or [])]


def gold_targets(cases: list) -> list:
    """The answer space: distinct gold_paths the benchmark can score."""
    seen = []
    for case in cases or []:
        for gold in case.get("gold_paths") or []:
            if gold not in seen:
                seen.append(gold)
    return seen


def clue_reach(pages: dict, targets: list, tokens: list) -> int:
    """How many scoreable targets the clue could name."""
    hits = pages_with(pages, tokens)
    return sum(1 for t in targets or []
               if any(path_hit(t, rel) for rel in hits))


def clue_verdict(pages: dict, targets: list, gold_paths: list, tokens: list):
    """(reject reason or None, number of targets the clue reaches).

    Distinguishes two failures that look alike but mean different things: a gold
    path that resolves to no page at all is a pre-existing defect in the source
    set (recall-bench warns about it separately, and mining a twin off it would
    only give the defect a second id), while a resolvable page that lacks its own
    clue is the dead-gold class the 2026-08-23 baseline described.
    """
    if not pages or not tokens:
        return None, 0
    if not gold_paths:
        return "no gold path to target", 0
    own = pages_for(pages, gold_paths)
    if not own:
        return "gold path resolves to no page", 0
    if not any(any(t and t in pages[rel] for t in tokens) for rel in own):
        return "clue absent from its own gold page", clue_reach(pages, targets, tokens)
    return None, clue_reach(pages, targets, tokens)


def excerpt_for(pages: dict, gold_paths: list, limit: int) -> str:
    """Longest page under the target — the one most likely to carry a clue."""
    bodies = [(pages or {}).get(rel, "") for rel in pages_for(pages, gold_paths)]
    if not bodies:
        return ""
    return max(bodies, key=len).strip()[:limit]


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
def build_prompt(case: dict, pages: dict, targets: list, excerpt_chars: int) -> str:
    """Assemble the rewrite prompt, grounded in the corpus when one is given."""
    kind = page_kind(case.get("gold_paths"))
    tokens = detail_tokens(case.get("must_contain"))
    _, reach = clue_verdict(pages, targets, case.get("gold_paths"), tokens)
    body = "" if reach == 1 else excerpt_for(pages, case.get("gold_paths"), excerpt_chars)
    if reach == 1:
        # The clue already names one target; there is nothing to disambiguate,
        # so neither the permission to pad nor the material to pad with.
        ambiguity, padding_rule = UNIQUE_NOTE.format(kind=kind), PAD_FORBIDDEN
    elif body:
        ambiguity = AMBIGUOUS_NOTE.format(n=reach) if reach > 1 else ""
        padding_rule = PAD_ALLOWED.format(kind=kind)
    else:
        # No excerpt to draw from. Telling the model to "pick one from the
        # excerpt below" when there is no excerpt is what drove the decline rate
        # to 95% — ask for a plain rewrite instead of an impossible one.
        ambiguity = AMBIGUOUS_NOTE.format(n=reach) if reach > 1 else ""
        padding_rule = PAD_NO_SOURCE
    excerpt = f"\n대상 페이지 발췌:\n---\n{body}\n---\n" if body else ""
    return PROMPT.format(question=case["question"],
                         detail=" / ".join(tokens) or "(없음)",
                         kind=kind, ambiguity=ambiguity,
                         padding_rule=padding_rule, excerpt=excerpt)


def mine(cases: list, model: str, ask=call_model, log=sys.stderr.write,
         pages=None, targets=None, excerpt_chars: int = 1500) -> tuple:
    """Return (emitted cases, rejection reasons) for the given direct cases."""
    emitted, rejects = [], []
    for case in cases:
        emitted.append(direct_case(case))
        tokens = detail_tokens(case.get("must_contain"))
        # Corpus check first: a case whose clue is not even on its own page is
        # broken at the source, and no rewrite can rescue it — refusing before
        # the model call also keeps the run from paying for it.
        broken, reach = clue_verdict(pages, targets, case.get("gold_paths"), tokens)
        if broken:
            rejects.append((case["id"], broken))
            continue
        try:
            reply = parse_reply(ask(model, build_prompt(case, pages, targets, excerpt_chars)))
        except Exception as exc:  # noqa: BLE001 - one case must not kill the run
            rejects.append((case["id"], f"model call failed: {exc}"))
            continue
        reason = reject_reason(case["question"], reply.get("subject", ""),
                               reply.get("reverse", ""), case.get("must_contain"))
        reason = reason or growth_reject(case["question"], reply.get("reverse", ""), reach)
        if reason:
            rejects.append((case["id"], reason))
            continue
        twin = reverse_case(case, reply["reverse"])
        if reach:
            # Kept on the case so a reviewer can see how hard the clue had to
            # work: reach=1 is pinned by the clue alone, higher needed the
            # model's added context to be answerable.
            twin["clue_reach"] = reach
        emitted.append(twin)
        log(f"  + {case['id']}-rev [reach={reach}]  {reply['reverse']}\n")
    return emitted, rejects


def load_wiki(wiki_dir: str) -> dict:
    """Read the wiki COPY into {relpath: text}. Read-only, whole corpus."""
    import pathlib
    root = pathlib.Path(wiki_dir)
    pages = {}
    for path in root.rglob("*.md"):
        try:
            pages[str(path.relative_to(root)).replace("\\", "/")] = path.read_text(
                encoding="utf-8")
        except OSError:
            continue
    return pages


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--gold", required=True, help="direct 골드 JSONL 경로")
    ap.add_argument("--model", default="glm-5.2", help="재작성 모델 (웜홀이 서빙 중이어야 함)")
    ap.add_argument("--limit", type=int, default=0, help="처음 N건만 (0=전부)")
    ap.add_argument("--out", default="", help="출력 JSONL (미지정 시 stdout)")
    ap.add_argument("--wiki", default="", help="위키 COPY 경로 — 단서 모호도 측정 + 발췌 주입")
    ap.add_argument("--excerpt-chars", type=int, default=1500, help="발췌 최대 길이")
    a = ap.parse_args(argv)

    pages = load_wiki(a.wiki) if a.wiki else None
    if pages:
        sys.stderr.write(f"== wiki {a.wiki}: {len(pages)} pages indexed\n")
    else:
        sys.stderr.write("== no --wiki: 단서 모호도는 모델 판단에만 의존한다\n")

    cases = load_gold(a.gold)
    if a.limit > 0:
        cases = cases[:a.limit]
    if not cases:
        sys.stderr.write(f"no gold cases in {a.gold}\n")
        return 1
    sys.stderr.write(f"== {len(cases)} direct cases → reverse via {a.model}\n")

    targets = gold_targets(cases)
    emitted, rejects = mine(cases, a.model, pages=pages, targets=targets,
                            excerpt_chars=a.excerpt_chars)

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
