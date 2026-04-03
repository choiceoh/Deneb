#!/usr/bin/env python3
"""
Virtual Telegram — 클로드 코드가 진짜 텔레그램 사용자처럼 Deneb을 테스트.

로컬에 가짜 Telegram API 서버를 띄우고, 게이트웨이가 진짜 텔레그램에 연결된 것처럼
동작하게 합니다. 클로드 코드가 직접 메시지를 보내고, 응답을 확인하며, 품질을 평가합니다.

Architecture:
    vchat.py start   →  Mock Telegram API (18792) + Gateway (18790) 시작
    vchat.py send    →  HTTP로 메시지 주입, 응답 대기, 렌더링된 결과 출력
    vchat.py stop    →  정리

    세션이 유지되므로 멀티턴 대화 가능:
        vchat.py send "안녕"
        vchat.py send "내 이름은 Peter야"
        vchat.py send "내 이름이 뭐라고 했지?"  # 컨텍스트 유지 확인
"""

import argparse
import http.server
import json
import os
import queue
import re
import signal
import socketserver
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

# ─── Configuration ───────────────────────────────────────────────────────────

MOCK_PORT = 18792
GATEWAY_PORT = 18790
BOT_TOKEN = "vchat-test-000000000:AAF-mock-token-for-local-testing"
BOT_USER = {
    "id": 999999999,
    "is_bot": True,
    "first_name": "Deneb",
    "username": "deneb_vchat_bot",
}
CHAT_ID = 42424242
USER = {
    "id": 100000001,
    "is_bot": False,
    "first_name": "Peter",
    "username": "peter",
}

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEV_BINARY = "/tmp/deneb-gateway-live"
DEV_LOG = "/tmp/deneb-vchat.log"
DEV_CONFIG = "/tmp/deneb-vchat-config.json"
PID_FILE = "/tmp/deneb-vchat.pid"

CONTROL_BASE = f"http://127.0.0.1:{MOCK_PORT}/control"

# Response detection: no new events for this many seconds → response complete.
SETTLE_SECS = 3.0
# Max wait per message.
TIMEOUT_SECS = 120


# ─── ANSI Helpers ────────────────────────────────────────────────────────────

class C:
    CYAN = "\033[36m"
    MAGENTA = "\033[35m"
    YELLOW = "\033[33m"
    BLUE = "\033[34m"
    GREEN = "\033[32m"
    RED = "\033[31m"
    GRAY = "\033[90m"
    BOLD = "\033[1m"
    DIM = "\033[2m"
    ITALIC = "\033[3m"
    UNDERLINE = "\033[4m"
    STRIKE = "\033[9m"
    RESET = "\033[0m"


# ─── Mock Telegram State (server-side only) ──────────────────────────────────

class MockState:
    def __init__(self):
        self.update_queue: queue.Queue = queue.Queue()
        self._update_id = 0
        self._message_id = 0
        self.events: list[dict] = []
        self._lock = threading.Lock()

    def next_update_id(self):
        with self._lock:
            self._update_id += 1
            return self._update_id

    def next_message_id(self):
        with self._lock:
            self._message_id += 1
            return self._message_id

    def add_event(self, etype, data):
        evt = {"time": time.time(), "type": etype, "data": data}
        with self._lock:
            self.events.append(evt)
        return evt

    def inject_user_message(self, text):
        uid = self.next_update_id()
        mid = self.next_message_id()
        update = {
            "update_id": uid,
            "message": {
                "message_id": mid,
                "from": USER,
                "chat": {"id": CHAT_ID, "type": "private"},
                "date": int(time.time()),
                "text": text,
            },
        }
        self.add_event("user_message", {"text": text, "message_id": mid})
        self.update_queue.put(update)
        return mid

    def events_since(self, idx):
        with self._lock:
            return list(self.events[idx:])

    def event_count(self):
        with self._lock:
            return len(self.events)

    def to_json_since(self, idx):
        with self._lock:
            return list(self.events[idx:])


# Global state — only used in server process.
state = MockState()


# ─── Mock Telegram API Server ────────────────────────────────────────────────

class MockHandler(http.server.BaseHTTPRequestHandler):

    def log_message(self, *args):
        pass

    def _read_body(self):
        cl = int(self.headers.get("Content-Length", 0))
        return self.rfile.read(cl) if cl > 0 else b""

    def _json(self, data, status=200):
        body = json.dumps(data, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _ok(self, result):
        self._json({"ok": True, "result": result})

    # ── Routing ──

    def do_GET(self):
        if self.path.startswith("/control/"):
            self._handle_control_get()
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path.startswith("/control/"):
            self._handle_control_post()
            return

        prefix = f"/bot{BOT_TOKEN}/"
        if not self.path.startswith(prefix):
            self.send_error(404)
            return

        method = self.path[len(prefix):]
        raw = self._read_body()

        ct = self.headers.get("Content-Type", "")
        if "multipart/form-data" in ct:
            self._handle_upload(method)
            return

        params = json.loads(raw) if raw else {}
        handler = getattr(self, f"_tg_{method}", None)
        if handler:
            handler(params)
        else:
            self._ok({})

    # ── Control API (for Claude Code to send messages + read timeline) ──

    def _handle_control_get(self):
        path = self.path.split("?")[0]
        qs = self.path.split("?", 1)[1] if "?" in self.path else ""
        params = dict(p.split("=", 1) for p in qs.split("&") if "=" in p)

        if path == "/control/status":
            self._json({"running": True, "events": state.event_count()})

        elif path == "/control/timeline":
            since = int(params.get("since", 0))
            events = state.to_json_since(since)
            self._json({"events": events, "total": state.event_count()})

        else:
            self.send_error(404)

    def _handle_control_post(self):
        path = self.path.split("?")[0]
        raw = self._read_body()
        params = json.loads(raw) if raw else {}

        if path == "/control/send":
            text = params.get("text", "")
            if not text:
                self._json({"error": "text required"}, 400)
                return
            mid = state.inject_user_message(text)
            self._json({"ok": True, "message_id": mid, "event_index": state.event_count()})

        elif path == "/control/reset":
            state.__init__()
            self._json({"ok": True})

        else:
            self.send_error(404)

    # ── Telegram Bot API ──

    def _tg_getMe(self, params):
        self._ok(BOT_USER)

    def _tg_getUpdates(self, params):
        timeout = params.get("timeout", 30)
        try:
            update = state.update_queue.get(timeout=timeout)
            self._ok([update])
        except queue.Empty:
            self._ok([])

    def _tg_sendMessage(self, params):
        mid = state.next_message_id()
        state.add_event("bot_message", {
            "message_id": mid,
            "text": params.get("text", ""),
            "parse_mode": params.get("parse_mode", ""),
            "reply_markup": params.get("reply_markup"),
        })
        self._ok({
            "message_id": mid,
            "from": BOT_USER,
            "chat": {"id": CHAT_ID, "type": "private"},
            "date": int(time.time()),
            "text": params.get("text", ""),
        })

    def _tg_editMessageText(self, params):
        mid = params.get("message_id")
        state.add_event("bot_edit", {
            "message_id": mid,
            "text": params.get("text", ""),
            "parse_mode": params.get("parse_mode", ""),
            "reply_markup": params.get("reply_markup"),
        })
        self._ok({
            "message_id": mid,
            "chat": {"id": CHAT_ID, "type": "private"},
            "text": params.get("text", ""),
        })

    def _tg_deleteMessage(self, params):
        state.add_event("bot_delete", {"message_id": params.get("message_id")})
        self._ok(True)

    def _tg_sendChatAction(self, params):
        state.add_event("typing", {"action": params.get("action", "typing")})
        self._ok(True)

    def _tg_setMessageReaction(self, params):
        reactions = params.get("reaction", [])
        emoji = reactions[0]["emoji"] if reactions else ""
        state.add_event("reaction", {
            "message_id": params.get("message_id"),
            "emoji": emoji,
        })
        self._ok(True)

    def _tg_answerCallbackQuery(self, params):
        self._ok(True)

    def _handle_upload(self, method):
        mid = state.next_message_id()
        etype = {
            "sendDocument": "bot_document", "sendPhoto": "bot_photo",
            "sendVideo": "bot_video", "sendAudio": "bot_audio",
            "sendVoice": "bot_voice",
        }.get(method, "bot_upload")
        state.add_event(etype, {"message_id": mid, "method": method})
        self._ok({
            "message_id": mid, "from": BOT_USER,
            "chat": {"id": CHAT_ID, "type": "private"},
            "date": int(time.time()),
        })


# ─── Server: start subcommand ───────────────────────────────────────────────

def cmd_start(args):
    """Start mock Telegram API server + dev gateway. Blocks until killed."""

    # Check for existing instance.
    if os.path.exists(PID_FILE):
        try:
            with open(PID_FILE) as f:
                old_pid = int(f.read().strip())
            os.kill(old_pid, 0)
            print(f"vchat already running (pid {old_pid}). Use 'vchat.py stop' first.")
            sys.exit(1)
        except (OSError, ValueError):
            os.remove(PID_FILE)

    # Write our PID.
    with open(PID_FILE, "w") as f:
        f.write(str(os.getpid()))

    # 1. Start mock server (threaded — getUpdates long-poll must not block control API).
    print(f"mock: starting on :{MOCK_PORT}")

    class ThreadedHTTPServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True
        allow_reuse_address = True

    server = ThreadedHTTPServer(("127.0.0.1", MOCK_PORT), MockHandler)
    mock_thread = threading.Thread(target=server.serve_forever, daemon=True)
    mock_thread.start()

    # 2. Build gateway if needed.
    if not args.no_build and not os.path.exists(DEV_BINARY):
        print("build: compiling gateway...")
        r = subprocess.run(["make", "go"], cwd=REPO_ROOT, capture_output=True, text=True, timeout=300)
        if r.returncode != 0:
            print(f"build: FAILED\n{r.stderr[-500:]}")
            sys.exit(1)
        src = os.path.join(REPO_ROOT, "gateway-go", "deneb-gateway")
        if os.path.exists(src):
            subprocess.run(["cp", src, DEV_BINARY], check=True)

    if not os.path.exists(DEV_BINARY):
        print(f"build: binary not found at {DEV_BINARY}")
        sys.exit(1)

    # 3. Write config: merge production config with vchat Telegram override.
    prod_config = {}
    for p in [os.path.expanduser("~/.deneb/deneb.json"), os.environ.get("DENEB_CONFIG_PATH", "")]:
        if p and os.path.exists(p):
            try:
                with open(p) as f:
                    prod_config = json.load(f)
                break
            except Exception:
                pass

    # Inject mock Telegram channel config (override any existing telegram section).
    if "channels" not in prod_config:
        prod_config["channels"] = {}
    prod_config["channels"]["telegram"] = {
        "botToken": BOT_TOKEN,
        "dmPolicy": "open",
        "streaming": "partial",
        "reactionLevel": "extensive",
    }
    with open(DEV_CONFIG, "w") as f:
        json.dump(prod_config, f)

    # 4. Start gateway.
    print(f"gateway: starting on :{GATEWAY_PORT}")
    env = os.environ.copy()
    env["DENEB_CONFIG_PATH"] = DEV_CONFIG
    env["TELEGRAM_API_BASE"] = f"http://127.0.0.1:{MOCK_PORT}/bot"

    log_file = open(DEV_LOG, "w")
    gw = subprocess.Popen(
        [DEV_BINARY, "--bind", "loopback", "--port", str(GATEWAY_PORT)],
        env=env, stdout=log_file, stderr=subprocess.STDOUT,
    )

    # Wait for health.
    ready = False
    for _ in range(60):
        try:
            urllib.request.urlopen(f"http://127.0.0.1:{GATEWAY_PORT}/health", timeout=1)
            ready = True
            break
        except Exception:
            if gw.poll() is not None:
                break
            time.sleep(0.3)

    if not ready:
        print("gateway: FAILED to start. Check " + DEV_LOG)
        gw.kill()
        log_file.close()
        os.remove(PID_FILE)
        sys.exit(1)

    # Wait for Telegram plugin to connect (getMe + first poll cycle).
    time.sleep(2)
    print(f"ready: mock=:{MOCK_PORT} gateway=:{GATEWAY_PORT} log={DEV_LOG}")
    sys.stdout.flush()

    # Block until killed.
    def cleanup(sig=None, frame=None):
        gw.terminate()
        try:
            gw.wait(timeout=5)
        except subprocess.TimeoutExpired:
            gw.kill()
        log_file.close()
        server.shutdown()
        try:
            os.remove(PID_FILE)
        except OSError:
            pass
        if sig:
            sys.exit(0)

    signal.signal(signal.SIGINT, cleanup)
    signal.signal(signal.SIGTERM, cleanup)

    try:
        gw.wait()
    finally:
        cleanup()


# ─── Client: send subcommand ────────────────────────────────────────────────

def _control_get(path):
    url = f"{CONTROL_BASE}/{path}"
    resp = urllib.request.urlopen(url, timeout=5)
    return json.loads(resp.read())


def _control_post(path, data):
    url = f"{CONTROL_BASE}/{path}"
    body = json.dumps(data).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    resp = urllib.request.urlopen(req, timeout=5)
    return json.loads(resp.read())


def _check_running():
    try:
        _control_get("status")
        return True
    except Exception:
        return False


def _wait_and_render(since_idx, timeout=TIMEOUT_SECS):
    """Poll timeline until response settles. Render events. Return final event list."""
    start = time.time()
    cursor = since_idx
    last_activity = time.time()
    seen_bot_msg = False
    all_new_events = []

    while time.time() - start < timeout:
        data = _control_get(f"timeline?since={cursor}")
        events = data.get("events", [])
        total = data.get("total", 0)

        if events:
            last_activity = time.time()
            for evt in events:
                _render_event(evt)
                all_new_events.append(evt)
                if evt["type"] == "bot_message":
                    seen_bot_msg = True
            cursor = total

        if seen_bot_msg and time.time() - last_activity > SETTLE_SECS:
            return all_new_events

        time.sleep(0.15)

    return all_new_events


def cmd_send(args):
    """Send a message through virtual Telegram and print the response."""
    if not _check_running():
        print("vchat not running. Start with: scripts/vchat.py start")
        sys.exit(1)

    # Get current event count so we only render new events.
    status = _control_get("status")
    since = status["events"]

    # Inject user message.
    result = _control_post("send", {"text": args.text})
    _render_event({"time": time.time(), "type": "user_message", "data": {"text": args.text, "message_id": result.get("message_id", 0)}})

    # Wait for response.
    events = _wait_and_render(since + 1, timeout=args.timeout)

    # Summary line.
    bot_msgs = [e for e in events if e["type"] == "bot_message"]
    reactions = [e for e in events if e["type"] == "reaction"]
    edits = [e for e in events if e["type"] == "bot_edit"]
    typing_count = sum(1 for e in events if e["type"] == "typing")

    print(f"\n{'─' * 52}")
    parts = []
    if bot_msgs:
        parts.append(f"응답 {len(bot_msgs)}개")
    if edits:
        parts.append(f"편집 {len(edits)}개")
    if reactions:
        emojis = [e["data"].get("emoji", "") for e in reactions if e["data"].get("emoji")]
        parts.append(f"리액션 {'→'.join(emojis)}")
    if typing_count:
        parts.append(f"타이핑 {typing_count}회")
    if events:
        elapsed = events[-1]["time"] - events[0]["time"]
        parts.append(f"{elapsed:.1f}초")

    print(f"  {C.GRAY}{'  |  '.join(parts)}{C.RESET}")


def cmd_multi(args):
    """Send multiple messages in sequence (multi-turn conversation)."""
    if not _check_running():
        print("vchat not running. Start with: scripts/vchat.py start")
        sys.exit(1)

    for i, text in enumerate(args.messages):
        if i > 0:
            print(f"\n{'═' * 52}")

        status = _control_get("status")
        since = status["events"]

        result = _control_post("send", {"text": text})
        _render_event({"time": time.time(), "type": "user_message", "data": {"text": text, "message_id": result.get("message_id", 0)}})

        _wait_and_render(since + 1, timeout=args.timeout)

    # Overall summary.
    data = _control_get("timeline?since=0")
    events = data.get("events", [])
    user_msgs = [e for e in events if e["type"] == "user_message"]
    bot_msgs = [e for e in events if e["type"] == "bot_message"]
    print(f"\n{'═' * 52}")
    print(f"  {C.BOLD}멀티턴 완료{C.RESET}: {len(user_msgs)} 메시지 → {len(bot_msgs)} 응답")
    if user_msgs and bot_msgs:
        print(f"  총 대화 시간: {bot_msgs[-1]['time'] - user_msgs[0]['time']:.1f}초")
    print(f"{'═' * 52}")


def cmd_timeline(args):
    """Print the full conversation timeline."""
    if not _check_running():
        print("vchat not running.")
        sys.exit(1)

    data = _control_get("timeline?since=0")
    for evt in data.get("events", []):
        _render_event(evt)


def cmd_status(args):
    """Check if vchat is running."""
    if _check_running():
        status = _control_get("status")
        print(f"running  events={status['events']}  mock=:{MOCK_PORT}  gateway=:{GATEWAY_PORT}")
    else:
        print("stopped")


def cmd_stop(args):
    """Stop the vchat server."""
    if os.path.exists(PID_FILE):
        try:
            with open(PID_FILE) as f:
                pid = int(f.read().strip())
            os.kill(pid, signal.SIGTERM)
            print(f"stopped (pid {pid})")
        except (OSError, ValueError) as e:
            print(f"stop failed: {e}")
        try:
            os.remove(PID_FILE)
        except OSError:
            pass
    else:
        print("not running (no pidfile)")


def cmd_reset(args):
    """Reset conversation state (clear timeline, keep server running)."""
    if not _check_running():
        print("vchat not running.")
        sys.exit(1)
    _control_post("reset", {})
    print("reset: timeline cleared, session state preserved in gateway")


def cmd_logs(args):
    """Show recent gateway logs."""
    n = args.lines
    try:
        with open(DEV_LOG) as f:
            lines = f.readlines()
        for line in lines[-n:]:
            print(line.rstrip())
    except FileNotFoundError:
        print(f"log not found: {DEV_LOG}")


# ─── Event Renderer ─────────────────────────────────────────────────────────

def _render_event(evt):
    ts = time.strftime("%H:%M:%S", time.localtime(evt["time"]))
    t = evt["type"]
    d = evt["data"]

    if t == "user_message":
        print(f"\n  {C.CYAN}👤 나{C.RESET}  {C.GRAY}{ts}{C.RESET}")
        for line in d["text"].split("\n"):
            print(f"  {line}")

    elif t == "reaction":
        emoji = d.get("emoji", "")
        if emoji:
            print(f"     {C.YELLOW}{emoji}{C.RESET}  {C.GRAY}리액션{C.RESET}")
        else:
            print(f"     {C.GRAY}리액션 제거{C.RESET}")

    elif t == "typing":
        print(f"     {C.GRAY}💬 입력 중...{C.RESET}")

    elif t == "bot_message":
        text = d["text"]
        display = _html_to_terminal(text)
        print(f"\n  {C.MAGENTA}🤖 Deneb{C.RESET}  {C.GRAY}{ts}{C.RESET}")
        for line in display.split("\n"):
            print(f"  {line}")

        markup = d.get("reply_markup")
        if markup and isinstance(markup, dict):
            rows = markup.get("inline_keyboard", [])
            btns = []
            for row in rows:
                for btn in row:
                    btns.append(f"[{btn.get('text', '?')}]")
            if btns:
                print(f"\n  {C.BLUE}{'  '.join(btns)}{C.RESET}")

    elif t == "bot_edit":
        text = d["text"]
        display = _html_to_terminal(text)
        lines = display.split("\n")
        mid = d.get("message_id", "?")
        if len(lines) <= 6:
            print(f"\n     {C.GRAY}✏️ 편집 #{mid}{C.RESET}  {C.GRAY}{ts}{C.RESET}")
            for line in lines:
                print(f"     {C.GRAY}{line}{C.RESET}")
        else:
            tail = lines[-1][:70]
            print(f"     {C.GRAY}✏️ ...{tail}{C.RESET}")

    elif t == "bot_delete":
        print(f"     {C.GRAY}🗑️ 메시지 #{d.get('message_id', '?')} 삭제{C.RESET}")

    elif t in ("bot_document", "bot_photo", "bot_video", "bot_audio", "bot_voice", "bot_upload"):
        icons = {"bot_photo": "🖼️", "bot_video": "🎬", "bot_audio": "🎵", "bot_voice": "🎤"}
        icon = icons.get(t, "📎")
        print(f"     {C.GRAY}{icon} 파일 전송{C.RESET}")


def _html_to_terminal(html):
    text = html
    # Code blocks.
    text = re.sub(r"<pre><code[^>]*>(.*?)</code></pre>",
                  lambda m: f"{C.GRAY}{_unesc(m.group(1))}{C.RESET}", text, flags=re.DOTALL)
    text = re.sub(r"<pre>(.*?)</pre>",
                  lambda m: f"{C.GRAY}{_unesc(m.group(1))}{C.RESET}", text, flags=re.DOTALL)
    text = re.sub(r"<code>(.*?)</code>", rf"{C.YELLOW}\1{C.RESET}", text)
    text = re.sub(r"<b>(.*?)</b>", rf"{C.BOLD}\1{C.RESET}", text)
    text = re.sub(r"<i>(.*?)</i>", rf"{C.ITALIC}\1{C.RESET}", text)
    text = re.sub(r"<s>(.*?)</s>", rf"{C.STRIKE}\1{C.RESET}", text)
    text = re.sub(r'<a href="[^"]*">(.*?)</a>', rf"{C.UNDERLINE}\1{C.RESET}", text)
    text = re.sub(r"<blockquote>(.*?)</blockquote>", r"│ \1", text)
    text = re.sub(r"<tg-spoiler>(.*?)</tg-spoiler>", r"[스포일러]", text)
    text = re.sub(r"</?[a-z][^>]*>", "", text)
    text = _unesc(text)
    return text


def _unesc(text):
    return text.replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&").replace("&quot;", '"')


# ─── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="vchat.py",
        description="Virtual Telegram — 클로드 코드가 직접 Deneb을 실사용 테스트",
    )
    sub = parser.add_subparsers(dest="command")

    # start
    p_start = sub.add_parser("start", help="Mock + Gateway 시작 (foreground)")
    p_start.add_argument("--no-build", action="store_true")
    p_start.set_defaults(func=cmd_start)

    # send
    p_send = sub.add_parser("send", help="메시지 전송 + 응답 확인")
    p_send.add_argument("text", help="보낼 메시지")
    p_send.add_argument("--timeout", type=int, default=TIMEOUT_SECS)
    p_send.set_defaults(func=cmd_send)

    # multi
    p_multi = sub.add_parser("multi", help="멀티턴 대화")
    p_multi.add_argument("messages", nargs="+", help="메시지 목록")
    p_multi.add_argument("--timeout", type=int, default=TIMEOUT_SECS)
    p_multi.set_defaults(func=cmd_multi)

    # timeline
    p_tl = sub.add_parser("timeline", help="전체 타임라인 출력")
    p_tl.set_defaults(func=cmd_timeline)

    # status
    p_status = sub.add_parser("status", help="실행 상태 확인")
    p_status.set_defaults(func=cmd_status)

    # stop
    p_stop = sub.add_parser("stop", help="서버 정지")
    p_stop.set_defaults(func=cmd_stop)

    # reset
    p_reset = sub.add_parser("reset", help="대화 초기화 (서버는 유지)")
    p_reset.set_defaults(func=cmd_reset)

    # logs
    p_logs = sub.add_parser("logs", help="게이트웨이 로그")
    p_logs.add_argument("-n", "--lines", type=int, default=50)
    p_logs.set_defaults(func=cmd_logs)

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    args.func(args)


if __name__ == "__main__":
    main()
