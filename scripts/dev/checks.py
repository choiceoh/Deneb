"""
Shared validation checks for Deneb quality testing.

All functions return tuple[bool, str] — (passed, detail).
Both quality-test.py and reproduce.py import from this module.
"""

import re
import subprocess
from pathlib import Path

_DENEB_UI_FENCE = re.compile(r"```\s*deneb-ui\s*\n.*?(?:\n```|\Z)", re.DOTALL | re.IGNORECASE)
# deneb-html documents are size-governed server-side (96KB cap in
# denebui/htmlanswer.go) and rendered sandboxed, so prose-level length/tag
# checks must not see their bodies.
_DENEB_HTML_FENCE = re.compile(r"```\s*deneb-html\s*\n.*?(?:\n```|\Z)", re.DOTALL | re.IGNORECASE)


def strip_deneb_ui_fences(text: str) -> str:
    """Remove ```deneb-ui blocks — card markup is validated separately
    (check_deneb_ui_valid) and must not trip prose-level tag/length checks."""
    return _DENEB_UI_FENCE.sub("", text)


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
    """Check response is safe for output delivery (legacy check name)."""
    issues = []
    # Length is a prose limit: a deneb-html document legitimately runs long
    # (server caps it at 96KB separately), so measure without its body.
    measured = _DENEB_HTML_FENCE.sub("", text)
    if len(measured) > 4096:
        issues.append(f"exceeds 4096 char limit ({len(measured)} chars)")
    # Tag balance applies to prose only: deneb-ui cards are labeled HTML with
    # deliberately lenient closing rules (<code> nodes, sibling auto-close),
    # validated separately by check_deneb_ui_valid; deneb-html is a full
    # sandboxed document and equally exempt.
    prose = _DENEB_HTML_FENCE.sub("", strip_deneb_ui_fences(text))
    open_tags = re.findall(r"<(b|i|code|pre|s|u|a|blockquote|tg-spoiler)[\s>]", prose)
    close_tags = re.findall(r"</(b|i|code|pre|s|u|a|blockquote|tg-spoiler)>", prose)
    if len(open_tags) != len(close_tags):
        issues.append(f"mismatched HTML tags (open={len(open_tags)}, close={len(close_tags)})")
    if issues:
        return False, "; ".join(issues)
    return True, f"length={len(text)} chars"


def check_deneb_ui_valid(text: str) -> tuple[bool, str]:
    """Validate any ```deneb-ui card in the reply against the node schema.

    Delegates to the gateway's denebui-check CLI (single source of truth for
    the labeled-HTML grammar + legacy JSON). Passes trivially when the reply
    has no card; skips (passes with a note) when Go is unavailable.
    """
    if not re.search(r"```\s*deneb-ui", text, re.IGNORECASE):
        return True, "no deneb-ui card"
    gateway_dir = Path(__file__).resolve().parents[2] / "gateway-go"
    try:
        proc = subprocess.run(
            ["go", "run", "./cmd/denebui-check"],
            input=text, capture_output=True, text=True,
            cwd=gateway_dir, timeout=120, check=False,
        )
    except FileNotFoundError:
        return True, "~ skipped (go unavailable)"
    except subprocess.TimeoutExpired:
        return False, "denebui-check timed out"
    if proc.returncode == 0:
        return True, "card valid"
    detail = (proc.stdout or proc.stderr).strip().splitlines()
    return False, "; ".join(detail[:3]) if detail else f"exit {proc.returncode}"


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
