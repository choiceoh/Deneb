#!/usr/bin/env python3
"""Restore Deneb CodeGraph MCP wrappers after `codegraph upgrade`.

The upstream installer rewrites IDE MCP configs to a bare
`codegraph serve --mcp --path …`. That drops worktree binding, the
explore→node proxy, and CODEGRAPH_MCP_TOOLS. This rewriter only touches
codegraph entries that match that fingerprint; custom servers stay put.
"""

from __future__ import annotations

import json
import os
import shutil
import sys
from pathlib import Path

TOOLS = "explore,node,search,impact,callers,callees"
WRAPPER_NAME = "deneb-codegraph-serve"
CURSOR_WRAPPER_NAME = "deneb-cursor-codegraph-serve"

REPO_CURSOR_MCP = {
    "mcpServers": {
        "codegraph": {
            "type": "stdio",
            "command": "bash",
            "args": [
                "${workspaceFolder}/scripts/dev/cursor-codegraph-serve.sh",
                "${workspaceFolder}",
            ],
            "env": {
                "PATH": "${env:PATH}:${userHome}/.local/bin:${userHome}/.npm-global/bin",
                "CURSOR_CODEGRAPH_FALLBACK": "${workspaceFolder}",
                "CODEGRAPH_MCP_TOOLS": TOOLS,
                "CODEGRAPH_AGENT": "cursor",
            },
        }
    }
}

REPO_MCP = {
    "mcpServers": {
        "codegraph": {
            "type": "stdio",
            "command": "bash",
            "args": ["scripts/dev/codegraph-serve.sh"],
            "env": {
                "CODEGRAPH_MCP_TOOLS": TOOLS,
                "CODEGRAPH_AGENT": "claude",
            },
        }
    }
}

ZCODE_SERVER = {
    "type": "stdio",
    "command": "bash",
    "args": ["scripts/dev/codegraph-serve.sh"],
    "env": {
        "CODEGRAPH_MCP_TOOLS": TOOLS,
        "CODEGRAPH_AGENT": "zcode",
    },
}

VSCODE_MCP = {
    "servers": {
        "codegraph": {
            "type": "stdio",
            "command": "bash",
            "args": ["${workspaceFolder}/scripts/dev/codegraph-serve.sh"],
            "env": {
                "CODEGRAPH_MCP_TOOLS": TOOLS,
                "CODEGRAPH_AGENT": "claude",
            },
        }
    }
}

TRAE_MCP = {
    "mcpServers": {
        "codegraph": {
            "type": "stdio",
            "command": "bash",
            "args": ["${workspaceFolder}/scripts/dev/codegraph-serve.sh"],
            "env": {
                "CODEGRAPH_MCP_TOOLS": TOOLS,
                "CODEGRAPH_AGENT": "trae",
            },
        }
    }
}

CODEX_TOML = """\
# Project-scoped CodeGraph MCP (trusted checkouts only).
[mcp_servers.codegraph]
command = "bash"
args = ["scripts/dev/codegraph-serve.sh"]

[mcp_servers.codegraph.env]
CODEGRAPH_MCP_TOOLS = "explore,node,search,impact,callers,callees"
CODEGRAPH_AGENT = "codex"
"""


def is_upgrade_clobber(cfg: object) -> bool:
    if not isinstance(cfg, dict):
        return False
    cg = _codegraph_entry(cfg)
    if not isinstance(cg, dict) or cg.get("command") != "codegraph":
        return False
    args = [str(a) for a in (cg.get("args") or [])]
    return "serve" in args and "--mcp" in args


def _codegraph_entry(cfg: dict) -> object:
    if isinstance(cfg.get("mcpServers"), dict):
        return cfg["mcpServers"].get("codegraph")
    servers = cfg.get("servers")
    if isinstance(servers, dict):
        return servers.get("codegraph")
    mcp = cfg.get("mcp")
    if isinstance(mcp, dict) and isinstance(mcp.get("servers"), dict):
        return mcp["servers"].get("codegraph")
    return None


def _load_json(path: Path) -> object | None:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def _write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def _replace_codegraph(cfg: dict, server: dict) -> dict:
    out = dict(cfg)
    if isinstance(out.get("mcpServers"), dict):
        servers = dict(out["mcpServers"])
        servers["codegraph"] = server
        out["mcpServers"] = servers
        return out
    if isinstance(out.get("servers"), dict):
        servers = dict(out["servers"])
        servers["codegraph"] = server
        out["servers"] = servers
        return out
    mcp = dict(out.get("mcp") or {})
    inner = dict(mcp.get("servers") or {})
    inner["codegraph"] = server
    mcp["servers"] = inner
    out["mcp"] = mcp
    return out


def _toml_codegraph_is_clobber(text: str) -> bool:
    if "[mcp_servers.codegraph]" not in text:
        return True
    chunk = text.split("[mcp_servers.codegraph]", 1)[1]
    nxt = chunk.find("\n[")
    body = chunk if nxt < 0 else chunk[:nxt]
    return "command = \"codegraph\"" in body and "serve" in body and "--mcp" in body


def restore_codex_toml(path: Path, payload: str) -> bool:
    if not path.is_file():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(payload, encoding="utf-8")
        return True
    current = path.read_text(encoding="utf-8")
    if not _toml_codegraph_is_clobber(current):
        return False
    if "[mcp_servers.codegraph]" not in current:
        path.write_text(current.rstrip() + "\n\n" + payload, encoding="utf-8")
        return True
    path.write_text(payload, encoding="utf-8")
    return True


def restore_repo(root: str) -> list[str]:
    """Rewrite repo MCP files when they look like `codegraph upgrade` output."""
    base = Path(root)
    changed: list[str] = []
    whole_files = (
        (base / ".cursor" / "mcp.json", REPO_CURSOR_MCP),
        (base / ".mcp.json", REPO_MCP),
        (base / ".trae" / "mcp.json", TRAE_MCP),
        (base / ".vscode" / "mcp.json", VSCODE_MCP),
    )
    for path, payload in whole_files:
        if not path.is_file():
            continue
        current = _load_json(path)
        if current is None or not is_upgrade_clobber(current):
            continue
        _write_json(path, payload)
        changed.append(str(path))

    zcode = base / ".zcode" / "config.json"
    current = _load_json(zcode) if zcode.is_file() else None
    if isinstance(current, dict) and is_upgrade_clobber(current):
        _write_json(zcode, _replace_codegraph(current, ZCODE_SERVER))
        changed.append(str(zcode))

    codex = base / ".codex" / "config.toml"
    if codex.is_file() and _toml_codegraph_is_clobber(codex.read_text(encoding="utf-8")):
        restore_codex_toml(codex, CODEX_TOML)
        changed.append(str(codex))
    return changed


def user_mcp_payload(wrapper: Path) -> dict:
    return {
        "mcpServers": {
            "codegraph": {
                "type": "stdio",
                "command": "bash",
                "args": [str(wrapper)],
                "env": {
                    "PATH": f"{wrapper.parent}:{os.path.expanduser('~/.npm-global/bin')}:/usr/local/bin:/usr/bin:/bin",
                    "CODEGRAPH_MCP_TOOLS": TOOLS,
                },
            }
        }
    }


def install_user_wrapper(repo: str, *, home: Path | None = None) -> Path | None:
    home_path = Path(home) if home is not None else Path.home()
    dest_dir = home_path / ".local" / "bin"
    dest_dir.mkdir(parents=True, exist_ok=True)
    installed: Path | None = None
    for name in (WRAPPER_NAME, CURSOR_WRAPPER_NAME):
        src = Path(repo) / "scripts" / "dev" / name
        if not src.is_file():
            continue
        dest = dest_dir / name
        shutil.copy2(src, dest)
        dest.chmod(dest.stat().st_mode | 0o111)
        if name == CURSOR_WRAPPER_NAME:
            installed = dest
        elif installed is None:
            installed = dest
    return installed


def restore_user(repo: str, *, home: Path | None = None) -> list[str]:
    home_path = Path(home) if home is not None else Path.home()
    path = home_path / ".cursor" / "mcp.json"
    current = _load_json(path) if path.is_file() else None
    if current is not None and not is_upgrade_clobber(current):
        return []
    wrapper = install_user_wrapper(repo, home=home_path)
    if wrapper is None:
        return []
    _write_json(path, user_mcp_payload(wrapper))
    return [str(path), str(wrapper)]


def main(argv: list[str] | None = None) -> int:
    args = argv if argv is not None else sys.argv[1:]
    root = os.getcwd()
    user = True
    i = 0
    while i < len(args):
        if args[i] == "--root" and i + 1 < len(args):
            root = args[i + 1]
            i += 2
            continue
        if args[i] == "--no-user":
            user = False
            i += 1
            continue
        i += 1
    changed = restore_repo(root)
    if user:
        changed.extend(restore_user(root))
    if changed:
        print("restored: " + ", ".join(changed))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # noqa: BLE001 — session-start convenience, never block
        sys.exit(0)
