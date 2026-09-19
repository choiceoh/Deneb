#!/usr/bin/env python3
"""Provision one isolated Deneb toolchain without changing host or serving tools."""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
INPUTS = ("scripts/devenv/manifest.json", "scripts/devenv/package.json",
          "scripts/devenv/package-lock.json", "requirements.lock", "requirements-dev.lock",
          "gateway-go/go.mod", "andromeda/package.json")


class SyncError(Exception):
    pass


def home():
    return Path(os.environ.get("DENEB_DEV_HOME", str(Path.home() / ".local/share/deneb-dev"))).expanduser().resolve()


def guard_path(path):
    resolved = path.resolve()
    production = (Path.home() / "deneb").resolve()
    if resolved in (Path("/"), Path.home().resolve()) or resolved.is_relative_to(production):
        raise SyncError("refusing a production or home-directory target: " + str(path))


def platform_key():
    machine = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64"}.get(platform.machine())
    if not machine:
        raise SyncError("unsupported architecture: " + platform.machine())
    return platform.system().lower() + "-" + machine


def configuration(root=ROOT):
    manifest = json.loads((root / INPUTS[0]).read_text())
    if manifest.get("schema_version") != 1:
        raise SyncError("unsupported development manifest schema")
    key = platform_key()
    if key not in manifest["platforms"]:
        raise SyncError("no checked release assets for " + key)
    package = json.loads((root / "andromeda/package.json").read_text())
    if package["packageManager"] != "pnpm@" + manifest["pnpm"]:
        raise SyncError("pnpm manifest drift; update scripts/devenv and its npm lock together")
    tools = json.loads((root / "scripts/devenv/package.json").read_text())["dependencies"]
    lock = json.loads((root / "scripts/devenv/package-lock.json").read_text())["packages"]
    for package, name in (("pnpm", "pnpm"), ("@colbymchenry/codegraph", "codegraph")):
        if tools[package] != manifest[name] or lock["node_modules/" + package]["version"] != manifest[name]:
            raise SyncError("npm tool manifest/lock drift: " + name)
    minimum_go = re.search(r"^go\s+(\S+)", (root / "gateway-go/go.mod").read_text(), re.M).group(1)
    def version(value):
        return tuple(int(part) for part in value.split("."))
    if version(manifest["go"]) < version(minimum_go):
        raise SyncError("pinned Go is older than gateway-go/go.mod")
    fingerprint = hashlib.sha256(key.encode())
    for name in INPUTS:
        fingerprint.update(name.encode() + b"\0" + (root / name).read_bytes())
    return manifest, key, fingerprint.hexdigest()


def environment(generation):
    env = os.environ.copy()
    env["PATH"] = str(generation / "bin") + os.pathsep + env.get("PATH", os.defpath)
    env["GOTOOLCHAIN"] = "local"
    env.pop("GOROOT", None)
    env.update({"GIT_TERMINAL_PROMPT": "0", "CI": "1", "UV_NO_PROGRESS": "1",
                "UV_PYTHON_INSTALL_DIR": str(generation.parents[1] / "pythons")})
    return env


def run(command, *, env=None, cwd=None, timeout=600):
    result = subprocess.run([str(arg) for arg in command], env=env, cwd=cwd,
                            capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        # Never echo inherited environment variables or credentials embedded in a URL.
        detail = re.sub(r"(https?://)[^/@\s]+:[^/@\s]+@", r"\1[redacted]@",
                        (result.stderr or result.stdout)[-3000:])
        raise SyncError(Path(str(command[0])).name + " failed: " + detail.strip())
    return result.stdout.strip()


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def download(asset, cache):
    expected = asset.get("digest", "")
    if not re.fullmatch(r"sha256:[a-f0-9]{64}", expected):
        raise SyncError("release is missing a pinned SHA-256")
    expected = expected.removeprefix("sha256:")
    if not asset["url"].startswith("https://"):
        raise SyncError("release URLs must use HTTPS")
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / expected
    if target.is_file() and digest(target) == expected:
        return target
    with tempfile.NamedTemporaryFile(dir=cache, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            request = urllib.request.Request(asset["url"], headers={"User-Agent": "Deneb-devenv/1"})
            with urllib.request.urlopen(request, timeout=60) as response:
                shutil.copyfileobj(response, stream)
            stream.flush()
            if digest(temporary) != expected:
                raise SyncError("release checksum mismatch; archive was not extracted")
            temporary.replace(target)
        finally:
            temporary.unlink(missing_ok=True)
    return target


def unpack(archive, target):
    target.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "r:*") as source:
        source.extractall(target, filter="data")


def link(target, destination):
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.symlink_to(target)


def expected_packages(root):
    packages = {}
    for name in ("requirements.lock", "requirements-dev.lock"):
        packages.update(re.findall(r"^([\w.-]+)==([^\s;]+)", (root / name).read_text(), re.M))
    return packages


def report(generation, manifest, root=ROOT):
    env = environment(generation)
    versions, failures = {}, []
    commands = {"uv": ["uv", "--version"], "go": ["go", "version"],
                "node": ["node", "--version"], "golangci-lint": ["golangci-lint", "--version"],
                "pnpm": ["pnpm", "--version"], "codegraph": ["codegraph", "--version"]}
    for name, command in commands.items():
        binary = generation / "bin" / command[0]
        if not binary.is_file():
            versions[name] = None
        else:
            try:
                output = run([binary, *command[1:]], env=env, timeout=30)
                match = re.search(r"\d+\.\d+\.\d+", output)
                versions[name] = match.group() if match else output
            except (SyncError, OSError, subprocess.TimeoutExpired):
                versions[name] = None
        if versions[name] != manifest[name]:
            failures.append(name)
    expected = expected_packages(root)
    probe = """import importlib.metadata as m,json,platform,sys
out={}
for name in sys.argv[1:]:
 try: out[name]=m.version(name)
 except m.PackageNotFoundError: out[name]=None
print(json.dumps({'python':platform.python_version(),'packages':out}))
"""
    try:
        metadata = json.loads(run([generation / "venv/bin/python", "-B", "-c", probe, *expected],
                                  env=env, timeout=30))
    except (SyncError, OSError, ValueError, subprocess.TimeoutExpired):
        metadata = {"python": None, "packages": {}}
    versions["python"] = metadata["python"]
    if metadata["python"] != manifest["python"]:
        failures.append("python")
    failures.extend(name for name, wanted in expected.items() if metadata["packages"].get(name) != wanted)
    return {"versions": versions, "packages": metadata["packages"], "failures": failures}


def verify(generation):
    """CPU-only toolchain smoke; no gateway, GPU, model or live application calls."""
    env = environment(generation)
    with tempfile.TemporaryDirectory(prefix="deneb-toolchain-") as directory:
        path = Path(directory) / "main.go"
        path.write_text('package main\nimport "fmt"\nfunc main() { fmt.Print("deneb-toolchain-ok") }\n')
        if run([generation / "bin/go", "run", path], env=env, cwd=directory) != "deneb-toolchain-ok":
            raise SyncError("Go toolchain smoke returned an unexpected result")
    run([generation / "bin/node", "-e", "require('node:assert').strictEqual(2+2,4)"], env=env)
    return "cpu_toolchain_smoke_passed"


def checkout(repo, url):
    """Preserve an existing feature branch, modified checkout, or production tree."""
    guard_path(repo)
    env = os.environ.copy()
    env["GIT_TERMINAL_PROMPT"] = "0"
    if not (repo / ".git").exists():
        if repo.exists() and any(repo.iterdir()):
            raise SyncError("checkout path is not an empty directory or a Git checkout")
        repo.parent.mkdir(parents=True, exist_ok=True)
        run(["git", "clone", "--depth", "1", "--branch", "main", "--single-branch", url, repo], env=env)
        return {"status": "cloned", "path": str(repo)}
    actual = run(["git", "-C", repo, "remote", "get-url", "origin"], env=env)
    def normalize(value):
        return value.removesuffix(".git").replace("git@github.com:", "https://github.com/").casefold()
    if normalize(actual) != normalize(url):
        raise SyncError("checkout origin is not the configured Deneb repository")
    branch = run(["git", "-C", repo, "branch", "--show-current"], env=env)
    dirty = run(["git", "-C", repo, "status", "--porcelain", "--untracked-files=no"], env=env)
    if branch != "main" or dirty:
        return {"status": "held", "path": str(repo), "branch": branch,
                "reason": "feature branch or tracked changes; checkout left untouched"}
    run(["git", "-C", repo, "fetch", "--quiet", "origin", "main"], env=env)
    run(["git", "-C", repo, "merge", "--ff-only", "origin/main"], env=env)
    return {"status": "updated", "path": str(repo), "head": run(["git", "-C", repo, "rev-parse", "HEAD"])}


@contextmanager
def locked(root):
    root.mkdir(parents=True, exist_ok=True)
    with (root / ".sync.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise SyncError("another Deneb environment sync is active on this host") from exc
        yield


def install(root, manifest, key, generation, bundle=ROOT):
    # Build at the final path: venv scripts contain absolute shebangs. Only the
    # current symlink moves after validation; old generations stay available.
    generation.mkdir(parents=True, exist_ok=False)
    (generation / "bin").mkdir()
    for name, asset in manifest["platforms"][key].items():
        unpack(download(asset, root / "cache"), generation / "sdk")
        directory = generation / "sdk" / asset["directory"]
        binaries = {"uv": ("uv", "uvx"), "go": ("bin/go", "bin/gofmt"),
                    "node": ("bin/node", "bin/npm", "bin/npx"),
                    "golangci-lint": ("golangci-lint",)}[name]
        for binary in binaries:
            target = directory / binary
            if not target.is_file():
                raise SyncError("verified archive is missing " + binary)
            link(target, generation / "bin" / Path(binary).name)
    env = environment(generation)
    run([generation / "bin/uv", "venv", "--quiet", "--python", manifest["python"],
         "--python-preference", "only-managed", generation / "venv"], env=env)
    run([generation / "bin/uv", "pip", "sync", "--quiet", "--python", generation / "venv/bin/python",
         "--require-hashes", bundle / "requirements.lock", bundle / "requirements-dev.lock"], env=env)
    for binary in ("python", "python3", "ruff"):
        link(generation / "venv/bin" / binary, generation / "bin" / binary)
    npm = generation / "npm"
    npm.mkdir()
    for name in ("package.json", "package-lock.json"):
        shutil.copyfile(bundle / "scripts/devenv" / name, npm / name)
    # npm rejects loading one file as both user and global configuration.
    # Separate empty files also avoid inheriting personal registry credentials.
    userconfig, globalconfig = npm / "empty-user.npmrc", npm / "empty-global.npmrc"
    userconfig.touch()
    globalconfig.touch()
    run([generation / "bin/npm", "ci", "--prefix", npm, "--ignore-scripts", "--omit=dev",
         "--no-audit", "--no-fund", "--loglevel=error", "--userconfig", userconfig,
         "--globalconfig", globalconfig], env=env)
    for binary in ("pnpm", "pnpx", "codegraph"):
        link(npm / "node_modules/.bin" / binary, generation / "bin" / binary)


def synchronize(apply=False, smoke=False, repo=None, root=None, bundle=ROOT):
    root = (root or home()).expanduser().resolve()
    guard_path(root)
    for name in ("generations", "cache", "pythons", ".sync.lock"):
        if (root / name).is_symlink():
            raise SyncError("managed profile component must not be a symlink: " + name)
    manifest, key, fingerprint = configuration(bundle)
    generation = root / "generations" / fingerprint
    result = {"platform": key, "fingerprint": fingerprint, "home": str(root),
              "scope": "Deneb development tools only; host PATH, serving services and GPU packages are unchanged"}
    current = root / "current"
    if current.is_symlink() and not current.resolve().is_relative_to((root / "generations").resolve()):
        raise SyncError("current points outside managed generations; it was left untouched")
    if current.exists() and not current.is_symlink():
        raise SyncError("current is not a managed symlink; it was left untouched")
    active = current.resolve() if current.is_symlink() else None
    receipt = active / "receipt.json" if active else None
    matches = receipt is not None and receipt.is_file() and \
        json.loads(receipt.read_text()).get("fingerprint") == fingerprint
    if not apply:
        if not matches:
            return {**result, "status": "needs_sync", "recovery": ["./dev", "run", "env.setup"]}, 2
        checked = report(active, manifest, bundle)
        return {**result, **checked, "status": "failed" if checked["failures"] else "ok"}, 1 if checked["failures"] else 0
    with locked(root):
        if matches and not report(active, manifest, bundle)["failures"]:
            generation = active
        elif generation.exists():
            generation = generation.with_name(fingerprint + "-" + str(time.time_ns()))
        ready = generation / "receipt.json"
        if not ready.is_file():
            install(root, manifest, key, generation, bundle)
        checked = report(generation, manifest, bundle)
        if checked["failures"]:
            raise SyncError("installed toolchain failed verification: " + ", ".join(checked["failures"]))
        result["verification"] = verify(generation) if smoke else "versions_and_locked_packages"
        ready.write_text(json.dumps({"fingerprint": fingerprint, "platform": key, **checked}, indent=2) + "\n")
        if repo is not None:
            result["checkout"] = checkout(repo, manifest["repository"])
        temporary = root / (".current-" + str(os.getpid()))
        temporary.unlink(missing_ok=True)
        temporary.symlink_to(generation)
        temporary.replace(current)
        return {**result, **checked, "status": "ok"}, 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--verify", action="store_true")
    parser.add_argument("--repo", type=Path)
    args = parser.parse_args(argv)
    try:
        result, code = synchronize(args.apply, args.verify, args.repo)
    except (SyncError, OSError, ValueError, KeyError, subprocess.SubprocessError, tarfile.TarError) as exc:
        result, code = {"status": "failed", "error": str(exc)}, 1
    print(json.dumps({"schema_version": 1, **result}, ensure_ascii=False, indent=2))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
