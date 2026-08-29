#!/usr/bin/env python3
"""Wiki recall reader stage: score the ANSWER, not the page hit.

The Korean probe (`recall/korean_probe_test.go`) scores RETRIEVAL — did the
recall block carry the gold page. It reached 98.0%, and that number says less
than it looks: on the session plane the same pipeline held retrieval at 93–94%
while answer accuracy sat at 44.5%, and every point won afterwards was won in
the reading stage, with the retrieval metric barely moving. A page-hit rate
cannot tell you whether the model then answered correctly from what was
rendered.

This is the missing half. It consumes the snapshot the probe freezes
(`DENEB_PROBE_EXPORT=...`) so a reading change is measured over the EXACT same
retrieval every run.

## The gold set was already built for this

`~/.deneb/wiki-qa-gold.jsonl` carries `must_contain` — the tokens an answer has
to have — on 117 of its 198 cases, and has since it was written. Nothing read
them: the probe's case struct parsed `question` and `gold_paths` and dropped
the rest. So the wiki plane has had answer-level ground truth sitting unused.
Scoring here is deterministic first (must_contain / must_not, alternatives
separated by "|") and falls back to an LLM judge only where tokens are absent.

## Ceiling

`--oracle` hands over the gold pages whole. That is the ceiling for a given
reader, and it has to be re-measured whenever the reader or judge changes — a
ceiling recorded under one reader says nothing about another. On the session
plane a stale ceiling would have turned a 4-point gap-closing move into a
reported "breakthrough".

## Usage

    # 1. freeze a retrieval snapshot (Go probe, against a COPY of the wiki)
    DENEB_WIKI_DIR=/tmp/wiki-copy DENEB_WIKI_GOLD=~/.deneb/wiki-qa-gold.jsonl \
    DENEB_EMBEDDING_URL=http://127.0.0.1:8002 \
    DENEB_RERANK_URL=http://127.0.0.1:8004 DENEB_RERANK_MODEL=xprovence-bgem3-v2 \
    DENEB_PROBE_EXPORT=/tmp/wiki-snap.jsonl \
      go test ./internal/pipeline/chat/recall/ -run TestKoreanRecallProbe

    # 2. score answers over it
    scripts/eval/wiki_reader_eval.py --export /tmp/wiki-snap.jsonl \
        --wiki /tmp/wiki-copy --out /tmp/wiki-base
    scripts/eval/wiki_reader_eval.py ... --oracle --out /tmp/wiki-ceiling
"""

import argparse
import json
import os
import pathlib
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

# Transport, protocol and aggregation are shared with the session harness on
# purpose: the retry, the UNSCORED verdict and the model probe are what keep a
# long run honest, and duplicating them would let the two drift.
from reader_tool_eval import (  # noqa: E402
    call_model, check_models, format_summary, parse_verdict, summarize,
)

PAGE_BYTES = 16_000
# The oracle is meant to be generous — it is the ceiling, not a budget test.
ORACLE_TOTAL_BYTES = 60_000
READ_BUDGET = 2

READER_SYSTEM = """You answer questions about the user's work wiki.

You are given automatically retrieved evidence. It is a SUBSET: retrieval
renders only the passages it ranked highest, so the answering fact may have
been cut even when the right page is present.

Reply with EXACTLY ONE directive, as the first line of your reply:

  READ <path>      read one wiki page in full. <path> is the ref shown on an
                   evidence row (e.g. 프로젝트/pl2-dsv-epc-001).
  ANSWER <text>    your final answer.

Rules:
- If an evidence row is from the right page but the excerpt does not contain
  the answer, READ that page rather than guessing from the excerpt.
- If the evidence genuinely does not contain the answer, ANSWER that you do
  not know. A wrong specific answer is worse than an honest one.
- Answer in Korean. Be brief: the answer, not an essay."""

JUDGE_SYSTEM = """You grade one answer to a question about a work wiki.

Reply with exactly one line: CORRECT or INCORRECT, then a space, then a short
reason (under 15 words).

Grade the SUBSTANCE against the reference. Wording, formatting and extra
correct detail do not matter. A specific claim that contradicts the reference
is INCORRECT. An honest "I don't know" is INCORRECT unless the reference says
the information is absent."""


# --- evidence (pure) --------------------------------------------------------- #
def load_snapshot(path: str) -> list:
    out = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            if line.strip():
                out.append(json.loads(line))
    return out


def resolve_page(wiki_dir: str, ref: str):
    """Evidence-row ref -> a real page path under the wiki, or None.

    Refs reach here as wiki-relative paths, sometimes with the `w:` prefix the
    knowledge tool uses, sometimes with a `.md` suffix and sometimes without.
    Everything is resolved under the wiki root and rejected if it escapes —
    a ref comes from model output and must not be able to read outside.
    """
    if not ref or not wiki_dir:
        return None
    candidate = ref.strip().strip("\"'`").removeprefix("w:").strip()
    if not candidate:
        return None
    root = pathlib.Path(wiki_dir).resolve()
    for name in (candidate, candidate + ".md"):
        try:
            target = (root / name).resolve()
        except (OSError, ValueError):
            continue
        if root not in target.parents and target != root:
            continue  # escaped the wiki root
        if target.is_file():
            return target
    return None


def render_page(path: pathlib.Path, limit: int = PAGE_BYTES) -> str:
    try:
        body = path.read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        return f"({path.name} 읽기 실패: {exc})"
    raw = body.encode()
    if len(raw) > limit:
        body = raw[:limit].decode("utf-8", "ignore") + "\n…(페이지 잘림)"
    return f"### {path.name}\n{body}"


def resolve_gold_pages(wiki_dir: str, ref: str) -> list:
    """Every page a gold_paths entry designates, most important first.

    gold_paths in this set are path SUBSTRINGS, and a third of them name a
    project FOLDER rather than a page: "프로젝트/pl2-dsv-epc-001" covers 대표,
    로그, o&m-입찰, 회의록/… — seven pages and more. Serving an arbitrary one
    of them is not an oracle, and it showed: the first oracle run scored BELOW
    the retrieval run (70.2 vs 79.8), an ordering no mechanism explains,
    because rglob order handed the reader 로그.md while the answer sat in 대표.md.

    Ordering puts an exact hit first, then 대표 (the project's summary page),
    then the rest by path, so the budget cuts the least important pages.
    """
    exact = resolve_page(wiki_dir, ref)
    if exact is not None:
        return [exact]
    root = pathlib.Path(wiki_dir)
    if not root.is_dir():
        return []
    matches = [p for p in root.rglob("*.md") if ref in str(p.relative_to(root))]
    matches.sort(key=lambda p: (p.stem != "대표", str(p)))
    return matches


def oracle_block(wiki_dir: str, gold_paths: list) -> str:
    """The gold pages handed over whole — the ceiling setting."""
    rendered, budget, seen = [], ORACLE_TOTAL_BYTES, set()
    for ref in gold_paths or []:
        for page in resolve_gold_pages(wiki_dir, ref):
            if page in seen or budget <= 0:
                continue
            seen.add(page)
            text = render_page(page, min(PAGE_BYTES, budget))
            budget -= len(text.encode())
            rendered.append(text)
    return "\n\n".join(rendered) if rendered else "(근거 페이지 없음)"


# --- deterministic scoring (pure) -------------------------------------------- #
def token_verdict(answer: str, must_contain: list, must_not: list):
    """(verdict, reason) from the gold tokens, or (None, "") when there are none.

    Deterministic scoring is preferred over the judge wherever the gold set
    supplies tokens: it costs nothing, cannot drift, and cannot be lenient on
    its own output. "a|b" means either alternative satisfies the requirement.
    """
    if not must_contain and not must_not:
        return (None, "")
    text = (answer or "")
    for group in must_contain or []:
        if not any(alt.strip() and alt.strip() in text for alt in str(group).split("|")):
            return ("INCORRECT", f"missing required token: {group}")
    for banned in must_not or []:
        if str(banned).strip() and str(banned).strip() in text:
            return ("INCORRECT", f"contains forbidden token: {banned}")
    return ("CORRECT", "all required tokens present")


def parse_directive(text: str):
    """First READ/ANSWER directive in the reply -> (verb, argument)."""
    if not text:
        return ("ANSWER", "")
    for raw in text.splitlines():
        line = raw.strip().lstrip("*# `>-").rstrip("`*")
        upper = line.upper()
        for verb in ("READ", "ANSWER"):
            if upper.startswith(verb):
                arg = line[len(verb):].strip().strip(":`\"'*").strip()
                if verb == "ANSWER":
                    idx = text.find(raw)
                    tail = text[idx + len(raw):].strip()
                    return ("ANSWER", (arg + "\n" + tail).strip() if tail else arg)
                if arg:
                    return ("READ", arg)
    return ("ANSWER", text.strip())


# --- run --------------------------------------------------------------------- #
def run_case(rec: dict, wiki_dir: str, model: str, read_budget: int,
             use_oracle: bool, verbose: bool = False) -> dict:
    evidence = (oracle_block(wiki_dir, rec.get("gold_paths"))
                if use_oracle else (rec.get("block") or "(근거 없음)"))
    messages = [{"role": "system", "content": READER_SYSTEM},
                {"role": "user", "content":
                 f"다음 질문에 답하라.\n\n질문: {rec['question']}\n\n"
                 f"--- 자동 검색된 근거 ---\n{evidence}\n"}]
    reads, answer, error = 0, "", ""
    for _ in range(read_budget + 1):
        try:
            reply = call_model(model, messages)
        except Exception as exc:  # noqa: BLE001 - reported, never scored wrong
            error = f"{type(exc).__name__}: {exc}"
            break
        verb, arg = parse_directive(reply)
        if verbose:
            print(f"    {rec.get('id')} <- {verb} {arg[:60]}", file=sys.stderr)
        if verb == "ANSWER":
            answer = arg
            break
        messages.append({"role": "assistant", "content": reply})
        if reads < read_budget:
            reads += 1
            target = resolve_page(wiki_dir, arg)
            served = (render_page(target) if target is not None
                      else f'"{arg}" 는 열 수 있는 페이지가 아니다. 근거의 ref를 그대로 쓰라.')
        else:
            served = "열람 예산이 소진됐다."
        if reads >= read_budget:
            served += "\n\n예산이 소진됐다. 이제 반드시 ANSWER로 답하라."
        messages.append({"role": "user", "content": served})
    if not answer and not error:
        try:
            messages.append({"role": "user", "content":
                             "더 이상 열람할 수 없다. 지금까지 본 것만으로 "
                             "'ANSWER '로 시작하는 한 줄로 답하라."})
            verb, arg = parse_directive(call_model(model, messages))
            answer = arg if verb == "ANSWER" else ""
        except Exception as exc:  # noqa: BLE001
            error = f"forced answer failed: {type(exc).__name__}: {exc}"
    return {"id": rec.get("id"), "category": rec.get("category"),
            "question": rec["question"], "gold_paths": rec.get("gold_paths"),
            "must_contain": rec.get("must_contain"), "answer": answer,
            "reads": reads, "gold_in_block": rec.get("gold_in_block"),
            "error": error}


def judge_case(out: dict, judge_model: str) -> tuple:
    if not out.get("answer"):
        return ("UNSCORED", f"reader failed: {out.get('error') or 'empty answer'}")
    verdict, reason = token_verdict(out["answer"], out.get("must_contain"),
                                    out.get("must_not"))
    if verdict is not None:
        return (verdict, "tokens: " + reason)
    user = (f"질문: {out['question']}\n\n"
            f"기준(정답이 있는 위키 경로): {out.get('gold_paths')}\n\n"
            f"채점할 답: {out['answer']}")
    try:
        return parse_verdict(call_model(
            judge_model, [{"role": "system", "content": JUDGE_SYSTEM},
                          {"role": "user", "content": user}], max_tokens=3000))
    except Exception as exc:  # noqa: BLE001
        return ("UNSCORED", f"judge failed: {type(exc).__name__}: {exc}")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--export", required=True, help="probe snapshot JSONL")
    ap.add_argument("--wiki", required=True, help="wiki root (use a COPY)")
    ap.add_argument("--model", default="deepseek-v4-flash-api")
    ap.add_argument("--judge-model", default="deepseek-v4-flash-api")
    ap.add_argument("--oracle", action="store_true",
                    help="hand over the gold pages whole (ceiling run)")
    ap.add_argument("--read-budget", type=int, default=READ_BUDGET)
    ap.add_argument("--workers", type=int, default=1)
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--out")
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    for warning in check_models([args.model, args.judge_model]):
        print(f"warning: {warning}", file=sys.stderr)
    records = load_snapshot(args.export)
    if args.limit:
        records = records[:args.limit]
    if not records:
        print("snapshot is empty — did the probe run with DENEB_PROBE_EXPORT?",
              file=sys.stderr)
        return 2
    tokened = sum(1 for r in records if r.get("must_contain"))
    print(f"{len(records)} cases; {tokened} scored deterministically by tokens, "
          f"{len(records) - tokened} by the judge", file=sys.stderr)

    def score_one(rec):
        out = run_case(rec, args.wiki, args.model, args.read_budget,
                       args.oracle, args.verbose)
        label, reason = judge_case(out, args.judge_model)
        out["verdict_label"] = label
        out["correct"] = label == "CORRECT"
        out["verdict"] = reason
        # summarize() groups by "type"; category is this plane's type.
        out["type"] = out.get("category") or "unknown"
        out["opens"], out["searches"] = out["reads"], 0
        return out

    results = [None] * len(records)
    if args.workers > 1:
        import concurrent.futures
        with concurrent.futures.ThreadPoolExecutor(args.workers) as pool:
            futures = {pool.submit(score_one, r): i for i, r in enumerate(records)}
            for done, fut in enumerate(concurrent.futures.as_completed(futures), 1):
                results[futures[fut]] = fut.result()
                _progress(done, len(records), results[futures[fut]])
    else:
        for i, rec in enumerate(records):
            results[i] = score_one(rec)
            _progress(i + 1, len(records), results[i])

    summary = summarize(results)
    label = f"wiki {'oracle' if args.oracle else 'retrieval'} · {args.model}"
    text = format_summary(label, summary)
    print("\n" + text)
    if args.out:
        outdir = pathlib.Path(args.out)
        outdir.mkdir(parents=True, exist_ok=True)
        with (outdir / "results.jsonl").open("w", encoding="utf-8") as fh:
            for r in results:
                fh.write(json.dumps(r, ensure_ascii=False) + "\n")
        (outdir / "summary.txt").write_text(text + "\n", encoding="utf-8")
        print(f"\nwrote {outdir}/results.jsonl, summary.txt")
    return 0


def _progress(done: int, total: int, rec: dict) -> None:
    mark = {"CORRECT": "o", "INCORRECT": "x"}.get(rec.get("verdict_label"), "?")
    print(f"[{done}/{total}] {mark} {rec.get('id')} (read {rec['reads']})",
          file=sys.stderr)


if __name__ == "__main__":
    sys.exit(main())
