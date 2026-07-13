"""Concurrency regression test for deploy-watch latest-head handoff."""

from __future__ import annotations

import os
import subprocess
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory


REPO_ROOT = Path(__file__).resolve().parents[2]
WATCH = REPO_ROOT / "scripts/deploy/deploy-watch.sh"


class DeployWatchShellTest(unittest.TestCase):
    def test_new_head_waits_for_old_watch_and_is_actually_watched(self):
        with TemporaryDirectory() as td:
            root = Path(td)
            state = root / "state"
            prod = root / "prod"
            fake_bin = root / "bin"
            state.mkdir()
            prod.mkdir()
            fake_bin.mkdir()
            log = root / "watch.log"
            lock = root / "watch.lock"
            ready = state / "deploy-watch.ready"
            state_file = state / "auto-deploy.deployed-head"
            state_file.write_text("head-a\n", encoding="utf-8")

            for name, body in {
                "curl": "#!/bin/sh\nexit 0\n",
                "journalctl": "#!/bin/sh\nexit 0\n",
                "flock": """#!/usr/bin/env python3
import fcntl
import sys
import time

timeout = float(sys.argv[2])
fd = int(sys.argv[3])
deadline = time.monotonic() + timeout
while True:
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        raise SystemExit(0)
    except BlockingIOError:
        if time.monotonic() >= deadline:
            raise SystemExit(1)
        time.sleep(0.05)
""",
            }.items():
                path = fake_bin / name
                path.write_text(body, encoding="utf-8")
                path.chmod(0o755)

            env = os.environ | {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "DENEB_STATE_DIR": str(state),
                "DENEB_PROD_DIR": str(prod),
                "DENEB_DEPLOY_WATCH_LOG_FILE": str(log),
                "DENEB_DEPLOY_WATCH_LOCK_FILE": str(lock),
                "DENEB_DEPLOY_WATCH_READY_FILE": str(ready),
                "DENEB_DEPLOY_WATCH_SEC": "2",
                "DENEB_DEPLOY_WATCH_POLL_SEC": "1",
                "DENEB_DEPLOY_WATCH_HANDOFF_SEC": "5",
            }
            old = subprocess.Popen(["bash", str(WATCH), "head-a"], env=env)
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                if log.exists() and "watch started for head head-a" in log.read_text(encoding="utf-8"):
                    break
                time.sleep(0.05)
            else:
                self.fail("old watcher did not acquire the lock")
            state_file.write_text("head-b\n", encoding="utf-8")
            new = subprocess.Popen(["bash", str(WATCH), "head-b"], env=env)

            self.assertEqual(old.wait(timeout=8), 0)
            self.assertEqual(new.wait(timeout=8), 0)
            text = log.read_text(encoding="utf-8")
            self.assertIn("newer deploy detected; ending watch for head-a", text)
            self.assertIn("watch started for head head-b", text)
            self.assertIn("watch window clear for head-b", text)
            self.assertNotIn("unwatched", text)
            self.assertNotIn("handoff timed out", text)
            self.assertEqual(ready.read_text(encoding="utf-8").split()[0], "head-b")


if __name__ == "__main__":
    unittest.main()
