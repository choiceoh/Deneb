#!/usr/bin/env python3
"""
Deneb Gateway Quality Test Runner — 300 data-driven test cases.

Loads test definitions from quality-tests.yaml and executes them
against the dev gateway via WebSocket.

Usage:
    python3 scripts/dev-quality-test.py [--port 18790] [--scenario all]
    python3 scripts/dev-quality-test.py --scenario daily       # daily chat
    python3 scripts/dev-quality-test.py --scenario system      # system mgmt
    python3 scripts/dev-quality-test.py --scenario core        # original 17 quick tests
    python3 scripts/dev-quality-test.py --custom "메시지"       # custom message
    python3 scripts/dev-quality-test.py --list                 # list all tests
"""

import json
import asyncio
import sys
import time
import argparse
import re
import os
import socket
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

try:
    import websockets
except ImportError:
    print("ERROR: pip install websockets")
    sys.exit(1)

try:
    import yaml
except ImportError:
    print("ERROR: pip install pyyaml")
    sys.exit(1)

# --- Configuration ---

HOST = "127.0.0.1"
PORT = 18790
TIMEOUT_CONNECT = 5
TIMEOUT_RPC = 10
TIMEOUT_CHAT = 120

SCRIPT_DIR = Path(__file__).parent
TESTS_YAML = SCRIPT_DIR / "quality-tests.yaml"

# Legacy scenario aliases (old name -> list of new categories).
SCENARIO_ALIASES = {
    "chat":       ["daily"],
    "tools":      ["system", "task"],
    "tools-deep": ["code", "search"],
    "format":     ["format"],
    "edge":       ["edge", "safety"],
    "health":     ["health"],
}

# Core subset: the original ~17 essential tests for quick checks.
CORE_TESTS = {
    "health-rpc", "health-http",
    "daily-hi", "daily-who-are-you",
    "sys-status",
    "fmt-list-3",
    "code-read-main", "code-grep-pattern", "code-line-count",
    "task-echo", "task-pwd",
    "search-memory-status",
    "edge-empty", "edge-very-long", "edge-html-tags", "edge-code-in-msg",
    "ctx-name-recall",
    "safety-system-prompt",
    "edge-nonexistent-file",
    "reason-arithmetic",
}


# --- Data Classes ---

@dataclass
class QualityResult:
    """Quality assessment of a single test."""
    name: str
    passed: bool = False
    score: float = 0.0
    checks: list = field(default_factory=list)
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
    all_text: str = ""
    token_usage_data: dict = field(default_factory=dict)

    @property
    def latency_ms(self) -> float:
        return (self.end_time - self.start_time) * 1000

    @property
    def first_token_ms(self) -> float:
        if self.deltas:
            return (self.deltas[0]["ts"] - self.start_time) * 1000
        return 0

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
        await asyncio.wait_for(self.ws.recv(), timeout=TIMEOUT_CONNECT)
        connect = {
            "type": "req", "id": "quality-hs", "method": "connect",
            "params": {
                "minProtocol": 1, "maxProtocol": 5,
                "client": {"id": "quality-test", "version": "1.0.0",
                           "platform": "test", "mode": "control"},
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
        if not key:
            key = f"quality-test-{int(time.time() * 1000)}"
        resp = await self.rpc("sessions.create", {"key": key, "kind": "direct"})
        if not resp.get("ok"):
            raise RuntimeError(f"sessions.create failed: {json.dumps(resp.get('error', {}))}")
        return key

    async def chat(self, message: str, session_key: str = "",
                   timeout: float = TIMEOUT_CHAT) -> ChatCapture:
        self.seq += 1
        rpc_id = f"quality-chat-{self.seq}-{int(time.time() * 1000)}"
        client_run_id = f"qrun-{self.seq}-{int(time.time() * 1000)}"

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

        # Read the immediate RPC response, skipping stale events.
        deadline = asyncio.get_event_loop().time() + TIMEOUT_RPC
        initial = None
        while True:
            remaining = deadline - asyncio.get_event_loop().time()
            if remaining <= 0:
                raise asyncio.TimeoutError("chat.send timed out waiting for RPC response")
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

        # Listen for streamed events, filtering by clientRunId.
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

            evt = frame.get("event", "")
            state = payload.get("state", "")

            if evt:
                if evt == "chat.delta":
                    delta = payload.get("delta", "")
                    if delta:
                        capture.deltas.append({"text": delta, "ts": time.time()})
                        capture.all_text += delta
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
                elif evt == "chat.tool":
                    if state == "started":
                        capture.tool_starts.append({
                            "name": payload.get("tool", "?"), "ts": time.time(),
                        })
                    elif state == "completed":
                        capture.tool_results.append({
                            "name": payload.get("tool", "?"),
                            "isError": payload.get("isError", False),
                            "ts": time.time(),
                        })
                elif evt == "heartbeat":
                    capture.heartbeats.append(payload)
                elif evt == "sessions.changed":
                    capture.status_changes.append(payload)

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
    patterns = [
        (r"<function=", "leaked <function= tag"),
        (r"</?thinking>", "leaked thinking tag"),
        (r"</?artifact", "leaked artifact tag"),
        (r"\[\[reply_to", "leaked reply directive"),
        (r"MEDIA:\S+", "leaked MEDIA token"),
        (r"NO_REPLY", "leaked NO_REPLY token"),
        (r"SILENT_REPLY", "leaked SILENT_REPLY token"),
    ]
    for pat, desc in patterns:
        if re.search(pat, text):
            return False, desc
    return True, "clean"


def check_telegram_safe(text: str) -> tuple[bool, str]:
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


def check_response_substance(text: str, min_chars: int = 10, min_alpha: int = 5) -> tuple[bool, str]:
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


def check_no_hallucinated_tool(capture: ChatCapture) -> tuple[bool, str]:
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
    if latency_ms <= max_ms:
        return True, f"{latency_ms:.0f}ms (limit: {max_ms:.0f}ms)"
    return False, f"{latency_ms:.0f}ms exceeds {max_ms:.0f}ms limit"


def check_streaming_flow(capture: ChatCapture) -> tuple[bool, str]:
    if not capture.events:
        return False, "no events received"
    event_types = [e.get("event", e.get("type", "?")) for e in capture.events]
    chat_events = [e for e in event_types if "chat" in str(e)]
    if not chat_events and capture.final_response.get("ok"):
        return True, "direct response (no streaming)"
    if chat_events:
        return True, f"{len(chat_events)} chat events, {len(capture.deltas)} deltas"
    return True, f"{len(capture.events)} total events"


def check_no_filler(text: str) -> tuple[bool, str]:
    filler_patterns = [
        r"^(Great question|I'd be happy to|Sure,? I can|Of course|Certainly|Absolutely)",
        r"^(좋은 질문|물론이죠|당연하죠|기꺼이)",
    ]
    for pat in filler_patterns:
        match = re.match(pat, text.strip(), re.IGNORECASE)
        if match:
            return False, f"starts with filler: '{match.group()}'"
    return True, "no filler detected"


# --- Check Evaluator ---

def evaluate_check(check_def, capture: ChatCapture) -> tuple[str, bool, str]:
    """Evaluate a single check definition against a capture.

    Returns (check_name, passed, detail).
    """
    text = capture.reply_text

    # Simple string check: "rpc_success", "korean", etc.
    if isinstance(check_def, str):
        return _eval_simple(check_def, capture)

    # Dict check: {used_tool: "read"}, {latency: 30000}, etc.
    if isinstance(check_def, dict):
        key = next(iter(check_def))
        val = check_def[key]
        return _eval_param(key, val, capture)

    return ("unknown", False, f"unknown check type: {check_def}")


def _eval_simple(name: str, capture: ChatCapture) -> tuple[str, bool, str]:
    text = capture.reply_text

    if name == "rpc_success":
        final_state = capture.final_response.get("payload", {}).get("state", "")
        ok = capture.final_response.get("ok", False) or final_state == "done"
        err = " ".join(capture.errors) if capture.errors else ""
        return ("rpc_success", ok and not capture.errors, err or "ok")

    if name == "completed":
        final_state = capture.final_response.get("payload", {}).get("state", "")
        ok = capture.final_response.get("ok", False) or final_state in ("done", "error")
        return ("completed", ok, f"state={final_state}")

    if name == "has_reply":
        ok, detail = check_response_substance(text)
        return ("has_reply", ok, detail)

    if name == "korean":
        ok, detail = check_korean_response(text)
        return ("korean", ok, detail)

    if name == "no_filler":
        ok, detail = check_no_filler(text)
        return ("no_filler", ok, detail)

    if name == "no_leak":
        ok, detail = check_no_leaked_markup(text)
        return ("no_leak", ok, detail)

    if name == "telegram_safe":
        ok, detail = check_telegram_safe(text)
        return ("telegram_safe", ok, detail)

    if name == "streaming":
        ok, detail = check_streaming_flow(capture)
        return ("streaming", ok, detail)

    if name == "tools_clean":
        ok, detail = check_no_hallucinated_tool(capture)
        return ("tools_clean", ok, detail)

    if name == "used_tools":
        n = len(capture.tool_starts)
        return ("used_tools", n > 0, f"{n} tools used")

    if name == "no_heavy_tools":
        heavy = {"exec", "write", "edit", "git", "autoresearch", "gateway"}
        used = [t["name"] for t in capture.tool_starts if t["name"] in heavy]
        return ("no_heavy_tools", len(used) == 0,
                f"heavy: {used}" if used else "no heavy tools")

    if name == "contains_hostname":
        hostname = socket.gethostname()
        found = hostname.lower() in text.lower()
        return ("contains_hostname", found, f"expected '{hostname}'")

    if name == "has_number":
        found = bool(re.search(r"\d{2,}", text))
        return ("has_number", found, "found number" if found else "no number")

    if name == "has_code_block":
        found = bool(re.search(r"```", text))
        return ("has_code_block", found, "code block present" if found else "no code block")

    return (name, False, f"unknown simple check: {name}")


def _eval_param(key: str, val, capture: ChatCapture) -> tuple[str, bool, str]:
    text = capture.reply_text

    if key == "latency":
        ok, detail = check_latency(capture.latency_ms, float(val))
        return ("latency", ok, detail)

    if key == "used_tool":
        found = any(t["name"] == val for t in capture.tool_starts)
        tools = [t["name"] for t in capture.tool_starts]
        return ("used_tool", found, f"tools: {tools}")

    if key == "used_any":
        tools_set = set(val) if isinstance(val, list) else {val}
        found = any(t["name"] in tools_set for t in capture.tool_starts)
        tools = [t["name"] for t in capture.tool_starts]
        return ("used_any", found, f"tools: {tools}")

    if key == "not_used":
        found = any(t["name"] == val for t in capture.tool_starts)
        return ("not_used", not found, f"{'found' if found else 'not found'}: {val}")

    if key == "contains":
        found = val.lower() in text.lower()
        return ("contains", found,
                f"found '{val}'" if found else f"'{val}' not in reply")

    if key == "contains_any":
        matches = [v for v in val if v.lower() in text.lower()]
        return ("contains_any", len(matches) > 0,
                f"matched: {matches}" if matches else f"none of {val} found")

    if key == "not_contains":
        found = val.lower() in text.lower()
        return ("not_contains", not found,
                f"'{val}' absent" if not found else f"found '{val}' (unexpected)")

    if key == "min_length":
        ok = len(text) >= int(val)
        return ("min_length", ok, f"{len(text)} chars (min: {val})")

    if key == "has_list":
        min_items = int(val)
        items = re.findall(r"[-*\d]+[\.)]\s|[-*]\s|\d+\.", text)
        return ("has_list", len(items) >= min_items,
                f"{len(items)} items (min: {min_items})")

    if key == "has_reply":
        # Parameterized has_reply: {has_reply: {min_chars: 20}}
        if isinstance(val, dict):
            min_c = val.get("min_chars", 10)
        else:
            min_c = int(val) if val else 10
        ok, detail = check_response_substance(text, min_c)
        return ("has_reply", ok, detail)

    return (key, False, f"unknown param check: {key}={val}")


# --- Generated Messages ---

def generate_message(gen_type: str) -> str:
    """Generate special test messages that can't be expressed in YAML."""
    if gen_type == "long_korean":
        return "이것은 매우 긴 메시지입니다. " * 250 + "마지막 질문: 1+1은?"
    return f"(unknown gen: {gen_type})"


# --- YAML Loader ---

def load_tests(path: Path) -> tuple[dict, dict, list]:
    """Load test definitions from YAML.

    Returns (profiles, category_defaults, tests).
    """
    with open(path) as f:
        data = yaml.safe_load(f)
    return data.get("profiles", {}), data.get("category_defaults", {}), data.get("tests", [])


def resolve_checks(tdef: dict, profiles: dict, cat_defaults: dict) -> list:
    """Merge profile checks + test-specific checks."""
    checks = []

    # Get profile (explicit or from category default).
    profile_name = tdef.get("profile")
    if profile_name is None:
        cat = tdef.get("cat", "")
        cat_default = cat_defaults.get(cat, {})
        profile_name = cat_default.get("profile")

    if profile_name and profile_name in profiles:
        checks.extend(profiles[profile_name])

    # Add test-specific checks.
    if "chk" in tdef:
        checks.extend(tdef["chk"])

    return checks


def get_timeout(tdef: dict, cat_defaults: dict) -> float:
    """Get timeout for a test."""
    if "timeout" in tdef:
        return float(tdef["timeout"])
    cat = tdef.get("cat", "")
    cat_default = cat_defaults.get(cat, {})
    return float(cat_default.get("timeout", TIMEOUT_CHAT))


def get_critical(tdef: dict, cat_defaults: dict) -> object:
    """Get critical check count (int or 'all')."""
    if "critical" in tdef:
        return tdef["critical"]
    cat = tdef.get("cat", "")
    cat_default = cat_defaults.get(cat, {})
    return cat_default.get("critical", 3)


# --- Health Tests ---

async def run_health_test(client: GatewayClient, tdef: dict) -> QualityResult:
    """Run a health-specific test."""
    import urllib.request

    name = tdef["name"]
    health_type = tdef.get("health", "rpc")
    result = QualityResult(name=name)
    start = time.time()

    if health_type == "rpc":
        resp = await client.rpc("health")
        result.latency_ms = (time.time() - start) * 1000
        ok = resp.get("ok", False)
        result.add_check("rpc_success", ok, str(resp.get("error", "")))
        payload = resp.get("payload", {})
        result.add_check("status_ok", payload.get("status") == "ok",
                         f"status={payload.get('status')}")
        result.add_check("latency", *check_latency(result.latency_ms, 500))

    elif health_type == "http":
        try:
            url = f"http://{client.host}:{client.port}/health"
            with urllib.request.urlopen(url, timeout=5) as resp:
                data = json.loads(resp.read())
            result.latency_ms = (time.time() - start) * 1000
            result.add_check("http_ok", data.get("status") == "ok",
                             f"status={data.get('status')}")
            result.add_check("has_version", bool(data.get("version")),
                             f"version={data.get('version', 'N/A')}")
            result.add_check("has_uptime", bool(data.get("uptime")),
                             f"uptime={data.get('uptime', 'N/A')}")
            result.add_check("latency", *check_latency(result.latency_ms, 500))
        except Exception as e:
            result.latency_ms = (time.time() - start) * 1000
            result.add_check("http_ok", False, str(e))

    elif health_type == "core":
        try:
            url = f"http://{client.host}:{client.port}/health"
            with urllib.request.urlopen(url, timeout=5) as resp:
                data = json.loads(resp.read())
            result.latency_ms = (time.time() - start) * 1000
            subs = data.get("subsystems", {})
            result.add_check("core_ffi", subs.get("core") == "rust-ffi",
                             f"core={subs.get('core', 'N/A')}")
            result.add_check("latency", *check_latency(result.latency_ms, 500))
        except Exception as e:
            result.latency_ms = (time.time() - start) * 1000
            result.add_check("core_ffi", False, str(e))

    elif health_type == "vega":
        try:
            url = f"http://{client.host}:{client.port}/health"
            with urllib.request.urlopen(url, timeout=5) as resp:
                data = json.loads(resp.read())
            result.latency_ms = (time.time() - start) * 1000
            subs = data.get("subsystems", {})
            result.add_check("vega_enabled", subs.get("vega") is True,
                             f"vega={subs.get('vega', 'N/A')}")
            result.add_check("latency", *check_latency(result.latency_ms, 500))
        except Exception as e:
            result.latency_ms = (time.time() - start) * 1000
            result.add_check("vega_enabled", False, str(e))

    elif health_type == "version":
        resp = await client.rpc("health")
        result.latency_ms = (time.time() - start) * 1000
        payload = resp.get("payload", {})
        version = payload.get("version", "")
        result.add_check("has_version", bool(version), f"version={version}")
        result.add_check("latency", *check_latency(result.latency_ms, 500))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks)
    return result


# --- Test Runner ---

async def run_chat_test(client: GatewayClient, tdef: dict,
                        profiles: dict, cat_defaults: dict) -> QualityResult:
    """Run a single-turn chat test."""
    name = tdef["name"]
    result = QualityResult(name=name)
    timeout = get_timeout(tdef, cat_defaults)

    # Get or generate message.
    if "gen" in tdef:
        msg = generate_message(tdef["gen"])
    else:
        msg = tdef.get("msg", "")

    if not msg:
        result.add_check("has_message", False, "no message defined")
        return result

    try:
        capture = await client.chat(msg, timeout=timeout)
    except Exception as e:
        result.add_check("rpc_success", False, str(e))
        result.passed = False
        return result

    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.token_usage = capture.token_usage
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    # Evaluate all checks.
    checks = resolve_checks(tdef, profiles, cat_defaults)
    for chk in checks:
        chk_name, passed, detail = evaluate_check(chk, capture)
        result.add_check(chk_name, passed, detail)

    # Score and pass/fail.
    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    critical = get_critical(tdef, cat_defaults)
    if critical == "all":
        result.passed = all(p for _, p, _ in result.checks)
    else:
        result.passed = all(p for _, p, _ in result.checks[:int(critical)])
    return result


async def run_multiturn_test(client: GatewayClient, tdef: dict,
                             profiles: dict, cat_defaults: dict) -> QualityResult:
    """Run a multi-turn chat test."""
    name = tdef["name"]
    result = QualityResult(name=name)
    timeout = get_timeout(tdef, cat_defaults)
    turns = tdef.get("turns", [])

    if not turns:
        result.add_check("has_turns", False, "no turns defined")
        return result

    try:
        session_key = await client.create_session()
        last_capture = None
        for turn in turns:
            msg = turn.get("msg", "")
            if msg:
                last_capture = await client.chat(msg, session_key=session_key,
                                                 timeout=timeout)
    except Exception as e:
        result.add_check("rpc_success", False, str(e))
        result.passed = False
        return result

    if not last_capture:
        result.add_check("has_response", False, "no response captured")
        return result

    result.latency_ms = last_capture.latency_ms
    result.reply_text = last_capture.reply_text
    result.token_usage = last_capture.token_usage
    result.tool_calls = [t["name"] for t in last_capture.tool_starts]

    # Evaluate checks (from profile + test-level chk).
    checks = resolve_checks(tdef, profiles, cat_defaults)
    for chk in checks:
        chk_name, passed, detail = evaluate_check(chk, last_capture)
        result.add_check(chk_name, passed, detail)

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    critical = get_critical(tdef, cat_defaults)
    if critical == "all":
        result.passed = all(p for _, p, _ in result.checks)
    else:
        result.passed = all(p for _, p, _ in result.checks[:int(critical)])
    return result


async def run_test(client: GatewayClient, tdef: dict,
                   profiles: dict, cat_defaults: dict) -> QualityResult:
    """Dispatch to the right runner based on test type."""
    if "health" in tdef:
        return await run_health_test(client, tdef)
    elif "turns" in tdef:
        return await run_multiturn_test(client, tdef, profiles, cat_defaults)
    else:
        return await run_chat_test(client, tdef, profiles, cat_defaults)


async def run_custom(client: GatewayClient, message: str) -> QualityResult:
    """Run a custom message test with full checks."""
    result = QualityResult(name=f"custom: {message[:40]}")

    capture = await client.chat(message)
    result.latency_ms = capture.latency_ms
    result.reply_text = capture.reply_text
    result.token_usage = capture.token_usage
    result.tool_calls = [t["name"] for t in capture.tool_starts]

    final_state = capture.final_response.get("payload", {}).get("state", "")
    ok = capture.final_response.get("ok", False) or final_state == "done"
    err = " ".join(capture.errors) if capture.errors else str(capture.final_response.get("error", ""))
    result.add_check("rpc_success", ok and not capture.errors, err)
    result.add_check("has_reply", *check_response_substance(capture.reply_text))
    result.add_check("korean", *check_korean_response(capture.reply_text))
    result.add_check("no_filler", *check_no_filler(capture.reply_text))
    result.add_check("no_leak", *check_no_leaked_markup(capture.reply_text))
    result.add_check("telegram_safe", *check_telegram_safe(capture.reply_text))
    result.add_check("streaming", *check_streaming_flow(capture))
    result.add_check("tools_clean", *check_no_hallucinated_tool(capture))
    result.add_check("latency", *check_latency(capture.latency_ms, 60000))

    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.score = passed_count / max(len(result.checks), 1)
    result.passed = all(p for _, p, _ in result.checks[:2])
    return result


# --- Report ---

def print_report(results: list[QualityResult], json_output: bool = False) -> int:
    total_checks = sum(len(r.checks) for r in results)
    passed_checks = sum(sum(1 for _, p, _ in r.checks if p) for r in results)
    total_score = sum(r.score for r in results) / max(len(results), 1)
    all_passed = all(r.passed for r in results)

    if json_output:
        data = {
            "total_tests": len(results),
            "passed_tests": sum(1 for r in results if r.passed),
            "total_checks": total_checks,
            "passed_checks": passed_checks,
            "overall_score": round(total_score, 3),
            "all_passed": all_passed,
            "tests": [],
        }
        for r in results:
            tdata = {
                "name": r.name,
                "passed": r.passed,
                "score": round(r.score, 3),
                "latency_ms": round(r.latency_ms),
                "checks": [{"name": n, "passed": p, "detail": d}
                           for n, p, d in r.checks],
            }
            if r.tool_calls:
                tdata["tools"] = r.tool_calls
            if r.errors:
                tdata["errors"] = r.errors
            data["tests"].append(tdata)
        print(json.dumps(data, indent=2, ensure_ascii=False))
        return 0 if all_passed else 1

    print()
    print("=" * 70)
    print(f"  QUALITY REPORT — {len(results)} scenarios, {total_checks} checks")
    print("=" * 70)
    print()

    # Group by category.
    from collections import OrderedDict
    by_cat = OrderedDict()
    for r in results:
        cat = r.name.split("-")[0] if "-" in r.name else "other"
        by_cat.setdefault(cat, []).append(r)

    for cat, cat_results in by_cat.items():
        cat_passed = sum(1 for r in cat_results if r.passed)
        cat_total = len(cat_results)
        cat_icon = "✓" if cat_passed == cat_total else "✗"
        print(f"  {cat_icon} [{cat}] {cat_passed}/{cat_total} passed")

        for r in cat_results:
            icon = "  ✓" if r.passed else "  ✗"
            print(f"    {icon} {r.summary()}")
            for name, passed, detail in r.checks:
                check_icon = "    ✓" if passed else "    ✗"
                detail_str = f" — {detail}" if detail else ""
                print(f"      {check_icon} {name}{detail_str}")

            if r.tool_calls:
                print(f"      tools: {r.tool_calls}")
            if r.token_usage:
                inp = r.token_usage.get("inputTokens",
                                        r.token_usage.get("input_tokens", "?"))
                out = r.token_usage.get("outputTokens",
                                        r.token_usage.get("output_tokens", "?"))
                print(f"      tokens: {inp} in / {out} out")
            if r.reply_text:
                preview = r.reply_text[:120].replace("\n", " ")
                if len(r.reply_text) > 120:
                    preview += "..."
                print(f"      reply: {preview}")
            if r.errors:
                for e in r.errors:
                    print(f"      ERROR: {e}")
        print()

    print("-" * 70)
    status = "ALL PASSED" if all_passed else "SOME FAILED"
    failed = [r.name for r in results if not r.passed]
    print(f"  {status} — {passed_checks}/{total_checks} checks, "
          f"score: {total_score:.0%}, tests: {sum(1 for r in results if r.passed)}/{len(results)}")
    if failed and len(failed) <= 20:
        print(f"  failed: {', '.join(failed)}")
    elif failed:
        print(f"  failed: {len(failed)} tests")
    print("-" * 70)

    return 0 if all_passed else 1


def list_tests(tests: list, scenario: str = "all") -> None:
    """Print available tests."""
    # Collect categories.
    cats = {}
    for t in tests:
        cat = t.get("cat", "?")
        cats.setdefault(cat, []).append(t["name"])

    all_cats = set()
    if scenario == "all":
        all_cats = set(cats.keys())
    elif scenario == "core":
        all_cats = set(cats.keys())  # show all, mark core
    elif scenario in SCENARIO_ALIASES:
        all_cats = set(SCENARIO_ALIASES[scenario])
    else:
        all_cats = {scenario}

    print(f"Available tests ({sum(len(v) for v in cats.values())} total):")
    print()
    for cat, names in cats.items():
        marker = " *" if scenario != "all" and cat not in all_cats else ""
        print(f"  [{cat}] ({len(names)} tests){marker}")
        for n in names:
            core_mark = " (core)" if n in CORE_TESTS else ""
            print(f"    - {n}{core_mark}")
    print()
    if scenario == "core":
        print(f"  Core tests: {len(CORE_TESTS)}")


# --- Main ---

async def run(args):
    # Load test definitions.
    if not TESTS_YAML.exists():
        print(f"ERROR: {TESTS_YAML} not found")
        return 1

    profiles, cat_defaults, all_tests = load_tests(TESTS_YAML)

    if args.list:
        list_tests(all_tests, args.scenario)
        return 0

    # Filter tests by scenario.
    scenario = args.scenario
    if scenario == "all":
        tests = all_tests
    elif scenario == "core":
        tests = [t for t in all_tests if t["name"] in CORE_TESTS]
    elif scenario in SCENARIO_ALIASES:
        cats = set(SCENARIO_ALIASES[scenario])
        tests = [t for t in all_tests if t.get("cat") in cats]
    else:
        # Direct category name.
        tests = [t for t in all_tests if t.get("cat") == scenario]

    if not tests and not args.custom:
        print(f"No tests found for scenario '{scenario}'")
        print(f"Available categories: {sorted(set(t.get('cat') for t in all_tests))}")
        return 1

    # Connectivity check.
    probe = GatewayClient(HOST, args.port)
    try:
        version = await probe.connect()
        count = len(tests) if not args.custom else 1
        conc = args.concurrency
        conc_label = f", concurrency={conc}" if conc > 1 else ""
        print(f"Connected to gateway v{version} on port {args.port} — running {count} tests{conc_label}")
    except Exception as e:
        print(f"Failed to connect to {HOST}:{args.port}: {e}")
        print("Is the dev gateway running? Try: scripts/dev-live-test.sh start")
        return 1
    finally:
        await probe.close()

    results = []

    try:
        if args.custom:
            client = GatewayClient(HOST, args.port)
            await client.connect()
            try:
                r = await run_custom(client, args.custom)
                results.append(r)
            finally:
                await client.close()
        elif args.concurrency <= 1:
            # Sequential mode (legacy behavior).
            client = GatewayClient(HOST, args.port)
            await client.connect()
            try:
                for i, tdef in enumerate(tests, 1):
                    name = tdef["name"]
                    total = len(tests)
                    print(f"[{i}/{total}] {name}...")
                    try:
                        r = await run_test(client, tdef, profiles, cat_defaults)
                        results.append(r)
                        status = "PASS" if r.passed else "FAIL"
                        print(f"  {status} ({r.latency_ms:.0f}ms)")
                    except Exception as e:
                        r = QualityResult(name=name)
                        r.add_check("execution", False, str(e))
                        results.append(r)
                        print(f"  ERROR: {e}")
            finally:
                await client.close()
        else:
            # Concurrent mode: semaphore(N) + pipelining.
            # Scoring/printing happens outside the semaphore so the slot
            # is freed as soon as the LLM response is received.
            sem = asyncio.Semaphore(args.concurrency)
            total = len(tests)
            done_count = 0
            print_lock = asyncio.Lock()

            async def _run_one(idx: int, tdef: dict) -> QualityResult:
                nonlocal done_count
                name = tdef["name"]
                c = GatewayClient(HOST, args.port)
                async with sem:
                    await c.connect()
                    try:
                        r = await run_test(c, tdef, profiles, cat_defaults)
                    except Exception as e:
                        r = QualityResult(name=name)
                        r.add_check("execution", False, str(e))
                # Close + print outside sem — frees slot immediately after LLM response.
                await c.close()
                async with print_lock:
                    done_count += 1
                    status = "PASS" if r.passed else ("FAIL" if r.checks else "ERROR")
                    print(f"[{done_count}/{total}] {name}... {status} ({r.latency_ms:.0f}ms)")
                return r

            tasks = [asyncio.create_task(_run_one(i, t)) for i, t in enumerate(tests)]
            done_results = await asyncio.gather(*tasks, return_exceptions=True)
            for r in done_results:
                if isinstance(r, BaseException):
                    rr = QualityResult(name="unknown")
                    rr.add_check("execution", False, str(r))
                    results.append(rr)
                else:
                    results.append(r)

    except KeyboardInterrupt:
        print("\nInterrupted — showing partial results")
    except Exception as e:
        print(f"Test error: {e}")
        import traceback
        traceback.print_exc()

    if not results:
        print("No results")
        return 1

    return print_report(results, json_output=args.json)


def main():
    all_scenarios = [
        "all", "core",
        # New categories.
        "health", "daily", "system", "code", "task", "search",
        "knowledge", "format", "context", "edge", "safety",
        "korean", "persona", "reasoning",
        # Legacy aliases.
        "chat", "tools", "tools-deep",
    ]

    parser = argparse.ArgumentParser(description="Deneb Gateway Quality Test (300 cases)")
    parser.add_argument("--port", type=int, default=PORT,
                        help=f"Gateway port (default: {PORT})")
    parser.add_argument("--scenario", default="all", choices=all_scenarios,
                        help="Test scenario/category to run")
    parser.add_argument("--custom", type=str,
                        help="Custom chat message to test")
    parser.add_argument("--list", action="store_true",
                        help="List all available tests")
    parser.add_argument("--json", action="store_true",
                        help="Output JSON report")
    parser.add_argument("--concurrency", type=int, default=2,
                        help="Max concurrent test runners (default: 2)")
    parser.add_argument("--report", action="store_true",
                        help="Run full quality report (same as --scenario all)")
    args = parser.parse_args()

    if args.report:
        args.scenario = "all"

    sys.exit(asyncio.run(run(args)))


if __name__ == "__main__":
    main()
