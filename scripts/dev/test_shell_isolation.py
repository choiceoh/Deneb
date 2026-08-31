"""Isolation contract for the shared shell-script test harness, and its audit.

This file exists because the harness quietly stopped isolating. `isolated_env`
prepended the fake bin but kept the host PATH, so every script under test could
still resolve whatever the operator had installed in user space. deploy.sh's
optional `command -v codegraph` gate found one: each of the eight deploy tests
that reach the index-refresh block spawned the real binary (~0.85s and a
telemetry HTTP call) against the fixture's throwaway checkout, which overran the
suite's 10s timeout whenever the box was busy — and exercised nothing at all in
CI, where that binary is not installed. Optional-tool branches must be decided by
the fixture, never by the machine running the suite.

`ShellIsolationTests` pins that contract: `isolated_env` hands out a fixed
`SYSTEM_PATH`, the fake bin shadows it, and a user-space install stays invisible.

The rest of the file measures what the contract does not settle. Pinning the
PATH is a claim about how the environment is *built*; it is not evidence about
what the lanes actually *reach*, and a lane passing has never been evidence of
either -- reaching a real tool usually produces roughly the answer a fake would
have given, which is why the codegraph reach survived so long in a green lane.
Two gaps outlive the pin:

  * `SYSTEM_PATH` admits `/usr/local/bin`, which is npm's default global prefix.
    The reach above came from an npm-global `codegraph`; on a machine whose npm
    prefix is `/usr/local` rather than `~/.npm-global`, the identical bug lands
    inside the pinned PATH and survives untouched.
  * A fixture may pass its own `PATH=` in `values`, which overrides the pin
    outright.

So `HostBinaryReachTests` plants a logging shim for every executable a fixture
could still resolve outside `/usr/bin`, `/bin`, `/usr/sbin`, and `/sbin`, splices
the shim directory in behind the fixture's own fakes but ahead of the directories
it stands in for, runs each lane once, and requires the shim log to come back
empty. An empty log is positive evidence of zero host reach.

Out of audit range, stated rather than implied: a script that prepends to `PATH`
at runtime lands ahead of the canary, and an absolute-path invocation never
consults `PATH` at all. Neither is reachable by shimming.
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile
import unittest
from contextlib import contextmanager
from pathlib import Path
from unittest import mock

from test_shell_support import (
    CANARY_BIN_ENV,
    CANARY_DIRS_ENV,
    CANARY_LOG_ENV,
    REPO_ROOT,
    SYSTEM_PATH,
    _with_canary,
    isolated_env,
    write_executable,
)

DEV_DIR = REPO_ROOT / "scripts" / "dev"

# The tools a fixture is entitled to resolve without stubbing. Whatever the
# distribution ships lands here; whatever a toolchain manager installs
# (/usr/local/bin, ~/.local/bin, ~/.cargo/bin, a node prefix) does not -- which
# is the line the fixtures have to hold. `SYSTEM_PATH` is deliberately wider
# than this, so the audit covers the difference.
SYSTEM_PREFIXES = ("/usr/bin", "/bin", "/usr/sbin", "/sbin")

# Interpreters the fixtures themselves run on -- their `#!/usr/bin/env bash`
# shebangs, and the `python3` a CI runner keeps in a version-managed toolcache
# rather than /usr/bin. Shadowing one of these would break every fixture the
# audit is trying to observe, so the audit would be reporting its own damage.
# Scripts that must not reach the host's interpreter say so the way `deploy.sh`
# does -- an env-var override, or a fake in the fixture's own bin.
NEVER_SHADOW = frozenset({"bash", "sh", "env", "python", "python3"})

# Per-lane ceiling for an audited run: generous against the lanes' own
# `run_script` timeouts, so a slow machine reports real reaches rather than a
# truncated -- and therefore unsound -- audit.
LANE_TIMEOUT_SEC = 300.0

# unittest's own end-of-run line. Its absence means the lane never got through
# its cases, so an empty canary log from that lane proves nothing.
LANE_COMPLETED = re.compile(r"^Ran \d+ tests? in ", re.MULTILINE)

CANARY_SHIM = """
#!/usr/bin/env bash
# Isolation canary: stands in for a real host binary and records that a lane
# reached this far. Exits 0 without output so the script under test keeps going
# and one run surfaces every reach rather than only the first.
printf '%s %s\\n' "${0##*/}" "$*" >> "$DENEB_SHELL_CANARY_LOG"
exit 0
"""


def _real(path: str) -> str:
    try:
        return os.path.realpath(path)
    except OSError:  # pragma: no cover - realpath on a hostile mount
        return path


def _executables(directory: str) -> set[str]:
    """Names of the runnable files in one directory, or nothing if it is unreadable."""
    names: set[str] = set()
    try:
        entries = list(os.scandir(directory))
    except OSError:
        return names
    for entry in entries:
        try:
            if entry.is_file() and os.access(entry.path, os.X_OK):
                names.add(entry.name)
        except OSError:
            continue
    return names


def system_dirs() -> set[str]:
    """Resolve the system prefixes, collapsing the usrmerge /bin -> /usr/bin link."""
    return {_real(prefix) for prefix in SYSTEM_PREFIXES}


def audited_dirs() -> list[str]:
    """Directories a fixture can put in front of a script that are not Unix prefixes.

    Two sources, because there are two ways one can still be reached: the part of
    `SYSTEM_PATH` that is not a Unix prefix (`/usr/local/bin`, handed to every
    fixture by default), and the host's own `PATH` -- no longer inherited, but a
    fixture that passes its own `PATH=` can put any of it back.
    """
    resolved = system_dirs()
    ordered: list[str] = []
    for entry in (*SYSTEM_PATH.split(os.pathsep), *os.environ.get("PATH", "").split(os.pathsep)):
        if not entry or entry in ordered or _real(entry) in resolved:
            continue
        ordered.append(entry)
    return ordered


def host_tool_names(dirs: list[str]) -> dict[str, str]:
    """Map each shadowable command name to the audited directory it came from.

    A name the Unix prefixes also carry is skipped: under any PATH a fixture
    hands out, the canary sits ahead of `/usr/bin` too, so shimming such a name
    would intercept the standard tool the fixture is entitled to reach and the
    audit would manufacture its own finding.
    """
    unix: set[str] = set()
    for prefix in SYSTEM_PREFIXES:
        unix |= _executables(prefix)
    found: dict[str, str] = {}
    for directory in dirs:
        for name in sorted(_executables(directory)):
            if name in found or name in unix or name in NEVER_SHADOW:
                continue
            found[name] = directory
    return found


def plant_canaries(bin_dir: Path, names: list[str]) -> Path:
    """Fill `bin_dir` with one shim plus a symlink per shadowed name."""
    bin_dir.mkdir(parents=True, exist_ok=True)
    shim = write_executable(bin_dir / "__canary__", CANARY_SHIM)
    for name in names:
        (bin_dir / name).symlink_to(shim)
    return bin_dir


def shell_test_modules() -> list[str]:
    """Every dev test module that drives a checked-in script through `isolated_env`."""
    skip = {"test_shell_support", Path(__file__).stem}
    return [
        path.stem
        for path in sorted(DEV_DIR.glob("test_*.py"))
        if path.stem not in skip and "isolated_env" in path.read_text(encoding="utf-8")
    ]


@contextmanager
def armed(bin_dir: Path, dirs: list[str], log: Path):
    """Arm the canary for `isolated_env` calls made inside this process."""
    previous = {key: os.environ.get(key) for key in (CANARY_BIN_ENV, CANARY_DIRS_ENV, CANARY_LOG_ENV)}
    os.environ[CANARY_BIN_ENV] = str(bin_dir)
    os.environ[CANARY_DIRS_ENV] = os.pathsep.join(dirs)
    os.environ[CANARY_LOG_ENV] = str(log)
    try:
        yield
    finally:
        for key, value in previous.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


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


class CanaryPlacementTests(unittest.TestCase):
    """Where the canary sits in `PATH` is the whole audit, so pin it."""

    def setUp(self) -> None:
        self.stack = armed(Path("/canary"), ["/usr/local/bin"], Path("/dev/null"))
        self.stack.__enter__()
        self.addCleanup(self.stack.__exit__, None, None, None)

    def test_canary_stays_out_until_an_audit_arms_it(self) -> None:
        os.environ.pop(CANARY_BIN_ENV)
        untouched = f"/fake{os.pathsep}{SYSTEM_PATH}"

        self.assertEqual(_with_canary(untouched), untouched)

    def test_canary_shadows_audited_dirs_without_shadowing_fixture_fakes(self) -> None:
        spliced = _with_canary("/fake:/usr/local/bin:/usr/bin")

        self.assertEqual(spliced, "/fake:/canary:/usr/local/bin:/usr/bin")

    def test_path_pinned_to_unix_prefixes_is_left_alone(self) -> None:
        # A fixture that pins PATH to its fake bin plus the Unix prefixes is
        # already airtight. Splicing a canary in behind the fake bin anyway would
        # put the audited directories back within reach and report a leak the
        # fixture had closed -- the audit inventing its own finding.
        untouched = "/fake:/usr/bin:/bin"

        self.assertEqual(_with_canary(untouched), untouched)

    def test_canary_stays_behind_every_directory_the_fixture_prepended(self) -> None:
        os.environ[CANARY_DIRS_ENV] = os.pathsep.join(["/usr/local/bin", "/opt/tools"])

        spliced = _with_canary("/fake:/fixture:/opt/tools:/usr/local/bin:/usr/bin")

        self.assertEqual(spliced, "/fake:/fixture:/canary:/opt/tools:/usr/local/bin:/usr/bin")


class HostBinaryReachTests(unittest.TestCase):
    """Run each shell lane once under the canary and require an empty log."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.tmp = tempfile.TemporaryDirectory(prefix="shell-isolation-")
        root = Path(cls.tmp.name)
        cls.log = root / "reach.log"
        cls.dirs = audited_dirs()
        cls.tools = host_tool_names(cls.dirs)
        cls.bin = plant_canaries(root / "bin", sorted(cls.tools))
        cls.env = {
            **os.environ,
            CANARY_BIN_ENV: str(cls.bin),
            CANARY_DIRS_ENV: os.pathsep.join(cls.dirs),
            CANARY_LOG_ENV: str(cls.log),
        }

    @classmethod
    def tearDownClass(cls) -> None:
        cls.tmp.cleanup()

    def reaches(self) -> list[str]:
        return sorted(set(self.log.read_text(encoding="utf-8").split("\n")) - {""})

    def test_the_audit_intercepts_a_reach_it_is_meant_to_catch(self) -> None:
        # Without this, "no lane reaches a host binary" and "the canary never
        # made it onto PATH" are the same empty log -- and the second is how a
        # guard rots into a no-op that reports success forever. Staged through a
        # fixture-supplied `PATH=`, which is how a lane can still reopen the pin,
        # rather than through whatever this machine happens to have installed.
        stage = Path(self.tmp.name) / "self-check"
        host_dir, witness = stage / "host", stage / "ran-for-real"
        write_executable(host_dir / "deneb-canary-probe", f'#!/usr/bin/env bash\ntouch "{witness}"\n')
        canary = plant_canaries(stage / "canary", ["deneb-canary-probe"])
        log = stage / "reach.log"
        script = write_executable(
            stage / "probe.sh",
            "#!/usr/bin/env bash\ndeneb-canary-probe --canary-probe\n",
        )

        with armed(canary, [str(host_dir)], log):
            env = isolated_env(stage, PATH=os.pathsep.join([str(host_dir), SYSTEM_PATH]))
        subprocess.run([str(script)], env=env, capture_output=True, text=True, timeout=30, check=False)

        self.assertEqual(log.read_text(encoding="utf-8").splitlines(), ["deneb-canary-probe --canary-probe"])
        self.assertFalse(witness.exists(), "the canary logged the reach but the real binary ran anyway")

    def test_shell_lanes_reach_no_host_binary(self) -> None:
        modules = shell_test_modules()
        self.assertTrue(modules, "no shell-driving test modules were discovered")

        offenders: dict[str, list[str]] = {}
        truncated: dict[str, str] = {}
        for module in modules:
            self.log.write_text("", encoding="utf-8")
            proc = subprocess.run(
                [sys.executable, "-m", "unittest", module],
                cwd=str(DEV_DIR),
                env=self.env,
                capture_output=True,
                text=True,
                timeout=LANE_TIMEOUT_SEC,
                check=False,
            )
            found = self.reaches()
            if found:
                offenders[module] = found
            if not LANE_COMPLETED.search(proc.stderr):
                truncated[module] = (proc.stderr or proc.stdout)[-2000:]

        self.assertEqual(
            truncated,
            {},
            "a lane never finished its cases, so its empty canary log is not evidence of isolation",
        )
        self.assertEqual(
            offenders,
            {},
            "these lanes executed a real host binary where a fixture fake was meant to answer. "
            "Add a fake for each name to the module's fake bin: an un-faked toolchain does real "
            "work whose duration tracks machine load, which is how a lane starts timing out under "
            "load.\n"
            + "\n".join(f"  {module}: {', '.join(found)}" for module, found in sorted(offenders.items())),
        )


if __name__ == "__main__":
    unittest.main()
