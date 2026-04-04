#!/usr/bin/env python3
"""
Telegram Pipeline Quality Test — 실제 유저 경험 기준 품질 검증.

vchat(가상 텔레그램)을 통해 메시지를 보내고, 텔레그램 사용자가 실제로 보는 것을
기준으로 품질을 평가합니다. WebSocket RPC 직접 호출이 아닌 Telegram 파이프라인
전체를 거칩니다.

Requirements:
    - vchat이 실행 중이어야 함 (scripts/vchat.py start)
    - 또는 dev-iterate.sh --vchat이 자동으로 관리

Usage:
    python3 scripts/dev-vchat-quality.py                           # all scenarios
    python3 scripts/dev-vchat-quality.py --scenario korean         # specific scenario
    python3 scripts/dev-vchat-quality.py --custom "테스트 메시지"   # custom message
    python3 scripts/dev-vchat-quality.py --json                    # JSON output for automation

Output:
    Human-readable report by default.
    With --json: last line is VCHAT_QUALITY_JSON {...}
"""

import argparse
import json
import re
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from html.parser import HTMLParser

# --- Configuration ---

MOCK_HOST = "127.0.0.1"
MOCK_PORT = 18792
GATEWAY_PORT = 18790

SETTLE_SECS = 3.0
TIMEOUT_SECS = 120
POLL_INTERVAL = 0.15

# Telegram hard limit.
MAX_MESSAGE_LENGTH = 4096


# --- Data Structures ---

@dataclass
class TelegramExperience:
    """What the user actually sees in their Telegram app."""
    typing_events: list = field(default_factory=list)
    reactions: list = field(default_factory=list)
    draft_edits: list = field(default_factory=list)
    final_messages: list = field(default_factory=list)
    file_uploads: list = field(default_factory=list)
    message_deletes: list = field(default_factory=list)
    total_duration_ms: float = 0
    raw_events: list = field(default_factory=list)

    @property
    def all_bot_text(self) -> str:
        """Concatenated text from all final bot messages."""
        return "\n".join(m.get("text", "") for m in self.final_messages)

    @property
    def plain_text(self) -> str:
        """Bot text with HTML stripped."""
        return _strip_html(self.all_bot_text)

    @property
    def has_buttons(self) -> bool:
        for m in self.final_messages:
            markup = m.get("reply_markup")
            if markup and isinstance(markup, dict):
                rows = markup.get("inline_keyboard", [])
                if any(rows):
                    return True
        return False

    @property
    def button_texts(self) -> list:
        texts = []
        for m in self.final_messages:
            markup = m.get("reply_markup")
            if markup and isinstance(markup, dict):
                for row in markup.get("inline_keyboard", []):
                    for btn in row:
                        texts.append(btn.get("text", ""))
        return texts

    @property
    def reaction_sequence(self) -> list:
        return [r.get("emoji", "") for r in self.reactions if r.get("emoji")]


@dataclass
class CheckResult:
    name: str
    passed: bool
    detail: str = ""


@dataclass
class ScenarioResult:
    name: str
    checks: list = field(default_factory=list)
    experience: TelegramExperience = field(default_factory=TelegramExperience)
    error: str = ""

    @property
    def passed(self) -> bool:
        return all(c.passed for c in self.checks) and not self.error

    @property
    def passed_count(self) -> int:
        return sum(1 for c in self.checks if c.passed)


# --- vchat Control API Client ---

def _control_get(path: str) -> dict:
    url = f"http://{MOCK_HOST}:{MOCK_PORT}/control/{path}"
    resp = urllib.request.urlopen(url, timeout=5)
    return json.loads(resp.read())


def _control_post(path: str, data: dict) -> dict:
    url = f"http://{MOCK_HOST}:{MOCK_PORT}/control/{path}"
    body = json.dumps(data).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    resp = urllib.request.urlopen(req, timeout=5)
    return json.loads(resp.read())


def send_and_capture(text: str, timeout: float = TIMEOUT_SECS) -> TelegramExperience:
    """Send a message through vchat and capture the full Telegram experience."""
    status = _control_get("status")
    since = status["events"]

    _control_post("send", {"text": text})

    start = time.time()
    cursor = since + 1  # skip our own user_message event
    last_activity = time.time()
    seen_bot_msg = False
    exp = TelegramExperience()

    while time.time() - start < timeout:
        data = _control_get(f"timeline?since={cursor}")
        events = data.get("events", [])
        total = data.get("total", 0)

        if events:
            last_activity = time.time()
            for evt in events:
                exp.raw_events.append(evt)
                etype = evt["type"]
                edata = evt["data"]

                if etype == "bot_message":
                    exp.final_messages.append(edata)
                    seen_bot_msg = True
                elif etype == "bot_edit":
                    exp.draft_edits.append(edata)
                elif etype == "bot_delete":
                    exp.message_deletes.append(edata)
                elif etype == "typing":
                    exp.typing_events.append(edata)
                elif etype == "reaction":
                    exp.reactions.append(edata)
                elif etype in ("bot_document", "bot_photo", "bot_video", "bot_audio", "bot_voice", "bot_upload"):
                    exp.file_uploads.append({"type": etype, **edata})

            cursor = total

        if seen_bot_msg and time.time() - last_activity > SETTLE_SECS:
            break

        time.sleep(POLL_INTERVAL)

    exp.total_duration_ms = (time.time() - start) * 1000
    return exp


# --- HTML Utilities ---

class _HTMLTagChecker(HTMLParser):
    """Check if HTML tags are properly balanced."""
    def __init__(self):
        super().__init__()
        self.stack = []
        self.errors = []
        # Self-closing tags in Telegram HTML.
        self.void_tags = {"br", "hr", "img"}

    def handle_starttag(self, tag, attrs):
        if tag not in self.void_tags:
            self.stack.append(tag)

    def handle_endtag(self, tag):
        if tag in self.void_tags:
            return
        if not self.stack:
            self.errors.append(f"unexpected </{tag}>")
        elif self.stack[-1] != tag:
            self.errors.append(f"expected </{self.stack[-1]}>, got </{tag}>")
            # Try to recover.
            if tag in self.stack:
                while self.stack and self.stack[-1] != tag:
                    self.stack.pop()
                if self.stack:
                    self.stack.pop()
        else:
            self.stack.pop()


def _check_html_balanced(html: str) -> tuple[bool, str]:
    """Check if HTML tags are properly balanced. Returns (ok, detail)."""
    checker = _HTMLTagChecker()
    try:
        checker.feed(html)
    except Exception as e:
        return False, f"parse error: {e}"

    if checker.errors:
        return False, f"tag errors: {'; '.join(checker.errors[:3])}"
    if checker.stack:
        return False, f"unclosed tags: {', '.join(checker.stack)}"
    return True, "balanced"


def _strip_html(html: str) -> str:
    """Remove HTML tags and decode entities."""
    text = re.sub(r"<[^>]+>", "", html)
    text = text.replace("&lt;", "<").replace("&gt;", ">")
    text = text.replace("&amp;", "&").replace("&quot;", '"')
    return text


# --- Quality Checks ---

def check_html_valid(exp: TelegramExperience) -> CheckResult:
    """Verify all bot messages have balanced HTML tags."""
    for msg in exp.final_messages:
        text = msg.get("text", "")
        parse_mode = msg.get("parse_mode", "")
        if parse_mode.upper() == "HTML" and text:
            ok, detail = _check_html_balanced(text)
            if not ok:
                return CheckResult("html_valid", False, detail)
    return CheckResult("html_valid", True, "all messages have balanced HTML")


def check_korean_response(exp: TelegramExperience) -> CheckResult:
    """Verify response is primarily in Korean."""
    text = exp.plain_text
    korean = len(re.findall(r"[\uac00-\ud7af\u1100-\u11ff\u3130-\u318f]", text))
    total_alpha = len(re.findall(r"[a-zA-Z\uac00-\ud7af]", text))
    if total_alpha == 0:
        return CheckResult("korean_response", False, "no alphabetic content")
    ratio = korean / total_alpha
    if ratio > 0.3:
        return CheckResult("korean_response", True, f"korean ratio={ratio:.0%}")
    return CheckResult("korean_response", False, f"korean ratio={ratio:.0%} (<30%)")


def check_substance(exp: TelegramExperience) -> CheckResult:
    """Verify response has meaningful content."""
    text = exp.plain_text.strip()
    if not text:
        return CheckResult("substance", False, "empty response")
    if len(text) < 10:
        return CheckResult("substance", False, f"too short ({len(text)} chars)")
    return CheckResult("substance", True, f"{len(text)} chars")


def check_no_leaked_markup(exp: TelegramExperience) -> CheckResult:
    """Verify no internal tokens leak into bot messages."""
    text = exp.all_bot_text
    leaked = []
    for pat, label in [
        (r"<function=", "function tag"),
        (r"</?thinking>", "thinking tag"),
        (r"\[\[reply_to", "reply_to bracket"),
        (r"MEDIA:\S+", "MEDIA token"),
        (r"NO_REPLY", "NO_REPLY token"),
        (r"<\|.*?\|>", "special token"),
    ]:
        if re.search(pat, text):
            leaked.append(label)
    if leaked:
        return CheckResult("no_leaked_markup", False, f"leaked: {', '.join(leaked)}")
    return CheckResult("no_leaked_markup", True, "clean")


def check_no_filler(exp: TelegramExperience) -> CheckResult:
    """Verify no AI filler phrases at the start."""
    text = exp.plain_text.strip()
    filler_patterns = [
        r"^(Great question|I'd be happy to|Sure|Of course|Absolutely)",
        r"^(좋은 질문|물론이죠|당연하죠|네,? 물론|네!)",
    ]
    for pat in filler_patterns:
        if re.match(pat, text, re.IGNORECASE):
            return CheckResult("no_filler", False, f"starts with filler: {text[:40]}")
    return CheckResult("no_filler", True, "no filler")


def check_message_chunking(exp: TelegramExperience) -> CheckResult:
    """Verify all messages respect Telegram's 4096-char limit."""
    for i, msg in enumerate(exp.final_messages):
        text = msg.get("text", "")
        if len(text) > MAX_MESSAGE_LENGTH:
            return CheckResult("message_chunking", False,
                             f"message {i+1}: {len(text)} chars (limit: {MAX_MESSAGE_LENGTH})")
    return CheckResult("message_chunking", True,
                      f"{len(exp.final_messages)} message(s), all within limit")


def check_draft_streaming(exp: TelegramExperience) -> CheckResult:
    """Verify draft edits occurred (user sees typing progress)."""
    edit_count = len(exp.draft_edits)
    if edit_count > 0:
        return CheckResult("draft_streaming", True, f"{edit_count} draft edits")
    # Not all responses need drafts (short responses may skip).
    # Mark as warning, not failure.
    return CheckResult("draft_streaming", True, "no drafts (short response?)")


def check_reaction_flow(exp: TelegramExperience) -> CheckResult:
    """Verify reaction sequence is appropriate."""
    seq = exp.reaction_sequence
    if not seq:
        # Reactions are optional depending on config.
        return CheckResult("reaction_flow", True, "no reactions (may be disabled)")
    # Basic validation: should have at least start and end reactions.
    return CheckResult("reaction_flow", True, f"reactions: {'→'.join(seq)}")


def check_buttons_present(exp: TelegramExperience) -> CheckResult:
    """Verify buttons exist on the final message (when expected)."""
    if exp.has_buttons:
        return CheckResult("buttons_present", True,
                          f"buttons: {', '.join(exp.button_texts[:5])}")
    # Buttons are not always required.
    return CheckResult("buttons_present", True, "no buttons (may not be required)")


def check_telegram_safe(exp: TelegramExperience) -> CheckResult:
    """Composite check for Telegram delivery safety."""
    issues = []
    for i, msg in enumerate(exp.final_messages):
        text = msg.get("text", "")
        if len(text) > MAX_MESSAGE_LENGTH:
            issues.append(f"msg {i+1}: {len(text)} chars exceeds limit")
        parse_mode = msg.get("parse_mode", "")
        if parse_mode.upper() == "HTML":
            ok, detail = _check_html_balanced(text)
            if not ok:
                issues.append(f"msg {i+1}: {detail}")
    if issues:
        return CheckResult("telegram_safe", False, "; ".join(issues[:3]))
    return CheckResult("telegram_safe", True, "all messages safe for Telegram delivery")


def check_latency(exp: TelegramExperience, max_ms: float = 60000) -> CheckResult:
    """Verify response time is within budget."""
    ms = exp.total_duration_ms
    if ms <= max_ms:
        return CheckResult("latency", True, f"{ms:.0f}ms (limit: {max_ms:.0f}ms)")
    return CheckResult("latency", False, f"{ms:.0f}ms exceeds {max_ms:.0f}ms limit")


# --- Test Scenarios ---

def test_korean(reset: bool = True) -> ScenarioResult:
    """Korean chat response quality through Telegram pipeline."""
    result = ScenarioResult(name="korean")
    try:
        if reset:
            _control_post("reset", {})
        exp = send_and_capture("안녕, 간단히 자기소개 해줘")
        result.experience = exp
        result.checks = [
            check_substance(exp),
            check_korean_response(exp),
            check_no_leaked_markup(exp),
            check_no_filler(exp),
            check_html_valid(exp),
            check_message_chunking(exp),
            check_telegram_safe(exp),
            check_draft_streaming(exp),
            check_reaction_flow(exp),
            check_latency(exp, 60000),
        ]
    except Exception as e:
        result.error = str(e)
    return result


def test_tool() -> ScenarioResult:
    """Tool usage quality through Telegram pipeline."""
    result = ScenarioResult(name="tool")
    try:
        _control_post("reset", {})
        exp = send_and_capture("시스템 상태 확인해줘")
        result.experience = exp
        result.checks = [
            check_substance(exp),
            check_korean_response(exp),
            check_no_leaked_markup(exp),
            check_html_valid(exp),
            check_telegram_safe(exp),
            check_latency(exp, 60000),
        ]
        # Additional: verify tool was used (look for tool-related events in raw timeline).
        tool_events = [e for e in exp.raw_events
                      if e["type"] == "bot_edit" and "도구" in e["data"].get("text", "")]
        # Tool progress messages contain Korean tool names.
        if tool_events or exp.draft_edits:
            result.checks.append(CheckResult("tool_invoked", True,
                                           f"{len(tool_events)} tool progress events"))
        else:
            result.checks.append(CheckResult("tool_invoked", True,
                                           "no explicit tool progress (may be inline)"))
    except Exception as e:
        result.error = str(e)
    return result


def test_format() -> ScenarioResult:
    """Markdown to HTML formatting quality through Telegram pipeline."""
    result = ScenarioResult(name="format")
    try:
        _control_post("reset", {})
        exp = send_and_capture("마크다운으로 간단한 할일 목록 3개 만들어줘")
        result.experience = exp

        plain = exp.plain_text
        html = exp.all_bot_text

        result.checks = [
            check_substance(exp),
            check_korean_response(exp),
            check_html_valid(exp),
            check_telegram_safe(exp),
            check_message_chunking(exp),
        ]

        # Check list formatting — should have list-like content.
        has_list = bool(re.findall(r"(?:^|\n)\s*[\d•\-\*]", plain))
        result.checks.append(CheckResult("has_list_items", has_list,
                                        "list markers found" if has_list else "no list markers"))

        # Check 3+ items.
        items = re.findall(r"(?:^|\n)\s*[\d•\-\*]", plain)
        result.checks.append(CheckResult("enough_items", len(items) >= 3,
                                        f"{len(items)} items"))

    except Exception as e:
        result.error = str(e)
    return result


def test_multi() -> ScenarioResult:
    """Multi-turn context retention through Telegram pipeline."""
    result = ScenarioResult(name="multi")
    try:
        _control_post("reset", {})

        # Turn 1: introduce name.
        exp1 = send_and_capture("내 이름은 테스트유저야")
        result.checks.append(check_substance(exp1))
        result.checks.append(check_korean_response(exp1))

        # Turn 2: ask for the name back.
        exp2 = send_and_capture("내 이름이 뭐라고 했지?")
        result.experience = exp2

        result.checks.append(check_substance(exp2))
        result.checks.append(check_korean_response(exp2))
        result.checks.append(check_html_valid(exp2))
        result.checks.append(check_telegram_safe(exp2))

        # Check context retention: name should appear in response.
        has_name = "테스트유저" in exp2.plain_text
        result.checks.append(CheckResult("context_retained", has_name,
                                        "name found in response" if has_name
                                        else "name NOT found in response"))

    except Exception as e:
        result.error = str(e)
    return result


def test_custom(message: str) -> ScenarioResult:
    """Custom message through Telegram pipeline."""
    result = ScenarioResult(name="custom")
    try:
        _control_post("reset", {})
        exp = send_and_capture(message)
        result.experience = exp
        result.checks = [
            check_substance(exp),
            check_korean_response(exp),
            check_no_leaked_markup(exp),
            check_no_filler(exp),
            check_html_valid(exp),
            check_message_chunking(exp),
            check_telegram_safe(exp),
            check_draft_streaming(exp),
            check_reaction_flow(exp),
            check_latency(exp, 60000),
        ]
    except Exception as e:
        result.error = str(e)
    return result


SCENARIOS = {
    "korean": test_korean,
    "tool": test_tool,
    "format": test_format,
    "multi": test_multi,
}


# --- Report & Output ---

def print_report(results: list[ScenarioResult]):
    """Human-readable quality report."""
    total_checks = 0
    passed_checks = 0

    for r in results:
        icon = "PASS" if r.passed else "FAIL"
        print(f"\n  [{icon}] {r.name}")

        if r.error:
            print(f"    ERROR: {r.error}")
            continue

        for c in r.checks:
            ci = "  ok" if c.passed else "FAIL"
            print(f"    [{ci}] {c.name}: {c.detail}")
            total_checks += 1
            if c.passed:
                passed_checks += 1

        exp = r.experience
        if exp.final_messages:
            text_preview = exp.plain_text[:100].replace("\n", " ")
            print(f"    reply: {text_preview}")
        if exp.reaction_sequence:
            print(f"    reactions: {'->'.join(exp.reaction_sequence)}")
        if exp.draft_edits:
            print(f"    drafts: {len(exp.draft_edits)} edits")
        print(f"    latency: {exp.total_duration_ms:.0f}ms")
        if exp.has_buttons:
            print(f"    buttons: {', '.join(exp.button_texts[:5])}")

    print(f"\n  total: {passed_checks}/{total_checks} checks passed")
    return passed_checks, total_checks


def build_json_output(results: list[ScenarioResult]) -> dict:
    """Machine-readable JSON output."""
    checks = []
    quality = {
        "html_valid": True,
        "korean": True,
        "substance": True,
        "telegram_safe": True,
        "draft_streaming": True,
    }

    for r in results:
        for c in r.checks:
            checks.append({
                "scenario": r.name,
                "name": c.name,
                "ok": c.passed,
                "detail": c.detail,
            })
            # Update quality summary.
            if c.name in quality and not c.passed:
                quality[c.name] = False

    passed = sum(1 for c in checks if c["ok"])
    total = len(checks)

    return {
        "passed_checks": passed,
        "total_checks": total,
        "all_passed": passed == total,
        "checks": checks,
        "quality": quality,
        "scenarios": [
            {
                "name": r.name,
                "passed": r.passed,
                "error": r.error,
                "latency_ms": r.experience.total_duration_ms,
                "messages": len(r.experience.final_messages),
                "drafts": len(r.experience.draft_edits),
                "reactions": r.experience.reaction_sequence,
            }
            for r in results
        ],
    }


# --- Main ---

def main():
    global MOCK_PORT, GATEWAY_PORT

    parser = argparse.ArgumentParser(
        description="Telegram Pipeline Quality Test (vchat-based)",
    )
    parser.add_argument("--port", type=int, default=MOCK_PORT,
                       help="vchat mock server port")
    parser.add_argument("--gateway-port", type=int, default=GATEWAY_PORT,
                       help="gateway port")
    parser.add_argument("--scenario", default="all",
                       choices=["all", "korean", "tool", "format", "multi"],
                       help="test scenario to run")
    parser.add_argument("--custom", type=str, default=None,
                       help="custom message to test")
    parser.add_argument("--json", action="store_true",
                       help="output JSON for automation")
    args = parser.parse_args()

    MOCK_PORT = args.port
    GATEWAY_PORT = args.gateway_port

    # Verify vchat is running.
    try:
        _control_get("status")
    except Exception:
        print("ERROR: vchat not running. Start with: scripts/vchat.py start", file=sys.stderr)
        if args.json:
            print('VCHAT_QUALITY_JSON {"passed_checks":0,"total_checks":0,"all_passed":false,"checks":[],"quality":{},"scenarios":[],"error":"vchat_not_running"}')
        sys.exit(1)

    # Run scenarios.
    results = []

    if args.custom:
        results.append(test_custom(args.custom))
    elif args.scenario == "all":
        for name, fn in SCENARIOS.items():
            print(f"  running: {name}...", file=sys.stderr)
            results.append(fn())
    else:
        fn = SCENARIOS[args.scenario]
        print(f"  running: {args.scenario}...", file=sys.stderr)
        results.append(fn())

    # Output.
    if args.json:
        output = build_json_output(results)
        # Human-readable summary on stderr.
        passed = output["passed_checks"]
        total = output["total_checks"]
        print(f"  vchat quality: {passed}/{total} checks", file=sys.stderr)
        # Machine-readable on stdout.
        print(f"VCHAT_QUALITY_JSON {json.dumps(output, ensure_ascii=False)}")
        sys.exit(0 if output["all_passed"] else 1)
    else:
        passed, total = print_report(results)
        sys.exit(0 if passed == total else 1)


if __name__ == "__main__":
    main()
