"""Harness contract: the shell-driving test lanes reach zero real host binaries.

`test_shell_support.isolated_env` keeps the host `PATH` on purpose, so the
standard Unix tools a script needs (`awk`, `sed`, `head`) resolve without every
fixture re-stubbing them. The cost of that convenience is that the same opening
lets *any* command through -- including a toolchain binary the fixture never
meant to run. Nothing in a fixture announces the gap: the lane still passes,
because reaching the real tool usually produces roughly the answer a fake would
have given.

That is how `deploy.sh`'s `codegraph index refresh` went unnoticed. The fixture
stubbed git/make/systemctl/ss/curl/sleep but not `codegraph`, so every case ran
the operator's real indexer against the temp production directory -- genuine
scanning, parsing, and SQLite writes. It was ~95% of each case's runtime (p50
0.822s against 0.048s once stubbed) and, being real work, its duration tracked
machine load; a loaded lane pushed the slowest case past `run_script`'s 10s cap
as a roughly 1-in-6 `TimeoutExpired`. The green lane was the symptom, not the
alarm.

So this guard asserts the absence directly instead of inferring it from a pass.
For each module it plants a logging shim named after every host executable
living outside the system prefixes, splices the shim directory in behind the
fixture's own fakes but ahead of the host directories it stands in for, runs the
lane once, and requires the shim log to come back empty. An empty log is
positive evidence of zero host reach; a passing lane is evidence of nothing.

Scope, stated plainly: the audit shadows what this host actually has installed
outside `/usr/bin`, `/bin`, `/usr/sbin`, and `/sbin`. A machine without
`codegraph` cannot observe the `codegraph` reach -- on that machine `deploy.sh`'s
`command -v` gate never opens and there is no reach to observe. The guard is
therefore strongest on a provisioned developer box, which is exactly where the
un-faked toolchains live and where the flake was found.
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
    _with_canary,
    isolated_env,
    write_executable,
)

DEV_DIR = REPO_ROOT / "scripts" / "dev"

# The tools `isolated_env` deliberately keeps reachable. Whatever the
# distribution ships lands here; whatever a toolchain manager installs
# (~/.local/bin, ~/.cargo/bin, /usr/local/bin, a node prefix) does not -- which
# is precisely the line the fixtures have to hold.
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


def system_dirs() -> set[str]:
    """Resolve the system prefixes, collapsing the usrmerge /bin -> /usr/bin link."""
    return {_real(prefix) for prefix in SYSTEM_PREFIXES}


def host_tool_dirs() -> list[str]:
    """Return the `PATH` directories that are not system prefixes, in `PATH` order."""
    resolved = system_dirs()
    ordered: list[str] = []
    for entry in os.environ.get("PATH", "").split(os.pathsep):
        if not entry or entry in ordered or _real(entry) in resolved:
            continue
        ordered.append(entry)
    return ordered


def host_tool_names() -> dict[str, str]:
    """Map each command name to the directory `PATH` order actually gives it to.

    Resolution is `which` semantics, done as one ordered walk instead of a
    lookup per name -- a CI runner's toolcache puts thousands of names on
    `PATH`, and re-scanning it once per name is quadratic for no gain.

    A name whose winner sits under a system prefix is dropped: shadowing it
    would put the canary in front of a tool the fixtures are entitled to reach,
    whatever else on `PATH` happens to share the name.
    """
    resolved = system_dirs()
    winner: dict[str, str] = {}
    for entry in os.environ.get("PATH", "").split(os.pathsep):
        if not entry:
            continue
        try:
            names = sorted(os.scandir(entry), key=lambda item: item.name)
        except OSError:
            continue
        for name in names:
            if name.name in winner or name.name in NEVER_SHADOW:
                continue
            try:
                if not name.is_file() or not os.access(name.path, os.X_OK):
                    continue
            except OSError:
                continue
            winner[name.name] = entry
    return {name: at for name, at in winner.items() if _real(at) not in resolved}


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


class CanaryPlacementTests(unittest.TestCase):
    """Where the canary sits in `PATH` is the whole contract, so pin it."""

    def setUp(self) -> None:
        self.stack = armed(Path("/canary"), ["/home/u/.local/bin"], Path("/dev/null"))
        self.stack.__enter__()
        self.addCleanup(self.stack.__exit__, None, None, None)

    def test_canary_stays_out_until_an_audit_arms_it(self) -> None:
        os.environ.pop(CANARY_BIN_ENV)
        untouched = "/fake:/home/u/.local/bin:/usr/bin"

        self.assertEqual(_with_canary(untouched), untouched)

    def test_canary_shadows_host_tools_without_shadowing_fixture_fakes(self) -> None:
        spliced = _with_canary("/fake:/home/u/.local/bin:/usr/bin")

        self.assertEqual(spliced, "/fake:/canary:/home/u/.local/bin:/usr/bin")

    def test_path_pinned_to_system_prefixes_is_left_alone(self) -> None:
        # A fixture that pins PATH to its fake bin plus the system prefixes is
        # already airtight. Splicing a canary in behind the fake bin anyway would
        # put ~/.local/bin back within reach and report a leak the fixture had
        # closed -- the audit inventing its own finding.
        untouched = "/fake:/usr/bin:/bin"

        self.assertEqual(_with_canary(untouched), untouched)

    def test_canary_stays_behind_every_directory_the_fixture_prepended(self) -> None:
        os.environ[CANARY_DIRS_ENV] = os.pathsep.join(["/home/u/.local/bin", "/opt/tools"])

        spliced = _with_canary("/fake:/fixture:/opt/tools:/home/u/.local/bin:/usr/bin")

        self.assertEqual(spliced, "/fake:/fixture:/canary:/opt/tools:/home/u/.local/bin:/usr/bin")


class HostBinaryReachTests(unittest.TestCase):
    """Run each shell lane once under the canary and require an empty log."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.tmp = tempfile.TemporaryDirectory(prefix="shell-isolation-")
        root = Path(cls.tmp.name)
        cls.log = root / "reach.log"
        cls.dirs = host_tool_dirs()
        cls.tools = host_tool_names()
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
        # guard rots into a no-op that reports success forever. Staged against a
        # planted host directory rather than whatever this machine happens to
        # have installed, so the check proves the same thing on a bare box and
        # never runs a real binary to find out.
        stage = Path(self.tmp.name) / "self-check"
        host_dir, witness = stage / "host", stage / "ran-for-real"
        write_executable(
            host_dir / "deneb-canary-probe",
            f'#!/usr/bin/env bash\ntouch "{witness}"\n',
        )
        canary = plant_canaries(stage / "canary", ["deneb-canary-probe"])
        log = stage / "reach.log"
        script = write_executable(
            stage / "probe.sh",
            "#!/usr/bin/env bash\ndeneb-canary-probe --canary-probe\n",
        )

        with mock.patch.dict(os.environ, {"PATH": os.pathsep.join([str(host_dir), *SYSTEM_PREFIXES])}):
            with armed(canary, [str(host_dir)], log):
                env = isolated_env(stage)
        subprocess.run([str(script)], env=env, capture_output=True, text=True, timeout=30, check=False)

        self.assertEqual(log.read_text(encoding="utf-8").split("\n")[:-1], ["deneb-canary-probe --canary-probe"])
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
