#!/usr/bin/env bash
# Quality metric for constant optimization.
# Sends a chat message via the mock Telegram server, measures response
# quality, and returns a numeric score.
#
# Usage:
#   scripts/quality-metric.sh [MSG]
#
# Output (last line):
#   metric_value=85.5
#
# Score components (0-100):
#   - korean_ratio (0-25): response is in Korean
#   - substance (0-25): response has meaningful content
#   - clean (0-20): no leaked markup, no filler
#   - latency (0-15): response time within budget
#   - streaming (0-15): draft edits flowed properly

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPTS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MESSAGE="${1:-안녕, 간단히 자기소개 해줘}"

MOCK_QUALITY_MESSAGE="$MESSAGE" \
DENEB_SCRIPTS_DIR="$SCRIPTS_DIR" \
python3 - <<'PYEOF'
import asyncio
import os
import re
import sys

sys.path.insert(0, os.environ["DENEB_SCRIPTS_DIR"])
from mock_telegram_client import TelegramTestClient, check_prerequisites

MESSAGE = os.environ.get("MOCK_QUALITY_MESSAGE", "")

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
    for pat in [r'<function=', r'</?thinking>', r'\[\[reply_to', r'MEDIA:\S+', r'NO_REPLY']:
        if re.search(pat, text): s -= 5
    for pat in [r'^(Great question|I.d be happy to|Sure)', r'^(좋은 질문|물론이죠|당연하죠)']:
        if re.match(pat, text.strip(), re.IGNORECASE): s -= 5
    return max(s, 0)

def score_latency(ms):
    if ms < 10000: return 15
    if ms < 20000: return 12
    if ms < 30000: return 8
    if ms < 60000: return 4
    return 0

def score_streaming(edit_count, event_count):
    if edit_count > 3 and event_count > 5: return 15
    if edit_count > 0: return 10
    if event_count > 0: return 5
    return 0

async def main():
    ok, detail = check_prerequisites()
    if not ok:
        print(f'prereq_error={detail}', file=sys.stderr)
        print('metric_value=0')
        return

    client = TelegramTestClient()
    try:
        await client.connect()
    except Exception as e:
        print(f'connect_error={e}', file=sys.stderr)
        print('metric_value=0')
        return

    try:
        capture = await client.chat(MESSAGE)
        text = capture.reply_text
        elapsed_ms = capture.latency_ms
        edit_count = len(capture.draft_edits)
        event_count = len(capture.raw_events)

        # Score.
        s_korean = score_korean(text)
        s_substance = score_substance(text)
        s_clean = score_clean(text)
        s_latency = score_latency(elapsed_ms)
        s_streaming = score_streaming(edit_count, event_count)

        # Penalty for errors.
        penalty = len(capture.errors) * 10
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
        await client.disconnect()

asyncio.run(main())
PYEOF
