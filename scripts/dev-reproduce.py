#!/usr/bin/env python3
"""
Deneb Gateway Live Reproduction Tool.

AI agents use this to reproduce user-reported symptoms live via Telegram.
Supports single/multi-turn chat with assertions, tool verification,
and symptom-based diagnosis.

Usage:
    # Single chat with assertions
    python3 scripts/dev-reproduce.py chat-check "메시지" \
        --expect "패턴" --expect-not "금지패턴" \
        --expect-tool health --expect-korean --max-latency 30000

    # Multi-turn chat (context carryover)
    python3 scripts/dev-reproduce.py multi-chat \
        "내 이름은 홍길동이야" \
        "내 이름이 뭐라고 했지?"

    # Tool invocation check
    python3 scripts/dev-reproduce.py tool-check health "시스템 상태 확인해줘"

"""

import json
import asyncio
import sys
import time
import argparse
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

# Add scripts dir to path for shared module.
sys.path.insert(0, str(Path(__file__).parent))
from telegram_test_client import TelegramTestClient, ChatCapture, check_prerequisites

# --- Configuration ---

HOST = "127.0.0.1"
DEFAULT_PORT = 18790
TIMEOUT_CONNECT = 5
TIMEOUT_RPC = 10
TIMEOUT_CHAT = 120


# --- Data Structures ---

# ChatCapture is imported from telegram_test_client.


@dataclass
class CheckResult:
    name: str
    passed: bool
    detail: str = ""

    def __str__(self):
        icon = "\u2713" if self.passed else "\u2717"
        detail_str = f" \u2014 {self.detail}" if self.detail else ""
        return f"  {icon} {self.name}{detail_str}"


@dataclass
class TurnResult:
    """Result of a single chat turn."""
    turn: int
    message: str
    capture: ChatCapture
    checks: list = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return all(c.passed for c in self.checks)

    @property
    def failed_checks(self) -> list:
        return [c for c in self.checks if not c.passed]


# --- Telegram Client Wrapper ---

class GatewayClient:
    """Telegram-based test client (replaces former WebSocket client)."""

    def __init__(self, host: str, port: int, bot: str = ""):
        self.host = host
        self.port = port
        self._tg = TelegramTestClient(bot_username=bot)
        self.session_key = ""

    async def connect(self) -> str:
        return await self._tg.connect()

    async def create_session(self, key: str = "") -> str:
        self.session_key = await self._tg.create_session(key)
        return self.session_key

    async def chat(self, message: str, session_key: str = "",
                   timeout: float = TIMEOUT_CHAT) -> ChatCapture:
        return await self._tg.chat(message, timeout=timeout)

    async def rpc(self, method: str, params: dict = None,
                  timeout: float = TIMEOUT_RPC) -> dict:
        return await self._tg.rpc(method, params, timeout)

    async def close(self):
        await self._tg.disconnect()


# --- Assertion Checks ---

def check_korean(text: str) -> CheckResult:
    """Response is primarily Korean."""
    korean_chars = len(re.findall(r"[\uac00-\ud7af\u1100-\u11ff\u3130-\u318f]", text))
    total_alpha = len(re.findall(r"[a-zA-Z\uac00-\ud7af]", text))
    if total_alpha == 0:
        return CheckResult("korean", False, "no alphabetic content")
    ratio = korean_chars / max(total_alpha, 1)
    passed = ratio > 0.3
    return CheckResult("korean", passed, f"ratio={ratio:.0%} ({korean_chars}/{total_alpha})")


def check_expect(text: str, pattern: str) -> CheckResult:
    """Response matches regex pattern."""
    match = re.search(pattern, text, re.IGNORECASE | re.DOTALL)
    return CheckResult(f"expect({pattern[:30]})", bool(match),
                       f"found: '{match.group()[:50]}'" if match else "not found")


def check_expect_not(text: str, pattern: str) -> CheckResult:
    """Response does NOT match regex pattern."""
    match = re.search(pattern, text, re.IGNORECASE | re.DOTALL)
    return CheckResult(f"expect_not({pattern[:30]})", not match,
                       f"found: '{match.group()[:50]}'" if match else "clean")


def check_expect_tool(capture: ChatCapture, tool_name: str) -> CheckResult:
    """Specific tool was called."""
    called = [t["name"] for t in capture.tool_starts]
    found = any(tool_name in name for name in called)
    return CheckResult(f"expect_tool({tool_name})", found,
                       f"called: {called}" if called else "no tools called")


def check_expect_no_tool(capture: ChatCapture) -> CheckResult:
    """No tools were called."""
    called = [t["name"] for t in capture.tool_starts]
    return CheckResult("expect_no_tool", len(called) == 0,
                       f"called: {called}" if called else "no tools")


def check_tool_success(capture: ChatCapture) -> CheckResult:
    """All invoked tools completed without errors."""
    starts = {t["name"] for t in capture.tool_starts}
    results = {t["name"] for t in capture.tool_results}
    errors = [t for t in capture.tool_results if t.get("isError")]
    orphaned = starts - results
    issues = []
    if orphaned:
        issues.append(f"incomplete: {orphaned}")
    if errors:
        issues.append(f"errors: {[t['name'] for t in errors]}")
    passed = not issues
    return CheckResult("tool_success", passed,
                       "; ".join(issues) if issues else f"{len(starts)} tools OK")


def check_latency(capture: ChatCapture, max_ms: float) -> CheckResult:
    """Response latency within limit."""
    ms = capture.latency_ms
    return CheckResult(f"latency(<{max_ms:.0f}ms)", ms <= max_ms, f"{ms:.0f}ms")


def check_first_token(capture: ChatCapture, max_ms: float) -> CheckResult:
    """Time to first token within limit."""
    ms = capture.first_token_ms
    if ms == 0:
        return CheckResult(f"first_token(<{max_ms:.0f}ms)", False, "no tokens received")
    return CheckResult(f"first_token(<{max_ms:.0f}ms)", ms <= max_ms, f"{ms:.0f}ms")


def check_no_error(capture: ChatCapture) -> CheckResult:
    """Chat completed without errors."""
    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    if capture.errors:
        return CheckResult("no_error", False, f"errors: {capture.errors}")
    return CheckResult("no_error", ok, final_state or ("ok" if ok else "no response"))


def check_has_reply(capture: ChatCapture, min_len: int = 1) -> CheckResult:
    """Response has non-empty reply."""
    text = capture.reply_text.strip()
    if not text:
        return CheckResult("has_reply", False, "empty response")
    if len(text) < min_len:
        return CheckResult("has_reply", False, f"too short: {len(text)} < {min_len} chars")
    return CheckResult("has_reply", True, f"{len(text)} chars")


def check_no_leaked_markup(text: str) -> CheckResult:
    """No internal tokens leaked to response."""
    patterns = [
        (r"<function=", "leaked <function= tag"),
        (r"</?thinking>", "leaked thinking tag"),
        (r"</?artifact", "leaked artifact tag"),
        (r"\[\[reply_to", "leaked reply directive"),
        (r"MEDIA:\S+", "leaked MEDIA token"),
        (r"NO_REPLY", "leaked NO_REPLY token"),
    ]
    for pat, desc in patterns:
        if re.search(pat, text):
            return CheckResult("no_leaked_markup", False, desc)
    return CheckResult("no_leaked_markup", True, "clean")


def check_no_filler(text: str) -> CheckResult:
    """No AI filler phrases at start."""
    filler_patterns = [
        r"^(Great question|I'd be happy to|Sure,? I can|Of course|Certainly|Absolutely)",
        r"^(좋은 질문|물론이죠|당연하죠|기꺼이)",
    ]
    for pat in filler_patterns:
        match = re.match(pat, text.strip(), re.IGNORECASE)
        if match:
            return CheckResult("no_filler", False, f"starts with: '{match.group()}'")
    return CheckResult("no_filler", True, "clean")


def check_telegram_safe(text: str) -> CheckResult:
    """Safe for Telegram delivery."""
    issues = []
    if len(text) > 4096:
        issues.append(f"exceeds 4096 chars ({len(text)})")
    open_tags = re.findall(r"<(b|i|code|pre|s|u|a|blockquote|tg-spoiler)[\s>]", text)
    close_tags = re.findall(r"</(b|i|code|pre|s|u|a|blockquote|tg-spoiler)>", text)
    if len(open_tags) != len(close_tags):
        issues.append(f"mismatched HTML (open={len(open_tags)}, close={len(close_tags)})")
    if issues:
        return CheckResult("telegram_safe", False, "; ".join(issues))
    return CheckResult("telegram_safe", True, f"{len(text)} chars")


def check_streaming(capture: ChatCapture) -> CheckResult:
    """Streaming events flowed properly."""
    n_deltas = len(capture.deltas)
    n_events = len(capture.events)
    if n_deltas == 0 and n_events <= 2:
        return CheckResult("streaming", False, f"no deltas, {n_events} events")
    return CheckResult("streaming", True, f"{n_deltas} deltas, {n_events} events")


def check_context_carryover(capture: ChatCapture, expected_pattern: str) -> CheckResult:
    """Response refers back to prior context."""
    match = re.search(expected_pattern, capture.reply_text, re.IGNORECASE | re.DOTALL)
    return CheckResult(f"context({expected_pattern[:30]})", bool(match),
                       f"found: '{match.group()[:50]}'" if match else "not found in response")


def check_min_length(text: str, min_len: int) -> CheckResult:
    """Response has minimum length."""
    actual = len(text.strip())
    return CheckResult(f"min_length({min_len})", actual >= min_len, f"{actual} chars")


def check_expect_error(capture: ChatCapture) -> CheckResult:
    """Expect the chat to produce an error."""
    has_error = bool(capture.errors) or capture.final_response.get("payload", {}).get("state") == "error"
    return CheckResult("expect_error", has_error,
                       f"errors: {capture.errors}" if has_error else "no error occurred")


# --- Commands ---

async def cmd_chat_check(args):
    """Send a chat message with configurable assertions."""
    ok, detail = check_prerequisites()
    if not ok:
        print(f"Telegram prerequisites not met: {detail}")
        return 1

    client = GatewayClient(HOST, args.port, bot=getattr(args, "bot", ""))
    try:
        bot_name = await client.connect()
        print(f"Connected to {bot_name} via Telegram")

        if args.session:
            await client.create_session(args.session)
        capture = await client.chat(args.message, timeout=args.timeout)

        # Build checks.
        checks = []
        checks.append(check_no_error(capture))
        checks.append(check_has_reply(capture, args.expect_min_len or 1))

        if args.expect_korean:
            checks.append(check_korean(capture.reply_text))
        if args.expect:
            for pat in args.expect:
                checks.append(check_expect(capture.reply_text, pat))
        if args.expect_not:
            for pat in args.expect_not:
                checks.append(check_expect_not(capture.reply_text, pat))
        if args.expect_tool:
            for tool in args.expect_tool:
                checks.append(check_expect_tool(capture, tool))
        if args.expect_no_tool:
            checks.append(check_expect_no_tool(capture))
        if args.expect_error:
            checks.append(check_expect_error(capture))
        if args.max_latency:
            checks.append(check_latency(capture, args.max_latency))
        if args.max_first_token:
            checks.append(check_first_token(capture, args.max_first_token))
        if args.expect_min_len and args.expect_min_len > 1:
            checks.append(check_min_length(capture.reply_text, args.expect_min_len))

        # Default checks (always run).
        checks.append(check_no_leaked_markup(capture.reply_text))
        checks.append(check_tool_success(capture))
        checks.append(check_telegram_safe(capture.reply_text))

        return print_turn_result(TurnResult(1, args.message, capture, checks))

    finally:
        await client.close()


async def cmd_multi_chat(args):
    """Multi-turn chat on the same session. Tests context carryover."""
    ok, detail = check_prerequisites()
    if not ok:
        print(f"Telegram prerequisites not met: {detail}")
        return 1

    client = GatewayClient(HOST, args.port, bot=getattr(args, "bot", ""))
    try:
        bot_name = await client.connect()
        print(f"Connected to {bot_name} via Telegram")

        session_key = await client.create_session()
        print(f"Session: {session_key}")
        print()

        all_results = []
        all_passed = True

        for i, message in enumerate(args.messages, 1):
            print(f"--- Turn {i}/{len(args.messages)} ---")
            print(f">>> {message}")
            print()

            capture = await client.chat(message, session_key=session_key, timeout=args.timeout)

            checks = []
            checks.append(check_no_error(capture))
            checks.append(check_has_reply(capture))
            checks.append(check_no_leaked_markup(capture.reply_text))
            checks.append(check_tool_success(capture))

            if args.expect_korean:
                checks.append(check_korean(capture.reply_text))

            # For turns after the first, check context carryover if patterns provided.
            if i > 1 and args.expect_context:
                for pat in args.expect_context:
                    checks.append(check_context_carryover(capture, pat))

            result = TurnResult(i, message, capture, checks)
            all_results.append(result)

            print_single_turn(result)
            if not result.passed:
                all_passed = False
            print()

        # Summary.
        print("=" * 60)
        total_checks = sum(len(r.checks) for r in all_results)
        passed_checks = sum(sum(1 for c in r.checks if c.passed) for r in all_results)
        status = "PASSED" if all_passed else "FAILED"
        print(f"  {status} \u2014 {len(all_results)} turns, {passed_checks}/{total_checks} checks")
        print("=" * 60)
        return 0 if all_passed else 1

    finally:
        await client.close()


async def cmd_tool_check(args):
    """Send a message designed to trigger a specific tool, verify it completes."""
    ok, detail = check_prerequisites()
    if not ok:
        print(f"Telegram prerequisites not met: {detail}")
        return 1

    client = GatewayClient(HOST, args.port, bot=getattr(args, "bot", ""))
    try:
        bot_name = await client.connect()
        print(f"Connected to {bot_name} via Telegram")

        capture = await client.chat(args.message, timeout=args.timeout)

        checks = []
        checks.append(check_no_error(capture))
        checks.append(check_has_reply(capture))
        checks.append(check_expect_tool(capture, args.tool))
        checks.append(check_tool_success(capture))
        checks.append(check_no_leaked_markup(capture.reply_text))

        if args.max_latency:
            checks.append(check_latency(capture, args.max_latency))

        result = TurnResult(1, args.message, capture, checks)
        return print_turn_result(result)

    finally:
        await client.close()


# --- Output ---

def print_single_turn(result: TurnResult, indent: int = 0):
    """Print results for a single turn."""
    prefix = " " * indent
    for check in result.checks:
        icon = "\u2713" if check.passed else "\u2717"
        detail_str = f" \u2014 {check.detail}" if check.detail else ""
        print(f"{prefix}    {icon} {check.name}{detail_str}")

    cap = result.capture
    if cap.tool_starts:
        tools = [t["name"] for t in cap.tool_starts]
        print(f"{prefix}    tools: {tools}")
    usage = cap.token_usage
    if usage:
        inp = usage.get("inputTokens", usage.get("input_tokens", "?"))
        out = usage.get("outputTokens", usage.get("output_tokens", "?"))
        print(f"{prefix}    tokens: {inp} in / {out} out")
    print(f"{prefix}    latency: {cap.latency_ms:.0f}ms (first_token: {cap.first_token_ms:.0f}ms)")
    if cap.reply_text:
        preview = cap.reply_text[:150].replace("\n", " ")
        if len(cap.reply_text) > 150:
            preview += "..."
        print(f"{prefix}    reply: {preview}")
    if cap.errors:
        for e in cap.errors:
            print(f"{prefix}    ERROR: {e}")


def print_turn_result(result: TurnResult) -> int:
    """Print full result for chat-check / tool-check."""
    print()
    print(f"{'=' * 60}")
    print(f"  Message: {result.message[:60]}")
    print(f"{'=' * 60}")
    print()

    print_single_turn(result)
    print()

    passed = sum(1 for c in result.checks if c.passed)
    total = len(result.checks)
    status = "PASSED" if result.passed else "FAILED"
    print(f"{'=' * 60}")
    print(f"  {status} \u2014 {passed}/{total} checks")
    print(f"{'=' * 60}")
    return 0 if result.passed else 1


# --- Main ---

def main():
    parser = argparse.ArgumentParser(description="Deneb Live Reproduction Tool (Telegram)")
    parser.add_argument("--port", type=int, default=DEFAULT_PORT,
                        help="Gateway HTTP port for health checks")
    parser.add_argument("--bot", type=str, default="",
                        help="Bot username (default: DENEB_DEV_BOT_USERNAME)")
    parser.add_argument("--timeout", type=float, default=TIMEOUT_CHAT)
    sub = parser.add_subparsers(dest="command")

    # chat-check
    p_chat = sub.add_parser("chat-check", help="Chat with assertions")
    p_chat.add_argument("message", help="Chat message")
    p_chat.add_argument("--expect", action="append", help="Response must match regex (repeatable)")
    p_chat.add_argument("--expect-not", action="append", help="Response must NOT match regex (repeatable)")
    p_chat.add_argument("--expect-tool", action="append", help="Tool must be called (repeatable)")
    p_chat.add_argument("--expect-no-tool", action="store_true", help="No tools should be called")
    p_chat.add_argument("--expect-korean", action="store_true", help="Response must be Korean")
    p_chat.add_argument("--expect-error", action="store_true", help="Expect an error response")
    p_chat.add_argument("--expect-min-len", type=int, help="Minimum response length")
    p_chat.add_argument("--max-latency", type=float, help="Max response latency in ms")
    p_chat.add_argument("--max-first-token", type=float, help="Max time to first token in ms")
    p_chat.add_argument("--session", help="Reuse session key")

    # multi-chat
    p_multi = sub.add_parser("multi-chat", help="Multi-turn chat with context carryover")
    p_multi.add_argument("messages", nargs="+", help="Chat messages (one per turn)")
    p_multi.add_argument("--expect-korean", action="store_true", help="Responses must be Korean")
    p_multi.add_argument("--expect-context", action="append",
                         help="Pattern to find in later turns (context carryover check)")

    # tool-check
    p_tool = sub.add_parser("tool-check", help="Verify specific tool invocation")
    p_tool.add_argument("tool", help="Expected tool name (substring match)")
    p_tool.add_argument("message", help="Chat message to trigger the tool")
    p_tool.add_argument("--max-latency", type=float, help="Max response latency in ms")

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(1)

    if args.command == "chat-check":
        sys.exit(asyncio.run(cmd_chat_check(args)))
    elif args.command == "multi-chat":
        sys.exit(asyncio.run(cmd_multi_chat(args)))
    elif args.command == "tool-check":
        sys.exit(asyncio.run(cmd_tool_check(args)))


if __name__ == "__main__":
    main()
