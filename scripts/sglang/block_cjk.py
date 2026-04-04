#!/usr/bin/env python3
"""Block CJK Unified Ideographs from sglang model output.

Scans a model's tokenizer to find all tokens containing CJK characters
(U+4E00-U+9FFF and extended ranges) and outputs their IDs. These can be
used as logit_bias=-100 in requests or loaded as a server-side processor.

Usage:
    # Scan tokenizer and save blocked token IDs:
    python block_cjk.py scan Qwen/Qwen3.5-35B-A3B

    # Scan with extended ranges (CJK Ext-A/B, Compatibility, Radicals):
    python block_cjk.py scan Qwen/Qwen3.5-35B-A3B --extended

    # Verify Korean is unaffected:
    python block_cjk.py verify Qwen/Qwen3.5-35B-A3B

    # Show stats only:
    python block_cjk.py stats Qwen/Qwen3.5-35B-A3B
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

# CJK Unicode ranges. Tokens containing characters in these ranges are blocked.
# Korean Hangul (U+AC00-U+D7AF, U+1100-U+11FF, U+3130-U+318F) is NOT included.
CJK_RANGES_CORE = [
    (0x4E00, 0x9FFF),   # CJK Unified Ideographs (main block)
]

CJK_RANGES_EXTENDED = [
    (0x3400, 0x4DBF),   # CJK Extension A
    (0x20000, 0x2A6DF), # CJK Extension B
    (0x2A700, 0x2B73F), # CJK Extension C
    (0x2B740, 0x2B81F), # CJK Extension D
    (0xF900, 0xFAFF),   # CJK Compatibility Ideographs
    (0x2F800, 0x2FA1F), # CJK Compat Ideographs Supplement
    (0x2E80, 0x2EFF),   # CJK Radicals Supplement
    (0x3100, 0x312F),   # Bopomofo (Chinese phonetic)
    (0x31A0, 0x31BF),   # Bopomofo Extended
]

# Korean ranges to explicitly preserve (safety check).
KOREAN_RANGES = [
    (0xAC00, 0xD7AF),   # Hangul Syllables
    (0x1100, 0x11FF),   # Hangul Jamo
    (0x3130, 0x318F),   # Hangul Compatibility Jamo
    (0xA960, 0xA97F),   # Hangul Jamo Extended-A
    (0xD7B0, 0xD7FF),   # Hangul Jamo Extended-B
    (0xFFA0, 0xFFDC),   # Halfwidth Hangul
]


def in_ranges(ch: str, ranges: list[tuple[int, int]]) -> bool:
    cp = ord(ch)
    return any(lo <= cp <= hi for lo, hi in ranges)


def is_cjk(ch: str, extended: bool = False) -> bool:
    ranges = CJK_RANGES_CORE + (CJK_RANGES_EXTENDED if extended else [])
    return in_ranges(ch, ranges)


def is_korean(ch: str) -> bool:
    return in_ranges(ch, KOREAN_RANGES)


def scan_tokenizer(model_name: str, extended: bool = False) -> dict:
    """Scan tokenizer and classify tokens by CJK/Korean content."""
    from transformers import AutoTokenizer

    print(f"Loading tokenizer: {model_name}", file=sys.stderr)
    tokenizer = AutoTokenizer.from_pretrained(model_name, trust_remote_code=True)
    vocab_size = tokenizer.vocab_size

    blocked_ids: list[int] = []
    korean_ids: list[int] = []
    mixed_ids: list[int] = []  # tokens with both CJK and Korean chars

    for tid in range(vocab_size):
        try:
            decoded = tokenizer.decode([tid])
        except Exception:
            continue

        has_cjk = any(is_cjk(ch, extended) for ch in decoded)
        has_korean = any(is_korean(ch) for ch in decoded)

        if has_cjk and has_korean:
            # Mixed token (e.g., "서울大") -- do NOT block, Korean needs it.
            mixed_ids.append(tid)
        elif has_cjk:
            blocked_ids.append(tid)
        elif has_korean:
            korean_ids.append(tid)

    return {
        "model": model_name,
        "vocab_size": vocab_size,
        "extended": extended,
        "blocked_ids": blocked_ids,
        "korean_ids": korean_ids,
        "mixed_ids": mixed_ids,
    }


def cmd_scan(args: argparse.Namespace) -> None:
    result = scan_tokenizer(args.model, extended=args.extended)

    blocked = result["blocked_ids"]
    korean = result["korean_ids"]
    mixed = result["mixed_ids"]
    vocab = result["vocab_size"]

    print(f"\n=== CJK Token Scan: {args.model} ===", file=sys.stderr)
    print(f"  Vocab size:       {vocab:,}", file=sys.stderr)
    print(f"  CJK blocked:      {len(blocked):,} ({len(blocked)/vocab*100:.1f}%)", file=sys.stderr)
    print(f"  Korean preserved:  {len(korean):,}", file=sys.stderr)
    print(f"  Mixed (kept):      {len(mixed):,}", file=sys.stderr)
    print(f"  Extended ranges:   {'yes' if args.extended else 'no'}", file=sys.stderr)

    # Output JSON.
    output = {
        "model": args.model,
        "vocab_size": vocab,
        "extended": args.extended,
        "blocked_count": len(blocked),
        "korean_count": len(korean),
        "mixed_count": len(mixed),
        "blocked_token_ids": blocked,
    }

    out_path = args.output or Path("scripts/sglang/cjk_tokens.json")
    out_path = Path(out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(output, separators=(",", ":")))
    print(f"\n  Saved {len(blocked):,} blocked token IDs to {out_path}", file=sys.stderr)


def cmd_verify(args: argparse.Namespace) -> None:
    """Verify that blocking CJK tokens does not affect Korean output."""
    from transformers import AutoTokenizer

    tokenizer = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)

    # Load blocked IDs.
    token_file = Path(args.token_file or "scripts/sglang/cjk_tokens.json")
    if not token_file.exists():
        print(f"Token file not found: {token_file}", file=sys.stderr)
        print("Run 'scan' first.", file=sys.stderr)
        sys.exit(1)

    data = json.loads(token_file.read_text())
    blocked = set(data["blocked_token_ids"])

    # Test Korean sentences.
    test_sentences = [
        "안녕하세요, 반갑습니다.",
        "오늘 날씨가 좋네요.",
        "서울특별시 강남구",
        "인공지능 기술이 발전하고 있습니다.",
        "대한민국의 수도는 서울입니다.",
        "프로그래밍을 배우고 싶어요.",
    ]

    print(f"\n=== Korean Verification ===", file=sys.stderr)
    all_ok = True
    for sentence in test_sentences:
        token_ids = tokenizer.encode(sentence)
        blocked_in_sentence = [tid for tid in token_ids if tid in blocked]
        ok = len(blocked_in_sentence) == 0

        status = "OK" if ok else "BLOCKED"
        print(f"  [{status}] {sentence}", file=sys.stderr)
        if not ok:
            all_ok = False
            for tid in blocked_in_sentence:
                decoded = tokenizer.decode([tid])
                print(f"         token {tid}: '{decoded}'", file=sys.stderr)

    # Test Chinese sentences (should be fully blocked).
    chinese_sentences = [
        "你好世界",
        "今天天气很好",
        "人工智能技术",
    ]

    print(f"\n=== Chinese Block Verification ===", file=sys.stderr)
    for sentence in chinese_sentences:
        token_ids = tokenizer.encode(sentence)
        blocked_in_sentence = [tid for tid in token_ids if tid in blocked]
        coverage = len(blocked_in_sentence) / max(len(token_ids), 1) * 100
        print(f"  {sentence} -> {coverage:.0f}% blocked ({len(blocked_in_sentence)}/{len(token_ids)} tokens)", file=sys.stderr)

    if all_ok:
        print(f"\nAll Korean sentences unaffected.", file=sys.stderr)
    else:
        print(f"\nWARNING: Some Korean tokens would be blocked!", file=sys.stderr)
        sys.exit(1)


def cmd_stats(args: argparse.Namespace) -> None:
    result = scan_tokenizer(args.model, extended=args.extended)
    blocked = result["blocked_ids"]
    print(f"CJK blocked: {len(blocked):,} / {result['vocab_size']:,} "
          f"({len(blocked)/result['vocab_size']*100:.1f}%)")
    print(f"Korean preserved: {len(result['korean_ids']):,}")
    print(f"Mixed (kept): {len(result['mixed_ids']):,}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Block CJK tokens from sglang output")
    sub = parser.add_subparsers(dest="command", required=True)

    # scan
    p_scan = sub.add_parser("scan", help="Scan tokenizer and save blocked IDs")
    p_scan.add_argument("model", help="HuggingFace model name or path")
    p_scan.add_argument("--extended", action="store_true", help="Include CJK Extension A/B, Compatibility, Radicals")
    p_scan.add_argument("--output", "-o", help="Output JSON path (default: scripts/sglang/cjk_tokens.json)")

    # verify
    p_verify = sub.add_parser("verify", help="Verify Korean is unaffected by blocking")
    p_verify.add_argument("model", help="HuggingFace model name or path")
    p_verify.add_argument("--token-file", help="Path to cjk_tokens.json")

    # stats
    p_stats = sub.add_parser("stats", help="Show token classification stats")
    p_stats.add_argument("model", help="HuggingFace model name or path")
    p_stats.add_argument("--extended", action="store_true")

    args = parser.parse_args()
    {"scan": cmd_scan, "verify": cmd_verify, "stats": cmd_stats}[args.command](args)


if __name__ == "__main__":
    main()
