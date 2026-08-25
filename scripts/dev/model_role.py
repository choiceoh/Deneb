#!/usr/bin/env python3
"""Resolve a Deneb model ROLE to the wormhole route name serving it.

Scripts must not pin model names. A name is a snapshot of today's fleet: it
does not fail when the model dies, it silently routes somewhere else. The
2026-08 case that motivated this module — ``recall-health.sh`` pinned
``qwen3.6-35b-a3b``, whose local serving died on 08-06; wormhole failed every
call over to a metered cloud endpoint, so ten days of recall benchmarks scored
a different model than production ran, and billed for it. Roles move with the
fleet (``deneb.json`` is the one place a model swap is recorded), names rot.

Usage from Python::

    from model_role import role_model
    model = role_model("tiny", fallback="dsv4-nothink")

Usage from shell::

    model="$(python3 scripts/dev/model_role.py tiny --fallback dsv4-nothink)"

The returned value is the BARE wormhole route name (``wormhole/glm-5.3`` →
``glm-5.3``), which is what the wormhole ``model`` field and the bench env vars
(``DENEB_EXPANSION_MODEL``) expect.
"""

from __future__ import annotations

import argparse
import json
import os
import sys

DENEB_CONFIG = os.path.expanduser("~/.deneb/deneb.json")

# role name -> deneb.json key. The keys live under agents/agents.defaults, but
# their exact nesting has moved between versions, so lookup is by key name
# anywhere in the tree (see _find) rather than by a fixed path.
# Only roles the gateway itself reads are listed. "analysis" is deliberately
# absent: the role was retired on 2026-07-07 (model-roles.md dogma #5) and the
# leftover deneb.json key is dead config — resolving it would quietly hand
# callers a value nothing else in the system honors.
ROLE_KEYS = {
    "coding": "codingModel",
    "evolver": "evolverModel",
    "fallback": "fallbackModel",
    "lightweight": "lightweightModel",
    "main": "defaultModel",
    "main2": "main2Model",
    "submain": "submainModel",
    "tiny": "tinyModel",
    "vision": "visionModel",
}


def _find(node: object, key: str) -> str:
    """First string value stored under `key` anywhere in the config tree."""
    if isinstance(node, dict):
        value = node.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
        for child in node.values():
            hit = _find(child, key)
            if hit:
                return hit
    elif isinstance(node, list):
        for child in node:
            hit = _find(child, key)
            if hit:
                return hit
    return ""


def role_model(role: str, fallback: str = "", config_path: str = DENEB_CONFIG) -> str:
    """Return the bare route name for `role`, or `fallback` when unresolvable.

    Unresolvable means: unknown role, missing/unreadable config, or the role is
    not configured. Callers get a usable model either way — this resolver is a
    lookup, never a gate.
    """
    key = ROLE_KEYS.get(role)
    if not key:
        return fallback
    try:
        with open(config_path, encoding="utf-8") as handle:
            config = json.load(handle)
    except (OSError, ValueError):
        return fallback
    return _find(config, key).rsplit("/", 1)[-1] or fallback


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("role", choices=sorted(ROLE_KEYS), help="Deneb model role")
    parser.add_argument("--fallback", default="", help="value when the role is unresolvable")
    parser.add_argument("--config", default=DENEB_CONFIG, help="deneb.json path")
    args = parser.parse_args(argv)
    model = role_model(args.role, args.fallback, args.config)
    if not model:
        print(
            f"model_role: role '{args.role}' is not configured and no --fallback given",
            file=sys.stderr,
        )
        return 1
    print(model)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
