#!/usr/bin/env python3
"""Discover, inspect and invoke Deneb tools through the ./dev JSON protocol."""
from __future__ import annotations

import argparse
import ast
from dataclasses import asdict
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tempfile

from agent_dev_catalog import BY_ID, DISCOVERY_DIRS, PACKAGE_DIRS, TOOLS

ROOT = Path(__file__).resolve().parents[2]
SCHEMA_VERSION = 1
MAX_OUTPUT = 12000
REGISTRY = "scripts/dev/agent_dev_catalog.py"
PYTHON_REPAIR = [["python3", "-m", "venv", ".venv"],
                 [".venv/bin/python", "-m", "pip", "install", "--require-hashes",
                  "-r", "requirements-dev.lock"]]


class ToolError(Exception):
    def __init__(self, message, recovery=None):
        super().__init__(message)
        self.recovery = recovery or []


def emit(value):
    print(json.dumps({"schema_version": SCHEMA_VERSION, **value}, ensure_ascii=False, indent=2))


def python_runtime():
    """Select a local interpreter without activating a shell or installing packages."""
    explicit = os.environ.get("DENEB_DEV_PYTHON")
    if explicit:
        candidate = shutil.which(explicit) or str(Path(explicit).expanduser().absolute())
        if not Path(candidate).is_file() or not os.access(candidate, os.X_OK):
            raise ToolError("DENEB_DEV_PYTHON is not executable",
                            ["set DENEB_DEV_PYTHON to an existing interpreter"])
        return candidate, "DENEB_DEV_PYTHON"
    candidate = ROOT / ".venv/bin/python"
    if candidate.is_file() and os.access(candidate, os.X_OK):
        return str(candidate), "checkout .venv"
    candidate = managed_bin().parent / "venv/bin/python"
    if candidate.is_file() and os.access(candidate, os.X_OK):
        return str(candidate), "managed Deneb environment"
    return sys.executable, "current interpreter"


def managed_bin():
    root = Path(os.environ.get("DENEB_DEV_HOME", str(Path.home() / ".local/share/deneb-dev")))
    return root.expanduser() / "current/bin"


def child_environment():
    python, _ = python_runtime()
    env = os.environ.copy()
    env["PATH"] = os.pathsep.join((str(Path(python).parent), str(managed_bin()), str(Path.home() / "go/bin"),
                                  env.get("PATH", os.defpath)))
    if managed_bin().is_dir():
        env["GOTOOLCHAIN"] = "local"
        env.pop("GOROOT", None)
    return env


def package_inventory():
    python, selected_by = python_runtime()
    expected = dict(re.findall(r"^([\w.-]+)==([^\s;]+)",
                              (ROOT / "requirements-dev.lock").read_text(), re.M))
    probe = """import importlib.metadata as m, json, platform, sys
versions = {}
for name in sys.argv[1:]:
    try: versions[name] = m.version(name)
    except m.PackageNotFoundError: versions[name] = None
print(json.dumps({'version': platform.python_version(), 'packages': versions}))
"""
    try:
        result = subprocess.run([python, "-c", probe, *expected], capture_output=True,
                                text=True, timeout=15)
        if result.returncode:
            raise ToolError("selected Python cannot read package metadata", PYTHON_REPAIR)
        actual = json.loads(result.stdout)
    except (OSError, subprocess.TimeoutExpired, ValueError) as exc:
        raise ToolError("cannot inspect selected Python: " + type(exc).__name__, PYTHON_REPAIR) from exc
    rows = [{"name": name, "expected": version, "installed": actual["packages"].get(name),
             "status": "ok" if actual["packages"].get(name) == version else
             "missing" if actual["packages"].get(name) is None else "version_mismatch"}
            for name, version in expected.items()]
    return {"executable": python, "selected_by": selected_by, "version": actual["version"],
            "packages": rows, "manifest": "requirements-dev.lock"}


def source_description(path):
    text = path.read_text(errors="replace")
    if path.suffix == ".py":
        try:
            return ast.get_docstring(ast.parse(text)) or ""
        except SyntaxError:
            return ""
    lines = []
    for line in text.splitlines():
        if line.startswith("#!"):
            continue
        if line.startswith("#"):
            lines.append(line.lstrip("# "))
        elif line.strip():
            break
    return "\n".join(lines)


def make_targets():
    """Read literal targets only; make -p/-n can still execute $(shell ...) code."""
    path = ROOT / "Makefile"
    return re.findall(r"^([A-Za-z][\w./-]*):(?!=)", path.read_text(), re.M) if path.is_file() else []


def package_scripts():
    for directory in PACKAGE_DIRS:
        path = ROOT / directory / "package.json"
        if path.is_file():
            for name, command in json.loads(path.read_text()).get("scripts", {}).items():
                yield directory, name, command


def discover():
    """Read entrypoints without importing scripts, evaluating make, or starting services."""
    managed = {tool.source for tool in TOOLS}
    managed.add("scripts/dev/agent_dev.py")
    rows = []
    for directory in DISCOVERY_DIRS:
        for parent, dirs, files in os.walk(ROOT / directory, followlinks=False):
            dirs[:] = sorted(d for d in dirs if not d.startswith(".") and
                             d not in ("__pycache__", "node_modules", "build", "dist") and
                             not (Path(parent) / d).is_symlink())
            for name in sorted(files):
                path = Path(parent) / name
                if path.is_symlink() or name.startswith((".", "test_")):
                    continue
                relative = path.relative_to(ROOT).as_posix()
                if relative in managed or path.suffix not in (".py", ".sh", ""):
                    continue
                text = path.read_text(errors="replace")
                if path.suffix == ".py":
                    if not re.search(r'if\s+__name__\s*==\s*[\'"]__main__[\'"]', text):
                        continue
                elif path.suffix == "" and not text.startswith("#!"):
                    continue
                description = source_description(path)
                rows.append({"id": relative, "summary": next((line for line in description.splitlines()
                             if line.strip()), relative), "source": relative})
    managed_make = {tool.command[1] for tool in TOOLS if tool.command[:1] == ("make",)}
    rows.extend({"id": "make:" + target, "summary": "Makefile target: " + target,
                 "source": "Makefile", "native_command": ["make", target]}
                for target in make_targets() if target not in managed_make)
    managed_package = {(tool.cwd, tool.command[1]) for tool in TOOLS
                       if tool.command[:1] == ("pnpm",)}
    rows.extend({"id": directory + ":" + name, "summary": command,
                 "source": directory + "/package.json", "cwd": directory,
                 "native_command": ["pnpm", "run", name]}
                for directory, name, command in package_scripts()
                if (directory, name) not in managed_package)
    for row in rows:
        row.update({"managed": False, "execution": "inspect_source",
                    "next": ["./dev", "describe", row["id"]]})
    return sorted(rows, key=lambda row: row["id"])


def compact(tool):
    return {"id": tool.id, "summary": tool.summary, "effects": tool.effects,
            "execution": tool.execution, "managed": True}


def describe(identifier):
    if identifier in BY_ID:
        tool = BY_ID[identifier]
        result = asdict(tool)
        result.update({"managed": True, "input_schema": {"type": "object", "properties": {
            "arguments": {"type": "array", "items": {"type": "string"},
                          "description": "Original arguments passed literally to the existing tool."}},
            "additionalProperties": False},
            "invoke": ["./dev", "run", identifier, "--", "<arguments...>"],
            "examples": [["./dev", "run", identifier, "--", *args] for args in tool.examples]})
        if tool.source:
            result["source_help"] = source_description(ROOT / tool.source)[:6000]
        return result
    match = next((row for row in discover() if row["id"] == identifier), None)
    if match:
        return {**match, "source_help": source_description(ROOT / match["source"])[:6000],
                "notes": "Inspect the source and its execution contract. Add a Tool adapter in " + REGISTRY + " for managed execution."}
    raise ToolError("unknown tool: " + identifier, [["./dev", "search", identifier]])


def registry_errors():
    errors = []
    if len(BY_ID) != len(TOOLS):
        errors.append("duplicate tool IDs")
    targets = set(make_targets())
    scripts = {(directory, name) for directory, name, _ in package_scripts()}
    for tool in TOOLS:
        for path in (tool.docs, tool.source):
            if path and not (ROOT / path).is_file():
                errors.append(tool.id + ": missing source or documentation " + path)
        if not (ROOT / tool.cwd).is_dir():
            errors.append(tool.id + ": missing working directory " + tool.cwd)
        if tool.command[:1] == ("make",) and tool.command[1] not in targets:
            errors.append(tool.id + ": missing Makefile target " + tool.command[1])
        if tool.command[:1] == ("pnpm",) and (tool.cwd, tool.command[1]) not in scripts:
            errors.append(tool.id + ": missing package script " + tool.command[1])
    return errors


def status():
    runtime = package_inventory()
    env = child_environment()
    binaries = sorted({name for tool in TOOLS for name in
                       (*tool.requires, *tool.command[:1]) if name != "{python}"})
    installed = [{"name": name, "path": shutil.which(name, path=env["PATH"]),
                  "version": "not_probed", "recovery": "See the owning tool's describe output and CLAUDE.md"}
                 for name in binaries]
    requirements = {"go": re.search(r"^go\s+(\S+)", (ROOT / "gateway-go/go.mod").read_text(), re.M).group(1),
                    "andromeda_package_manager": json.loads((ROOT / "andromeda/package.json").read_text())["packageManager"],
                    "python_target": re.search(r'target-version\s*=\s*"([^"]+)"',
                                               (ROOT / "pyproject.toml").read_text()).group(1)}
    paths = [{"path": path, "present": (ROOT / path).exists()} for path in
             sorted({path for tool in TOOLS for path in tool.paths})]
    errors = registry_errors()
    package_problems = any(row["status"] != "ok" for row in runtime["packages"])
    needs_attention = errors or package_problems or any(not row["path"] for row in installed) or \
        any(not row["present"] for row in paths)
    recovery = list(PYTHON_REPAIR) if package_problems else []
    recovery.extend(["pnpm", "--dir", str(Path(row["path"]).parent), "install", "--frozen-lockfile"]
                    for row in paths if not row["present"])
    if errors:
        recovery.append(["./dev", "audit"])
    return {"status": "needs_attention" if needs_attention else "ok", "python": runtime,
            "requirements": requirements, "tools": installed, "paths": paths,
            "registry_errors": errors, "recovery": recovery,
            "scope": "Local metadata only; no installations, service startup, network or credential reads.",
            "limitations": "Executable paths do not prove versions, authentication or live readiness. Optional lanes may be unavailable."}


def command_for(tool, arguments):
    python, _ = python_runtime()
    command = [python if part == "{python}" else part for part in tool.command]
    if command[:1] == ["make"] and managed_bin().is_dir():
        # The Makefile prepends ~/go/bin. A command-line PATH keeps the pinned
        # linter/toolchain first and propagates to nested make calls in ci.fast.
        command.append("PATH=" + child_environment()["PATH"])
    return command + list(arguments)


def prerequisites(tool, env):
    missing = [name for name in (*tool.requires, *tool.command[:1]) if name != "{python}"
               and not shutil.which(name, path=env["PATH"])]
    if missing:
        raise ToolError("missing executables: " + ", ".join(missing),
                        [["./dev", "status"], ["./dev", "describe", tool.id]])
    if tool.packages:
        rows = {row["name"].casefold(): row for row in package_inventory()["packages"]}
        missing = [name for name in tool.packages if rows.get(name.casefold(), {}).get("status") != "ok"]
        if missing:
            raise ToolError("missing or mismatched locked Python packages: " + ", ".join(missing), PYTHON_REPAIR)
    for path in tool.paths:
        if not (ROOT / path).exists():
            raise ToolError("missing prerequisite: " + path,
                            [["pnpm", "--dir", tool.cwd, "install", "--frozen-lockfile"]])
    if tool.source and not (ROOT / tool.source).is_file():
        raise ToolError("missing tool source: " + tool.source, [["./dev", "audit"]])


def terminate_group(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            pass
        # A grandchild can ignore TERM even after the immediate child exits.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    except ProcessLookupError:
        pass
    process.wait()


def read_tail(stream, limit):
    stream.seek(0, os.SEEK_END)
    size = stream.tell()
    stream.seek(max(0, size - limit))
    return stream.read().decode("utf-8", errors="replace"), size > limit


def execute(tool, arguments, timeout=None, limit=MAX_OUTPUT, dry_run=False):
    if tool.execution == "builtin":
        if arguments:
            raise ToolError("env.status takes no arguments")
        return {"status": "planned" if dry_run else "completed", "tool": tool.id,
                **({} if dry_run else {"result": status()})}, 0
    command = command_for(tool, arguments)
    cwd = str(ROOT / tool.cwd)
    if dry_run:
        return {"status": "planned", "tool": tool.id, "argv": command, "cwd": cwd,
                "effects": tool.effects, "execution": tool.execution,
                "prerequisites": {"executables": tool.requires, "packages": tool.packages,
                                  "paths": tool.paths}, "readiness": "not_checked"}, 0
    env = child_environment()
    prerequisites(tool, env)
    state = "completed"
    with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=stdout, stderr=stderr,
                                   start_new_session=True)
        try:
            process.wait(timeout=timeout if timeout is not None else tool.timeout)
        except subprocess.TimeoutExpired:
            state = "timeout"
            terminate_group(process)
        except KeyboardInterrupt:
            state = "cancelled"
            terminate_group(process)
        out, out_cut = read_tail(stdout, limit)
        err, err_cut = read_tail(stderr, limit)
    code = process.returncode
    result = {"tool": tool.id, "status": state, "process_exit_code": code,
              "execution": tool.execution, "cwd": cwd, "stdout": out, "stderr": err,
              "truncated": {"stdout": out_cut, "stderr": err_cut}, "validation": "not_assessed"}
    if state in ("timeout", "cancelled"):
        result["recovery"] = [["./dev", "describe", tool.id]]
        result["note"] = "The local process group was terminated. Inspect partial outputs and remote state before retrying; remote actions cannot be rolled back."
        return result, 124 if state == "timeout" else 130
    if code:
        result["status"] = "failed"
        result["recovery"] = [["./dev", "describe", tool.id], ["./dev", "status"]]
    elif ((tool.id == "env.doctor" and (out_cut or re.search(r"\[missing\]|Warning: \d+ tool", out))) or
          (tool.id == "ci.fast" and (out_cut or "nothing to gate" in out))):
        result["status"] = "incomplete"
        result["recovery"] = [["./dev", "status"], ["./dev", "describe", tool.id]]
        return result, 3
    if not out_cut:
        try:
            result["data"] = json.loads(out)
            result["stdout"] = None
        except ValueError:
            pass
    return result, code if code >= 0 else 128 - code


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise ToolError(message, [["./dev", "--help"]])


def main(argv=None):
    parser = Parser(description=__doc__)
    sub = parser.add_subparsers(dest="action")
    sub.add_parser("status", help="local environment and tool prerequisites")
    sub.add_parser("audit", help="adapter drift and unmanaged entrypoints")
    search = sub.add_parser("search", help="search adapters, scripts, make targets and package scripts")
    search.add_argument("query", nargs="?", default="")
    search.add_argument("--limit", type=int, default=20)
    search.add_argument("--offset", type=int, default=0)
    sub.add_parser("list", help="compact managed-tool catalog")
    show = sub.add_parser("describe", help="input contract, effects, source help and examples")
    show.add_argument("tool")
    run = sub.add_parser("run", help="invoke an existing tool with a bounded JSON result")
    run.add_argument("--dry-run", action="store_true")
    run.add_argument("--timeout", type=float)
    run.add_argument("--max-output", type=int, default=MAX_OUTPUT)
    run.add_argument("tool")
    run.add_argument("arguments", nargs=argparse.REMAINDER)
    try:
        args = parser.parse_args(argv)
        if args.action == "status":
            emit(status())
        elif args.action == "audit":
            errors = registry_errors()
            emit({"status": "failed" if errors else "ok", "errors": errors,
                  "managed_tools": len(TOOLS), "unmanaged_entrypoints": len(discover()),
                  "next": ["./dev", "search", ""], "registry": str(ROOT / REGISTRY)})
            return 1 if errors else 0
        elif args.action == "list":
            emit({"tools": [compact(tool) for tool in TOOLS]})
        elif args.action == "search":
            if not 1 <= args.limit <= 100 or args.offset < 0:
                raise ToolError("search requires 1 <= limit <= 100 and offset >= 0")
            terms = args.query.casefold().split()
            rows = [compact(tool) for tool in TOOLS] + discover()
            matches = [row for row in rows if all(term in (row["id"] + " " + row["summary"] + " " +
                       (BY_ID[row["id"]].keywords if row["managed"] else "")).casefold() for term in terms)]
            end = args.offset + args.limit
            emit({"query": args.query, "total": len(matches), "items": matches[args.offset:end],
                  "next_offset": end if end < len(matches) else None})
        elif args.action == "describe":
            emit(describe(args.tool))
        elif args.action == "run":
            if args.timeout is not None and not 0 < args.timeout <= 86400:
                raise ToolError("timeout must be between 0 and 86400 seconds")
            if not 256 <= args.max_output <= 1024 * 1024:
                raise ToolError("max-output must be between 256 and 1048576 bytes")
            if args.tool not in BY_ID:
                raise ToolError("tool has no execution adapter: " + args.tool, [["./dev", "describe", args.tool]])
            arguments = args.arguments[1:] if args.arguments[:1] == ["--"] else args.arguments
            result, code = execute(BY_ID[args.tool], arguments, args.timeout, args.max_output, args.dry_run)
            emit(result)
            return code
        else:
            python, selected_by = python_runtime()
            emit({"entrypoint": "./dev", "project": "Deneb", "repo": str(ROOT),
                  "python": {"executable": python, "selected_by": selected_by},
                  "catalog_sha256": hashlib.sha256((ROOT / REGISTRY).read_bytes()).hexdigest(),
                  "managed_tools": len(TOOLS),
                  "commands": {"discover": "./dev search <intent>", "inventory": "./dev status",
                               "contract": "./dev describe <tool>", "execute": "./dev run <tool> -- <arguments>",
                               "preview": "./dev run --dry-run <tool> -- <arguments>", "maintenance": "./dev audit"},
                  "workflow": [
                      {"when": "find existing tools before adding one", "command": ["./dev", "search", "<intent>"]},
                      {"when": "dependency failure", "command": ["./dev", "status"]},
                      {"when": "inspect a string-keyed RPC", "command": ["./dev", "describe", "rpcmap"]},
                      {"when": "before pushing a single-lane diff", "command": ["./dev", "describe", "ci.fast"]},
                      {"when": "requested PR merge", "command": ["./dev", "describe", "pr.land"]}],
                  "evidence": "Process completion does not prove skipped lanes, live readiness, deployment or model quality.",
                  "tools": [{"id": tool.id, "summary": tool.summary} for tool in TOOLS]})
        return 0
    except ToolError as exc:
        emit({"status": "blocked", "error": str(exc), "recovery": exc.recovery})
        return 2
    except (OSError, ValueError, KeyError, TypeError, AttributeError) as exc:
        emit({"status": "failed", "error": str(exc), "recovery": [["./dev", "audit"]]})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
