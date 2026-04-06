#!/usr/bin/env python3
"""One-time Telethon session initialization.

Creates ~/.deneb/telegram-test.session for real e2e Telegram testing.
Requires SMS verification on first run; subsequent runs reuse the session file.

Usage:
    python3 scripts/telegram-session-init.py
"""

import asyncio
import os
import sys

def _load_dotenv(path: str) -> None:
    """Load KEY=VALUE from dotenv without overriding existing env."""
    if not os.path.isfile(path):
        return
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" not in line:
                continue
            key, _, val = line.partition("=")
            key = key.strip()
            val = val.strip().strip("'\"")
            if key not in os.environ:
                os.environ[key] = val

_load_dotenv(os.path.expanduser("~/.deneb/.env"))

API_ID = os.environ.get("TELEGRAM_API_ID")
API_HASH = os.environ.get("TELEGRAM_API_HASH")
PHONE = os.environ.get("TELEGRAM_TEST_PHONE")
SESSION_PATH = os.path.expanduser("~/.deneb/telegram-test")

if not API_ID or not API_HASH or not PHONE:
    print("Missing credentials in ~/.deneb/.env:")
    print("  TELEGRAM_API_ID, TELEGRAM_API_HASH, TELEGRAM_TEST_PHONE")
    sys.exit(1)

async def main() -> None:
    from telethon import TelegramClient

    client = TelegramClient(SESSION_PATH, int(API_ID), API_HASH)
    await client.start(phone=PHONE)

    me = await client.get_me()
    print(f"Authenticated as: {me.first_name} (ID: {me.id})")
    print(f"Session saved: {SESSION_PATH}.session")
    await client.disconnect()

asyncio.run(main())
