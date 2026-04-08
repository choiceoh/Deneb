"""
Shared validation checks for Deneb quality testing.

All functions return tuple[bool, str] — (passed, detail).
Both quality-test.py and reproduce.py import from this module.
"""

import re


def check_korean_response(text: str) -> tuple[bool, str]:
    """Check response language is Korean or English (rejects other languages)."""
    # Strip fenced code blocks and inline code which are inherently English.
    prose = re.sub(r"```[\s\S]*?```", "", text)
    prose = re.sub(r"`[^`]+`", "", prose)
    korean_chars = len(re.findall(r"[\uac00-\ud7af\u1100-\u11ff\u3130-\u318f]", prose))
    english_chars = len(re.findall(r"[a-zA-Z]", prose))
    ko_en = korean_chars + english_chars
    total_alpha = sum(1 for c in prose if c.isalpha())
    if total_alpha == 0:
        return True, "no alphabetic content (ok)"
    ratio = ko_en / total_alpha
    if ratio > 0.7:
        return True, f"ko+en: {ratio:.0%} (ko={korean_chars}, en={english_chars})"
    return False, f"ko+en ratio too low: {ratio:.0%} ({ko_en}/{total_alpha})"


# Patterns for leaked internal markup.
_LEAKED_MARKUP_PATTERNS = [
    (r"<function=", "leaked <function= tag"),
    (r"</?thinking>", "leaked thinking tag"),
    (r"</?artifact", "leaked artifact tag"),
    (r"\[\[reply_to", "leaked reply directive"),
    (r"MEDIA:\S+", "leaked MEDIA token"),
    (r"NO_REPLY", "leaked NO_REPLY token"),
    (r"SILENT_REPLY", "leaked SILENT_REPLY token"),
]


def check_no_leaked_markup(text: str) -> tuple[bool, str]:
    """Check no internal tokens leaked into the response."""
    for pat, desc in _LEAKED_MARKUP_PATTERNS:
        if re.search(pat, text):
            return False, desc
    return True, "clean"


def check_telegram_safe(text: str) -> tuple[bool, str]:
    """Check response is safe for Telegram delivery."""
    issues = []
    if len(text) > 4096:
        issues.append(f"exceeds 4096 char limit ({len(text)} chars)")
    open_tags = re.findall(r"<(b|i|code|pre|s|u|a|blockquote|tg-spoiler)[\s>]", text)
    close_tags = re.findall(r"</(b|i|code|pre|s|u|a|blockquote|tg-spoiler)>", text)
    if len(open_tags) != len(close_tags):
        issues.append(f"mismatched HTML tags (open={len(open_tags)}, close={len(close_tags)})")
    if issues:
        return False, "; ".join(issues)
    return True, f"length={len(text)} chars"


def check_response_substance(text: str, min_chars: int = 10,
                             min_alpha: int = 5) -> tuple[bool, str]:
    """Check if response has actual substance (not empty/trivial)."""
    stripped = text.strip()
    if not stripped:
        return False, "empty response"
    if len(stripped) < min_chars:
        return False, f"too short ({len(stripped)} chars)"
    alpha = re.findall(r"[\w]", stripped)
    if len(alpha) < min_alpha:
        return False, "no meaningful content"
    return True, f"{len(stripped)} chars"


# AI filler patterns.
_FILLER_PATTERNS = [
    r"^(Great question|I'd be happy to|Sure,? I can|Of course|Certainly|Absolutely)",
    r"^(좋은 질문|물론이죠|당연하죠|기꺼이)",
]


def check_no_filler(text: str) -> tuple[bool, str]:
    """Check no AI filler phrases at start."""
    for pat in _FILLER_PATTERNS:
        match = re.match(pat, text.strip(), re.IGNORECASE)
        if match:
            return False, f"starts with filler: '{match.group()}'"
    return True, "no filler detected"


def check_latency(latency_ms: float, max_ms: float) -> tuple[bool, str]:
    """Check response latency within limit."""
    if latency_ms <= max_ms:
        return True, f"{latency_ms:.0f}ms (limit: {max_ms:.0f}ms)"
    return False, f"{latency_ms:.0f}ms exceeds {max_ms:.0f}ms limit"
