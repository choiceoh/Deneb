#!/usr/bin/env python3
"""Live compaction test: interleaved inject + chat.send to trigger compaction.

Injects ~120K tokens of content via chat.inject (instant, no LLM), then
triggers compaction via chat.send after each article. Finally, checks
whether 5 anchor facts survive compaction via recall questions.

Flow per article:
  1. chat.inject x N chunks — user message content (instant, no LLM)
  2. chat.send — short prompt triggers context assembly + compaction
  3. Repeat for all 5 articles
  4. 5 recall questions via chat.send

Requirements:
  - Dev gateway running (scripts/dev-live-test.sh restart)
  - Test data generated (scripts/dev-compaction-gen-testdata.py)
  - pip install websockets

Usage:
  python3 scripts/dev-compaction-live-test.py [PORT]
  # Default port: 18791

Output:
  COMPACTION_LIVE_RESULT passed=N total=5 score=N
"""
import asyncio
import json
import os
import sys
import time

try:
    import websockets
except ImportError:
    print("pip install websockets", file=sys.stderr)
    sys.exit(1)

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18791
HOST = "127.0.0.1"
WS_URL = f"ws://{HOST}:{PORT}/ws"
DATA_DIR = os.environ.get("COMPACTION_TEST_DATA", "/tmp/deneb-compaction-testdata")
SESSION_KEY = f"compaction-{int(time.time())}"

ANCHORS = [
    ("아까 읽은 보고서에서 목표 아키텍처 패턴 이름이 뭐였어?", "AURORA-7X"),
    ("QA 환경 고정 IP가 뭐였지?", "10.42.88.15"),
    ("다음 배포 일정이 정확히 언제야?", "4월 18일"),
    ("프로덕션 DB 엔드포인트 주소 알려줘", "db-prod-kr1"),
    ("연간 인프라 예산이 얼마라고 했어?", "247,500"),
]

req_counter = 0


def next_id():
    global req_counter
    req_counter += 1
    return f"r-{req_counter}-{int(time.time() * 1000)}"


async def rpc_call(ws, method, params, timeout=10):
    """Send RPC request and wait for response (skips events)."""
    rid = next_id()
    await ws.send(json.dumps({"type": "req", "id": rid, "method": method, "params": params}))
    while True:
        raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
        msg = json.loads(raw)
        if msg.get("id") == rid:
            return msg


async def chat_send_and_wait(ws, session_key, message, timeout=120):
    """Send chat.send and collect streaming reply until done."""
    rid = next_id()
    await ws.send(json.dumps({
        "type": "req", "id": rid, "method": "chat.send",
        "params": {"sessionKey": session_key, "message": message},
    }))

    reply_text = ""
    start = time.time()

    while True:
        try:
            raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
        except asyncio.TimeoutError:
            break

        msg = json.loads(raw)
        evt = msg.get("event", "")
        payload = msg.get("payload", {})

        # Streaming text deltas.
        if evt == "chat.delta":
            reply_text += payload.get("delta", payload.get("text", ""))
        # Chat completion event (state: done/failed/killed/timeout).
        elif evt == "chat":
            state = payload.get("state", "")
            if state == "done":
                if not reply_text:
                    reply_text = payload.get("text", "")
                break
            elif state in ("failed", "killed", "timeout"):
                break
        # Session transition (fallback).
        elif evt == "session.transition":
            phase = payload.get("phase", "")
            if phase in ("done", "failed", "killed", "timeout"):
                break
        # Initial "started" response — skip.
        elif msg.get("type") == "res" and msg.get("id") == rid:
            if payload.get("status", "") == "started":
                continue
            if not reply_text:
                reply_text = payload.get("reply", payload.get("message", ""))
            break

    return reply_text.strip(), time.time() - start


async def main():
    # Check test data exists.
    for i in range(1, 6):
        path = os.path.join(DATA_DIR, f"article_{i}.txt")
        if not os.path.exists(path):
            print(f"Missing test data: {path}")
            print(f"Run: python3 scripts/dev-compaction-gen-testdata.py")
            sys.exit(1)

    print(f"=== Compaction Live Test ===")
    print(f"  URL: {WS_URL}")
    print(f"  Session: {SESSION_KEY}")
    print()

    async with websockets.connect(WS_URL, max_size=20 * 1024 * 1024, ping_interval=None) as ws:
        # Challenge.
        await asyncio.wait_for(ws.recv(), timeout=5)

        # Handshake.
        resp = await rpc_call(ws, "connect", {
            "minProtocol": 1, "maxProtocol": 5,
            "client": {"id": "compaction-test", "version": "1.0.0",
                       "platform": "test", "mode": "control"},
        })
        if not resp.get("ok"):
            print(f"Handshake FAILED: {resp}")
            sys.exit(1)

        # Create session.
        resp = await rpc_call(ws, "sessions.create", {"key": SESSION_KEY, "kind": "direct"})
        if not resp.get("ok"):
            print(f"Session FAILED: {resp}")
            sys.exit(1)
        print(f"  Connected + session created\n")

        # Phase 1: Inject articles in chunks + chat.send to trigger compaction.
        print("--- Phase 1: Inject + compact ---")
        total_chars = 0
        phase1_start = time.time()
        chunk_size = 10000  # ~5K tokens per chunk

        for i in range(1, 6):
            path = os.path.join(DATA_DIR, f"article_{i}.txt")
            with open(path) as f:
                article = f.read()

            chunks = [article[j:j + chunk_size] for j in range(0, len(article), chunk_size)]
            print(f"  [{i}/5] article_{i}.txt ({len(article)} chars, {len(chunks)} chunks)")

            for ci, chunk in enumerate(chunks):
                await rpc_call(ws, "chat.inject", {
                    "sessionKey": SESSION_KEY, "role": "user", "content": chunk,
                }, timeout=10)
                await rpc_call(ws, "chat.inject", {
                    "sessionKey": SESSION_KEY, "role": "assistant",
                    "content": f"파트 {i} 섹션 {ci + 1} 확인.",
                }, timeout=10)
                total_chars += len(chunk)

            # Brief pause then chat.send to trigger compaction evaluation.
            await asyncio.sleep(5)
            print(f"         chat.send...", end=" ", flush=True)
            reply, elapsed = await chat_send_and_wait(
                ws, SESSION_KEY,
                f"파트 {i} 문서의 핵심 결정사항을 한 줄로 요약해줘.",
                timeout=120,
            )
            short = reply[:80].replace("\n", " ").strip() if reply else "(empty)"
            print(f"done ({elapsed:.0f}s) {short}")

        phase1_elapsed = time.time() - phase1_start
        print(f"\n  Injected: {total_chars} chars (~{total_chars // 2} tokens) in {phase1_elapsed:.0f}s\n")

        # Phase 2: Recall questions.
        print("--- Phase 2: Recall ---")
        passed = 0
        total = len(ANCHORS)

        for i, (question, anchor) in enumerate(ANCHORS):
            print(f"  [{i + 1}/{total}] {question}...", end=" ", flush=True)
            reply, elapsed = await chat_send_and_wait(ws, SESSION_KEY, question, timeout=60)

            if anchor in reply:
                print(f"PASS ({elapsed:.0f}s)")
                passed += 1
            else:
                print(f"FAIL ({elapsed:.0f}s)")
                short = reply[:200].replace("\n", " ").strip() if reply else "(empty)"
                print(f"         reply: {short}")

        total_elapsed = time.time() - phase1_start
        print()
        print("=" * 50)
        score = passed * 100 // total if total > 0 else 0
        print(f"  Anchors: {passed}/{total} ({score}%)")
        print(f"  Time: {total_elapsed:.0f}s")
        print("=" * 50)
        print(f"COMPACTION_LIVE_RESULT passed={passed} total={total} score={score}")


if __name__ == "__main__":
    asyncio.run(main())
