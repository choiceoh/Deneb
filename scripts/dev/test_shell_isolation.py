"""Isolation contract for the shared shell-script test harness.

This file exists because the harness quietly stopped isolating. `isolated_env`
prepended the fake bin but kept the host PATH, so every script under test could
still resolve whatever the operator had installed in user space. deploy.sh's
optional `command -v codegraph` gate found one: each of the eight deploy tests
that reach the index-refresh block spawned the real binary (~0.85s and a
telemetry HTTP call) against the fixture's throwaway checkout, which overran the
suite's 10s timeout whenever the box was busy — and exercised nothing at all in
CI, where that binary is not installed. Optional-tool branches must be decided by
the fixture, never by the machine running the suite.
"""

from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_shell_support import SYSTEM_PATH, isolated_env, write_executable


class ShellIsolationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.home.mkdir()

    def resolves(self, command: str, env: dict[str, str]) -> str:
        """Report whether a script running under `env` can resolve one command."""
        script = write_executable(
            self.root / "probe.sh",
            f"""
            #!/usr/bin/env bash
            if command -v {command} >/dev/null 2>&1; then echo VISIBLE; else echo HIDDEN; fi
            """,
        )
        proc = subprocess.run(
            [str(script)],
            env=env,
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
        return proc.stdout.strip()

    def test_host_path_is_not_inherited_into_the_isolated_environment(self) -> None:
        with mock.patch.dict(os.environ, {"PATH": f"/opt/operator-toolchain{os.pathsep}/usr/bin"}):
            env = isolated_env(self.home)
        self.assertEqual(env["PATH"], SYSTEM_PATH)

    def test_fake_bin_shadows_the_system_toolchain(self) -> None:
        fake_bin = self.root / "bin"
        fake_bin.mkdir()
        env = isolated_env(self.home, fake_bin)
        self.assertEqual(env["PATH"], f"{fake_bin}{os.pathsep}{SYSTEM_PATH}")

    def test_a_user_space_install_stays_invisible_to_the_script_under_test(self) -> None:
        host_bin = self.root / "host-bin"
        write_executable(host_bin / "operator-tool", "#!/usr/bin/env bash\nexit 0\n")
        with mock.patch.dict(
            os.environ, {"PATH": f"{host_bin}{os.pathsep}{os.environ.get('PATH', '')}"}
        ):
            env = isolated_env(self.home)
        self.assertEqual(self.resolves("operator-tool", env), "HIDDEN")

    def test_the_standard_unix_toolchain_stays_reachable(self) -> None:
        env = isolated_env(self.home)
        for command in ("bash", "sha256sum", "awk", "cut", "head"):
            with self.subTest(command=command):
                self.assertEqual(self.resolves(command, env), "VISIBLE")


if __name__ == "__main__":
    unittest.main()
