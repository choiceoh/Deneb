#!/usr/bin/env python3
"""
Deneb Gateway Quality Test Runner.

Connects to the dev gateway via WebSocket, sends test scenarios,
and evaluates response QUALITY — not just "did it work."

Usage:
    python3 scripts/dev-quality-test.py [--port 18790] [--scenario all|chat|tools|format|tools-deep|edge]
    python3 scripts/dev-quality-test.py --scenario tools-deep  # deep tool correctness tests
    python3 scripts/dev-quality-test.py --scenario edge         # edge case input tests
    python3 scripts/dev-quality-test.py --custom "안녕, 오늘 날씨 어때?"
    python3 scripts/dev-quality-test.py --report  # full quality report
"""

import json
import asyncio
import sys
import time
import argparse
import re
from dataclasses import dataclass, field
from typing import Optional

try:
    import websockets
except ImportError:
    print("ERROR: pip install websockets")
    sys.exit(1)

# --- Configuration ---

HOST = "127.0.0.1"
PORT = 18790
TIMEOUT_CONNECT = 5
TIMEOUT_RPC = 10
TIMEOUT_CHAT = 120  # chat can take a while with tool calls


# --- Quality Criteria ---

@dataclass
class QualityResult:
    """Quality assessment of a single test."""
    name: str
    passed: bool = False
    score: float = 0.0  # 0.0 ~ 1.0
    checks: list = field(default_factory=list)  # (check_name, passed, detail)
    events: list = field(default_factory=list)
    reply_text: str = ""
    latency_ms: float = 0
    token_usage: dict = field(default_factory=dict)
    tool_calls: list = field(default_factory=list)
    errors: list = field(default_factory=list)
    warnings: list = field(default_factory=list)

    def add_check(self, name: str, passed: bool, detail: str = ""):
        self.checks.append((name, passed, detail))

    def summary(self) -> str:
        total = len(self.checks)
        passed = sum(1 for _, p, _ in self.checks if p)
        status = "PASS" if self.passed else "FAIL"
        return f"[{status}] {self.name} ({passed}/{total} checks, score={self.score:.0%}, {self.latency_ms:.0f}ms)"


@dataclass
class ChatCapture:
    """Captures everything from a chat interaction."""
    events: list = field(default_factory=list)
    deltas: list = field(default_factory=list)
    tool_starts: list = field(default_factory=list)
    tool_results: list = field(default_factory=list)
    heartbeats: list = field(default_factory=list)
    status_changes: list = field(default_factory=list)
    final_response: dict = field(default_factory=dict)
    errors: list = field(default_factory=list)
    reply_text: str = ""
    start_time: float = 0
    end_time: float = 0
    all_text: str = ""  # accumulated deltas

    @property
    def latency_ms(self) -> float:
        return (self.end_time - self.start_time) * 1000

    @property
    def first_token_ms(self) -> float:
        if self.deltas:
            return (self.deltas[0]["ts"] - self.start_time) * 1000
        return 0

    token_usage_data: dict = field(default_factory=dict)

    @property
    def token_usage(self) -> dict:
        if self.token_usage_data:
            return self.token_usage_data
        payload = self.final_response.get("payload", {})
        return payload.get("usage", {})


# --- WebSocket Client ---

class GatewayClient:
    def __init__(self, host: str, port: int):
        self.host = host
        self.port = port
        self.uri = f"ws://{host}:{port}/ws"
        self.ws = None
        self.seq = 0

    async def connect(self):
        self.ws = await websockets.connect(self.uri, max_size=10 * 1024 * 1024)
        # Read challenge.
        await asyncio.wait_for(self.ws.recv(), timeout=TIMEOUT_CONNECT)
        # Handshake.
        connect = {
            "type": "req", "id": "quality-hs", "method": "connect",
            "params": {
                "minProtocol": 1, "maxProtocol": 5,
                "client": {"id": "quality-test", "version": "1.0.0", "platform": "test", "mode": "control"},
            },
        }
        await self.ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(self.ws.recv(), timeout=TIMEOUT_CONNECT))
        if not hello.get("ok"):
            raise RuntimeError(f"Handshake failed: {json.dumps(hello)}")
        return hello.get("payload", {}).get("server", {}).get("version", "?")

    async def rpc(self, method: str, params: dict = None, timeout: float = TIMEOUT_RPC) -> dict:
        self.seq += 1
        rpc_id = f"quality-{self.seq}-{int(time.time() * 1000)}"
        msg = {"type": "req", "id": rpc_id, "method": method, "params": params or {}}
        await self.ws.send(json.dumps(msg))
        deadline = asyncio.get_event_loop().time() + timeout
        while True:
            remaining = deadline - asyncio.get_event_loop().time()
            if remaining <= 0:
                raise asyncio.TimeoutError(f"rpc {method} timed out waiting for id={rpc_id}")
            raw = await asyncio.wait_for(self.ws.recv(), timeout=remaining)
            resp = json.loads(raw)
            # Skip event frames and responses for other request IDs.
            if resp.get("type") == "evt":
                continue
            if resp.get("id") == rpc_id:
                return resp
            # Non-matching response — skip (stale from previous session).

    async def create_session(self, key: str = "") -> str:
        """Create a session and return its key."""
        if not key:
            key = f"quality-test-{int(time.time() * 1000)}"
        resp = await self.rpc("sessions.create", {"key": key, "kind": "direct"})
        if not resp.get("ok"):
            raise RuntimeError(f"sessions.create failed: {json.dumps(resp.get('error', {}))}")
        return key

    async def chat(self, message: str, session_key: str = "", timeout: float = TIMEOUT_CHAT) -> ChatCapture:
        """Send a chat message and capture ALL events until completion.

        chat.send is async: it returns {status: "started"} immediately,
        then streams results via "chat" events. We listen until state="done"
        or state="error" or state="aborted".
        """
        self.seq += 1
        rpc_id = f"quality-chat-{self.seq}-{int(time.time() * 1000)}"
        client_run_id = f"qrun-{self.seq}-{int(time.time() * 1000)}"

        # Create session if needed.
        if not session_key:
            session_key = await self.create_session()

        msg = {
            "type": "req", "id": rpc_id, "method": "chat.send",
            "params": {
                "sessionKey": session_key,
                "message": message,
                "clientRunId": client_run_id,
            },
        }

        capture = ChatCapture(start_time=time.time())
        await self.ws.send(json.dumps(msg))

        # First: read the immediate RPC response, skipping stale events.
        deadline = asyncio.get_event_loop().time() + TIMEOUT_RPC
        initial = None
        while True:
            remaining = deadline - asyncio.get_event_loop().time()
            if remaining <= 0:
                raise asyncio.TimeoutError(f"chat.send timed out waiting for RPC response")
            raw = await asyncio.wait_for(self.ws.recv(), timeout=remaining)
            frame = json.loads(raw)
            if frame.get("id") == rpc_id:
                initial = frame
                break
            # Skip event frames and non-matching responses.

        capture.events.append(initial)
        if not initial.get("ok"):
            capture.final_response = initial
            capture.end_time = time.time()
            return capture

        # Now listen for streamed events until we get state="done"/"error"/"aborted".
        # Filter by clientRunId to ignore autonomous continuation events.
        done_states = {"done", "error", "aborted"}
        while True:
            try:
                raw = await asyncio.wait_for(self.ws.recv(), timeout=timeout)
            except asyncio.TimeoutError:
                capture.end_time = time.time()
                capture.errors = [f"Timeout after {timeout}s"]
                break

            frame = json.loads(raw)
            frame["_recv_ts"] = time.time()

            # Filter: only process events for our run (skip autonomous continuations).
            payload = frame.get("payload", {})
            frame_run_id = payload.get("clientRunId", "")
            if frame_run_id and frame_run_id != client_run_id:
                continue

            capture.events.append(frame)

            # Event frames may or may not have "type" key.
            evt = frame.get("event", "")
            state = payload.get("state", "")

            if evt:
                # Text deltas (streaming).
                if evt == "chat.delta":
                    delta = payload.get("delta", "")
                    if delta:
                        capture.deltas.append({"text": delta, "ts": time.time()})
                        capture.all_text += delta

                # Main chat lifecycle events (started/done/error/aborted).
                elif evt == "chat":
                    if state in done_states:
                        capture.final_response = frame
                        capture.end_time = time.time()
                        if state == "done":
                            capture.reply_text = payload.get("text", capture.all_text)
                            capture.token_usage_data = payload.get("usage", {})
                        elif state in ("error", "aborted"):
                            capture.errors.append(payload.get("error", f"state={state}"))
                        break

                # Tool events.
                elif evt == "chat.tool":
                    if state == "started":
                        capture.tool_starts.append({
                            "name": payload.get("tool", "?"),
                            "ts": time.time(),
                        })
                    elif state == "completed":
                        capture.tool_results.append({
                            "name": payload.get("tool", "?"),
                            "isError": payload.get("isError", False),
                            "ts": time.time(),
                        })

                # Heartbeats.
                elif evt == "heartbeat":
                    capture.heartbeats.append(payload)

                # Session lifecycle.
                elif evt == "sessions.changed":
                    capture.status_changes.append(payload)

                # Skip tick events.
                elif evt == "tick":
                    pass

        # Fallback: if reply_text was never set, use accumulated deltas.
        if not capture.reply_text and capture.all_text:
            capture.reply_text = capture.all_text
        if not capture.end_time:
            capture.end_time = time.time()

        return capture

    async def close(self):
        if self.ws:
            await self.ws.close()


# --- Quality Checks ---

def check_korean_response(text: str) -> tuple[bool, str]:
    """Check if response contains Korean characters (excludes code refs)."""
    # Strip backtick-wrapped code references which are inherently English.
    prose = re.sub(r"`[^`]+`", "", text)
    korean_chars = len(re.findall(r"[\uac00-\ud7af\u1100-\u11ff\u3130-\u318f]", prose))
    total_alpha = len(re.findall(r"[a-zA-Z\uac00-\ud7af]", prose))
    if total_alpha == 0:
        # Fallback: if all content is code, check original text has any Korean.
        korean_chars = len(re.findall(r"[\uac00-\ud7af\u1100-\u11ff\u3130-\u318f]", text))
        if korean_chars > 0:
            return True, f"Korean ratio: code-heavy ({korean_chars} Korean chars)"
        return False, "no alphabetic content"
    ratio = korean_chars / max(total_alpha, 1)
    if ratio > 0.3:
        return True, f"Korean ratio: {ratio:.0%} ({korean_chars} chars)"
    return False, f"Korean ratio too low: {ratio:.0%} ({korean_chars}/{total_alpha})"


def check_no_leaked_markup(text: str) -> tuple[bool, str]:
    """Check for leaked tool call markup, thinking tags, etc."""
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
            return False, desc
    return True, "clean"


def check_telegram_safe(text: str) -> tuple[bool, str]:
    """Check if text is safe for Telegram delivery."""
    issues = []
    if len(text) > 4096:
        issues.append(f"exceeds 4096 char limit ({len(text)} chars)")
    # Unclosed HTML tags.
    open_tags = re.findall(r"<(b|i|code|pre|s|u|a|blockquote|tg-spoiler)[\s>]", text)
    close_tags = re.findall(r"</(b|i|code|pre|s|u|a|blockquote|tg-spoiler)>", text)
    if len(open_tags) != len(close_tags):
        issues.append(f"mismatched HTML tags (open={len(open_tags)}, close={len(close_tags)})")
    if issues:
        return False, "; ".join(issues)
    return True, f"length={len(text)} chars"


def check_response_substance(text: str, min_chars: int = 10, min_alpha: int = 5) -> tuple[bool, str]:
    """Check if response has actual substance (not empty/trivial)."""
    stripped = text.strip()
    if not stripped:
        return False, "empty response"
    if len(stripped) < min_chars:
        return False, f"too short ({len(stripped)} chars)"
    # Check it's not just whitespace or punctuation.
    alpha = re.findall(r"[\w]", stripped)
    if len(alpha) < min_alpha:
        return False, "no meaningful content"
    return True, f"{len(stripped)} chars"


def check_no_hallucinated_tool(capture: ChatCapture) -> tuple[bool, str]:
    """Check that tool calls actually completed (no phantom tool starts)."""
    starts = {t["name"] for t in capture.tool_starts}
    results = {t["name"] for t in capture.tool_results}
    orphaned = starts - results
    if orphaned:
        return False, f"tools started but never completed: {orphaned}"
    errors = [t for t in capture.tool_results if t.get("isError")]
    if errors:
        names = [t["name"] for t in errors]
        return False, f"tool errors: {names}"
    return True, f"{len(starts)} tools OK"


def check_latency(latency_ms: float, max_ms: float) -> tuple[bool, str]:
    """Check if response latency is within acceptable range."""
    if latency_ms <= max_ms:
        return True, f"{latency_ms:.0f}ms (limit: {max_ms:.0f}ms)"
    return False, f"{latency_ms:.0f}ms exceeds {max_ms:.0f}ms limit"


def check_streaming_flow(capture: ChatCapture) -> tuple[bool, str]:
    """Check that streaming events flowed properly."""
    if not capture.events:
        return False, "no events received"
    event_types = [e.get("event", e.get("type", "?")) for e in capture.events]
    # Should have at least some chat events.
    chat_events = [e for e in event_types if "chat" in str(e)]
    if not chat_events and capture.final_response.get("ok"):
        return True, "direct response (no streaming)"
    if chat_events:
        return True, f"{len(chat_events)} chat events, {len(capture.deltas)} deltas"
    return True, f"{len(capture.events)} total events"


def check_no_filler(text: str) -> tuple[bool, str]:
    """Check response doesn't start with AI filler phrases."""
    filler_patterns = [
        r"^(Great question|I'd be happy to|Sure,? I can|Of course|Certainly|Absolutely)",
        r"^(좋은 질문|물론이죠|당연하죠|기꺼이)",
    ]
    for pat in filler_patterns:
        match = re.match(pat, text.strip(), re.IGNORECASE)
        if match:
            return False, f"starts with filler: '{match.group()}'"
    return True, "no filler detected"


# --- Test Scenarios ---

async def test_health_quality(client: GatewayClient) -> QualityResult:
    """Test health endpoint quality — uses both HTTP and RPC."""
    import urllib.request
    result = QualityResult(name="health-quality")
    start = time.time()

    # HTTP health (detailed).
    try:
        url = f"http://{client.host}:{client.port}/health"
        with urllib.request.urlopen(url, timeout=5) as resp:
            http_health = json.loads(resp.read())
    except Exception:
        http_health = {}

    # RPC health (basic).
    rpc_resp = await client.rpc("health")
    result.latency_ms = (time.time() - start) * 1000

    rpc_ok = rpc_resp.get("ok", False)
    rpc_payload = rpc_resp.get("payload", {})

    result.add_check("rpc_success", rpc_ok, str(rpc_resp.get("error", "")))
    result.add_check("status_ok",
                     rpc_payload.get("status") == "ok" or http_health.get("status") == "ok",
                     f"rpc={rpc_payload.get('status')}, http={http_health.get('status')}")
    result.add_check("latency", *check_latency(result.latency_ms, 500))

    # HTTP-specific detailed checks.
    subs = http_health.get("subsystems", {})
    result.add_check("core_ffi", subs.get("core") == "rust-ffi", f"core={subs.get('core', 'N/A')}")
    result.add_check("vega_enabled", subs.get("vega") is True, f"vega={subs.get('vega', 'N/A')}")
    result.add_check("has_version", bool(http_health.get("version")), f"version={http_health.get('version', 'N/A')}")
    result.add_check("has_uptime", bool(http_health.get("uptime")), f"uptime={http_health.get('uptime', 'N/A')}")

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_chat_korean(client: GatewayClient) -> QualityResult:
    """Test: Korean chat response quality."""
    result = QualityResult(name="chat-korean")

    capture = await client.chat("안녕, 간단히 자기소개 해줘")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.token_usage = capture.token_usage
    result.tool_calls = [t["name"] for t in capture.tool_starts]
    result.events = [e.get("event", e.get("type")) for e in capture.events]

    # Chat completion: state="done" in final event, or "ok" in RPC response.
    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    err_detail = " ".join(capture.errors) if capture.errors else str(capture.final_response.get("error", ""))
    result.add_check("rpc_success", ok and not capture.errors, err_detail)
    result.add_check("has_reply", *check_response_substance(capture.reply_text))
    result.add_check("korean_response", *check_korean_response(capture.reply_text))
    result.add_check("no_filler", *check_no_filler(capture.reply_text))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("streaming_flow", *check_streaming_flow(capture))
    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 30000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])  # first 3 are critical
    return result


async def test_chat_tool_usage(client: GatewayClient) -> QualityResult:
    """Test: tool usage quality (does the agent use tools correctly?)."""
    result = QualityResult(name="chat-tool-usage")

    # Ask something that should trigger tool use (health check).
    capture = await client.chat("시스템 상태 확인해줘")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.token_usage = capture.token_usage
    result.tool_calls = [t["name"] for t in capture.tool_starts]
    result.events = [e.get("event", e.get("type")) for e in capture.events]

    # Chat completion: state="done" in final event, or "ok" in RPC response.
    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    err_detail = " ".join(capture.errors) if capture.errors else str(capture.final_response.get("error", ""))
    result.add_check("rpc_success", ok and not capture.errors, err_detail)
    result.add_check("has_reply", *check_response_substance(capture.reply_text))
    result.add_check("korean_response", *check_korean_response(capture.reply_text))

    # Should have used at least one tool.
    used_tools = len(capture.tool_starts) > 0
    result.add_check("used_tools", used_tools, f"tools: {[t['name'] for t in capture.tool_starts]}")
    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_chat_formatting(client: GatewayClient) -> QualityResult:
    """Test: response formatting quality."""
    result = QualityResult(name="chat-formatting")

    capture = await client.chat("마크다운으로 간단한 할일 목록 3개 만들어줘")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text

    # Chat completion: state="done" in final event, or "ok" in RPC response.
    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    err_detail = " ".join(capture.errors) if capture.errors else str(capture.final_response.get("error", ""))
    result.add_check("rpc_success", ok and not capture.errors, err_detail)
    result.add_check("has_reply", *check_response_substance(capture.reply_text))
    result.add_check("korean_response", *check_korean_response(capture.reply_text))

    # Should contain list formatting.
    has_list = bool(re.search(r"[-*\d]\.\s|[-*]\s", capture.reply_text))
    result.add_check("has_list_format", has_list, "list markers present" if has_list else "no list markers")

    # Check it has at least 3 items (numbered: "1. ", bulleted: "- ", checklist: "- [ ]").
    list_items = re.findall(r"(?:^|\n)\s*[-*]\s|(?:^|\n)\s*\d+[\.)]\s|- \[[ x]\]", capture.reply_text)
    result.add_check("has_3_items", len(list_items) >= 3, f"found {len(list_items)} items")

    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("telegram_safe", *check_telegram_safe(capture.reply_text))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_tools_file_read(client: GatewayClient) -> QualityResult:
    """Test: file read tool correctness — ask to read a known file and verify content."""
    result = QualityResult(name="tools-file-read")

    # /etc/hostname always exists on Linux and has predictable content.
    capture = await client.chat("read 도구로 /etc/hostname 파일 읽어서 내용 알려줘. 파일 내용을 그대로 보여줘.")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("rpc_success", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    # Should have used the read tool.
    read_used = any(t["name"] == "read" for t in capture.tool_starts)
    result.add_check("used_read_tool", read_used,
                     f"tools: {[t['name'] for t in capture.tool_starts]}")

    # Tool should have completed without error.
    read_errors = [t for t in capture.tool_results if t["name"] == "read" and t.get("isError")]
    result.add_check("read_no_error", len(read_errors) == 0,
                     f"read errors: {len(read_errors)}")

    # Reply should contain the actual hostname (check tool result was relayed).
    import socket
    hostname = socket.gethostname()
    has_hostname = hostname.lower() in capture.reply_text.lower()
    # Also check tool result content if available.
    if not has_hostname:
        for tr in capture.tool_results:
            if tr.get("name") == "read" and "result" in tr:
                has_hostname = hostname.lower() in tr["result"].lower()
                break
    result.add_check("result_contains_hostname", has_hostname,
                     f"expected '{hostname}' in reply")

    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_tools_grep_search(client: GatewayClient) -> QualityResult:
    """Test: grep/search tool — search for a known pattern and verify results."""
    result = QualityResult(name="tools-grep-search")

    capture = await client.chat(
        "grep 도구로 gateway-go/cmd/gateway/main.go 파일에서 'func main' 패턴을 찾아줘. "
        "몇 번째 줄에 있는지 알려줘."
    )
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("rpc_success", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    # Should have used grep or search_and_read or find.
    search_tools = {"grep", "search_and_read", "find", "read"}
    used_search = any(t["name"] in search_tools for t in capture.tool_starts)
    result.add_check("used_search_tool", used_search,
                     f"tools: {[t['name'] for t in capture.tool_starts]}")

    # Reply should mention "func main" or a line number.
    has_result = bool(re.search(r"func\s+main|line\s*\d+|\d+\s*번", capture.reply_text, re.IGNORECASE))
    result.add_check("result_has_match", has_result,
                     "reply references func main or line number")

    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_tools_exec(client: GatewayClient) -> QualityResult:
    """Test: exec tool — run a command and verify output correctness."""
    result = QualityResult(name="tools-exec")

    capture = await client.chat("exec 도구로 'echo hello-deneb-test' 명령어 실행하고 결과 보여줘")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("rpc_success", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    # Should have used exec tool.
    exec_used = any(t["name"] == "exec" for t in capture.tool_starts)
    result.add_check("used_exec_tool", exec_used,
                     f"tools: {[t['name'] for t in capture.tool_starts]}")

    # Reply or tool result should contain the echo output.
    has_output = "hello-deneb-test" in capture.reply_text
    if not has_output:
        for tr in capture.tool_results:
            if tr.get("name") == "exec" and "result" in tr:
                has_output = "hello-deneb-test" in tr["result"]
                break
    result.add_check("output_correct", has_output,
                     "expected 'hello-deneb-test' in output")

    # Exec should not have errored.
    exec_errors = [t for t in capture.tool_results if t["name"] == "exec" and t.get("isError")]
    result.add_check("exec_no_error", len(exec_errors) == 0,
                     f"exec errors: {len(exec_errors)}")

    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_tools_multi_step(client: GatewayClient) -> QualityResult:
    """Test: multi-tool chain — task requiring multiple tools in sequence."""
    result = QualityResult(name="tools-multi-step")

    capture = await client.chat(
        "gateway-go/cmd/gateway/main.go 파일의 총 줄 수를 알려줘. "
        "exec 도구로 'wc -l' 명령을 써도 되고, read 도구로 직접 읽어도 돼."
    )
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("rpc_success", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    # Should have used at least one tool.
    result.add_check("used_tools", len(capture.tool_starts) > 0,
                     f"{len(capture.tool_starts)} tools used")

    # Reply should contain a number (the line count).
    has_number = bool(re.search(r"\d{2,}", capture.reply_text))
    result.add_check("result_has_line_count", has_number,
                     "reply contains a numeric line count")

    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_tools_error_handling(client: GatewayClient) -> QualityResult:
    """Test: tool error handling — request that triggers a tool error gracefully."""
    result = QualityResult(name="tools-error-handling")

    # Ask to read a nonexistent file — should handle error gracefully.
    capture = await client.chat(
        "/tmp/nonexistent-deneb-test-file-12345.txt 파일 읽어줘"
    )
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    # The chat itself should complete (not crash), even if a tool errored.
    result.add_check("chat_completed", ok,
                     "chat completed despite tool error")

    # Reply should acknowledge the error (file not found, etc.).
    error_keywords = ["없", "존재하지", "not found", "찾을 수", "에러", "error", "실패"]
    has_error_ack = any(kw in capture.reply_text.lower() for kw in error_keywords)
    result.add_check("error_acknowledged", has_error_ack,
                     "reply acknowledges file not found")

    # Should not have crashed or returned empty (short error acks are valid).
    result.add_check("has_reply", *check_response_substance(capture.reply_text, min_chars=5))

    # No leaked internal errors.
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))

    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_tools_memory(client: GatewayClient) -> QualityResult:
    """Test: memory tool — search/status action works correctly."""
    result = QualityResult(name="tools-memory")

    capture = await client.chat("memory 도구로 현재 메모리 상태 확인해줘. status action 사용해.")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("rpc_success", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    # Should have used memory tool.
    memory_used = any(t["name"] == "memory" for t in capture.tool_starts)
    result.add_check("used_memory_tool", memory_used,
                     f"tools: {[t['name'] for t in capture.tool_starts]}")

    # Reply should contain memory status info (count, size, etc.).
    status_keywords = ["메모리", "memory", "항목", "entries", "count", "상태", "총"]
    has_status = any(kw in capture.reply_text.lower() for kw in status_keywords)
    result.add_check("result_has_status", has_status,
                     "reply contains memory status info")

    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


# --- Edge Case Test Scenarios ---

async def test_edge_empty_message(client: GatewayClient) -> QualityResult:
    """Test: empty/whitespace-only message handling."""
    result = QualityResult(name="edge-empty-message")

    capture = await client.chat("   ")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    # Should either complete gracefully or return an error — not crash.
    completed = ok or final_state in ("done", "error")
    result.add_check("no_crash", completed,
                     f"state={final_state}, ok={ok}")

    # Should not have leaked internal tokens.
    if capture.reply_text:
        result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    else:
        result.add_check("no_leaked_markup", True, "no reply (acceptable for empty input)")

    result.add_check("latency", *check_latency(capture.latency_ms, 30000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_edge_long_message(client: GatewayClient) -> QualityResult:
    """Test: very long input message handling."""
    result = QualityResult(name="edge-long-message")

    # 5000 chars of Korean text — should handle without crash.
    long_msg = "이것은 매우 긴 메시지입니다. " * 250  # ~5000 chars
    long_msg += "이 긴 메시지의 마지막에 질문: 1+1은?"
    capture = await client.chat(long_msg)
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("chat_completed", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    # Short answers are valid for a simple math question.
    result.add_check("has_reply", *check_response_substance(capture.reply_text, min_chars=1, min_alpha=1))

    # Should answer the question at the end.
    has_answer = "2" in capture.reply_text
    result.add_check("answered_question", has_answer,
                     "reply contains '2' (answer to 1+1)")

    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:2])
    return result


async def test_edge_special_chars(client: GatewayClient) -> QualityResult:
    """Test: special characters, emoji, and markup in input."""
    result = QualityResult(name="edge-special-chars")

    msg = '이모지 테스트 🎉🚀💻 & 특수문자 <b>bold</b> "quotes" \'single\' `code` $var {json}'
    capture = await client.chat(msg)
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("chat_completed", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    result.add_check("has_reply", *check_response_substance(capture.reply_text))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("telegram_safe", *check_telegram_safe(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 30000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:2])
    return result


async def test_edge_code_block(client: GatewayClient) -> QualityResult:
    """Test: code block in user message — should not confuse the parser."""
    result = QualityResult(name="edge-code-block")

    msg = """다음 코드를 설명해줘:
```python
def hello():
    print("안녕하세요")
    return {"status": "ok"}
```
이 함수가 뭘 하는 거야?"""
    capture = await client.chat(msg)
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("chat_completed", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    result.add_check("has_reply", *check_response_substance(capture.reply_text, min_chars=20))

    # Reply should reference the function or its behavior.
    code_keywords = ["함수", "function", "hello", "print", "안녕", "return", "dict", "출력"]
    has_explanation = any(kw in capture.reply_text.lower() for kw in code_keywords)
    result.add_check("explains_code", has_explanation,
                     "reply explains the code")

    result.add_check("korean_response", *check_korean_response(capture.reply_text))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 30000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_edge_ambiguous_intent(client: GatewayClient) -> QualityResult:
    """Test: ambiguous message — should respond helpfully, not use tools unnecessarily."""
    result = QualityResult(name="edge-ambiguous-intent")

    capture = await client.chat("음...")
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("chat_completed", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    result.add_check("has_reply", *check_response_substance(capture.reply_text))

    # Should NOT have used heavy tools for a vague message.
    heavy_tools = {"exec", "write", "edit", "git", "autoresearch", "gateway"}
    heavy_used = [t["name"] for t in capture.tool_starts if t["name"] in heavy_tools]
    result.add_check("no_unnecessary_tools", len(heavy_used) == 0,
                     f"heavy tools used: {heavy_used}" if heavy_used else "no heavy tools")

    result.add_check("korean_response", *check_korean_response(capture.reply_text))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 30000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:2])
    return result


async def test_edge_mixed_language(client: GatewayClient) -> QualityResult:
    """Test: mixed Korean/English input — should respond in Korean per Korean-first policy."""
    result = QualityResult(name="edge-mixed-language")

    capture = await client.chat(
        "Hey, 이 프로젝트의 main entry point가 어디에 있어? "
        "gateway-go directory 안에 있을 것 같은데 확인해줘."
    )
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    result.add_check("chat_completed", ok and not capture.errors,
                     " ".join(capture.errors) if capture.errors else "")

    result.add_check("has_reply", *check_response_substance(capture.reply_text))

    # Should still respond in Korean (Korean-first policy).
    result.add_check("korean_response", *check_korean_response(capture.reply_text))

    # Should mention gateway-go or main.go.
    path_keywords = ["gateway-go", "main.go", "cmd/gateway", "entry point", "진입점"]
    has_path = any(kw in capture.reply_text.lower() for kw in path_keywords)
    result.add_check("mentions_path", has_path,
                     "reply references the entry point location")

    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_edge_rapid_followup(client: GatewayClient) -> QualityResult:
    """Test: rapid follow-up messages in the same session — context coherence."""
    result = QualityResult(name="edge-rapid-followup")

    session_key = await client.create_session()

    # First message.
    capture1 = await client.chat("내 이름은 테스트유저야. 기억해.", session_key=session_key)
    # Second message referencing the first.
    capture2 = await client.chat("내 이름이 뭐라고 했지?", session_key=session_key)

    result.latency_ms = capture2.latency_ms
    result.reply_text = capture2.reply_text

    final_state = capture2.final_response.get("payload", {}).get("state", "")
    ok = capture2.final_response.get("ok", False) or final_state == "done"
    result.add_check("chat_completed", ok and not capture2.errors,
                     " ".join(capture2.errors) if capture2.errors else "")

    result.add_check("has_reply", *check_response_substance(capture2.reply_text))

    # Should remember "테스트유저" from the previous turn.
    has_context = "테스트유저" in capture2.reply_text
    result.add_check("remembers_context", has_context,
                     "'테스트유저' in reply" if has_context else "failed to recall name")

    result.add_check("korean_response", *check_korean_response(capture2.reply_text))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture2.reply_text))
    result.add_check("latency", *check_latency(capture2.latency_ms, 30000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:3])
    return result


async def test_custom_message(client: GatewayClient, message: str) -> QualityResult:
    """Test: custom user message with full quality checks."""
    result = QualityResult(name=f"custom: {message[:40]}")

    capture = await client.chat(message)
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.token_usage = capture.token_usage
    result.tool_calls = [t["name"] for t in capture.tool_starts]
    result.events = [e.get("event", e.get("type")) for e in capture.events]

    # Chat completion: state="done" in final event, or "ok" in RPC response.
    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    err_detail = " ".join(capture.errors) if capture.errors else str(capture.final_response.get("error", ""))
    result.add_check("rpc_success", ok and not capture.errors, err_detail)
    result.add_check("has_reply", *check_response_substance(capture.reply_text))
    result.add_check("korean_response", *check_korean_response(capture.reply_text))
    result.add_check("no_filler", *check_no_filler(capture.reply_text))
    result.add_check("no_leaked_markup", *check_no_leaked_markup(capture.reply_text))
    result.add_check("telegram_safe", *check_telegram_safe(capture.reply_text))
    result.add_check("streaming_flow", *check_streaming_flow(capture))
    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:2])
    return result


# --- Report ---

def print_report(results: list[QualityResult]):
    """Print a quality report."""
    total_checks = sum(len(r.checks) for r in results)
    passed_checks = sum(sum(1 for _, p, _ in r.checks if p) for r in results)
    total_score = sum(r.score for r in results) / max(len(results), 1)
    all_passed = all(r.passed for r in results)

    print()
    print("=" * 70)
    print(f"  QUALITY REPORT — {len(results)} scenarios, {total_checks} checks")
    print("=" * 70)
    print()

    for r in results:
        icon = "✓" if r.passed else "✗"
        print(f"  {icon} {r.summary()}")
        for name, passed, detail in r.checks:
            check_icon = "  ✓" if passed else "  ✗"
            detail_str = f" — {detail}" if detail else ""
            print(f"    {check_icon} {name}{detail_str}")

        if r.tool_calls:
            print(f"    tools: {r.tool_calls}")
        if r.token_usage:
            inp = r.token_usage.get("inputTokens", r.token_usage.get("input_tokens", "?"))
            out = r.token_usage.get("outputTokens", r.token_usage.get("output_tokens", "?"))
            print(f"    tokens: {inp} in / {out} out")
        if r.reply_text:
            preview = r.reply_text[:150].replace("\n", " ")
            if len(r.reply_text) > 150:
                preview += "..."
            print(f"    reply: {preview}")
        if r.errors:
            for e in r.errors:
                print(f"    ERROR: {e}")
        print()

    print("-" * 70)
    status = "ALL PASSED" if all_passed else "SOME FAILED"
    print(f"  {status} — {passed_checks}/{total_checks} checks passed, overall score: {total_score:.0%}")
    print("-" * 70)

    return 0 if all_passed else 1


# --- Main ---

async def run(args):
    client = GatewayClient(HOST, args.port)

    try:
        version = await client.connect()
        print(f"Connected to gateway v{version} on port {args.port}")
    except Exception as e:
        print(f"Failed to connect to {HOST}:{args.port}: {e}")
        print("Is the dev gateway running? Try: scripts/dev-live-test.sh start")
        return 1

    results = []

    try:
        if args.custom:
            r = await test_custom_message(client, args.custom)
            results.append(r)
        else:
            scenario = args.scenario

            if scenario in ("all", "health"):
                print("Running: health-quality...")
                results.append(await test_health_quality(client))

            if scenario in ("all", "chat"):
                print("Running: chat-korean...")
                results.append(await test_chat_korean(client))

            if scenario in ("all", "tools"):
                print("Running: chat-tool-usage...")
                results.append(await test_chat_tool_usage(client))

            if scenario in ("all", "format"):
                print("Running: chat-formatting...")
                results.append(await test_chat_formatting(client))

            if scenario in ("all", "tools-deep"):
                print("Running: tools-file-read...")
                results.append(await test_tools_file_read(client))
                print("Running: tools-grep-search...")
                results.append(await test_tools_grep_search(client))
                print("Running: tools-exec...")
                results.append(await test_tools_exec(client))
                print("Running: tools-multi-step...")
                results.append(await test_tools_multi_step(client))
                print("Running: tools-error-handling...")
                results.append(await test_tools_error_handling(client))
                print("Running: tools-memory...")
                results.append(await test_tools_memory(client))

            if scenario in ("all", "edge"):
                print("Running: edge-empty-message...")
                results.append(await test_edge_empty_message(client))
                print("Running: edge-long-message...")
                results.append(await test_edge_long_message(client))
                print("Running: edge-special-chars...")
                results.append(await test_edge_special_chars(client))
                print("Running: edge-code-block...")
                results.append(await test_edge_code_block(client))
                print("Running: edge-ambiguous-intent...")
                results.append(await test_edge_ambiguous_intent(client))
                print("Running: edge-mixed-language...")
                results.append(await test_edge_mixed_language(client))
                print("Running: edge-rapid-followup...")
                results.append(await test_edge_rapid_followup(client))

    except Exception as e:
        print(f"Test error: {e}")
        import traceback
        traceback.print_exc()
        return 1
    finally:
        await client.close()

    return print_report(results)


def main():
    parser = argparse.ArgumentParser(description="Deneb Gateway Quality Test")
    parser.add_argument("--port", type=int, default=PORT, help=f"Gateway port (default: {PORT})")
    parser.add_argument("--scenario", default="all",
                        choices=["all", "health", "chat", "tools", "format", "tools-deep", "edge"],
                        help="Test scenario to run")
    parser.add_argument("--custom", type=str, help="Custom chat message to test")
    parser.add_argument("--report", action="store_true", help="Run full quality report (same as --scenario all)")
    args = parser.parse_args()

    if args.report:
        args.scenario = "all"

    sys.exit(asyncio.run(run(args)))


if __name__ == "__main__":
    main()
