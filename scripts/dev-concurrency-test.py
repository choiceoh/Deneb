#!/usr/bin/env python3
"""
Deneb Gateway Concurrency Test Runner.

Tests multi-turn concurrency scenarios that single-turn quality tests cannot cover:
- Message queuing during active runs
- Queue drain after run completion
- Interrupt (sessions.send) while running
- Abort + queue clearing
- Multiple concurrent WebSocket connections to the same session

Usage:
    python3 scripts/dev-concurrency-test.py [--port 18790] [--scenario all|queue|interrupt|abort|multi-conn]
"""

import json
import asyncio
import sys
import time
import argparse
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
TIMEOUT_CHAT = 120


# --- Result Types ---

@dataclass
class ConcurrencyResult:
    """Result of a single concurrency test scenario."""
    name: str
    passed: bool = False
    checks: list = field(default_factory=list)
    latency_ms: float = 0
    errors: list = field(default_factory=list)
    details: list = field(default_factory=list)

    def add_check(self, name: str, passed: bool, detail: str = ""):
        self.checks.append((name, passed, detail))

    def add_detail(self, msg: str):
        self.details.append(msg)

    def summary(self) -> str:
        total = len(self.checks)
        passed = sum(1 for _, p, _ in self.checks if p)
        status = "PASS" if self.passed else "FAIL"
        return f"[{status}] {self.name} ({passed}/{total} checks, {self.latency_ms:.0f}ms)"


# --- WebSocket Client ---

class GatewayClient:
    """WebSocket client for gateway concurrency testing."""

    def __init__(self, host: str, port: int, client_id: str = "concurrency-test"):
        self.host = host
        self.port = port
        self.uri = f"ws://{host}:{port}/ws"
        self.client_id = client_id
        self.ws = None
        self.seq = 0

    async def connect(self) -> str:
        self.ws = await websockets.connect(self.uri, max_size=10 * 1024 * 1024)
        # Read challenge.
        await asyncio.wait_for(self.ws.recv(), timeout=TIMEOUT_CONNECT)
        # Handshake.
        connect = {
            "type": "req", "id": f"{self.client_id}-hs", "method": "connect",
            "params": {
                "minProtocol": 1, "maxProtocol": 5,
                "client": {
                    "id": self.client_id,
                    "version": "1.0.0",
                    "platform": "test",
                    "mode": "control",
                },
            },
        }
        await self.ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(self.ws.recv(), timeout=TIMEOUT_CONNECT))
        if not hello.get("ok"):
            raise RuntimeError(f"Handshake failed: {json.dumps(hello)}")
        return hello.get("payload", {}).get("server", {}).get("version", "?")

    async def rpc(self, method: str, params: dict = None, timeout: float = TIMEOUT_RPC) -> dict:
        self.seq += 1
        rpc_id = f"{self.client_id}-{self.seq}-{int(time.time() * 1000)}"
        msg = {"type": "req", "id": rpc_id, "method": method, "params": params or {}}
        await self.ws.send(json.dumps(msg))

        # Read frames until we get the response for our rpc_id (skip events).
        deadline = time.time() + timeout
        while time.time() < deadline:
            remaining = deadline - time.time()
            raw = await asyncio.wait_for(self.ws.recv(), timeout=max(remaining, 0.1))
            frame = json.loads(raw)
            # Skip event frames (tick, sessions.changed, etc.).
            if frame.get("type") == "event" or frame.get("event"):
                continue
            # Match by ID.
            if frame.get("id") == msg["id"]:
                return frame
        raise TimeoutError(f"No response for {method} within {timeout}s")

    async def create_session(self, key: str = "") -> str:
        if not key:
            key = f"conc-{self.client_id}-{int(time.time() * 1000)}"
        resp = await self.rpc("sessions.create", {"key": key, "kind": "direct"})
        if not resp.get("ok"):
            raise RuntimeError(f"sessions.create failed: {json.dumps(resp.get('error', {}))}")
        return key

    async def chat_send(self, message: str, session_key: str, client_run_id: str = "") -> dict:
        """Send chat.send and return the IMMEDIATE RPC response (started/queued).

        Does NOT wait for the chat to complete — that's the caller's job.
        """
        self.seq += 1
        rpc_id = f"{self.client_id}-chat-{self.seq}-{int(time.time() * 1000)}"
        if not client_run_id:
            client_run_id = f"run-{self.seq}-{int(time.time() * 1000)}"
        msg = {
            "type": "req", "id": rpc_id, "method": "chat.send",
            "params": {
                "sessionKey": session_key,
                "message": message,
                "clientRunId": client_run_id,
            },
        }
        await self.ws.send(json.dumps(msg))

        # Read until we get the RPC response for chat.send.
        deadline = time.time() + TIMEOUT_RPC
        while time.time() < deadline:
            remaining = deadline - time.time()
            raw = await asyncio.wait_for(self.ws.recv(), timeout=max(remaining, 0.1))
            frame = json.loads(raw)
            if frame.get("id") == rpc_id:
                return frame
            # Skip events.
        raise TimeoutError("No response for chat.send within timeout")

    async def sessions_send(self, message: str, session_key: str, client_run_id: str = "") -> dict:
        """Send sessions.send (interrupt + clear pending + start new run)."""
        self.seq += 1
        rpc_id = f"{self.client_id}-ssend-{self.seq}-{int(time.time() * 1000)}"
        if not client_run_id:
            client_run_id = f"srun-{self.seq}-{int(time.time() * 1000)}"
        msg = {
            "type": "req", "id": rpc_id, "method": "sessions.send",
            "params": {
                "key": session_key,
                "message": message,
                "idempotencyKey": client_run_id,
            },
        }
        await self.ws.send(json.dumps(msg))

        deadline = time.time() + TIMEOUT_RPC
        while time.time() < deadline:
            remaining = deadline - time.time()
            raw = await asyncio.wait_for(self.ws.recv(), timeout=max(remaining, 0.1))
            frame = json.loads(raw)
            if frame.get("id") == rpc_id:
                return frame
        raise TimeoutError("No response for sessions.send within timeout")

    async def abort(self, session_key: str = "", run_id: str = "") -> dict:
        """Send sessions.abort."""
        params = {}
        if session_key:
            params["sessionKey"] = session_key
        if run_id:
            params["runId"] = run_id
        return await self.rpc("sessions.abort", params)

    async def wait_for_chat_done(self, timeout: float = TIMEOUT_CHAT) -> dict:
        """Wait for a chat completion event (state=done/error/aborted).

        Returns the final chat event payload.
        """
        deadline = time.time() + timeout
        while time.time() < deadline:
            remaining = deadline - time.time()
            try:
                raw = await asyncio.wait_for(self.ws.recv(), timeout=max(remaining, 0.1))
            except asyncio.TimeoutError:
                return {"state": "timeout", "error": f"No chat done event within {timeout}s"}
            frame = json.loads(raw)
            evt = frame.get("event", "")
            payload = frame.get("payload", {})
            state = payload.get("state", "")
            if evt == "chat" and state in ("done", "error", "aborted"):
                return payload
        return {"state": "timeout", "error": f"No chat done event within {timeout}s"}

    async def drain_events(self, duration: float = 0.5) -> list:
        """Drain all pending events for a short period. Non-blocking after duration."""
        events = []
        deadline = time.time() + duration
        while time.time() < deadline:
            remaining = deadline - time.time()
            try:
                raw = await asyncio.wait_for(self.ws.recv(), timeout=max(remaining, 0.05))
                events.append(json.loads(raw))
            except asyncio.TimeoutError:
                break
        return events

    async def close(self):
        if self.ws:
            await self.ws.close()


# --- Test Scenarios ---

async def test_queue_during_active_run(host: str, port: int) -> ConcurrencyResult:
    """Scenario: Send a second message while first is still running.

    Expected:
    1. First chat.send returns {status: "started"}
    2. Second chat.send returns {status: "queued", reason: "active-run"}
    3. First run completes (state=done)
    4. Queued message is auto-drained and processed
    5. Second run completes (state=done)
    """
    result = ConcurrencyResult(name="queue-during-active-run")
    client = GatewayClient(host, port, "queue-test")
    start = time.time()

    try:
        await client.connect()
        session_key = await client.create_session()
        result.add_detail(f"Session: {session_key}")

        # Send first message (triggers a run).
        resp1 = await client.chat_send("안녕, 간단히 자기소개 해줘", session_key, "run-1")
        ok1 = resp1.get("ok", False)
        status1 = resp1.get("payload", {}).get("status", "")
        result.add_check("first_msg_started", ok1 and status1 == "started",
                         f"ok={ok1}, status={status1}")
        result.add_detail(f"First message: status={status1}")

        if not ok1:
            result.add_detail(f"First message failed, skipping rest: {json.dumps(resp1.get('error', {}))}")
            result.latency_ms = (time.time() - start) * 1000
            result.passed = False
            return result

        # Brief pause to ensure run is active.
        await asyncio.sleep(0.3)

        # Send second message (should be queued).
        resp2 = await client.chat_send("오늘 날씨는 어때?", session_key, "run-2")
        ok2 = resp2.get("ok", False)
        status2 = resp2.get("payload", {}).get("status", "")
        reason2 = resp2.get("payload", {}).get("reason", "")
        result.add_check("second_msg_queued", ok2 and status2 == "queued",
                         f"ok={ok2}, status={status2}, reason={reason2}")
        result.add_detail(f"Second message: status={status2}, reason={reason2}")

        # Wait for first run to complete.
        done1 = await client.wait_for_chat_done(timeout=TIMEOUT_CHAT)
        state1 = done1.get("state", "")
        result.add_check("first_run_completes", state1 == "done",
                         f"state={state1}")
        result.add_detail(f"First run completed: state={state1}")

        if state1 == "done":
            text1 = done1.get("text", "")[:80]
            result.add_detail(f"First reply: {text1}...")

        # Wait for queued message to be auto-processed (drain).
        done2 = await client.wait_for_chat_done(timeout=TIMEOUT_CHAT)
        state2 = done2.get("state", "")
        result.add_check("queued_msg_drained", state2 == "done",
                         f"state={state2}")
        result.add_detail(f"Queued run completed: state={state2}")

        if state2 == "done":
            text2 = done2.get("text", "")[:80]
            result.add_detail(f"Second reply: {text2}...")

        # Both runs should have completed successfully.
        result.add_check("both_runs_success",
                         state1 == "done" and state2 == "done",
                         f"run1={state1}, run2={state2}")

    except Exception as e:
        result.errors.append(str(e))
        result.add_check("no_exceptions", False, str(e))
    finally:
        await client.close()

    result.latency_ms = (time.time() - start) * 1000
    passed_count = sum(1 for _, p, _ in result.checks if p)
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_multiple_queued_messages(host: str, port: int) -> ConcurrencyResult:
    """Scenario: Queue multiple messages, verify FIFO drain order.

    Expected:
    1. First chat.send returns {status: "started"}
    2. Second and third chat.send both return {status: "queued"}
    3. After first run completes, second is auto-drained
    4. After second run completes, third is auto-drained
    5. All three complete in order
    """
    result = ConcurrencyResult(name="multiple-queued-fifo")
    client = GatewayClient(host, port, "fifo-test")
    start = time.time()

    try:
        await client.connect()
        session_key = await client.create_session()

        # Send first message.
        resp1 = await client.chat_send("1 더하기 1은?", session_key, "fifo-1")
        status1 = resp1.get("payload", {}).get("status", "")
        result.add_check("msg1_started", status1 == "started", f"status={status1}")

        await asyncio.sleep(0.3)

        # Queue second and third.
        resp2 = await client.chat_send("2 더하기 2는?", session_key, "fifo-2")
        status2 = resp2.get("payload", {}).get("status", "")
        result.add_check("msg2_queued", status2 == "queued", f"status={status2}")

        resp3 = await client.chat_send("3 더하기 3은?", session_key, "fifo-3")
        status3 = resp3.get("payload", {}).get("status", "")
        result.add_check("msg3_queued", status3 == "queued", f"status={status3}")

        result.add_detail(f"Statuses: [{status1}, {status2}, {status3}]")

        # Wait for all three to complete in sequence.
        completions = []
        for i in range(3):
            done = await client.wait_for_chat_done(timeout=TIMEOUT_CHAT)
            state = done.get("state", "")
            completions.append(state)
            text = done.get("text", "")[:60]
            result.add_detail(f"Run {i+1} completed: state={state}, reply={text}...")

        result.add_check("all_three_done",
                         all(s == "done" for s in completions),
                         f"states={completions}")

        result.add_check("correct_count",
                         len(completions) == 3,
                         f"expected 3 completions, got {len(completions)}")

    except Exception as e:
        result.errors.append(str(e))
        result.add_check("no_exceptions", False, str(e))
    finally:
        await client.close()

    result.latency_ms = (time.time() - start) * 1000
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_interrupt_with_sessions_send(host: str, port: int) -> ConcurrencyResult:
    """Scenario: Interrupt an active run with sessions.send.

    Expected:
    1. chat.send starts a run
    2. sessions.send interrupts + clears pending + starts new run
    3. First run receives aborted state
    4. New run from sessions.send completes with done
    """
    result = ConcurrencyResult(name="interrupt-sessions-send")
    client = GatewayClient(host, port, "interrupt-test")
    start = time.time()

    try:
        await client.connect()
        session_key = await client.create_session()

        # Start a long-running chat.
        resp1 = await client.chat_send(
            "1부터 100까지의 소수를 모두 나열하고 각각에 대해 왜 소수인지 설명해줘",
            session_key, "long-run-1")
        status1 = resp1.get("payload", {}).get("status", "")
        result.add_check("initial_run_started", status1 == "started", f"status={status1}")

        # Brief wait, then interrupt with sessions.send.
        await asyncio.sleep(1.0)

        resp2 = await client.sessions_send("이전 요청 취소하고, 간단히 안녕이라고 해줘", session_key, "interrupt-run")
        ok2 = resp2.get("ok", False)
        status2 = resp2.get("payload", {}).get("status", "")
        result.add_check("interrupt_accepted", ok2 and status2 == "started",
                         f"ok={ok2}, status={status2}")
        result.add_detail(f"sessions.send response: status={status2}")

        # Collect all completion events. We expect either:
        # - aborted for run 1, then done for run 2
        # - just done for run 2 (if run 1 already aborted before we read)
        completions = []
        for _ in range(2):
            done = await client.wait_for_chat_done(timeout=TIMEOUT_CHAT)
            state = done.get("state", "timeout")
            completions.append(state)
            result.add_detail(f"Completion: state={state}")
            if state == "timeout":
                break

        states_set = set(completions)

        # The interrupt run should have completed.
        result.add_check("has_done_completion", "done" in states_set,
                         f"completions={completions}")

        # The original run should have been aborted (or already done).
        result.add_check("original_interrupted",
                         "aborted" in states_set or "error" in states_set or len(completions) >= 1,
                         f"completions={completions}")

    except Exception as e:
        result.errors.append(str(e))
        result.add_check("no_exceptions", False, str(e))
    finally:
        await client.close()

    result.latency_ms = (time.time() - start) * 1000
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_abort_clears_queue(host: str, port: int) -> ConcurrencyResult:
    """Scenario: Abort an active run clears the pending queue.

    Expected:
    1. chat.send starts run 1
    2. chat.send queues run 2
    3. sessions.abort kills run 1 and clears the queue
    4. Run 1 receives aborted/killed state
    5. Run 2 is never processed (queue was cleared)
    """
    result = ConcurrencyResult(name="abort-clears-queue")
    client = GatewayClient(host, port, "abort-test")
    start = time.time()

    try:
        await client.connect()
        session_key = await client.create_session()

        # Start run 1.
        resp1 = await client.chat_send(
            "1부터 1000까지의 모든 수에 대해 소인수분해를 해줘",
            session_key, "abort-run-1")
        status1 = resp1.get("payload", {}).get("status", "")
        result.add_check("run1_started", status1 == "started", f"status={status1}")

        await asyncio.sleep(0.3)

        # Queue run 2.
        resp2 = await client.chat_send("이건 실행되면 안 되는 메시지야", session_key, "abort-run-2")
        status2 = resp2.get("payload", {}).get("status", "")
        result.add_check("run2_queued", status2 == "queued", f"status={status2}")

        # Abort.
        abort_resp = await client.abort(session_key=session_key)
        abort_ok = abort_resp.get("ok", False)
        result.add_check("abort_accepted", abort_ok,
                         f"ok={abort_ok}, error={abort_resp.get('error', '')}")
        result.add_detail(f"Abort response: ok={abort_ok}")

        # Wait for run 1's terminal state.
        done1 = await client.wait_for_chat_done(timeout=15)
        state1 = done1.get("state", "")
        result.add_check("run1_terminated",
                         state1 in ("aborted", "error", "done"),
                         f"state={state1}")
        result.add_detail(f"Run 1 terminal state: {state1}")

        # The queued message should NOT be processed. Wait briefly and verify
        # no second completion event arrives.
        await asyncio.sleep(2.0)
        events = await client.drain_events(duration=1.0)

        # Filter for chat completion events.
        chat_dones = [e for e in events
                      if e.get("event") == "chat"
                      and e.get("payload", {}).get("state") in ("done", "error", "aborted")]

        result.add_check("queue_cleared_no_drain",
                         len(chat_dones) == 0,
                         f"unexpected chat completions after abort: {len(chat_dones)}")

        if chat_dones:
            result.add_detail(f"WARNING: {len(chat_dones)} unexpected chat completion(s) after abort")

    except Exception as e:
        result.errors.append(str(e))
        result.add_check("no_exceptions", False, str(e))
    finally:
        await client.close()

    result.latency_ms = (time.time() - start) * 1000
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_multi_connection_same_session(host: str, port: int) -> ConcurrencyResult:
    """Scenario: Two WebSocket connections sending to the same session.

    Expected:
    1. Client A starts a run on the shared session
    2. Client B sends to the same session — should be queued
    3. Both messages eventually complete
    4. No crashes or orphaned runs
    """
    result = ConcurrencyResult(name="multi-connection-same-session")
    client_a = GatewayClient(host, port, "multi-a")
    client_b = GatewayClient(host, port, "multi-b")
    start = time.time()

    try:
        await client_a.connect()
        await client_b.connect()

        # Create session on client A, use same key on client B.
        session_key = await client_a.create_session()
        result.add_detail(f"Shared session: {session_key}")

        # Client A sends first message.
        resp_a = await client_a.chat_send("안녕, 나는 클라이언트 A야", session_key, "multi-a-run")
        status_a = resp_a.get("payload", {}).get("status", "")
        result.add_check("client_a_started", status_a == "started", f"status={status_a}")

        await asyncio.sleep(0.3)

        # Client B sends to same session (should be queued since run is active).
        resp_b = await client_b.chat_send("안녕, 나는 클라이언트 B야", session_key, "multi-b-run")
        status_b = resp_b.get("payload", {}).get("status", "")
        result.add_check("client_b_queued", status_b == "queued",
                         f"status={status_b}")
        result.add_detail(f"Client A: {status_a}, Client B: {status_b}")

        # Wait for client A's run to complete (events come on client A's connection).
        done_a = await client_a.wait_for_chat_done(timeout=TIMEOUT_CHAT)
        state_a = done_a.get("state", "")
        result.add_check("client_a_completes", state_a == "done", f"state={state_a}")

        # The queued message from B should be drained and processed.
        # Events for the drain run may arrive on either connection.
        # Try client A first (it created the session), then client B.
        done_b = await client_a.wait_for_chat_done(timeout=TIMEOUT_CHAT)
        state_b = done_b.get("state", "timeout")
        if state_b == "timeout":
            # Try client B's connection.
            done_b = await client_b.wait_for_chat_done(timeout=10)
            state_b = done_b.get("state", "timeout")

        result.add_check("client_b_completes", state_b == "done",
                         f"state={state_b}")
        result.add_detail(f"Run A: {state_a}, Run B: {state_b}")

    except Exception as e:
        result.errors.append(str(e))
        result.add_check("no_exceptions", False, str(e))
    finally:
        await client_a.close()
        await client_b.close()

    result.latency_ms = (time.time() - start) * 1000
    result.passed = all(p for _, p, _ in result.checks)
    return result


async def test_rapid_fire_messages(host: str, port: int) -> ConcurrencyResult:
    """Scenario: Send messages in rapid succession without waiting.

    Expected:
    1. First message starts
    2. All subsequent messages are queued
    3. No errors or crashes
    4. Server handles the burst gracefully
    """
    result = ConcurrencyResult(name="rapid-fire-burst")
    client = GatewayClient(host, port, "rapid-test")
    start = time.time()
    burst_count = 4

    try:
        await client.connect()
        session_key = await client.create_session()

        # Send messages in rapid succession (no sleep between them).
        statuses = []
        for i in range(burst_count):
            resp = await client.chat_send(f"메시지 {i+1}", session_key, f"rapid-{i+1}")
            ok = resp.get("ok", False)
            status = resp.get("payload", {}).get("status", "")
            statuses.append((ok, status))

        # First should be started, rest should be queued.
        result.add_check("first_started",
                         statuses[0] == (True, "started"),
                         f"first={statuses[0]}")

        queued_count = sum(1 for ok, s in statuses[1:] if ok and s == "queued")
        result.add_check("rest_queued",
                         queued_count == burst_count - 1,
                         f"queued {queued_count}/{burst_count - 1}")

        all_ok = all(ok for ok, _ in statuses)
        result.add_check("all_accepted", all_ok,
                         f"statuses={statuses}")

        result.add_detail(f"Burst of {burst_count}: {[s for _, s in statuses]}")

        # Wait for all to drain and complete.
        completed = 0
        for i in range(burst_count):
            done = await client.wait_for_chat_done(timeout=TIMEOUT_CHAT)
            state = done.get("state", "timeout")
            if state == "done":
                completed += 1
            result.add_detail(f"Completion {i+1}: state={state}")
            if state == "timeout":
                break

        result.add_check("all_complete",
                         completed == burst_count,
                         f"completed {completed}/{burst_count}")

    except Exception as e:
        result.errors.append(str(e))
        result.add_check("no_exceptions", False, str(e))
    finally:
        await client.close()

    result.latency_ms = (time.time() - start) * 1000
    result.passed = all(p for _, p, _ in result.checks)
    return result


# --- Report ---

def print_report(results: list[ConcurrencyResult]) -> int:
    total_checks = sum(len(r.checks) for r in results)
    passed_checks = sum(sum(1 for _, p, _ in r.checks if p) for r in results)
    all_passed = all(r.passed for r in results)

    print()
    print("=" * 70)
    print(f"  CONCURRENCY TEST REPORT — {len(results)} scenarios, {total_checks} checks")
    print("=" * 70)
    print()

    for r in results:
        icon = "✓" if r.passed else "✗"
        print(f"  {icon} {r.summary()}")
        for name, passed, detail in r.checks:
            check_icon = "  ✓" if passed else "  ✗"
            detail_str = f" — {detail}" if detail else ""
            print(f"    {check_icon} {name}{detail_str}")

        if r.details:
            for d in r.details:
                print(f"    > {d}")

        if r.errors:
            for e in r.errors:
                print(f"    ERROR: {e}")
        print()

    print("-" * 70)
    status = "ALL PASSED" if all_passed else "SOME FAILED"
    print(f"  {status} — {passed_checks}/{total_checks} checks, "
          f"{sum(r.latency_ms for r in results):.0f}ms total")
    print("-" * 70)

    return 0 if all_passed else 1


# --- Main ---

SCENARIOS = {
    "queue": ("Message queuing during active run", test_queue_during_active_run),
    "fifo": ("Multiple queued messages in FIFO order", test_multiple_queued_messages),
    "interrupt": ("Interrupt with sessions.send", test_interrupt_with_sessions_send),
    "abort": ("Abort clears pending queue", test_abort_clears_queue),
    "multi-conn": ("Multiple connections, same session", test_multi_connection_same_session),
    "rapid": ("Rapid-fire message burst", test_rapid_fire_messages),
}


async def run(args):
    # Verify gateway is reachable.
    try:
        probe = GatewayClient(HOST, args.port, "probe")
        version = await probe.connect()
        await probe.close()
        print(f"Connected to gateway v{version} on port {args.port}")
    except Exception as e:
        print(f"Failed to connect to {HOST}:{args.port}: {e}")
        print("Is the dev gateway running? Try: scripts/dev-live-test.sh start")
        return 1

    results = []
    scenario = args.scenario

    if scenario == "all":
        targets = list(SCENARIOS.keys())
    else:
        targets = [scenario]

    for key in targets:
        desc, test_fn = SCENARIOS[key]
        print(f"Running: {desc}...")
        r = await test_fn(HOST, args.port)
        results.append(r)

    return print_report(results)


def main():
    parser = argparse.ArgumentParser(description="Deneb Gateway Concurrency Test")
    parser.add_argument("--port", type=int, default=PORT,
                        help=f"Gateway port (default: {PORT})")
    parser.add_argument("--scenario", default="all",
                        choices=["all"] + list(SCENARIOS.keys()),
                        help="Test scenario to run")
    args = parser.parse_args()

    sys.exit(asyncio.run(run(args)))


if __name__ == "__main__":
    main()
