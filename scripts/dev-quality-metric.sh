#!/usr/bin/env bash
# Quality metric for constant optimization.
# Sends a real chat message, measures response quality, returns a numeric score.
#
# Usage:
#   scripts/dev-quality-metric.sh [--port PORT] [--message MSG]
#
# Output (last line):
#   metric_value=85.5
#
# Score components (0-100):
#   - korean_ratio (0-25): response is in Korean
#   - substance (0-25): response has meaningful content
#   - clean (0-20): no leaked markup, no filler
#   - latency (0-15): response time within budget
#   - streaming (0-15): events flowed properly

set -euo pipefail

PORT="${1:-${ITERATE_PORT:-18791}}"
MESSAGE="${2:-안녕, 간단히 자기소개 해줘}"
HOST="127.0.0.1"

python3 -c "
import json, asyncio, time, re, sys, websockets

HOST = '$HOST'
PORT = $PORT
MESSAGE = '''$MESSAGE'''

def score_korean(text):
    korean = len(re.findall(r'[\uac00-\ud7af\u1100-\u11ff\u3130-\u318f]', text))
    total_alpha = len(re.findall(r'[a-zA-Z\uac00-\ud7af]', text))
    if total_alpha == 0: return 0
    ratio = korean / total_alpha
    if ratio > 0.5: return 25
    if ratio > 0.3: return 20
    if ratio > 0.1: return 10
    return 0

def score_substance(text):
    stripped = text.strip()
    if not stripped: return 0
    if len(stripped) < 10: return 5
    if len(stripped) < 30: return 10
    if len(stripped) < 100: return 20
    return 25

def score_clean(text):
    s = 20
    # Leaked markup.
    for pat in [r'<function=', r'</?thinking>', r'\[\[reply_to', r'MEDIA:\S+', r'NO_REPLY']:
        if re.search(pat, text): s -= 5
    # Filler.
    for pat in [r'^(Great question|I.d be happy to|Sure)', r'^(좋은 질문|물론이죠|당연하죠)']:
        if re.match(pat, text.strip(), re.IGNORECASE): s -= 5
    return max(s, 0)

def score_latency(ms):
    if ms < 10000: return 15
    if ms < 20000: return 12
    if ms < 30000: return 8
    if ms < 60000: return 4
    return 0

def score_streaming(delta_count, event_count):
    if delta_count > 5 and event_count > 10: return 15
    if delta_count > 0: return 10
    if event_count > 0: return 5
    return 0

async def main():
    try:
        ws = await asyncio.wait_for(
            websockets.connect(f'ws://{HOST}:{PORT}/ws', max_size=10*1024*1024, ping_interval=None),
            timeout=5)
    except Exception as e:
        print(f'connect_error={e}', file=sys.stderr)
        print('metric_value=0')
        return

    try:
        # Handshake.
        await asyncio.wait_for(ws.recv(), timeout=3)
        connect = {'type':'req','id':'qm-hs','method':'connect','params':{
            'minProtocol':1,'maxProtocol':5,
            'client':{'id':'quality-metric','version':'1.0.0','platform':'test','mode':'control'}
        }}
        await ws.send(json.dumps(connect))
        hello = json.loads(await asyncio.wait_for(ws.recv(), timeout=3))
        if not hello.get('ok'):
            print('metric_value=0')
            return

        # Create session.
        sess = f'qm-{int(time.time()*1000)}'
        await ws.send(json.dumps({'type':'req','id':'qm-sess','method':'sessions.create',
            'params':{'key':sess,'kind':'direct'}}))
        await asyncio.wait_for(ws.recv(), timeout=5)

        # Chat.
        run_id = f'qm-run-{int(time.time()*1000)}'
        await ws.send(json.dumps({'type':'req','id':'qm-chat','method':'chat.send',
            'params':{'sessionKey':sess,'message':MESSAGE,'clientRunId':run_id}}))

        # Read initial response.
        await asyncio.wait_for(ws.recv(), timeout=5)

        # Collect events.
        start = time.time()
        text = ''
        delta_count = 0
        event_count = 0
        tool_errors = 0

        for _ in range(2000):
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=30)
            except asyncio.TimeoutError:
                break
            frame = json.loads(raw)
            evt = frame.get('event', '')
            payload = frame.get('payload', {})
            state = payload.get('state', '')
            event_count += 1

            if evt == 'chat.delta':
                text += payload.get('delta', '')
                delta_count += 1
            elif evt == 'chat' and state in ('done', 'error', 'aborted'):
                if state == 'done':
                    text = payload.get('text', text)
                elif state == 'error':
                    tool_errors += 1
                break
            elif evt == 'chat.tool' and state == 'completed' and payload.get('isError'):
                tool_errors += 1
            elif evt == 'tick':
                continue

        elapsed_ms = (time.time() - start) * 1000

        # Score.
        s_korean = score_korean(text)
        s_substance = score_substance(text)
        s_clean = score_clean(text)
        s_latency = score_latency(elapsed_ms)
        s_streaming = score_streaming(delta_count, event_count)

        # Penalty for tool errors.
        penalty = tool_errors * 10
        total = max(s_korean + s_substance + s_clean + s_latency + s_streaming - penalty, 0)

        # Detail output (stderr for human readability).
        print(f'  korean={s_korean} substance={s_substance} clean={s_clean} '
              f'latency={s_latency}({elapsed_ms:.0f}ms) streaming={s_streaming} '
              f'penalty={penalty} text_len={len(text)}', file=sys.stderr)
        if text:
            preview = text[:100].replace(chr(10), ' ')
            print(f'  reply: {preview}', file=sys.stderr)

        # Machine-readable output (stdout).
        print(f'metric_value={total}')
        print(f'DENEB_METRIC_DETAIL korean={s_korean} substance={s_substance} clean={s_clean} latency={s_latency} streaming={s_streaming} penalty={penalty}')

    finally:
        await ws.close()

asyncio.run(main())
"
