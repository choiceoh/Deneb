"""Host-drift, remote-probe, signing, and freshness tests for topology-parity.sh."""

from __future__ import annotations

import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]


def write_executable(path: Path, body: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body.lstrip("\n"), encoding="utf-8")
    path.chmod(0o755)


class TopologyParityShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "fixture"
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.audit = self.root / "scripts/audit"
        self.calls = self.root / "calls.log"
        self.home.mkdir(parents=True)
        self.bin.mkdir()
        self.audit.mkdir(parents=True)
        self.script = self.audit / "topology-parity.sh"
        shutil.copy2(REPO_ROOT / "scripts/audit/topology-parity.sh", self.script)
        self.script.chmod(0o755)

        write_executable(self.bin / "hostname", r"""
#!/usr/bin/env bash
printf 'hostname %s\n' "$*" >> "$FAKE_CALLS"
printf '%s\n' "${TEST_HOST:-srv4}"
""")
        write_executable(self.bin / "id", r"""
#!/usr/bin/env bash
[[ "${1:-}" == -u ]] && printf '%s\n' "${TEST_UID:-4242}"
""")
        write_executable(self.bin / "curl", r"""
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >> "$FAKE_CALLS"
case "$*" in
  *127.0.0.1:18789*) code="${GATEWAY_CODE:-200}" ;;
  *127.0.0.1:18800*) code="${WORMHOLE_CODE:-200}" ;;
  *100.105.145.6:18011*) code="${OCR_CODE:-200}" ;;
  *100.105.145.6:18013*) code="${ASR_CODE:-200}" ;;
  *100.105.145.6:8000*) code="${QWEN_CODE:-200}" ;;
  *100.125.220.117:8000*) code="${DSV4_CODE:-200}" ;;
  *deneb.topworks.ltd*) code="${TUNNEL_CODE:-200}" ;;
  *) code="${DEFAULT_HTTP_CODE:-500}" ;;
esac
printf '%s' "$code"
""")
        write_executable(self.bin / "systemctl", r"""
#!/usr/bin/env bash
printf 'systemctl xdg=%s args=%s\n' "${XDG_RUNTIME_DIR:-}" "$*" >> "$FAKE_CALLS"
if [[ "$*" == *"--user is-active --quiet deneb-auto-deploy.timer"* ]]; then
  [[ "${TIMER_ACTIVE:-1}" == 1 ]]
elif [[ "$*" == *"--user is-active --quiet deneb-lmtp.socket"* ]]; then
  [[ "${LMTP_ACTIVE:-1}" == 1 ]]
elif [[ "$*" == *"list-units actions.runner."* ]]; then
  [[ "${RUNNER_ACTIVE:-1}" == 1 ]] && printf 'actions.runner.choiceoh-Deneb.srv4-apk.service loaded active running\n'
else
  exit 9
fi
""")
        write_executable(self.bin / "ssh", r"""
#!/usr/bin/env bash
printf 'ssh %s\n' "$*" >> "$FAKE_CALLS"
if [[ "$*" == *"srv1 echo OK"* ]]; then
  [[ "${FAKE_SRV1_REACHABLE:-1}" == 1 ]] && printf 'OK\n'
elif [[ "$*" == *actions-runner-deneb* ]]; then
  printf '%s\n' "${OLD_RUNNER_PROBE-NONE}"
elif [[ "$*" == *':18800 '* ]]; then
  printf '%s\n' "${OLD_WORMHOLE_PROBE-NONE}"
fi
""")
        write_executable(self.bin / "git", r"""
#!/usr/bin/env bash
printf 'git %s\n' "$*" >> "$FAKE_CALLS"
case "$*" in
  *" fetch --quiet origin main") [[ "${FETCH_OK:-1}" == 1 ]] ;;
  *" rev-parse HEAD") printf '%s\n' "${PROD_HEAD:-aaaaaaaaaaaaaaaa}" ;;
  *" rev-parse origin/main") printf '%s\n' "${MAIN_HEAD:-aaaaaaaaaaaaaaaa}" ;;
  *" log -1 --format=%ct origin/main") printf '%s\n' "${MAIN_TIME:-999500}" ;;
  *) exit 11 ;;
esac
""")
        write_executable(self.bin / "date", r"""
#!/usr/bin/env bash
[[ "$*" == "+%s" ]] && printf '%s\n' "${NOW:-1000000}"
""")
        write_executable(self.bin / "find", r"""
#!/usr/bin/env bash
printf 'find %s\n' "$*" >> "$FAKE_CALLS"
if [[ "$*" == *"-name apksigner"* ]]; then
  [[ -x "$FAKE_APKSIGNER" ]] && printf '%s\n' "$FAKE_APKSIGNER"
elif [[ "$*" == *"docs/agent-rules"* && "${RULE_SIZE:-0}" -gt 0 ]]; then
  printf '%s /fake/docs/agent-rules/rules.md\n' "$RULE_SIZE"
fi
""")
        write_executable(self.bin / "docker", r"""
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "$FAKE_CALLS"
if [[ "${1:-}" == ps ]]; then
  [[ "${MAIL_RUNNING:-1}" == 1 ]] && printf 'deneb-mailserver\n'
elif [[ "${1:-}" == logs ]]; then
  if [[ "$*" == *deneb-mailarchive* ]]; then n="${ARCH_ERRORS:-0}"; else n="${DATA_ERRORS:-0}"; fi
  i=0; while (( i < n )); do printf 'DATA error fixture\n'; i=$((i + 1)); done
fi
""")
        write_executable(self.bin / "timeout", r"""
#!/usr/bin/env bash
shift
exec "$@"
""")
        write_executable(self.bin / "nc", r"""
#!/usr/bin/env bash
cat >/dev/null
[[ "${SMTP_OK:-1}" == 1 ]] && printf '220 fixture ESMTP\r\n'
""")

        self.apksigner = self.home / "android-sdk/build-tools/35.0.0/apksigner"
        write_executable(self.apksigner, r"""
#!/usr/bin/env bash
printf 'Signer #1 certificate SHA-256 digest: %s\n' "${CERT_GOT:-AABBCC}"
""")

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "PATH": f"{self.bin}:/usr/bin:/bin",
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "FAKE_CALLS": str(self.calls),
            "FAKE_APKSIGNER": str(self.apksigner),
            "TEST_HOST": "srv4",
            "TEST_UID": "4242",
            "GATEWAY_CODE": "200",
            "WORMHOLE_CODE": "200",
            "OCR_CODE": "200",
            "ASR_CODE": "200",
            "DSV4_CODE": "200",
            "TUNNEL_CODE": "200",
            "TIMER_ACTIVE": "1",
            "LMTP_ACTIVE": "1",
            "RUNNER_ACTIVE": "1",
            "FAKE_SRV1_REACHABLE": "1",
            "OLD_RUNNER_PROBE": "NONE",
            "OLD_WORMHOLE_PROBE": "NONE",
            "FETCH_OK": "1",
            "PROD_HEAD": "aaaaaaaaaaaaaaaa",
            "MAIN_HEAD": "aaaaaaaaaaaaaaaa",
            "MAIN_TIME": "999500",
            "NOW": "1000000",
            "RULE_SIZE": "0",
            "MAIL_RUNNING": "1",
            "SMTP_OK": "1",
            "DATA_ERRORS": "0",
            "ARCH_ERRORS": "0",
            "CERT_GOT": "AABBCC",
        }
        defaults.update(values)
        return defaults

    def invoke(self, **values: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [str(self.script)],
            cwd=self.root,
            env=self.env(**values),
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )

    def setup_release_surface(
        self,
        *,
        advertised: int = 12,
        builds: tuple[int, ...] = (12,),
        expected_cert: str = "AABBCC",
    ) -> None:
        deneb = self.home / ".deneb"
        keys = deneb / "keys"
        apk_dir = self.home / ".cache/deneb-apk"
        keys.mkdir(parents=True, exist_ok=True)
        apk_dir.mkdir(parents=True, exist_ok=True)
        (deneb / "apk-signing.env").write_text("KEYSTORE_PASSWORD=x\n", encoding="utf-8")
        (keys / "deneb-release.p12").write_bytes(b"fixture")
        (keys / "deneb-release-cert.sha256").write_text(expected_cert + "\n", encoding="utf-8")
        for code in builds:
            (apk_dir / f"deneb-{code}-foss.apk").write_bytes(b"apk")
        (apk_dir / "version.json").write_text(
            json.dumps({"code": advertised}), encoding="utf-8"
        )

    def result(self, proc: subprocess.CompletedProcess[str]) -> tuple[int, int]:
        line = next(line for line in proc.stdout.splitlines() if line.startswith("PARITY_RESULT"))
        fields = dict(field.split("=", 1) for field in line.split()[1:])
        return int(fields["fail"]), int(fields["warn"])

    def test_wrong_host_stops_before_any_service_or_network_probe(self) -> None:
        proc = self.invoke(TEST_HOST="workstation")
        self.assertEqual(proc.returncode, 2)
        self.assertIn("srv4", proc.stderr)
        calls = self.calls.read_text(encoding="utf-8")
        self.assertEqual(calls.splitlines(), ["hostname -s"])

    def test_all_documented_surfaces_healthy_returns_clean_result(self) -> None:
        self.setup_release_surface()
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(self.result(proc), (0, 0))
        self.assertIn("최신 APK(deneb-12-foss.apk) 서명 == release 인증서", proc.stdout)
        self.assertIn("version.json code=12 == serve dir 최고 빌드", proc.stdout)
        self.assertIn("srv1 구 러너 잔재 없음", proc.stdout)
        self.assertIn("srv1 에 wormhole 좀비 없음", proc.stdout)
        self.assertIn("systemctl xdg=/run/user/4242", self.calls.read_text())

    def test_custom_runtime_dir_is_preserved_for_user_systemd_checks(self) -> None:
        self.setup_release_surface()
        proc = self.invoke(XDG_RUNTIME_DIR="/custom/runtime")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        systemctl_calls = [
            line for line in self.calls.read_text().splitlines() if line.startswith("systemctl ")
        ]
        self.assertTrue(systemctl_calls)
        self.assertTrue(all("xdg=/custom/runtime" in line for line in systemctl_calls))

    def test_unreachable_srv1_warns_and_ignores_both_remote_claims(self) -> None:
        self.setup_release_surface()
        proc = self.invoke(FAKE_SRV1_REACHABLE="0")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(self.result(proc), (0, 2))
        self.assertEqual(proc.stdout.count("srv1 ssh 프로브 불가 — 스킵"), 2)
        self.assertNotIn("srv1 구 러너 디렉토리 부활", proc.stdout)
        self.assertNotIn("srv1 :18800 리스너 존재", proc.stdout)

    def test_unknown_remote_tokens_are_warnings_never_false_pass_or_fail(self) -> None:
        self.setup_release_surface()
        proc = self.invoke(OLD_RUNNER_PROBE="", OLD_WORMHOLE_PROBE="MAYBE")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(self.result(proc), (0, 2))
        self.assertEqual(proc.stdout.count("프로브 결과 불명(수동 확인)"), 2)
        self.assertNotIn("PASS  srv1 구 러너 잔재 없음", proc.stdout)
        self.assertNotIn("PASS  srv1 에 wormhole 좀비 없음", proc.stdout)

    def test_auto_deploy_quiet_period_passes_but_stale_divergence_fails(self) -> None:
        self.setup_release_surface()
        quiet = self.invoke(
            PROD_HEAD="aaaaaaaaaaaaaaaa",
            MAIN_HEAD="bbbbbbbbbbbbbbbb",
            MAIN_TIME="999500",
        )
        self.assertEqual(quiet.returncode, 0, quiet.stdout + quiet.stderr)
        self.assertIn("500초 전 갱신 — quiet period", quiet.stdout)
        self.assertEqual(self.result(quiet), (0, 0))

        self.calls.unlink()
        stale = self.invoke(
            PROD_HEAD="aaaaaaaaaaaaaaaa",
            MAIN_HEAD="bbbbbbbbbbbbbbbb",
            MAIN_TIME="998000",
        )
        self.assertEqual(stale.returncode, 1)
        self.assertIn("2000초 경과", stale.stdout)
        self.assertEqual(self.result(stale), (1, 0))

    def test_fetch_failure_is_warning_and_does_not_invent_freshness(self) -> None:
        self.setup_release_surface()
        proc = self.invoke(FETCH_OK="0")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(self.result(proc), (0, 1))
        self.assertIn("fetch 실패 — auto-deploy 신선도 체크 스킵", proc.stdout)
        self.assertNotIn("프로덕션 HEAD == origin/main", proc.stdout)

    def test_release_cert_and_ota_manifest_are_checked_at_artifact_boundary(self) -> None:
        self.setup_release_surface(advertised=11, builds=(10, 12), expected_cert="DDEEFF")
        proc = self.invoke(CERT_GOT="AABBCC")
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(self.result(proc), (2, 0))
        self.assertIn("서명이 release 인증서와 다름", proc.stdout)
        self.assertIn("version.json code=11 ≠ serve dir 최고 빌드 12", proc.stdout)

    def test_when_multiple_degraded_surfaces_are_all_counted_in_one_sweep(self) -> None:
        proc = self.invoke(
            GATEWAY_CODE="503",
            TIMER_ACTIVE="0",
            LMTP_ACTIVE="0",
            FETCH_OK="0",
            RUNNER_ACTIVE="0",
            FAKE_SRV1_REACHABLE="0",
            WORMHOLE_CODE="503",
            OCR_CODE="503",
            ASR_CODE="503",
            DSV4_CODE="503",
            QWEN_CODE="503",
            MAIL_RUNNING="0",
            SMTP_OK="0",
            DATA_ERRORS="2",
            ARCH_ERRORS="1",
            TUNNEL_CODE="503",
            RULE_SIZE="25000",
        )
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(self.result(proc), (10, 10))
        self.assertIn("FAIL 항목은", proc.stderr)
        self.assertIn("maddy DATA 거부 인입=2 아카이브=1", proc.stdout)
        self.assertIn("rules.md 25000B > 20KB", proc.stdout)


if __name__ == "__main__":
    unittest.main()
