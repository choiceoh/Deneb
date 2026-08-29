#!/usr/bin/env python3
"""LongMemEval reader stage: score the ANSWER, not the retrieval.

The Go bench (`internal/pipeline/chat/recall/longmemeval_bench_test.go`) scores
retrieval deterministically — did the evidence session get rendered. It cannot
say whether a model then *answers* correctly, and the two diverge: the ladder
from one-shot 53.0 to 77.0 was won almost entirely in the reader, by giving it
follow-up reads rather than by retrieving better.

This is the reader half. It consumes the retrieval snapshot the Go bench
freezes (`LONGMEMEVAL_EXPORT=...`) so a reader change is measured over the
EXACT same retrieval every run — swap readers over a frozen snapshot instead of
re-retrieving. Rebuilt 2026-08-29 after the original lived only in /tmp and was
lost with it; it is checked in for that reason.

## The escalation protocol

The reader replies with ONE directive per turn, as plain text. It is text and
not the tool-calling API on purpose: every model behind the wormhole proxy can
emit text, and tool-call support there is uneven.

    OPEN <ref>       read one past conversation in full
    SEARCH <query>   keyword-search every past conversation
    ANSWER <text>    final answer

Budgets are spent per question; when they run out the harness demands an
ANSWER. Windows are DEEP READS, not previews (10 messages x 4000B): the old
20x1200B window re-truncated the very message the reader had escalated to
reach, and the model said so ("items 1-21 only").

## Usage

    # 1. freeze a retrieval snapshot (Go bench)
    LONGMEMEVAL_DATA=~/.deneb/bench/longmemeval/longmemeval_s.json \
    LONGMEMEVAL_QIDS=~/.deneb/bench/longmemeval/half_qids.txt \
    LONGMEMEVAL_EXPORT=/tmp/snap.jsonl \
    DENEB_EMBEDDING_URL=http://127.0.0.1:8002 \
      go test ./internal/pipeline/chat/recall/ -run TestLongMemEvalRetrieval -timeout 60m

    # 2. score readers over it
    scripts/eval/reader_tool_eval.py --export /tmp/snap.jsonl \
        --data ~/.deneb/bench/longmemeval/longmemeval_s.json --out /tmp/r9

Numbers move less than they look: single-sentence reader levers measure below
+/-2.6 overall and +/-6 on multi-session run to run (sampled reader AND sampled
judge). Only mechanism-plus-large-signature levers are worth reporting, and a
paired run (same snapshot, same judge) beats comparing two runs taken apart.
"""

import argparse
import concurrent.futures
import json
import os
import pathlib
import re
import sys
import urllib.error
import urllib.request

WORMHOLE_URL = "http://127.0.0.1:18800/v1/chat/completions"
WORMHOLE_CFG = os.path.expanduser("~/.wormhole/config.json")

# Deep read, not preview (#4893): a window that re-truncates the message the
# reader escalated to reach defeats the escalation.
WINDOW_MESSAGES = 10
WINDOW_MSG_BYTES = 4000
WINDOW_TOTAL_BYTES = 24_000
SEARCH_MAX_RESULTS = 20
SEARCH_SNIPPET_BYTES = 400
# The judge models reason first; a small allowance yields an EMPTY verdict.
JUDGE_MAX_TOKENS = 3000

DEFAULT_OPEN_BUDGET = 2
DEFAULT_SEARCH_BUDGET = 2

READER_SYSTEM = """You answer questions about a user's past conversations.

You are given automatically retrieved evidence. It is a SUBSET: retrieval
renders only what it ranked highest, so for counting or listing questions the
rows you see may be missing instances. You can escalate.

Reply with EXACTLY ONE directive, as the first line of your reply:

  OPEN <ref>       read one past conversation in full. <ref> is the ref shown
                   on an evidence row (e.g. cl:lm:q7:s12#3/user or s12).
  SEARCH <query>   keyword-search every past conversation. Use it when no row
                   is relevant, AND before summing on a counting or listing
                   question — you cannot see what retrieval did not render.
  ANSWER <text>    your final answer.

Rules:
- Counting/listing: enumerate the matching conversations with their dates
  first, then count that list. If a conversation appears both as an evidence
  row and in search results, count it ONCE.
- If the evidence genuinely does not contain the answer, ANSWER that you do
  not know. A wrong specific answer is worse than an honest one.
- Answer in the question's language. Be brief: the answer, not an essay."""

JUDGE_SYSTEM = """You grade one answer against a reference answer.

Reply with exactly one line: CORRECT or INCORRECT, then a space, then a short
reason (under 15 words).

Grade the SUBSTANCE. Wording, format, extra correct detail, and units spelled
out differently do not matter. A specific claim that contradicts the reference
is INCORRECT. If the reference says the information is absent and the answer
says it does not know, that is CORRECT."""


# --- model transport --------------------------------------------------------- #
def wormhole_token() -> str:
    with open(WORMHOLE_CFG, encoding="utf-8") as fh:
        return os.path.expandvars(json.load(fh)["token"])


def list_models() -> list:
    """Model ids the wormhole is actually serving right now."""
    req = urllib.request.Request(
        WORMHOLE_URL.replace("/chat/completions", "/models"),
        headers={"Authorization": f"Bearer {wormhole_token()}"})
    with urllib.request.urlopen(req, timeout=15) as r:
        return [m["id"] for m in json.load(r).get("data", [])]


def check_models(names: list) -> list:
    """Refuse a model that cannot actually answer; flag the billed twins.

    Validation is a real one-token CALL, not a lookup in /v1/models. The
    listing lies: it advertised `deepseek-v4-flash` while every request came
    back `404 The model does not exist`, so a 236-question run burned itself
    out against a model that was listed but not served. A dead pin does not
    fail loudly on its own — it has to be probed.

    The `-api` suffixed ids are PAID cloud twins of local models; selecting one
    by accident has gone unnoticed for weeks before, so it stays loud even when
    the choice is deliberate.
    """
    warnings = []
    for name in dict.fromkeys(names):
        try:
            call_model(name, [{"role": "user", "content": "ok"}],
                       max_tokens=2000, timeout=120)
        except urllib.error.HTTPError as exc:
            raise SystemExit(
                f"model {name!r} does not answer: HTTP {exc.code}. "
                f"Listed models are not necessarily served — pick another.")
        except (urllib.error.URLError, OSError, KeyError, ValueError) as exc:
            raise SystemExit(f"model {name!r} probe failed: {exc}")
        if name.endswith("-api"):
            warnings.append(f"{name} is a BILLED cloud twin, not the local model")
    return warnings


def call_model(model: str, messages: list, max_tokens: int = 2000,
               timeout: int = 300) -> str:
    """One chat completion through the wormhole proxy. Raises on transport error."""
    payload = json.dumps({
        "model": model, "messages": messages,
        "max_tokens": max_tokens, "stream": False,
    }).encode()
    req = urllib.request.Request(
        WORMHOLE_URL, data=payload,
        headers={"Content-Type": "application/json",
                 "Authorization": f"Bearer {wormhole_token()}"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        resp = json.load(r)
    return (resp["choices"][0]["message"].get("content") or "").strip()


# --- protocol (pure) --------------------------------------------------------- #
# Markdown emphasis and fences ride on both sides of the verb ("**OPEN** s3",
# "`OPEN s3`"), so they are absorbed by the pattern rather than pre-stripped:
# stripping only the line's edges leaves the closing "**" glued to the argument.
DIRECTIVE_RE = re.compile(r"^[\s*#`>-]*(OPEN|SEARCH|ANSWER)\b[*`:\s]*(.*)$",
                          re.IGNORECASE | re.DOTALL)


def parse_directive(text: str):
    """First directive in the reply -> (verb, argument).

    Models wrap the directive in prose, code fences and markdown bold often
    enough that anchoring on line 1 alone loses answers that are really there.
    Scan lines, take the first that starts with a verb; fall back to treating
    the whole reply as an ANSWER, which is what an un-prefixed reply means.
    """
    if not text:
        return ("ANSWER", "")
    for raw in text.splitlines():
        m = DIRECTIVE_RE.match(raw.strip())
        if not m:
            continue
        verb = m.group(1).upper()
        arg = m.group(2).strip().strip("`\"'*").strip()
        if verb == "ANSWER":
            # The answer may continue past the first line.
            idx = text.find(raw)
            tail = text[idx + len(raw):].strip()
            return ("ANSWER", (arg + "\n" + tail).strip() if tail else arg)
        if arg:
            return (verb, arg)
    return ("ANSWER", text.strip())


SESSION_TAIL_RE = re.compile(r"s(\d+)")


def resolve_ref(ref: str, session_count: int):
    """Evidence-row ref -> haystack session index, or None.

    Refs reach here in every shape the recall renderer emits and the model
    echoes back: the full key (client:lm:<qid>:s12), the abbreviated one
    (cl:lm:...), an anchor and role tail (#3/user), a bare tail (s12), and —
    the one that cost a run — a summary row's ref with a trailing word
    ("cl:lm:q1:s12 요약"). Take the LAST sNN token before any anchor, which is
    the session in every one of those shapes.
    """
    if not ref:
        return None
    matches = SESSION_TAIL_RE.findall(ref.split("#")[0])
    if not matches:
        return None
    idx = int(matches[-1])
    return idx if 0 <= idx < session_count else None


def _clip(text: str, limit: int) -> str:
    raw = (text or "").strip()
    if limit <= 0:
        return ""
    if len(raw.encode()) <= limit:
        return raw
    return raw.encode()[:limit].decode("utf-8", "ignore") + "…(잘림)"


def render_window(turns: list, date: str, session_label: str, center: int = 0) -> str:
    """One past conversation as a deep-read window.

    The date rides in the header: opening a window used to ERASE the temporal
    context the evidence row carried, so the reader confirmed WHAT happened and
    lost WHEN (temporal-reasoning gained 7.5 when this was restored).
    """
    total = len(turns)
    start = max(0, min(center - WINDOW_MESSAGES // 2, total - WINDOW_MESSAGES))
    end = min(total, start + WINDOW_MESSAGES)
    head = (f'Session "{session_label}" ({date}) — messages {start + 1}..{end} '
            f'of {total}:\n')
    out, budget = [head], WINDOW_TOTAL_BYTES
    for i in range(start, end):
        turn = turns[i]
        body = _clip(turn.get("content", ""), min(WINDOW_MSG_BYTES, budget))
        line = f'[{i}] {turn.get("role", "?")}: {body}'
        budget -= len(line.encode())
        out.append(line)
        if budget <= 0:
            out.append("…(창 예산 소진)")
            break
    return "\n".join(out)


def search_sessions(sessions: list, dates: list, labels: list, query: str,
                    max_results: int = SEARCH_MAX_RESULTS) -> str:
    """Keyword search across every past conversation of one question.

    Headers carry the conversation date for the same reason windows do: a
    period-limited count has to filter the listed conversations by time, and
    match snippets rarely carry a date themselves.
    """
    terms = [t for t in re.split(r"\s+", (query or "").lower().strip()) if len(t) > 1]
    if not terms:
        return "검색어가 비어 있습니다."
    hits, remaining = [], max_results
    for si, turns in enumerate(sessions):
        if remaining <= 0:
            break
        matched = []
        for mi, turn in enumerate(turns):
            body = turn.get("content") or ""
            low = body.lower()
            if any(t in low for t in terms):
                matched.append((mi, turn.get("role", "?"), body))
                if len(matched) >= 3:
                    break
        if not matched:
            continue
        remaining -= 1
        lines = [f"### Session: {labels[si]} ({dates[si]})"]
        for mi, role, body in matched:
            lines.append(f"  [{mi}] {role}: {_clip(body, SEARCH_SNIPPET_BYTES)}")
        hits.append("\n".join(lines))
    if not hits:
        return f'"{query}" 와 일치하는 과거 대화가 없습니다.'
    return f'"{query}" 검색 결과 ({len(hits)}개 대화):\n\n' + "\n\n".join(hits)


# --- scoring (pure) ---------------------------------------------------------- #
def is_abstention(qid: str) -> bool:
    """LongMemEval marks the answer-absent questions with an _abs suffix.

    The Go bench SKIPS these before the export — a retrieval metric has nothing
    to score when there is no evidence session — so a snapshot normally
    contains none, and abstention ACCURACY is therefore not measured here. The
    `abstain_pct` this harness does report is a different quantity: how often
    the reader declined to answer, over the questions it did get. main() says
    so out loud rather than letting the gap pass for a measurement.
    """
    return qid.endswith("_abs")


def summarize(records: list) -> dict:
    """Accuracy overall, by question type, and the escalation/abstain mix."""
    by_type, opens, searches, abstained, correct, unscored = {}, 0, 0, 0, 0, 0
    for rec in records:
        qtype = rec.get("type") or "unknown"
        slot = by_type.setdefault(qtype, {"n": 0, "correct": 0})
        slot["n"] += 1
        if rec.get("verdict_label") == "UNSCORED":
            unscored += 1
        if rec.get("correct"):
            slot["correct"] += 1
            correct += 1
        opens += rec.get("opens", 0)
        searches += rec.get("searches", 0)
        if rec.get("abstained"):
            abstained += 1
    n = len(records)
    return {
        "n": n,
        "overall": round(100.0 * correct / n, 1) if n else 0.0,
        "by_type": {
            k: round(100.0 * v["correct"] / v["n"], 1) if v["n"] else 0.0
            for k, v in sorted(by_type.items())
        },
        "type_n": {k: v["n"] for k, v in sorted(by_type.items())},
        "opens": opens,
        "searches": searches,
        "abstain_pct": round(100.0 * abstained / n, 1) if n else 0.0,
        # Not folded into the accuracy: an ungraded question is a hole in the
        # measurement, and a run with holes must not read as a run with losses.
        "unscored": unscored,
    }


def format_summary(label: str, s: dict) -> str:
    lines = [f"=== {label} ===",
             f"overall {s['overall']}%  (n={s['n']})",
             f"opens {s['opens']}  searches {s['searches']}  "
             f"abstain {s['abstain_pct']}%"]
    if s.get("unscored"):
        lines.append(f"!! {s['unscored']} question(s) UNSCORED — the accuracy "
                     f"above is over {s['n']} including them; fix before "
                     f"reporting")
    for qtype, acc in s["by_type"].items():
        lines.append(f"  {qtype:<34} {acc:>5}%  (n={s['type_n'][qtype]})")
    return "\n".join(lines)


# --- run --------------------------------------------------------------------- #
def load_export(path: str) -> list:
    out = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            if line.strip():
                out.append(json.loads(line))
    return out


def load_haystacks(path: str, qids: set) -> dict:
    """qid -> {sessions, dates, labels} for the questions being scored."""
    with open(path, encoding="utf-8") as fh:
        questions = json.load(fh)
    out = {}
    for q in questions:
        qid = q["question_id"]
        if qid not in qids:
            continue
        sessions = q.get("haystack_sessions") or []
        out[qid] = {
            "sessions": sessions,
            "dates": q.get("haystack_dates") or [""] * len(sessions),
            "labels": [f"cl:lm:{qid}:s{i}" for i in range(len(sessions))],
        }
    return out


def serve_directive(verb: str, arg: str, hay: dict, opens: int, searches: int,
                    open_budget: int, search_budget: int) -> tuple:
    """(served_text, opens, searches) for one escalation step."""
    hay = hay or {}
    sessions = hay.get("sessions") or []
    dates = hay.get("dates") or [""] * len(sessions)
    labels = hay.get("labels") or [f"s{i}" for i in range(len(sessions))]
    if verb == "OPEN" and opens < open_budget:
        opens += 1
        idx = resolve_ref(arg, len(sessions))
        if idx is None:
            return (f'"{arg}" 는 열 수 있는 대화가 아니다. 다른 ref를 쓰거나 SEARCH하라.',
                    opens, searches)
        return (render_window(sessions[idx], dates[idx], labels[idx]),
                opens, searches)
    if verb == "SEARCH" and searches < search_budget:
        searches += 1
        return (search_sessions(sessions, dates, labels, arg), opens, searches)
    return ("그 escalation 예산은 소진됐다.", opens, searches)


def run_question(rec: dict, hay: dict, model: str, open_budget: int,
                 search_budget: int, verbose: bool = False) -> dict:
    """Drive one question through the escalation loop to a final answer."""
    prompt = (f"다음 질문에 답하라.\n\n질문: {rec['question']}\n\n"
              f"--- 자동 검색된 근거 ---\n{rec.get('block') or '(근거 없음)'}\n")
    messages = [{"role": "system", "content": READER_SYSTEM},
                {"role": "user", "content": prompt}]
    opens = searches = 0
    answer, error = "", ""
    for _ in range(open_budget + search_budget + 1):
        try:
            reply = call_model(model, messages)
        except (urllib.error.URLError, OSError, KeyError, ValueError) as exc:
            error = f"{type(exc).__name__}: {exc}"
            break
        verb, arg = parse_directive(reply)
        if verbose:
            print(f"    {rec['qid']} <- {verb} {arg[:70]}", file=sys.stderr)
        if verb == "ANSWER":
            answer = arg
            break
        messages.append({"role": "assistant", "content": reply})
        served, opens, searches = serve_directive(
            verb, arg, hay, opens, searches, open_budget, search_budget)
        if opens >= open_budget and searches >= search_budget:
            served += "\n\n예산이 모두 소진됐다. 이제 반드시 ANSWER로 답하라."
        messages.append({"role": "user", "content": served})
    if not answer and not error:
        error = "no answer produced"
    return {"qid": rec["qid"], "type": rec.get("type"), "question": rec["question"],
            "gold": rec.get("gold"), "answer": answer, "opens": opens,
            "searches": searches, "error": error}


def judge(rec: dict, judge_model: str) -> tuple:
    """(verdict, reason) where verdict is CORRECT | INCORRECT | UNSCORED.

    UNSCORED is a separate outcome on purpose. An unparseable verdict scored as
    INCORRECT silently DEFLATES every number the harness reports, and that is
    exactly what happened on the first real run: the judge models here reason
    before answering, a small max_tokens was spent entirely on reasoning, and
    the empty reply that came back read as "wrong" for answers that matched the
    reference word for word. A grader that cannot grade must say so.
    """
    if not rec.get("answer"):
        return ("UNSCORED", f"reader failed: {rec.get('error') or 'empty answer'}")
    user = (f"질문: {rec['question']}\n\n"
            f"기준 답(reference): {json.dumps(rec.get('gold'), ensure_ascii=False)}\n\n"
            f"채점할 답: {rec['answer']}")
    try:
        # Generous budget: these models reason before answering, and reasoning
        # tokens come out of the same allowance as the verdict.
        verdict = call_model(judge_model,
                             [{"role": "system", "content": JUDGE_SYSTEM},
                              {"role": "user", "content": user}], max_tokens=JUDGE_MAX_TOKENS)
    except (urllib.error.URLError, OSError, KeyError, ValueError) as exc:
        return ("UNSCORED", f"judge failed: {type(exc).__name__}: {exc}")
    return parse_verdict(verdict)


def parse_verdict(verdict: str) -> tuple:
    """Judge reply -> (CORRECT | INCORRECT | UNSCORED, reason)."""
    text = (verdict or "").strip()
    if not text:
        return ("UNSCORED", "judge returned an empty verdict")
    upper = text.upper()
    # Scan for the token rather than requiring it first: the models prepend
    # a sentence often enough that anchoring on position loses real verdicts.
    pos_c, pos_i = upper.find("CORRECT"), upper.find("INCORRECT")
    if pos_i >= 0 and (pos_c < 0 or pos_i <= pos_c):
        return ("INCORRECT", text[:120])
    if pos_c >= 0:
        return ("CORRECT", text[:120])
    return ("UNSCORED", f"no verdict token in: {text[:100]}")


ABSTAIN_MARKERS = ("모르", "없습니다", "없음", "not know", "no information",
                   "cannot determine")


def looks_abstained(answer: str) -> bool:
    low = (answer or "").lower()
    return any(m in low for m in ABSTAIN_MARKERS)


def _progress(done: int, total: int, rec: dict) -> None:
    mark = {"CORRECT": "o", "INCORRECT": "x"}.get(rec.get("verdict_label"), "?")
    print(f"[{done}/{total}] {mark} {rec['qid']} "
          f"(open {rec['opens']} search {rec['searches']})", file=sys.stderr)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--export", required=True,
                    help="retrieval snapshot JSONL (LONGMEMEVAL_EXPORT)")
    ap.add_argument("--data", required=True, help="longmemeval_s.json")
    ap.add_argument("--model", default="deepseek-v4-flash",
                    help="reader model (must be served by the wormhole)")
    ap.add_argument("--judge-model", default="deepseek-v4-flash")
    ap.add_argument("--out", help="directory for results.jsonl + summary.txt")
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--open-budget", type=int, default=DEFAULT_OPEN_BUDGET)
    ap.add_argument("--search-budget", type=int, default=DEFAULT_SEARCH_BUDGET)
    ap.add_argument("--workers", type=int, default=1,
                    help="concurrent questions (default 1; see the note in main)")
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    for warning in check_models([args.model, args.judge_model]):
        print(f"warning: {warning}", file=sys.stderr)

    records = load_export(args.export)
    if args.limit:
        records = records[:args.limit]
    if not records:
        print("export is empty — did the Go bench run with LONGMEMEVAL_EXPORT?",
              file=sys.stderr)
        return 2
    if not any(is_abstention(r["qid"]) for r in records):
        print("note: snapshot carries no _abs questions (the Go bench skips "
              "them), so abstention ACCURACY is unmeasured; abstain_pct below "
              "is the reader's decline RATE, not a score", file=sys.stderr)
    haystacks = load_haystacks(args.data, {r["qid"] for r in records})
    missing = [r["qid"] for r in records if r["qid"] not in haystacks]
    if missing:
        print(f"warning: {len(missing)} qids absent from --data "
              f"(OPEN/SEARCH unavailable for them)", file=sys.stderr)

    def score_one(rec: dict) -> dict:
        out = run_question(rec, haystacks.get(rec["qid"]), args.model,
                           args.open_budget, args.search_budget, args.verbose)
        label, reason = judge(out, args.judge_model)
        out["verdict_label"] = label
        out["correct"] = label == "CORRECT"
        out["verdict"] = reason
        out["abstained"] = looks_abstained(out["answer"])
        return out

    # Questions are independent and every one of them is latency-bound on the
    # wormhole, so workers buy wall clock almost linearly. Serial stays the
    # DEFAULT because concurrency is not free of measurement risk in general —
    # the Go bench measures a different configuration under load, when its
    # rerank sidecar times out and silently falls back. Here that class of
    # error cannot hide: a call that fails under load becomes UNSCORED and is
    # reported, not folded into the accuracy as a wrong answer.
    results = [None] * len(records)
    done = 0
    if args.workers > 1:
        with concurrent.futures.ThreadPoolExecutor(args.workers) as pool:
            futures = {pool.submit(score_one, rec): i
                       for i, rec in enumerate(records)}
            for fut in concurrent.futures.as_completed(futures):
                i = futures[fut]
                results[i] = fut.result()
                done += 1
                _progress(done, len(records), results[i])
    else:
        for i, rec in enumerate(records):
            results[i] = score_one(rec)
            _progress(i + 1, len(records), results[i])

    summary = summarize(results)
    text = format_summary(f"{args.model} over {pathlib.Path(args.export).name}",
                          summary)
    print("\n" + text)
    if args.out:
        outdir = pathlib.Path(args.out)
        outdir.mkdir(parents=True, exist_ok=True)
        with (outdir / "results.jsonl").open("w", encoding="utf-8") as fh:
            for r in results:
                fh.write(json.dumps(r, ensure_ascii=False) + "\n")
        (outdir / "summary.txt").write_text(text + "\n", encoding="utf-8")
        (outdir / "summary.json").write_text(
            json.dumps(summary, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8")
        print(f"\nwrote {outdir}/results.jsonl, summary.txt, summary.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())
