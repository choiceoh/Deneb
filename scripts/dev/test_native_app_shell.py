"""Command, display, input, OCR, and teardown tests for native-app.sh."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, write_executable

NATIVE_SCRIPT = REPO_ROOT / "scripts/dev/native-app.sh"


def extract_locator_program() -> str:
    lines = NATIVE_SCRIPT.read_text(encoding="utf-8").splitlines()
    function = lines.index("_ocr_locate() {")
    start = next(
        index for index in range(function, len(lines))
        if lines[index] == "  python3 - \"$1\" \"$2\" \"$3\" <<'PY'"
    ) + 1
    end = lines.index("PY", start)
    return "\n".join(lines[start:end]) + "\n"


LOCATOR_PROGRAM = extract_locator_program()


class NativeAppShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.dev = self.root / "scripts/dev"
        self.app = self.root / "client-android/app"
        self.state = self.root / "state"
        self.log = self.root / "calls.log"
        self.search_counter = self.root / "search-counter"
        self.home.mkdir(parents=True)
        self.bin.mkdir()
        bash = shutil.which("bash")
        if bash:
            (self.bin / "bash").symlink_to(bash)
        self.dev.mkdir(parents=True)
        self.app.mkdir(parents=True)
        shutil.copy2(NATIVE_SCRIPT, self.dev / "native-app.sh")
        (self.dev / "native-app.sh").chmod(0o755)
        for name in ("Xvfb", "scrot", "xdpyinfo", "setsid", "matchbox-window-manager"):
            write_executable(self.bin / name, self.fake_binary(name))
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexit 0\n")
        write_executable(self.bin / "pgrep", """
            #!/usr/bin/env bash
            printf 'pgrep %s\n' "$*" >> "$FAKE_LOG"
            case "$*" in
              *"Xvfb "*) [[ -n "${XVFB_PID:-}" ]] && echo "$XVFB_PID" ;;
              *"x11vnc "*) [[ -n "${X11VNC_PID:-}" ]] && echo "$X11VNC_PID" ;;
              *"websockify"*) [[ -n "${NOVNC_PID:-}" ]] && echo "$NOVNC_PID" ;;
            esac
        """)
        write_executable(self.bin / "xdotool", """
            #!/usr/bin/env bash
            printf 'xdotool DISPLAY=%s %s\n' "${DISPLAY:-}" "$*" >> "$FAKE_LOG"
            case "$1" in
              search)
                n=$(cat "$SEARCH_COUNTER" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$SEARCH_COUNTER"
                if [[ -n "${APP_WID:-}" && $n -gt ${SEARCH_AFTER:-0} ]]; then echo "$APP_WID"; fi
                ;;
              getwindowpid) [[ -n "${WINDOW_PID:-}" ]] && echo "$WINDOW_PID" ;;
              getwindowfocus) echo "${FOCUS_WID:-0}" ;;
              getwindowgeometry) echo "  Geometry: ${WINDOW_GEOMETRY:-412x915}" ;;
            esac
        """)
        write_executable(self.bin / "python3", """
            #!/usr/bin/env bash
            printf 'python3 %s\n' "$*" >> "$FAKE_LOG"
            cat >/dev/null
            if [[ "$#" -ge 2 ]]; then
              printf 'seeded fixture url=%s token=%s\n' "$1" "$2"
            fi
        """)
        write_executable(self.app / "gradlew", "#!/usr/bin/env bash\nexit 0\n")

    def fake_binary(self, name: str) -> str:
        if name == "setsid":
            # The app JVM's flags ride JAVA_TOOL_OPTIONS, not argv, so record them on
            # their own line — otherwise the window/density contract is untestable.
            return """
                #!/usr/bin/env bash
                printf 'setsid %s\n' "$*" >> "$FAKE_LOG"
                printf 'setsid JAVA_TOOL_OPTIONS=%s\n' "${JAVA_TOOL_OPTIONS:-}" >> "$FAKE_LOG"
                exit 0
            """
        if name == "scrot":
            return """
                #!/usr/bin/env bash
                printf 'scrot %s\n' "$*" >> "$FAKE_LOG"
                if [[ "$1" == "--overwrite" && "${SCROT_PRIMARY_RC:-0}" != 0 ]]; then
                  exit "$SCROT_PRIMARY_RC"
                fi
                out="${@: -1}"; mkdir -p "$(dirname "$out")"; printf 'png' > "$out"
            """
        return f"""
            #!/usr/bin/env bash
            printf '{name} %s\\n' "$*" >> "$FAKE_LOG"
            exit 0
        """

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "FAKE_LOG": str(self.log),
            "SEARCH_COUNTER": str(self.search_counter),
            "DENEB_NATIVE_STATE": str(self.state),
            "DENEB_INSTANCE": "fixture",
            "NATIVE_DISPLAY": ":199",
            "NATIVE_VNC_PORT": "5999",
            "NATIVE_NOVNC_PORT": "6099",
            "NATIVE_TAILNET_IP": "100.64.0.9",
            "DENEB_GATEWAY_URL": "http://gateway.test:18789",
            "XVFB_PID": "12345",
            "APP_WID": "777",
            "SEARCH_AFTER": "0",
            "WINDOW_PID": str(os.getpid()),
            "WINDOW_GEOMETRY": "412x915",
            "FOCUS_WID": "777",
            "NATIVE_WM": "0",
            "SCROT_PRIMARY_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)


    def link_host_tool(self, name: str) -> None:
        if (self.bin / name).exists():
            return
        source = shutil.which(name)
        if source:
            (self.bin / name).symlink_to(source)

    def link_essential_host_tools(self) -> None:
        for tool in (
            "bash", "dirname", "cksum", "cut", "mkdir", "readlink", "pwd",
            "printf", "echo", "test", "grep", "sed", "cat", "rm", "mv", "cp",
            "chmod", "ln", "date", "sleep", "kill", "ps", "env", "tr", "wc",
            "head", "tail", "sort", "uniq", "mktemp", "realpath", "stat",
        ):
            self.link_host_tool(tool)

    def invoke(self, *args: str, env=None):
        return subprocess.run(
            ["/bin/bash", str(self.dev / "native-app.sh"), *args],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )

    def calls(self) -> list[str]:
        return self.log.read_text(encoding="utf-8").splitlines() if self.log.exists() else []

    def write_token(self, value="client-token") -> None:
        (self.home / ".deneb").mkdir(parents=True, exist_ok=True)
        (self.home / ".deneb/client_token").write_text(value + "\n", encoding="utf-8")

    def write_profile(self, profile="phone", width=412, height=915) -> None:
        self.state.mkdir(parents=True, exist_ok=True)
        (self.state / "profile").write_text(
            f"{profile} {width} {height} 1 {width} {height}\n",
            encoding="utf-8",
        )

    def test_usage_and_unknown_command_exit_one_without_external_processes(self) -> None:
        for args in [(), ("help",), ("unknown",)]:
            with self.subTest(args=args):
                self.log.unlink(missing_ok=True)
                proc = self.invoke(*args)
                self.assertEqual(proc.returncode, 1)
                self.assertIn("run the real native client headlessly", proc.stderr)
                self.assertIn("start [phone|phone2x|desktop]", proc.stderr)
                self.assertEqual(self.calls(), [])

    def test_status_reports_instance_display_gateway_profile_and_artifact_paths(self) -> None:
        self.write_profile("desktop", 1280, 800)
        proc = self.invoke("status")
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("instance:  fixture", proc.stdout)
        self.assertIn("display:   :199   (Xvfb pid: 12345)", proc.stdout)
        self.assertIn("profile:   desktop  1280x800dp @1x", proc.stdout)
        self.assertIn("gateway:   http://gateway.test:18789", proc.stdout)
        self.assertIn("app:       running (wid=777)", proc.stdout)
        self.assertIn(f"shots:     {self.state}/shots", proc.stdout)
        self.assertIn(f"app log:   {self.state}/app.log", proc.stdout)

    def test_unknown_profile_fails_before_dependency_or_token_checks(self) -> None:
        proc = self.invoke("start", "tablet")
        self.assertEqual(proc.returncode, 1)
        self.assertIn("unknown profile 'tablet' (use: phone | phone2x | desktop)", proc.stderr)
        self.assertEqual(self.calls(), [])

    def test_start_reports_first_missing_display_dependency(self) -> None:
        self.link_essential_host_tools()
        (self.bin / "scrot").unlink(missing_ok=True)
        self.write_token()
        proc = self.invoke("start", env=self.env(PATH=str(self.bin)))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("missing 'scrot'", proc.stderr)
        self.assertNotIn("python3", "\n".join(self.calls()))

    def test_existing_window_start_seeds_settings_reasserts_geometry_and_records_pid(self) -> None:
        self.write_token("secret-token")
        proc = self.invoke("start", "phone")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("Xvfb already on :199", proc.stderr)
        self.assertIn("app already running", proc.stderr)
        profile = (self.state / "profile").read_text().split()
        self.assertEqual(profile, ["phone", "412", "915", "1", "412", "915"])
        self.assertEqual((self.state / "app_jvm.pid").read_text().strip(), str(os.getpid()))
        calls = "\n".join(self.calls())
        self.assertIn("python3 - http://gateway.test:18789 secret-token", calls)
        self.assertIn("xdotool DISPLAY=:199 windowmove 777 0 0 windowsize 777 412 915", calls)
        self.assertNotIn("setsid", calls)

    def test_fresh_start_launches_gradle_with_phone_jvm_contract(self) -> None:
        self.write_token()
        proc = self.invoke(
            "start", "phone",
            env=self.env(XVFB_PID="", SEARCH_AFTER="1"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = "\n".join(self.calls())
        self.assertIn("Xvfb :199 -screen 0 412x915x24 -nolisten tcp -ac", calls)
        self.assertIn("setsid ./gradlew :composeApp:run --console=plain", calls)
        # 1x: the window opens at the dp box itself and the app is told density 1.
        self.assertIn("-Ddeneb.window.width=412 -Ddeneb.window.height=915 -Ddeneb.ui.scale=1", calls)
        self.assertIn("app window ready (wid=777)", proc.stderr)
        self.assertTrue((self.state / "app.pid").exists())
        self.assertEqual((self.state / "app_jvm.pid").read_text().strip(), str(os.getpid()))

    def test_hidpi_profile_opens_a_scaled_window_and_hands_the_app_its_density(self) -> None:
        # phone2x keeps the DP box (412x915) and scales only the pixel grid, so the
        # X screen, the window geometry and the app's own density must all agree on
        # 2x. If they drift apart the app either lays out at half size in a big
        # window or renders a phone-width sliver on a 2x screen.
        self.write_token()
        proc = self.invoke(
            "start", "phone2x",
            env=self.env(XVFB_PID="", SEARCH_AFTER="1"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(
            (self.state / "profile").read_text().split(),
            ["phone2x", "412", "915", "2", "824", "1830"],
        )
        calls = "\n".join(self.calls())
        self.assertIn("Xvfb :199 -screen 0 824x1830x24 -nolisten tcp -ac", calls)
        # The window opens at PHYSICAL px and the app is told the matching density;
        # dp = 824/2 = 412, so the layout is byte-identical to the phone profile.
        self.assertIn("-Ddeneb.window.width=824 -Ddeneb.window.height=1830 -Ddeneb.ui.scale=2", calls)
        # phone2x is still the mobile branch (bottom bar, modal drawers).
        self.assertIn("-Ddeneb.platform=phone", calls)

    def test_when_seed_requires_token_file_but_explicit_token_bypasses_it(self) -> None:
        missing = self.invoke("seed", "http://custom")
        self.assertEqual(missing.returncode, 1)
        self.assertIn("no token given", missing.stderr)

        self.log.unlink(missing_ok=True)
        seeded = self.invoke("seed", "http://custom", "explicit-token")
        self.assertEqual(seeded.returncode, 0, seeded.stdout + seeded.stderr)
        self.assertIn("python3 - http://custom explicit-token", self.calls())

    def test_shot_requires_display_and_primary_scrot_falls_back_to_legacy_flag(self) -> None:
        missing = self.invoke("shot", "home", env=self.env(XVFB_PID=""))
        self.assertEqual(missing.returncode, 1)
        self.assertIn("nothing running", missing.stderr)

        self.log.unlink(missing_ok=True)
        shot = self.invoke("shot", "home", env=self.env(SCROT_PRIMARY_RC="1"))
        self.assertEqual(shot.returncode, 0, shot.stdout + shot.stderr)
        output = self.state / "shots/home.png"
        self.assertEqual(shot.stdout.strip(), str(output))
        self.assertEqual(output.read_text(), "png")
        calls = "\n".join(self.calls())
        self.assertIn(f"scrot --overwrite {output}", calls)
        self.assertIn(f"scrot -o {output}", calls)

    def test_when_tap_doubletap_and_swipe_forward_exact_window_coordinates(self) -> None:
        commands = [
            (("tap", "20", "30"), "mousemove --window 777 20 30 click 1"),
            (("dbltap", "40", "50"), "mousemove --window 777 40 50 click --repeat 2 1"),
            (("swipe", "1", "2", "300", "400"), "mousemove --window 777 1 2 mousedown 1 mousemove --window 777 300 400 mouseup 1"),
        ]
        for args, expected in commands:
            with self.subTest(args=args):
                self.log.unlink(missing_ok=True)
                proc = self.invoke(*args)
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertIn(expected, "\n".join(self.calls()))

    def test_type_and_key_refocus_only_when_x_window_focus_differs(self) -> None:
        focused = self.invoke("type", "hello world")
        self.assertEqual(focused.returncode, 0)
        calls = "\n".join(self.calls())
        self.assertIn("type --clearmodifiers --delay 35 -- hello world", calls)
        self.assertNotIn("windowfocus 777", calls)

        self.log.unlink()
        unfocused = self.invoke(
            "key", "ctrl+a", "BackSpace",
            env=self.env(FOCUS_WID="999"),
        )
        self.assertEqual(unfocused.returncode, 0)
        calls = "\n".join(self.calls())
        self.assertIn("windowfocus 777", calls)
        self.assertIn("key --clearmodifiers -- ctrl+a BackSpace", calls)

    def test_when_scroll_uses_profile_center_and_direction_specific_mouse_button(self) -> None:
        self.write_profile()
        down = self.invoke("scroll", "down", "6")
        self.assertEqual(down.returncode, 0)
        self.assertIn(
            "mousemove --window 777 206 457 click --repeat 6 5",
            "\n".join(self.calls()),
        )
        self.log.unlink()
        up = self.invoke("scroll", "up", "2")
        self.assertEqual(up.returncode, 0)
        self.assertIn(
            "mousemove --window 777 206 457 click --repeat 2 4",
            "\n".join(self.calls()),
        )

    def test_logs_tail_existing_file_and_missing_file_is_error(self) -> None:
        self.state.mkdir(parents=True, exist_ok=True)
        (self.state / "app.log").write_text("one\ntwo\nthree\n", encoding="utf-8")
        logs = self.invoke("logs", "2")
        self.assertEqual(logs.returncode, 0)
        self.assertEqual(logs.stdout, "two\nthree\n")
        (self.state / "app.log").unlink()
        missing = self.invoke("logs")
        self.assertEqual(missing.returncode, 1)
        self.assertIn("no app log yet", missing.stderr)

    def test_stop_removes_stale_instance_pidfiles_without_global_process_kill(self) -> None:
        self.state.mkdir(parents=True, exist_ok=True)
        for name in ("app_jvm.pid", "app.pid", "novnc.pid", "wm.pid"):
            (self.state / name).write_text("99999999\n", encoding="utf-8")
        proc = self.invoke("stop", env=self.env(XVFB_PID=""))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("stopping", proc.stderr)
        self.assertIn("stopped", proc.stderr)
        for name in ("app_jvm.pid", "app.pid", "novnc.pid", "wm.pid"):
            self.assertFalse((self.state / name).exists())
        self.assertFalse(any("pkill -f" in line for line in self.calls()))


class OCRLocatorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.tsv = Path(self.tmp.name) / "ocr.tsv"

    def run_locator(self, mode: str, query: str):
        return subprocess.run(
            ["python3", "-", mode, query, str(self.tsv)],
            input=LOCATOR_PROGRAM,
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )

    def test_when_locator_joins_words_by_line_filters_confidence_and_scales_center(self) -> None:
        self.tsv.write_text(
            "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"
            "5\t1\t1\t1\t1\t1\t300\t150\t90\t30\t96\t받은\n"
            "5\t1\t1\t1\t1\t2\t405\t150\t90\t30\t94\t메일\n"
            "5\t1\t1\t1\t2\t1\t0\t0\t30\t30\t20\t받은 메일\n",
            encoding="utf-8",
        )
        found = self.run_locator("find", "받은 메일")
        self.assertEqual(found.returncode, 0, found.stderr)
        self.assertEqual(found.stdout.strip(), "132 55")
        asserted = self.run_locator("assert", "받은 메일")
        self.assertEqual((asserted.returncode, asserted.stdout), (0, ""))

    def test_locator_rejects_missing_blank_and_low_confidence_queries(self) -> None:
        self.tsv.write_text(
            "5\t1\t1\t1\t1\t1\t0\t0\t90\t30\t39\thidden\n",
            encoding="utf-8",
        )
        for query in ("hidden", "missing", ""):
            with self.subTest(query=query):
                proc = self.run_locator("assert", query)
                self.assertEqual(proc.returncode, 1)
                self.assertEqual(proc.stdout, "")


if __name__ == "__main__":
    unittest.main()
