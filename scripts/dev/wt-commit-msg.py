#!/usr/bin/env python3
"""wt-commit-msg.py — LLM commit-message command for Worktrunk (wt).

Reads the wt-provided commit prompt (diff + instructions, including the
project `template-append` from .config/wt.toml) on stdin and prints the
commit message on stdout. Backed by the wormhole (single local URL + token,
fronting glm-5.2 and the local GPU models) so it needs no external API key
and works unmetered. Wire it in the worktrunk *user* config:

    [commit.generation]
    command = "python3 scripts/dev/wt-commit-msg.py"

The command runs with the worktree as cwd, so the relative path resolves in
any Deneb checkout. Default model is the local GPU one; override with
WT_COMMIT_MODEL=glm-5.2 for higher quality.
"""

import json
import os
import re
import sys
import urllib.request

WORMHOLE_CFG = os.path.expanduser("~/.wormhole/config.json")
WORMHOLE_URL = "http://127.0.0.1:18800/v1/chat/completions"
DEFAULT_MODEL = "qwen3.6-35b-a3b"


def main() -> int:
    prompt = sys.stdin.read().strip()
    if not prompt:
        print("wt-commit-msg: empty prompt on stdin", file=sys.stderr)
        return 1
    try:
        with open(WORMHOLE_CFG, encoding="utf-8") as f:
            token = json.load(f)["token"]
    except (OSError, KeyError, ValueError) as e:
        print(f"wt-commit-msg: cannot read wormhole token: {e}", file=sys.stderr)
        return 1
    body = json.dumps(
        {
            "model": os.environ.get("WT_COMMIT_MODEL", DEFAULT_MODEL),
            "messages": [{"role": "user", "content": prompt}],
            # generous cap: reasoning models spend tokens thinking before the
            # final message, and a small cap yields an empty response
            "max_tokens": 4096,
        }
    ).encode()
    req = urllib.request.Request(
        WORMHOLE_URL,
        data=body,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.load(resp)
        msg = data["choices"][0]["message"]["content"]
    except Exception as e:  # noqa: BLE001 — single funnel; detail goes to stderr
        print(f"wt-commit-msg: wormhole call failed: {e}", file=sys.stderr)
        return 1
    msg = re.sub(r"<think>.*?</think>", "", msg, flags=re.S).strip()
    msg = re.sub(r"^```[a-z]*\n(.*?)\n```$", r"\1", msg, flags=re.S).strip()
    if not msg:
        print("wt-commit-msg: model returned an empty message", file=sys.stderr)
        return 1
    print(msg)
    return 0


if __name__ == "__main__":
    sys.exit(main())
