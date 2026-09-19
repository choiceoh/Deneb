"""Failure, isolation and fleet contracts for development environment synchronization."""
from contextlib import redirect_stdout
import hashlib
import importlib.machinery
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import shlex
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock

import devenv_host as host
import devenv_sync as sync


class HostTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name).resolve()
        self.profile = self.directory / "profile"
        self.healthy = {"versions": {"go": "1.25.14"}, "packages": {}, "failures": []}

    def fake_install(self, root, manifest, key, generation, bundle):
        generation.mkdir(parents=True)
        (generation / "retained-tool").write_text("installed")

    def apply(self):
        with mock.patch.object(host, "install", side_effect=self.fake_install), \
             mock.patch.object(host, "report", return_value=self.healthy):
            return host.synchronize(apply=True, root=self.profile)

    def test_committed_platforms_have_checked_assets_and_matching_locks(self):
        for key in ("linux-arm64", "linux-amd64", "darwin-arm64"):
            with self.subTest(platform=key), mock.patch.object(host, "platform_key", return_value=key):
                manifest, _, fingerprint = host.configuration()
                self.assertEqual(len(fingerprint), 64)
                self.assertEqual(set(manifest["platforms"][key]), {"uv", "go", "node", "golangci-lint"})
                for asset in manifest["platforms"][key].values():
                    self.assertRegex(asset["digest"], r"^sha256:[a-f0-9]{64}$")
                    self.assertTrue(asset["url"].startswith("https://"))

    def test_manifest_drift_is_blocked_before_any_install(self):
        bundle = self.directory / "bundle"
        for name in host.INPUTS:
            path = bundle / name
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(host.ROOT / name, path)
        path = bundle / "scripts/devenv/manifest.json"
        original = path.read_text()
        for field, value in (("pnpm", "0.0.1"), ("codegraph", "0.0.1"), ("go", "1.0.0"), ("schema_version", 2)):
            with self.subTest(field=field):
                manifest = json.loads(original)
                manifest[field] = value
                path.write_text(json.dumps(manifest))
                with self.assertRaises(host.SyncError):
                    host.configuration(bundle)

    def test_check_mode_creates_no_profile_or_checkout(self):
        with mock.patch.object(host, "install", side_effect=AssertionError("install")), \
             mock.patch.object(host, "checkout", side_effect=AssertionError("checkout")):
            result, code = host.synchronize(root=self.profile, repo=self.directory / "repo")
        self.assertEqual((result["status"], code), ("needs_sync", 2))
        self.assertFalse(self.profile.exists())

    def test_apply_is_idempotent_and_check_reads_actual_versions(self):
        self.assertEqual(self.apply()[1], 0)
        previous = (self.profile / "current").resolve()
        with mock.patch.object(host, "install", side_effect=AssertionError("duplicate install")), \
             mock.patch.object(host, "report", return_value=self.healthy):
            self.assertEqual(host.synchronize(apply=True, root=self.profile)[1], 0)
            self.assertEqual(host.synchronize(root=self.profile)[1], 0)
        self.assertEqual((self.profile / "current").resolve(), previous)
        with mock.patch.object(host, "report", return_value={**self.healthy, "failures": ["go"]}):
            self.assertEqual(host.synchronize(root=self.profile)[1], 1)

    def test_failed_repair_preserves_existing_generation_even_with_same_fingerprint(self):
        self.apply()
        previous = (self.profile / "current").resolve()
        with mock.patch.object(host, "report", return_value={**self.healthy, "failures": ["go"]}), \
             mock.patch.object(host, "install", side_effect=host.SyncError("network failed")):
            with self.assertRaisesRegex(host.SyncError, "network failed"):
                host.synchronize(apply=True, root=self.profile)
        self.assertEqual((self.profile / "current").resolve(), previous)
        self.assertEqual((previous / "retained-tool").read_text(), "installed")

    def test_smoke_failure_never_activates_new_generation(self):
        with mock.patch.object(host, "install", side_effect=self.fake_install), \
             mock.patch.object(host, "report", return_value=self.healthy), \
             mock.patch.object(host, "verify", side_effect=host.SyncError("compiler failed")):
            with self.assertRaisesRegex(host.SyncError, "compiler failed"):
                host.synchronize(apply=True, smoke=True, root=self.profile)
        self.assertFalse((self.profile / "current").exists())

    def test_foreign_current_directory_or_symlink_is_never_replaced(self):
        self.profile.mkdir()
        current = self.profile / "current"
        current.mkdir()
        with self.assertRaisesRegex(host.SyncError, "not a managed symlink"):
            host.synchronize(apply=True, root=self.profile)
        current.rmdir()
        current.symlink_to(self.directory)
        with self.assertRaisesRegex(host.SyncError, "outside managed generations"):
            host.synchronize(apply=True, root=self.profile)
        self.assertEqual(current.resolve(), self.directory)

    def test_production_home_and_symlink_targets_are_rejected(self):
        production = self.directory / "deneb"
        production.mkdir()
        alias = self.directory / "production-alias"
        alias.symlink_to(production)
        with mock.patch.object(Path, "home", return_value=self.directory):
            for path in (Path("/"), self.directory, production, production / "child", alias / "child"):
                with self.subTest(path=path), self.assertRaises(host.SyncError):
                    host.guard_path(path)
            host.guard_path(self.directory / "deneb-dev")

    def test_managed_subdirectory_cannot_redirect_writes_to_another_tree(self):
        self.profile.mkdir()
        destination = self.directory / "foreign"
        destination.mkdir()
        (self.profile / "generations").symlink_to(destination)
        with self.assertRaisesRegex(host.SyncError, "component must not be a symlink"):
            host.synchronize(apply=True, root=self.profile)
        self.assertEqual(list(destination.iterdir()), [])

    def test_concurrent_install_is_rejected(self):
        with host.locked(self.profile):
            with self.assertRaisesRegex(host.SyncError, "another Deneb"):
                with host.locked(self.profile):
                    self.fail("second installer acquired lock")

    def test_download_requires_digest_and_rejects_mismatch_before_extraction(self):
        cache = self.directory / "cache"
        with mock.patch.object(host.urllib.request, "urlopen", side_effect=AssertionError("network")):
            with self.assertRaisesRegex(host.SyncError, "SHA-256"):
                host.download({"url": "https://example.invalid/archive"}, cache)
        asset = {"url": "https://example.invalid/archive", "digest": "sha256:" + "0" * 64}
        with mock.patch.object(host.urllib.request, "urlopen", return_value=io.BytesIO(b"corrupt")):
            with self.assertRaisesRegex(host.SyncError, "checksum mismatch"):
                host.download(asset, cache)
        self.assertEqual(list(cache.iterdir()), [])

    def test_verified_download_is_reused_without_network(self):
        cache = self.directory / "cache"
        content = b"checked archive"
        asset = {"url": "https://example.invalid/archive", "digest": "sha256:" + hashlib.sha256(content).hexdigest()}
        with mock.patch.object(host.urllib.request, "urlopen", return_value=io.BytesIO(content)):
            path = host.download(asset, cache)
        with mock.patch.object(host.urllib.request, "urlopen", side_effect=AssertionError("network")):
            self.assertEqual(host.download(asset, cache), path)

    def test_npm_uses_distinct_empty_configs_and_disables_lifecycle_scripts(self):
        generation = self.profile / "generations/test"
        manifest = {"platforms": {"fixture": {}}, "python": "3.12.13"}
        with mock.patch.object(host, "run") as run:
            host.install(self.profile, manifest, "fixture", generation)
        command = run.call_args.args[0]
        user = command[command.index("--userconfig") + 1]
        system = command[command.index("--globalconfig") + 1]
        self.assertNotEqual(user, system)
        self.assertEqual((user.read_text(), system.read_text()), ("", ""))
        self.assertIn("--ignore-scripts", command)

    def test_archive_cannot_write_outside_generation(self):
        archive = self.directory / "unsafe.tar"
        with tarfile.open(archive, "w") as stream:
            entry = tarfile.TarInfo("../escaped")
            entry.size = 1
            stream.addfile(entry, io.BytesIO(b"x"))
        with self.assertRaises(tarfile.FilterError):
            host.unpack(archive, self.directory / "sdk")
        self.assertFalse((self.directory / "escaped").exists())

    def test_feature_or_dirty_checkout_does_not_fetch_or_change_branch(self):
        repo = self.directory / "repo"
        (repo / ".git").mkdir(parents=True)
        for branch, dirty in (("feat/in-progress", ""), ("main", " M tracked.py")):
            with self.subTest(branch=branch), mock.patch.object(host, "run", side_effect=[
                "git@github.com:choiceoh/Deneb.git", branch, dirty,
            ]) as run:
                result = host.checkout(repo, "https://github.com/choiceoh/Deneb.git")
                self.assertEqual(result["status"], "held")
                self.assertEqual(run.call_count, 3)

    def test_foreign_repository_and_non_fast_forward_are_not_overwritten(self):
        repo = self.directory / "repo"
        (repo / ".git").mkdir(parents=True)
        with mock.patch.object(host, "run", return_value="https://example.invalid/other.git") as run:
            with self.assertRaisesRegex(host.SyncError, "origin"):
                host.checkout(repo, "https://github.com/choiceoh/Deneb.git")
            self.assertEqual(run.call_count, 1)
        with mock.patch.object(host, "run", side_effect=[
            "https://github.com/choiceoh/Deneb.git", "main", "", "", host.SyncError("diverged"),
        ]) as run:
            with self.assertRaisesRegex(host.SyncError, "diverged"):
                host.checkout(repo, "https://github.com/choiceoh/Deneb.git")
            self.assertIn("--ff-only", run.call_args.args[0])


class FleetTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name).resolve()

    def test_mac_covers_six_hosts_and_linux_deduplicates_itself(self):
        with mock.patch.object(sync.platform, "system", return_value="Darwin"), \
             mock.patch.object(sync.platform, "node", return_value="laptop.local"):
            targets = sync.targets()
            self.assertEqual(len(targets), 6)
            self.assertEqual([row["id"] for row in targets if row["local"]], ["mac"])
        with mock.patch.object(sync.platform, "system", return_value="Linux"), \
             mock.patch.object(sync.platform, "node", return_value="srv4"):
            self.assertEqual(len(sync.targets()), 5)
            self.assertEqual([row["id"] for row in sync.targets() if row["local"]], ["srv4"])
            self.assertEqual(len(sync.targets(["local", "srv4", "srv1"])), 2)

    def test_invalid_host_cannot_inject_ssh_options_or_shell(self):
        for name in ("-oProxyCommand=bad", "server;touch bad", "server/path", "$(bad)", "server\nother"):
            with self.subTest(name=name), self.assertRaises(host.SyncError):
                sync.targets([name])

    def test_payload_contains_only_declared_inputs(self):
        with tarfile.open(fileobj=io.BytesIO(sync.payload())) as archive:
            self.assertEqual(set(archive.getnames()), {*host.INPUTS, "scripts/dev/devenv_host.py"})

    def test_optional_transport_failure_differs_from_reachable_install_failure(self):
        target = {"id": "workstation", "address": "workstation", "local": False, "optional": True}
        identity = ("example-user", "example-pass")
        diagnostic = f"https://{identity[0]}:{identity[1]}@example.invalid/fail".encode()
        for code, output, optional, expected in ((255, b"", True, "skipped"), (255, b"", False, "unreachable"),
                                                 (1, b'{"status":"failed"}', True, "failed"),
                                                 (0, b"not json", True, "failed")):
            with self.subTest(code=code, optional=optional), mock.patch.object(sync.subprocess, "run", return_value=
                    subprocess.CompletedProcess([], code, output, diagnostic)) as run:
                result = sync.invoke({**target, "optional": optional}, b"bundle", True, True, self.directory)
                self.assertEqual(result["status"], expected)
                self.assertNotIn(identity[1], Path(result["log"]).read_text())
                remote = shlex.split(run.call_args.args[0][-1])
                self.assertEqual(remote, ["python3", "-c", sync.BOOTSTRAP, "--apply", "--verify"])

    def test_timeout_is_unknown_even_for_optional_host(self):
        target = {"id": "workstation", "address": "workstation", "local": False, "optional": True}
        with mock.patch.object(sync.subprocess, "run", side_effect=subprocess.TimeoutExpired("ssh", 1)):
            result = sync.invoke(target, b"bundle", True, False, self.directory)
        self.assertEqual(result["status"], "unknown")

    def test_all_skipped_is_incomplete_and_required_failure_fails_fleet(self):
        for rows, expected, code in ((["skipped"], "incomplete", 3), (["ok", "skipped"], "degraded", 0),
                                     (["ok", "failed"], "failed", 1), (["ok", "ok"], "ok", 0)):
            logroot = self.directory / expected
            selected = [{"id": str(i)} for i in range(len(rows))]
            with self.subTest(rows=rows), mock.patch.dict(os.environ, {"DENEB_DEV_LOGS": str(logroot)}), \
                 mock.patch.object(sync, "targets", return_value=selected), \
                 mock.patch.object(sync, "invoke", side_effect=lambda row, *_: {"host": row["id"], "status": rows[int(row["id"])]}):
                result, actual = sync.synchronize()
            self.assertEqual((result["status"], actual), (expected, code))

    def test_scheduled_mac_payload_does_not_create_a_linux_checkout(self):
        target = {"id": "mac", "local": True, "optional": False}
        with mock.patch.object(sync, "ROOT", self.directory), \
             mock.patch.object(sync.platform, "system", return_value="Darwin"), \
             mock.patch.object(sync.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, b'{"status":"ok"}', b"")) as run:
            sync.invoke(target, b"", True, True, self.directory)
        self.assertNotIn("--repo", run.call_args.args[0])

    def test_timer_requires_explicit_separate_action_and_valid_scope(self):
        with mock.patch.object(sync, "install_timer", side_effect=AssertionError("timer")), \
             mock.patch.object(sync, "synchronize", return_value=({"status": "ok"}, 0)), \
             redirect_stdout(io.StringIO()):
            self.assertEqual(sync.main(["--apply", "--local"]), 0)
            self.assertEqual(sync.main(["--install-timer", "--hosts", "srv1"]), 1)
            self.assertEqual(sync.main(["--verify"]), 1)

    def test_namespaced_timer_templates_use_managed_python_and_mac_is_local(self):
        profile = self.directory / "profile"
        python = profile / "current/bin/python"
        python.parent.mkdir(parents=True)
        python.touch()
        for system in ("Darwin", "Linux"):
            with self.subTest(system=system), mock.patch.object(sync.platform, "system", return_value=system), \
                 mock.patch.object(Path, "home", return_value=self.directory), \
                 mock.patch.object(host, "home", return_value=profile), \
                 mock.patch.object(host, "run") as run, mock.patch.object(sync.subprocess, "run"):
                result = sync.install_timer()
                self.assertEqual(result["status"], "installed")
                if system == "Darwin":
                    job = plistlib.loads((self.directory / "Library/LaunchAgents/ai.deneb.devenv-sync.plist").read_bytes())
                    self.assertEqual(job["ProgramArguments"][0], str(python))
                    self.assertEqual(job["ProgramArguments"][-1], "--local")
                else:
                    service = (self.directory / ".config/systemd/user/deneb-devenv-sync.service").read_text()
                    self.assertIn(str(python), service)
                    self.assertIn("--apply --verify", service)
                    self.assertEqual(run.call_args.args[0][-1], "deneb-devenv-sync.timer")

    def test_bootstrap_archives_a_frozen_main_commit_from_private_bare_mirror(self):
        source = host.ROOT / "scripts/devenv/deneb-devenv-sync"
        loader = importlib.machinery.SourceFileLoader("deneb_sync_bootstrap_test", str(source))
        module = importlib.util.module_from_spec(importlib.util.spec_from_loader(loader.name, loader))
        loader.exec_module(module)
        revision = "a" * 40
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w"):
            pass
        replies = [b"", b"", module.REPOSITORY.encode(), b"true", b"", revision.encode(), archive.getvalue()]
        with mock.patch.dict(os.environ, {"DENEB_DEV_HOME": str(self.directory / "profile")}), \
             mock.patch.object(module, "run", side_effect=replies) as run, \
             mock.patch.object(module.subprocess, "call", return_value=0) as execute, \
             mock.patch.object(module.sys, "argv", [str(source), "--apply", "--verify"]), \
             mock.patch.object(module.sys, "stderr", io.StringIO()):
            self.assertEqual(module.main(), 0)
        archive_call = run.call_args.args
        self.assertEqual(archive_call[3:5], ("archive", revision))
        self.assertTrue(all("deneb-dev" not in str(arg) for call in run.call_args_list for arg in call.args))
        self.assertEqual(execute.call_args.args[0][-2:], ["--apply", "--verify"])


if __name__ == "__main__":
    unittest.main()
