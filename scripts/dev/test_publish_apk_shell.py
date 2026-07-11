"""Signing, locking, smoke-gate, and artifact-contract tests for publish-apk.sh."""

from __future__ import annotations

import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, write_executable


class PublishAPKShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.dev = self.root / "scripts/dev"
        self.app = self.root / "client-android/app"
        self.apk_dir = self.root / "published"
        self.log = self.root / "calls.log"
        self.home.mkdir(parents=True)
        self.bin.mkdir()
        self.dev.mkdir(parents=True)
        (self.app / "gradle").mkdir(parents=True)
        (self.app / "androidApp").mkdir()
        shutil.copy2(REPO_ROOT / "scripts/dev/publish-apk.sh", self.dev / "publish-apk.sh")
        (self.dev / "publish-apk.sh").chmod(0o755)
        (self.app / "gradle/libs.versions.toml").write_text(
            'android-versionCode = "600"\n',
            encoding="utf-8",
        )
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git %s\n' "$*" >> "$FAKE_LOG"
            [[ "$*" == *"rev-parse --short=8 HEAD"* ]] && echo "${GIT_SHA:-deadbeef}"
        """)
        write_executable(self.bin / "flock", """
            #!/usr/bin/env bash
            printf 'flock %s\n' "$*" >> "$FAKE_LOG"
            exit "${FLOCK_RC:-0}"
        """)
        write_executable(self.dev / "native-app.sh", """
            #!/usr/bin/env bash
            printf 'native-app %s\n' "$*" >> "$FAKE_LOG"
            case "$1" in
              start) exit "${NATIVE_START_RC:-0}" ;;
              stop) exit "${NATIVE_STOP_RC:-0}" ;;
            esac
        """)
        write_executable(self.dev / "native-app-smoke.sh", """
            #!/usr/bin/env bash
            printf 'native-smoke\n' >> "$FAKE_LOG"
            exit "${SMOKE_RC:-0}"
        """)
        write_executable(self.app / "gradlew", """
            #!/usr/bin/env bash
            printf 'gradle cwd=%s BUILD_SHA=%s ANDROID_HOME=%s args=%s KEYSTORE=%s ALIAS=%s\n' \
              "$PWD" "${DENEB_BUILD_SHA:-}" "${ANDROID_HOME:-}" "$*" \
              "${KEYSTORE_FILE:-}" "${KEY_ALIAS:-}" >> "$FAKE_LOG"
            [[ "${GRADLE_RC:-0}" == 0 ]] || exit "$GRADLE_RC"
            code=""; for arg in "$@"; do [[ "$arg" == -PdenebVersionCode=* ]] && code="${arg#*=}"; done
            if [[ "$*" == *packageFossReleaseUniversalApk* ]]; then
              out="androidApp/build/outputs/apk_from_bundle/fossRelease/androidApp-foss-release-universal.apk"
            else
              out="androidApp/build/outputs/apk/foss/debug/deneb-${code}-${DENEB_BUILD_SHA}-fossDebug.apk"
            fi
            if [[ "${CREATE_APK:-1}" == 1 ]]; then
              mkdir -p "$(dirname "$out")"; printf 'apk-%s\n' "$code" > "$out"
            fi
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "FAKE_LOG": str(self.log),
            "DENEB_APK_DIR": str(self.apk_dir),
            "DENEB_APK_BASE_URL": "https://updates.example.test",
            "ANDROID_HOME": str(self.root / "android-sdk"),
            "DENEB_APK_VARIANT": "fossDebug",
            "DENEB_SKIP_SMOKE": "1",
            "GIT_SHA": "deadbeef",
            "FLOCK_RC": "0",
            "GRADLE_RC": "0",
            "CREATE_APK": "1",
            "NATIVE_START_RC": "0",
            "NATIVE_STOP_RC": "0",
            "SMOKE_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, notes="", env=None):
        return subprocess.run(
            [str(self.dev / "publish-apk.sh"), notes],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )

    def calls(self) -> str:
        return self.log.read_text(encoding="utf-8") if self.log.exists() else ""

    def signing_env(self, *, valid=True) -> Path:
        signing = self.root / "signing.env"
        key = self.root / "release.p12"
        if valid:
            key.write_text("keystore", encoding="utf-8")
        signing.write_text(
            f"KEYSTORE_FILE={key}\n"
            "KEYSTORE_PASSWORD=secret\n"
            "KEY_ALIAS=deneb-release\n",
            encoding="utf-8",
        )
        return signing

    def test_unknown_variant_fails_before_touching_app_or_publish_directory(self) -> None:
        proc = self.invoke(env=self.env(DENEB_APK_VARIANT="benchmark"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("unknown DENEB_APK_VARIANT: benchmark", proc.stderr)
        self.assertFalse(self.apk_dir.exists())
        self.assertEqual(self.calls(), "")

    def test_release_requires_default_signing_material_unless_explicitly_overridden(self) -> None:
        proc = self.invoke(env=self.env(
            DENEB_APK_VARIANT="fossRelease",
            DENEB_SKIP_SMOKE="1",
        ))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("refusing to publish a debug-signed fossRelease", proc.stderr)
        self.assertIn("DENEB_ALLOW_DEBUG_SIGNING=1", proc.stderr)
        self.assertNotIn("gradle", self.calls())

    def test_explicit_missing_signing_path_is_treated_as_typo(self) -> None:
        missing = self.root / "missing-signing.env"
        proc = self.invoke(env=self.env(
            DENEB_APK_VARIANT="fossRelease",
            DENEB_APK_SIGNING_ENV=str(missing),
        ))
        self.assertEqual(proc.returncode, 1)
        self.assertIn(f"points at a missing file: {missing}", proc.stderr)
        self.assertIn("Fix the path", proc.stderr)

    def test_present_but_invalid_signing_env_fails_closed(self) -> None:
        signing = self.signing_env(valid=False)
        proc = self.invoke(env=self.env(
            DENEB_APK_VARIANT="fossRelease",
            DENEB_APK_SIGNING_ENV=str(signing),
        ))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("exists but KEYSTORE_FILE/KEYSTORE_PASSWORD/KEY_ALIAS is unset", proc.stderr)
        self.assertNotIn("gradle", self.calls())

    def test_debug_signing_override_is_loud_but_allows_release_build(self) -> None:
        proc = self.invoke(env=self.env(
            DENEB_APK_VARIANT="fossRelease",
            DENEB_ALLOW_DEBUG_SIGNING="1",
        ))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("DENEB_ALLOW_DEBUG_SIGNING=1", proc.stderr)
        self.assertIn("packageFossReleaseUniversalApk", self.calls())

    def test_valid_signing_env_is_exported_to_gradle_and_release_artifact_is_renamed(self) -> None:
        signing = self.signing_env()
        proc = self.invoke("release", env=self.env(
            DENEB_APK_VARIANT="fossRelease",
            DENEB_APK_SIGNING_ENV=str(signing),
        ))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("release signing: release.p12 (alias deneb-release)", proc.stdout)
        calls = self.calls()
        self.assertIn(f"KEYSTORE={self.root / 'release.p12'} ALIAS=deneb-release", calls)
        artifact = self.apk_dir / "deneb-600-deadbeef-fossRelease.apk"
        self.assertEqual(artifact.read_text(), "apk-600\n")

    def test_google_services_is_injected_when_present_and_removed_when_absent(self) -> None:
        google = self.root / "google-services.json"
        google.write_text('{"project_info":{}}', encoding="utf-8")
        injected = self.invoke(env=self.env(DENEB_GOOGLE_SERVICES_JSON=str(google)))
        self.assertEqual(injected.returncode, 0, injected.stdout + injected.stderr)
        target = self.app / "androidApp/google-services.json"
        self.assertEqual(target.read_text(), google.read_text())
        self.assertIn("FCM push enabled", injected.stdout)

        target.write_text("stale", encoding="utf-8")
        absent = self.invoke(env=self.env(DENEB_GOOGLE_SERVICES_JSON=str(self.root / "missing.json")))
        self.assertEqual(absent.returncode, 0, absent.stdout + absent.stderr)
        self.assertFalse(target.exists())
        self.assertIn("building WITHOUT FCM push", absent.stderr)

    def test_version_code_uses_max_of_serve_increment_and_library_floor(self) -> None:
        self.apk_dir.mkdir()
        (self.apk_dir / "deneb-598-old-fossDebug.apk").write_text("old")
        (self.apk_dir / "deneb-4.2.1-605-legacy.apk").write_text("legacy")
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("auto-assigned versionCode 606 (serve max=605, libs floor=600)", proc.stdout)
        self.assertTrue((self.apk_dir / "deneb-606-deadbeef-fossDebug.apk").exists())

    def test_library_floor_wins_when_publish_directory_is_empty(self) -> None:
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("versionCode 600 (serve max=none, libs floor=600)", proc.stdout)
        self.assertIn("-PdenebVersionCode=600", self.calls())

    def test_unrelated_or_malformed_apk_names_do_not_inflate_next_version(self) -> None:
        self.apk_dir.mkdir()
        for name in (
            "unrelated-9999.apk",
            "deneb-latest.apk",
            "deneb-4.x-900-bad.apk",
            "deneb-599-no-suffix.txt",
        ):
            (self.apk_dir / name).write_text("not a published Deneb APK")
        (self.apk_dir / "deneb-601-good-fossDebug.apk").write_text("real")
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("versionCode 602 (serve max=601, libs floor=600)", proc.stdout)
        self.assertTrue((self.apk_dir / "deneb-602-deadbeef-fossDebug.apk").exists())

    def test_publish_lock_timeout_aborts_before_smoke_or_gradle(self) -> None:
        proc = self.invoke(env=self.env(FLOCK_RC="1"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("could not acquire", proc.stderr)
        self.assertIn("another publish is", proc.stderr)
        self.assertIn("running or wedged", proc.stderr)
        calls = self.calls()
        self.assertIn("flock -w 600 200", calls)
        self.assertNotIn("native-app", calls)
        self.assertNotIn("gradle", calls)

    def test_explicit_skip_smoke_never_invokes_live_harness(self) -> None:
        proc = self.invoke(env=self.env(DENEB_SKIP_SMOKE="reason"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("pre-publish smoke: SKIPPED", proc.stdout)
        self.assertNotIn("native-app", self.calls())
        self.assertNotIn("native-smoke", self.calls())

    def test_unavailable_live_harness_warns_but_does_not_block_publish(self) -> None:
        proc = self.invoke(env=self.env(DENEB_SKIP_SMOKE="", NATIVE_START_RC="3"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("native live-app harness could not start", proc.stderr)
        self.assertIn("render crashes will NOT be caught", proc.stderr)
        self.assertIn("native-app start", self.calls())
        self.assertNotIn("native-smoke", self.calls())

    def test_failed_smoke_blocks_build_and_leaves_harness_running_for_diagnosis(self) -> None:
        proc = self.invoke(env=self.env(DENEB_SKIP_SMOKE="", SMOKE_RC="1"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("PRE-PUBLISH SMOKE FAILED", proc.stderr)
        calls = self.calls()
        self.assertIn("native-app start", calls)
        self.assertIn("native-smoke", calls)
        self.assertNotIn("native-app stop", calls)
        self.assertNotIn("gradle", calls)

    def test_successful_smoke_stops_harness_before_gradle(self) -> None:
        proc = self.invoke(env=self.env(DENEB_SKIP_SMOKE=""))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls().splitlines()
        self.assertLess(calls.index("native-app start"), calls.index("native-smoke"))
        self.assertLess(calls.index("native-smoke"), calls.index("native-app stop"))
        gradle_index = next(
            index for index, line in enumerate(calls) if line.startswith("gradle ")
        )
        self.assertLess(calls.index("native-app stop"), gradle_index)
        self.assertIn("pre-publish smoke: PASS", proc.stdout)

    def test_gradle_failure_propagates_without_publishing_partial_metadata(self) -> None:
        proc = self.invoke(env=self.env(GRADLE_RC="8"))
        self.assertEqual(proc.returncode, 8)
        self.assertFalse((self.apk_dir / "version.json").exists())
        self.assertFalse(any(self.apk_dir.glob("*.apk")))

    def test_missing_expected_apk_is_failure_and_preserves_existing_manifest(self) -> None:
        self.apk_dir.mkdir()
        manifest = self.apk_dir / "version.json"
        manifest.write_text('{"code":599}\n', encoding="utf-8")
        proc = self.invoke(env=self.env(CREATE_APK="0"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("build did not produce", proc.stderr)
        self.assertEqual(manifest.read_text(), '{"code":599}\n')

    def test_notes_backslashes_and_quotes_remain_valid_json(self) -> None:
        notes = 'Windows C:\\Deneb says "ready"'
        proc = self.invoke(notes)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        manifest = json.loads((self.apk_dir / "version.json").read_text())
        self.assertEqual(manifest, {
            "code": 600,
            "url": "https://updates.example.test/deneb-600-deadbeef-fossDebug.apk",
            "notes": notes,
        })

    def test_unreadable_version_code_fails_before_lock_and_build(self) -> None:
        (self.app / "gradle/libs.versions.toml").write_text(
            'android-versionName = "legacy"\n',
            encoding="utf-8",
        )
        proc = self.invoke()
        self.assertEqual(proc.returncode, 1)
        self.assertIn("could not read android-versionCode", proc.stderr)
        self.assertNotIn("flock", self.calls())
        self.assertNotIn("gradle", self.calls())


if __name__ == "__main__":
    unittest.main()
