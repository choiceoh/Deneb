#!/usr/bin/env python3
"""
LLM-as-Judge evaluator for Deneb gateway benchmarks.

Two evaluation modes:
  absolute  — Score a single (message, response) on 5 dimensions (0-10 each → 0-100 overall)
  pairwise  — Compare two responses, pick winner (A/B/tie)

Usage (CLI):
  python3 dev-bench-judge.py absolute --message "..." --response "..."
  python3 dev-bench-judge.py pairwise --message "..." --response-a "..." --response-b "..."

Usage (module):
  from dev_bench_judge import judge_absolute, judge_pairwise
  scores = judge_absolute("msg", "response")
  result = judge_pairwise("msg", "resp_a", "resp_b")

Output (CLI):
  metric_value=82
  DENEB_JUDGE_DETAIL helpfulness=9 accuracy=8 korean=8 tools=8 completeness=8

Environment:
  JUDGE_API_KEY    API key (default: $ANTHROPIC_API_KEY)
  JUDGE_MODEL      Model (default: claude-haiku-4-5-20251001 for speed)
  JUDGE_API_BASE   API base URL (default: https://api.anthropic.com)
                   Set to http://localhost:PORT/v1 for local model
"""

import argparse
import json
import os
import re
import sys
import urllib.request

# --- Judge Prompts ---

JUDGE_SYSTEM_ABSOLUTE = """\
You are an expert evaluator for Deneb, a Korean-first personal AI gateway on \
NVIDIA DGX Spark. It has 130+ tools (file ops, exec, grep, memory, git, etc.) \
and communicates via Telegram.

Score the assistant's response on each dimension (integer 0-10):

1. **helpfulness** — Did it actually help? Actionable, relevant, not evasive?
2. **accuracy** — Factually correct? Tool results interpreted right? No hallucination?
3. **korean_quality** — Natural Korean? Appropriate register? No awkward translation?
   (Score 7 if response is English but user asked in English; 0 if Korean expected but got English.)
4. **tool_usage** — Tools used appropriately? Not over/under-used? Results integrated well?
   (Score 7 if no tools were needed and none used.)
5. **completeness** — All parts of the question addressed? Nothing omitted?

Respond with ONLY a JSON object, no other text:
{"helpfulness": N, "accuracy": N, "korean_quality": N, "tool_usage": N, "completeness": N}\
"""

JUDGE_SYSTEM_PAIRWISE = """\
You are an expert evaluator comparing two AI assistant responses from Deneb, \
a Korean-first personal AI gateway on NVIDIA DGX Spark.

Compare Response A and Response B on overall quality: helpfulness, accuracy, \
Korean naturalness, tool usage appropriateness, and completeness.

Respond with ONLY a JSON object, no other text:
{"winner": "A", "confidence": 0.85, "reason": "A is more complete and natural"}\
(winner must be "A", "B", or "tie"; confidence 0.0-1.0)\
"""

# --- API Callers ---

def _call_anthropic(system: str, user_msg: str, api_key: str, model: str,
                    api_base: str) -> str:
    """Call Anthropic Messages API."""
    url = f"{api_base}/v1/messages"
    headers = {
        "Content-Type": "application/json",
        "x-api-key": api_key,
        "anthropic-version": "2023-06-01",
    }
    body = json.dumps({
        "model": model,
        "max_tokens": 256,
        "temperature": 0.0,
        "system": system,
        "messages": [{"role": "user", "content": user_msg}],
    }).encode()

    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=30) as resp:
        data = json.loads(resp.read())
    return data["content"][0]["text"]


def _call_openai_compat(system: str, user_msg: str, api_key: str, model: str,
                        api_base: str) -> str:
    """Call OpenAI-compatible API (local models, etc.)."""
    url = f"{api_base}/chat/completions"
    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}",
    }
    body = json.dumps({
        "model": model,
        "max_tokens": 256,
        "temperature": 0.0,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user_msg},
        ],
    }).encode()

    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=30) as resp:
        data = json.loads(resp.read())
    return data["choices"][0]["message"]["content"]


def _call_judge(system: str, user_msg: str) -> str:
    """Route to the right API based on config."""
    api_key = os.environ.get("JUDGE_API_KEY",
                             os.environ.get("ANTHROPIC_API_KEY", ""))
    model = os.environ.get("JUDGE_MODEL", "claude-haiku-4-5-20251001")
    api_base = os.environ.get("JUDGE_API_BASE", "https://api.anthropic.com")

    if not api_key:
        raise RuntimeError("No JUDGE_API_KEY or ANTHROPIC_API_KEY set")

    if "anthropic.com" in api_base:
        return _call_anthropic(system, user_msg, api_key, model, api_base)
    else:
        return _call_openai_compat(system, user_msg, api_key, model, api_base)


def _parse_json(raw: str) -> dict:
    """Extract first JSON object from raw text."""
    # Try strict parse first.
    raw = raw.strip()
    if raw.startswith("{"):
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            pass
    # Fallback: find JSON in text.
    match = re.search(r"\{[^{}]+\}", raw)
    if match:
        try:
            return json.loads(match.group())
        except json.JSONDecodeError:
            pass
    return {}


# --- Public API ---

_DIMENSIONS = ["helpfulness", "accuracy", "korean_quality", "tool_usage", "completeness"]
_DEFAULT_SCORES = {d: 5 for d in _DIMENSIONS}


def judge_available() -> bool:
    """Check if judge API is configured."""
    return bool(os.environ.get("JUDGE_API_KEY") or
                os.environ.get("ANTHROPIC_API_KEY"))


def judge_absolute(message: str, response: str, tool_info: str = "") -> dict:
    """Score a single response. Returns {dim: 0-10, ...}."""
    user_msg = f"## User Message\n{message}\n\n## Assistant Response\n{response}"
    if tool_info:
        user_msg += f"\n\n## Tool Usage\n{tool_info}"

    try:
        raw = _call_judge(JUDGE_SYSTEM_ABSOLUTE, user_msg)
        scores = _parse_json(raw)
        # Validate and clamp.
        result = {}
        for dim in _DIMENSIONS:
            val = scores.get(dim, 5)
            result[dim] = max(0, min(10, int(val)))
        return result
    except Exception as e:
        print(f"  judge error: {e}", file=sys.stderr)
        return dict(_DEFAULT_SCORES)


def judge_absolute_score(message: str, response: str, tool_info: str = "") -> float:
    """Score a single response. Returns 0-100 overall score."""
    scores = judge_absolute(message, response, tool_info)
    return sum(scores.values()) / len(scores) * 10  # 5 dims × 10 max → /5 × 10 = 0-100


def judge_pairwise(message: str, response_a: str, response_b: str) -> dict:
    """Compare two responses. Returns {winner, confidence, reason}."""
    user_msg = (
        f"## User Message\n{message}\n\n"
        f"## Response A\n{response_a}\n\n"
        f"## Response B\n{response_b}"
    )

    try:
        raw = _call_judge(JUDGE_SYSTEM_PAIRWISE, user_msg)
        result = _parse_json(raw)
        # Validate.
        winner = result.get("winner", "tie")
        if winner not in ("A", "B", "tie"):
            winner = "tie"
        return {
            "winner": winner,
            "confidence": max(0.0, min(1.0, float(result.get("confidence", 0.5)))),
            "reason": result.get("reason", ""),
        }
    except Exception as e:
        print(f"  judge error: {e}", file=sys.stderr)
        return {"winner": "tie", "confidence": 0.0, "reason": f"error: {e}"}


# --- CLI ---

def main():
    parser = argparse.ArgumentParser(description="LLM-as-Judge evaluator for Deneb benchmarks")
    sub = parser.add_subparsers(dest="mode", required=True)

    # Absolute mode.
    p_abs = sub.add_parser("absolute", help="Score a single response (0-100)")
    p_abs.add_argument("--message", required=True, help="User message")
    p_abs.add_argument("--response", required=True, help="Assistant response")
    p_abs.add_argument("--tools", default="", help="Tool usage info")

    # Pairwise mode.
    p_pw = sub.add_parser("pairwise", help="Compare two responses")
    p_pw.add_argument("--message", required=True, help="User message")
    p_pw.add_argument("--response-a", required=True, help="Response A")
    p_pw.add_argument("--response-b", required=True, help="Response B")

    # Check mode (just test if judge is available).
    sub.add_parser("check", help="Check if judge API is available")

    args = parser.parse_args()

    if args.mode == "check":
        ok = judge_available()
        print(f"judge_available={'yes' if ok else 'no'}")
        sys.exit(0 if ok else 1)

    if not judge_available():
        print("ERROR: No JUDGE_API_KEY or ANTHROPIC_API_KEY set", file=sys.stderr)
        print("metric_value=0")
        sys.exit(1)

    if args.mode == "absolute":
        scores = judge_absolute(args.message, args.response, args.tools)
        overall = sum(scores.values()) / len(scores) * 10
        detail = " ".join(f"{k}={v}" for k, v in scores.items())
        print(f"metric_value={overall:.0f}")
        print(f"DENEB_JUDGE_DETAIL {detail} overall={overall:.0f}")

    elif args.mode == "pairwise":
        result = judge_pairwise(args.message, args.response_a, args.response_b)
        # Pairwise metric: A wins=100, tie=50, B wins=0
        metric = {"A": 100, "tie": 50, "B": 0}[result["winner"]]
        print(f"metric_value={metric}")
        print(f"DENEB_JUDGE_DETAIL winner={result['winner']} "
              f"confidence={result['confidence']:.2f} reason={result['reason']}")


if __name__ == "__main__":
    main()
