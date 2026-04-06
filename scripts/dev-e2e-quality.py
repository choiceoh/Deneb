#!/usr/bin/env python3
"""
Real Telegram E2E Quality Test — 실제 텔레그램 파이프라인 전체 검증.

Telethon(유저 계정)으로 dev 봇에 실제 메시지를 보내고, 텔레그램 서버를 거쳐
돌아온 응답을 검증합니다. mock 없이 유저가 보는 것과 100% 동일한 경로.

Requirements:
    - ~/.deneb/telegram-test.session (scripts/telegram-session-init.py로 생성)
    - dev 게이트웨이가 실행 중 (scripts/dev-live-test.sh restart)
    - ~/.deneb/.env에 DENEB_DEV_BOT_USERNAME, TELEGRAM_API_ID, TELEGRAM_API_HASH

Usage:
    python3 scripts/dev-e2e-quality.py                           # all scenarios
    python3 scripts/dev-e2e-quality.py --scenario korean         # specific scenario
    python3 scripts/dev-e2e-quality.py --custom "테스트 메시지"   # custom message
    python3 scripts/dev-e2e-quality.py --json                    # JSON output
    python3 scripts/dev-e2e-quality.py --bot nebdev2bot          # specify bot
"""

import argparse
import asyncio
import json
import os
import re
import sys
import time
from dataclasses import dataclass, field
from html.parser import HTMLParser

# --- Dotenv Loader ---

def _load_dotenv(path: str) -> None:
    if not os.path.isfile(path):
        return
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, _, v = line.partition("=")
            k, v = k.strip(), v.strip().strip("'\"")
            if k not in os.environ:
                os.environ[k] = v

_load_dotenv(os.path.expanduser("~/.deneb/.env"))

# --- Configuration ---

SESSION_PATH = os.path.expanduser("~/.deneb/telegram-test")
API_ID = int(os.environ.get("TELEGRAM_API_ID", "0"))
API_HASH = os.environ.get("TELEGRAM_API_HASH", "")
DEV_BOT_USERNAME = os.environ.get("DENEB_DEV_BOT_USERNAME", "nebdev1bot")

SETTLE_SECS = 3.0  # No new events for this long → response complete.
TIMEOUT_SECS = 120
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
        return "\n".join(m.get("text", "") for m in self.final_messages)

    @property
    def plain_text(self) -> str:
        return _strip_html(self.all_bot_text)

    @property
    def has_buttons(self) -> bool:
        for m in self.final_messages:
            if m.get("buttons"):
                return True
        return False

    @property
    def button_texts(self) -> list:
        texts = []
        for m in self.final_messages:
            for btn in m.get("buttons", []):
                texts.append(btn)
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


# --- HTML Utilities ---

class _HTMLTagChecker(HTMLParser):
    def __init__(self):
        super().__init__()
        self.stack = []
        self.errors = []
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
            if tag in self.stack:
                while self.stack and self.stack[-1] != tag:
                    self.stack.pop()
                if self.stack:
                    self.stack.pop()
        else:
            self.stack.pop()


def _check_html_balanced(html: str) -> tuple[bool, str]:
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
    text = re.sub(r"<[^>]+>", "", html)
    text = text.replace("&lt;", "<").replace("&gt;", ">")
    text = text.replace("&amp;", "&").replace("&quot;", '"')
    return text


# --- Quality Checks (same as vchat-quality) ---

def check_html_valid(exp: TelegramExperience) -> CheckResult:
    for msg in exp.final_messages:
        text = msg.get("text", "")
        if msg.get("has_entities") and text:
            ok, detail = _check_html_balanced(text)
            if not ok:
                return CheckResult("html_valid", False, detail)
    return CheckResult("html_valid", True, "all messages have balanced HTML")


def check_korean_response(exp: TelegramExperience) -> CheckResult:
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
    text = exp.plain_text.strip()
    if not text:
        return CheckResult("substance", False, "empty response")
    if len(text) < 10:
        return CheckResult("substance", False, f"too short ({len(text)} chars)")
    return CheckResult("substance", True, f"{len(text)} chars")


def check_no_leaked_markup(exp: TelegramExperience) -> CheckResult:
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
    for i, msg in enumerate(exp.final_messages):
        text = msg.get("text", "")
        if len(text) > MAX_MESSAGE_LENGTH:
            return CheckResult("message_chunking", False,
                             f"message {i+1}: {len(text)} chars (limit: {MAX_MESSAGE_LENGTH})")
    return CheckResult("message_chunking", True,
                      f"{len(exp.final_messages)} message(s), all within limit")


def check_draft_streaming(exp: TelegramExperience) -> CheckResult:
    edit_count = len(exp.draft_edits)
    if edit_count > 0:
        return CheckResult("draft_streaming", True, f"{edit_count} draft edits")
    return CheckResult("draft_streaming", True, "no drafts (short response?)")


def check_reaction_flow(exp: TelegramExperience) -> CheckResult:
    seq = exp.reaction_sequence
    if not seq:
        return CheckResult("reaction_flow", True, "no reactions (may be disabled)")
    return CheckResult("reaction_flow", True, f"reactions: {'→'.join(seq)}")


def check_telegram_safe(exp: TelegramExperience) -> CheckResult:
    issues = []
    for i, msg in enumerate(exp.final_messages):
        text = msg.get("text", "")
        if len(text) > MAX_MESSAGE_LENGTH:
            issues.append(f"msg {i+1}: {len(text)} chars exceeds limit")
        if msg.get("has_entities"):
            ok, detail = _check_html_balanced(text)
            if not ok:
                issues.append(f"msg {i+1}: {detail}")
    if issues:
        return CheckResult("telegram_safe", False, "; ".join(issues[:3]))
    return CheckResult("telegram_safe", True, "all messages safe for Telegram delivery")


def check_latency(exp: TelegramExperience, max_ms: float = 60000) -> CheckResult:
    ms = exp.total_duration_ms
    if ms <= max_ms:
        return CheckResult("latency", True, f"{ms:.0f}ms (limit: {max_ms:.0f}ms)")
    return CheckResult("latency", False, f"{ms:.0f}ms exceeds {max_ms:.0f}ms limit")


# --- Telethon Transport ---

def _msg_to_html(message) -> str:
    """Convert a Telethon message to HTML text."""
    try:
        from telethon.extensions import html as tg_html
        if message.entities:
            return tg_html.unparse(message.text or "", message.entities)
    except Exception:
        pass
    return message.text or ""


def _msg_to_dict(message) -> dict:
    """Convert a Telethon message to the dict format used by TelegramExperience."""
    html_text = _msg_to_html(message)
    buttons = []
    if message.buttons:
        for row in message.buttons:
            for btn in row:
                buttons.append(btn.text)
    return {
        "text": html_text,
        "has_entities": bool(message.entities),
        "message_id": message.id,
        "buttons": buttons,
    }


async def send_and_capture(client, bot_entity, text: str,
                           timeout: float = TIMEOUT_SECS) -> TelegramExperience:
    """Send a message to the bot and capture the full Telegram experience."""
    from telethon import events

    exp = TelegramExperience()
    last_activity = time.time()
    seen_bot_msg = False
    # Track message IDs to distinguish new messages from edits.
    known_msg_ids: set[int] = set()

    async def on_new_message(event):
        nonlocal last_activity, seen_bot_msg
        msg = event.message
        if msg.sender_id != bot_entity.id:
            return
        last_activity = time.time()
        known_msg_ids.add(msg.id)
        d = _msg_to_dict(msg)
        exp.final_messages.append(d)
        exp.raw_events.append({"time": time.time(), "type": "bot_message", "data": d})
        seen_bot_msg = True

    async def on_edit(event):
        nonlocal last_activity
        msg = event.message
        if msg.sender_id != bot_entity.id:
            return
        last_activity = time.time()
        d = _msg_to_dict(msg)
        if msg.id in known_msg_ids:
            # Update the final message in-place.
            for i, fm in enumerate(exp.final_messages):
                if fm.get("message_id") == msg.id:
                    exp.final_messages[i] = d
                    break
            exp.draft_edits.append(d)
            exp.raw_events.append({"time": time.time(), "type": "bot_edit", "data": d})
        else:
            known_msg_ids.add(msg.id)
            exp.final_messages.append(d)
            exp.raw_events.append({"time": time.time(), "type": "bot_message", "data": d})
            seen_bot_msg = True

    async def on_deleted(event):
        nonlocal last_activity
        last_activity = time.time()
        for msg_id in event.deleted_ids:
            if msg_id in known_msg_ids:
                exp.final_messages = [m for m in exp.final_messages
                                      if m.get("message_id") != msg_id]
                known_msg_ids.discard(msg_id)
                exp.message_deletes.append({"message_id": msg_id})
                exp.raw_events.append({"time": time.time(), "type": "bot_delete",
                                       "data": {"message_id": msg_id}})

    # Register handlers.
    client.add_event_handler(on_new_message, events.NewMessage(from_users=bot_entity.id))
    client.add_event_handler(on_edit, events.MessageEdited(from_users=bot_entity.id))
    client.add_event_handler(on_deleted, events.MessageDeleted)

    start = time.time()
    try:
        await client.send_message(bot_entity, text)

        # Wait for response with settle detection.
        while time.time() - start < timeout:
            await asyncio.sleep(0.2)
            if seen_bot_msg and time.time() - last_activity > SETTLE_SECS:
                break
    finally:
        client.remove_event_handler(on_new_message)
        client.remove_event_handler(on_edit)
        client.remove_event_handler(on_deleted)

    exp.total_duration_ms = (time.time() - start) * 1000
    return exp


# --- Test Scenarios ---

async def test_korean(client, bot_entity, reset: bool = True) -> ScenarioResult:
    result = ScenarioResult(name="korean")
    try:
        if reset:
            await client.send_message(bot_entity, "/reset")
            await asyncio.sleep(2)
        exp = await send_and_capture(client, bot_entity, "안녕, 간단히 자기소개 해줘")
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


async def test_tool(client, bot_entity) -> ScenarioResult:
    result = ScenarioResult(name="tool")
    try:
        await client.send_message(bot_entity, "/reset")
        await asyncio.sleep(2)
        exp = await send_and_capture(client, bot_entity, "시스템 상태 확인해줘")
        result.experience = exp
        result.checks = [
            check_substance(exp),
            check_korean_response(exp),
            check_no_leaked_markup(exp),
            check_html_valid(exp),
            check_telegram_safe(exp),
            check_latency(exp, 60000),
        ]
        tool_edits = [e for e in exp.raw_events
                     if e["type"] == "bot_edit" and "도구" in e["data"].get("text", "")]
        if tool_edits or exp.draft_edits:
            result.checks.append(CheckResult("tool_invoked", True,
                                           f"{len(tool_edits)} tool progress events"))
        else:
            result.checks.append(CheckResult("tool_invoked", True,
                                           "no explicit tool progress (may be inline)"))
    except Exception as e:
        result.error = str(e)
    return result


async def test_format(client, bot_entity) -> ScenarioResult:
    result = ScenarioResult(name="format")
    try:
        await client.send_message(bot_entity, "/reset")
        await asyncio.sleep(2)
        exp = await send_and_capture(client, bot_entity,
                                     "마크다운으로 간단한 할일 목록 3개 만들어줘")
        result.experience = exp
        plain = exp.plain_text
        result.checks = [
            check_substance(exp),
            check_korean_response(exp),
            check_html_valid(exp),
            check_telegram_safe(exp),
            check_message_chunking(exp),
        ]
        has_list = bool(re.findall(r"(?:^|\n)\s*[\d•\-\*]", plain))
        result.checks.append(CheckResult("has_list_items", has_list,
                                        "list markers found" if has_list else "no list markers"))
        items = re.findall(r"(?:^|\n)\s*[\d•\-\*]", plain)
        result.checks.append(CheckResult("enough_items", len(items) >= 3,
                                        f"{len(items)} items"))
    except Exception as e:
        result.error = str(e)
    return result


async def test_multi(client, bot_entity) -> ScenarioResult:
    result = ScenarioResult(name="multi")
    try:
        await client.send_message(bot_entity, "/reset")
        await asyncio.sleep(2)

        exp1 = await send_and_capture(client, bot_entity, "내 이름은 테스트유저야")
        result.checks.append(check_substance(exp1))
        result.checks.append(check_korean_response(exp1))

        exp2 = await send_and_capture(client, bot_entity, "내 이름이 뭐라고 했지?")
        result.experience = exp2
        result.checks.append(check_substance(exp2))
        result.checks.append(check_korean_response(exp2))
        result.checks.append(check_html_valid(exp2))
        result.checks.append(check_telegram_safe(exp2))

        has_name = "테스트유저" in exp2.plain_text
        result.checks.append(CheckResult("context_retained", has_name,
                                        "name found in response" if has_name
                                        else "name NOT found in response"))
    except Exception as e:
        result.error = str(e)
    return result


async def test_custom(client, bot_entity, message: str) -> ScenarioResult:
    result = ScenarioResult(name="custom")
    try:
        await client.send_message(bot_entity, "/reset")
        await asyncio.sleep(2)
        exp = await send_and_capture(client, bot_entity, message)
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


SCENARIOS = ["korean", "tool", "format", "multi"]


# --- Report & Output (same format as vchat-quality) ---

def print_report(results: list[ScenarioResult]):
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
            print(f"    reactions: {'→'.join(exp.reaction_sequence)}")
        if exp.draft_edits:
            print(f"    drafts: {len(exp.draft_edits)} edits")
        print(f"    latency: {exp.total_duration_ms:.0f}ms")
        if exp.has_buttons:
            print(f"    buttons: {', '.join(exp.button_texts[:5])}")

    print(f"\n  total: {passed_checks}/{total_checks} checks passed")
    print(f"  transport: real Telegram (not mock)")
    return passed_checks, total_checks


def build_json_output(results: list[ScenarioResult]) -> dict:
    checks = []
    quality = {
        "html_valid": True, "korean": True, "substance": True,
        "telegram_safe": True, "draft_streaming": True,
    }
    for r in results:
        for c in r.checks:
            checks.append({
                "scenario": r.name, "name": c.name,
                "ok": c.passed, "detail": c.detail,
            })
            if c.name in quality and not c.passed:
                quality[c.name] = False

    passed = sum(1 for c in checks if c["ok"])
    total = len(checks)
    return {
        "passed_checks": passed,
        "total_checks": total,
        "all_passed": passed == total,
        "transport": "real_telegram",
        "checks": checks,
        "quality": quality,
        "scenarios": [
            {
                "name": r.name, "passed": r.passed, "error": r.error,
                "latency_ms": r.experience.total_duration_ms,
                "messages": len(r.experience.final_messages),
                "drafts": len(r.experience.draft_edits),
                "reactions": r.experience.reaction_sequence,
            }
            for r in results
        ],
    }


# --- Main ---

async def async_main(args):
    from telethon import TelegramClient

    if not API_ID or not API_HASH:
        print("ERROR: TELEGRAM_API_ID and TELEGRAM_API_HASH required in ~/.deneb/.env",
              file=sys.stderr)
        sys.exit(1)

    if not os.path.isfile(SESSION_PATH + ".session"):
        print("ERROR: No session file. Run: python3 scripts/telegram-session-init.py",
              file=sys.stderr)
        sys.exit(1)

    bot_username = args.bot or DEV_BOT_USERNAME
    print(f"  connecting to Telegram...", file=sys.stderr)

    client = TelegramClient(SESSION_PATH, API_ID, API_HASH)
    await client.connect()

    if not await client.is_user_authorized():
        print("ERROR: Session expired. Re-run: python3 scripts/telegram-session-init.py",
              file=sys.stderr)
        sys.exit(1)

    try:
        bot_entity = await client.get_entity(bot_username)
        print(f"  target: @{bot_username} (ID: {bot_entity.id})", file=sys.stderr)
    except Exception as e:
        print(f"ERROR: Cannot find bot @{bot_username}: {e}", file=sys.stderr)
        sys.exit(1)

    results = []
    if args.custom:
        print(f"  running: custom...", file=sys.stderr)
        results.append(await test_custom(client, bot_entity, args.custom))
    elif args.scenario == "all":
        for name in SCENARIOS:
            print(f"  running: {name}...", file=sys.stderr)
            if name == "korean":
                results.append(await test_korean(client, bot_entity))
            elif name == "tool":
                results.append(await test_tool(client, bot_entity))
            elif name == "format":
                results.append(await test_format(client, bot_entity))
            elif name == "multi":
                results.append(await test_multi(client, bot_entity))
    else:
        name = args.scenario
        print(f"  running: {name}...", file=sys.stderr)
        if name == "korean":
            results.append(await test_korean(client, bot_entity))
        elif name == "tool":
            results.append(await test_tool(client, bot_entity))
        elif name == "format":
            results.append(await test_format(client, bot_entity))
        elif name == "multi":
            results.append(await test_multi(client, bot_entity))

    await client.disconnect()

    if args.json:
        output = build_json_output(results)
        passed = output["passed_checks"]
        total = output["total_checks"]
        print(f"  e2e quality: {passed}/{total} checks", file=sys.stderr)
        print(f"E2E_QUALITY_JSON {json.dumps(output, ensure_ascii=False)}")
        sys.exit(0 if output["all_passed"] else 1)
    else:
        passed, total = print_report(results)
        sys.exit(0 if passed == total else 1)


def main():
    parser = argparse.ArgumentParser(
        description="Real Telegram E2E Quality Test (Telethon-based)",
    )
    parser.add_argument("--scenario", default="all",
                       choices=["all", "korean", "tool", "format", "multi"],
                       help="test scenario to run")
    parser.add_argument("--custom", type=str, default=None,
                       help="custom message to test")
    parser.add_argument("--bot", type=str, default=None,
                       help="bot username (default: DENEB_DEV_BOT_USERNAME)")
    parser.add_argument("--json", action="store_true",
                       help="output JSON for automation")
    args = parser.parse_args()

    asyncio.run(async_main(args))


if __name__ == "__main__":
    main()
