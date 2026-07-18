#!/usr/bin/env python3
"""Stable shim installed at ~/.claude/hooks/deneb-concurrency-guard.py.

Do not put guard logic here. This execs the production checkout's canonical
guard (scripts/dev/deneb-concurrency-guard.py), so logic updates ship via the
normal merge → auto-deploy pipeline. When the target is absent (pre-deploy,
repo moved) it exits 0 — a real file that no-ops, never a dangling symlink
(python3 on a missing file exits 2, which PreToolUse would treat as a block).

Installed by scripts/dev/install-user-hooks.sh; wired in ~/.claude/settings.json
hooks.PreToolUse (matcher Write|Edit|MultiEdit|Bash).
"""

import os
import sys

TARGET = os.path.expanduser("~/deneb/scripts/dev/deneb-concurrency-guard.py")

if os.path.isfile(TARGET):
    interpreter = sys.executable or "python3"
    os.execv(interpreter, [interpreter, TARGET])
sys.exit(0)
