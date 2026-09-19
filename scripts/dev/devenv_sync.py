#!/usr/bin/env python3
"""Synchronize Deneb's Mac, four servers and optional WSL development environment."""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
import io
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import time

import devenv_host as host

ROOT = Path(__file__).resolve().parents[2]
HOST_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.@-]*")
BOOTSTRAP = """import os,pathlib,subprocess,sys,tarfile,tempfile
with tempfile.TemporaryDirectory(prefix='deneb-env-payload-') as directory:
 with tarfile.open(fileobj=sys.stdin.buffer,mode='r|') as archive:
  archive.extractall(directory,filter='data')
 command=[sys.executable,directory+'/scripts/dev/devenv_host.py',*sys.argv[1:]]
 if '--apply' in sys.argv:
  command += ['--repo',os.environ.get('DENEB_DEV_REPO',str(pathlib.Path.home()/'deneb-dev'))]
 raise SystemExit(subprocess.call(command))
"""


def payload():
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w") as archive:
        for name in (*host.INPUTS, "scripts/dev/devenv_host.py"):
            archive.add(ROOT / name, arcname=name, recursive=False)
    return stream.getvalue()


def targets(names=None, local=False, optional=None):
    manifest = json.loads((ROOT / "scripts/devenv/manifest.json").read_text())
    optional = manifest["optional_hosts"] if optional is None else optional
    machine = platform.node().split(".")[0].casefold()
    names = (["local"] if local else list(names)) if names is not None or local else \
        (["local"] if platform.system() == "Darwin" else []) + manifest["hosts"]
    result, seen = [], set()
    for name in names:
        if not HOST_PATTERN.fullmatch(name):
            raise host.SyncError("invalid SSH host: " + name)
        is_local = name == "local" or name.casefold() == machine
        identity = "mac" if is_local and platform.system() == "Darwin" else machine if is_local else name
        if identity.casefold() in seen:
            continue
        seen.add(identity.casefold())
        result.append({"id": identity, "address": name, "local": is_local, "optional": name in optional})
    if not result:
        raise host.SyncError("at least one target is required")
    return result


def invoke(target, bundle, apply, verify, logs):
    arguments = (["--apply"] if apply else []) + (["--verify"] if verify else [])
    if target["local"]:
        command = [sys.executable, str(ROOT / "scripts/dev/devenv_host.py"), *arguments]
        if apply and ((ROOT / ".git").exists() or platform.system() != "Darwin"):
            repo = ROOT if (ROOT / ".git").exists() else Path.home() / "deneb-dev"
            command += ["--repo", str(repo)]
        data = None
    else:
        command = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", target["address"],
                   shlex.join(["python3", "-c", BOOTSTRAP, *arguments])]
        data = bundle
    try:
        process = subprocess.run(command, input=data, capture_output=True, timeout=1800 if apply else 120)
        output = process.stdout.decode(errors="replace")
        error = process.stderr.decode(errors="replace")
        if process.returncode == 255 and not target["local"]:
            result = {"status": "skipped" if target["optional"] else "unreachable",
                      "reason": "SSH transport failed", "optional": target["optional"]}
        else:
            try:
                result = json.loads(output)
            except ValueError:
                result = {"status": "failed", "error": "host did not return valid JSON"}
            result["process_exit_code"] = process.returncode
            if process.returncode and result.get("status") == "ok":
                result["status"] = "failed"
        # Commands never contain credentials; redact URL credentials in upstream diagnostics too.
        diagnostic = re.sub(r"(https?://)[^/@\s]+:[^/@\s]+@", r"\1[redacted]@", error)
        if len(diagnostic) > 12000:
            diagnostic = "[earlier diagnostics truncated]\n" + diagnostic[-12000:]
        (logs / (target["id"] + ".log")).write_text(diagnostic)
    except subprocess.TimeoutExpired:
        result = {"status": "unknown", "error": "host timed out; remote work may remain; inspect before retrying"}
    except OSError as exc:
        result = {"status": "failed", "error": str(exc)}
    log = logs / (target["id"] + ".log")
    if not log.exists():
        log.write_text(result.get("error", "") + "\n")
    result.update({"host": target["id"], "log": str(log)})
    (logs / (target["id"] + ".json")).write_text(json.dumps(result, indent=2) + "\n")
    return result


def synchronize(names=None, local=False, optional=None, apply=False, verify=False):
    selected = targets(names, local, optional)
    bundle = payload()
    logs = Path(os.environ.get("DENEB_DEV_LOGS", str(Path.home() / ".local/state/deneb-devenv-sync")))
    logs = logs / (time.strftime("%Y%m%d-%H%M%S") + "-" + str(os.getpid()))
    logs.mkdir(parents=True, exist_ok=False)
    with ThreadPoolExecutor(max_workers=min(len(selected), 6)) as pool:
        jobs = [pool.submit(invoke, target, bundle, apply, verify, logs) for target in selected]
        results = [job.result() for job in jobs]
    failed = [row["host"] for row in results if row["status"] not in ("ok", "skipped")]
    completed = sum(row["status"] == "ok" for row in results)
    status = "failed" if failed else "ok" if completed == len(results) else "degraded" if completed else "incomplete"
    return {"status": status, "mode": "apply" if apply else "check", "hosts": results,
            "failed_hosts": failed, "logs": str(logs),
            "mac": "included locally on macOS; its own launchd job handles scheduled self-sync"}, \
        1 if failed else 0 if completed else 3


def install_timer():
    """Explicit operator action; ordinary sync never installs a recurring job."""
    if platform.system() not in ("Darwin", "Linux"):
        raise host.SyncError("timers require macOS launchd or Linux systemd")
    python = host.home() / "current/bin/python"
    if not python.is_file():
        raise host.SyncError("run env.setup successfully before installing a timer")
    destination = Path.home() / ".local/bin/deneb-devenv-sync"
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
        shutil.copy2(destination, destination.with_name(destination.name + ".previous-" + str(time.time_ns())))
    shutil.copy2(ROOT / "scripts/devenv/deneb-devenv-sync", destination)
    destination.chmod(0o755)
    if platform.system() == "Darwin":
        path = Path.home() / "Library/LaunchAgents/ai.deneb.devenv-sync.plist"
        path.parent.mkdir(parents=True, exist_ok=True)
        value = {"Label": "ai.deneb.devenv-sync",
                 "ProgramArguments": [str(python), str(destination), "--apply", "--verify", "--local"],
                 "StartCalendarInterval": {"Hour": 5, "Minute": 20},
                 "EnvironmentVariables": {"PATH": str(host.home() / "current/bin") + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
                                          "DENEB_DEV_HOME": str(host.home())}}
        path.write_bytes(plistlib.dumps(value))
        domain = "gui/" + str(os.getuid())
        subprocess.run(["launchctl", "bootout", domain, str(path)], capture_output=True)
        host.run(["launchctl", "bootstrap", domain, path])
    else:
        units = Path.home() / ".config/systemd/user"
        units.mkdir(parents=True, exist_ok=True)
        def quote(value):
            return '"' + str(value).replace('%', '%%').replace('\\', '\\\\').replace('"', '\\"') + '"'
        unit = ("[Unit]\nDescription=Deneb development environment from origin/main\nAfter=network-online.target\n\n"
                "[Service]\nType=oneshot\nExecStart=" + quote(python) + " " + quote(destination) + " --apply --verify\n"
                "Environment=" + quote("DENEB_DEV_HOME=" + str(host.home())) + "\n"
                "Environment=" + quote("PATH=" + str(host.home() / "current/bin") + ":" + str(Path.home() / ".local/bin") + ":/usr/local/bin:/usr/bin:/bin") + "\n")
        (units / "deneb-devenv-sync.service").write_text(unit)
        (units / "deneb-devenv-sync.timer").write_text(
            "[Unit]\nDescription=Synchronize Deneb development tools daily\n\n[Timer]\n"
            "OnCalendar=*-*-* 05:20:00\nRandomizedDelaySec=600\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n")
        env = {**os.environ, "XDG_RUNTIME_DIR": os.environ.get("XDG_RUNTIME_DIR", "/run/user/" + str(os.getuid()))}
        host.run(["systemctl", "--user", "daemon-reload"], env=env)
        host.run(["systemctl", "--user", "enable", "--now", "deneb-devenv-sync.timer"], env=env)
    return {"status": "installed", "bootstrap": str(destination),
            "scope": "local Mac only" if platform.system() == "Darwin" else "configured Linux peers"}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    action = parser.add_mutually_exclusive_group()
    action.add_argument("--apply", action="store_true")
    action.add_argument("--check", action="store_true")
    action.add_argument("--install-timer", action="store_true")
    parser.add_argument("--verify", action="store_true")
    parser.add_argument("--local", action="store_true")
    parser.add_argument("--hosts", nargs="+")
    parser.add_argument("--optional", nargs="*")
    args = parser.parse_args(argv)
    try:
        if args.local and args.hosts:
            raise host.SyncError("choose --local or --hosts, not both")
        if args.verify and not args.apply:
            raise host.SyncError("--verify requires --apply")
        if args.install_timer and (args.local or args.hosts or args.optional is not None):
            raise host.SyncError("timer installation is local; invoke it on the intended coordinator or Mac")
        if args.install_timer:
            result, code = install_timer(), 0
        else:
            result, code = synchronize(args.hosts, args.local, args.optional, args.apply, args.verify)
    except (host.SyncError, OSError, ValueError, subprocess.SubprocessError) as exc:
        result, code = {"status": "failed", "error": str(exc)}, 1
    print(json.dumps({"schema_version": 1, **result}, ensure_ascii=False, indent=2))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
