"""Branch, build, and systemd cutover tests for deploy.sh."""

from __future__ import annotations

import hashlib
import tempfile
import time
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script, write_executable


WORMHOLE_FIXTURE = b"fixture-wormhole\n"


class DeployShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.prod = self.root / "prod"
        self.bin = self.root / "bin"
        self.log = self.root / "calls.log"
        self.pid_counter = self.root / "pid-counter"
        self.active_marker = self.root / "active"
        self.wormhole_live_sum = self.root / "wormhole-live.sha256"
        self.home.mkdir()
        self.prod.mkdir()
        self.bin.mkdir()
        self.wormhole_live_sum.write_text(
            hashlib.sha256(WORMHOLE_FIXTURE).hexdigest() + "\n",
            encoding="utf-8",
        )
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_LOG"
            case "$*" in
              "branch --show-current") echo "${GIT_BRANCH:-main}" ;;
              "-c pull.rebase=false pull --ff-only origin main") exit "${GIT_PULL_RC:-0}" ;;
              *) echo "unexpected git args: $*" >&2; exit 81 ;;
            esac
        """)
        write_executable(self.bin / "make", """
            #!/usr/bin/env bash
            printf 'make cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_LOG"
            if [[ "$*" == "wormhole" && "${MAKE_RC:-0}" == "0" ]]; then
              mkdir -p dist
              printf 'fixture-wormhole\n' > dist/wormhole
            fi
            exit "${MAKE_RC:-0}"
        """)
        write_executable(self.bin / "systemctl", """
            #!/usr/bin/env bash
            printf 'systemctl %s\n' "$*" >> "$FAKE_LOG"
            case "$*" in
              "--user show "*" -p LoadState --value") echo "${SYSTEMD_LOAD_STATE:-loaded}" ;;
              "--user show "*" -p MainPID --value")
                n=$(cat "$FAKE_PID_COUNTER" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$FAKE_PID_COUNTER"
                if [[ $n -eq 1 ]]; then echo "${SYSTEMD_OLD_PID:-111}"
                else echo "${SYSTEMD_NEW_PID:-222}"; fi
                ;;
              "--user is-active --quiet "*)
                if [[ "${SYSTEMD_INITIAL_ACTIVE:-1}" == 1 || -f "$FAKE_ACTIVE_MARKER" ]]; then exit 0; fi
                exit 3
                ;;
              "--user kill "*) exit "${SYSTEMD_KILL_RC:-0}" ;;
              "--user restart "*|"--user start "*) touch "$FAKE_ACTIVE_MARKER" ;;
              "--user status "*) echo 'service status fixture' ;;
              *) echo "unexpected systemctl args: $*" >&2; exit 82 ;;
            esac
        """)
        write_executable(self.bin / "ss", """
            #!/usr/bin/env bash
            printf 'ss %s\n' "$*" >> "$FAKE_LOG"
            if [[ "${SS_LISTEN:-1}" == 1 ]]; then
              printf 'LISTEN 0 128 %s 0.0.0.0:*\n' "${SS_ADDRESS:-127.0.0.1:18789}"
            fi
        """)
        write_executable(self.bin / "curl", """
            #!/usr/bin/env bash
            printf 'curl %s\n' "$*" >> "$FAKE_LOG"
            case "$*" in
              *"${DENEB_EMBEDDING_URL:-http://embedder.invalid}"*) exit "${EMBEDDING_CURL_RC:-0}" ;;
            esac
            exit "${CURL_RC:-0}"
        """)
        self.topology_python = write_executable(self.bin / "model-topology-python", """
            #!/usr/bin/env bash
            printf 'topology-python cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_LOG"
            exit "${TOPOLOGY_RC:-0}"
        """)
        # deploy.sh refreshes the CodeGraph/code_search indexes near the end, and
        # both `codegraph` and `go` are real host binaries. Unstubbed, the test
        # shelled out to the genuine indexer and did real scanning/parsing work in
        # the temp prod dir — unbounded, load-sensitive, and the actual source of
        # the `TimeoutExpired` flake on a busy machine. Faking them keeps the lane
        # hermetic and turns the branch into something assertable.
        write_executable(self.bin / "codegraph", """
            #!/usr/bin/env bash
            printf 'codegraph %s\n' "$*" >> "$FAKE_LOG"
            rc="${CODEGRAPH_RC:-0}"
            # Reproduce the real indexer's observable side effect, so the
            # code_search gate downstream still sees what it would in production.
            [[ "$rc" == 0 ]] && mkdir -p .codegraph && : > .codegraph/codegraph.db
            exit "$rc"
        """)
        write_executable(self.bin / "go", """
            #!/usr/bin/env bash
            printf 'go cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_LOG"
            exit "${GO_RC:-0}"
        """)
        # Paced, not instant. deploy.sh's retry loops are bounded by WALL CLOCK
        # (`SECONDS`/`date`), not by iteration count, so a no-op `sleep` turns any
        # unmet retry condition into a fork storm that spins until the deadline
        # instead of retrying. 20ms keeps the suite fast while letting a transient
        # blip actually recover on the next pass.
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexec /bin/sleep 0.02\n")

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "DENEB_PROD_DIR": str(self.prod),
            "DENEB_GATEWAY_PORT": "18789",
            "DENEB_GATEWAY_SERVICE": "deneb-gateway.service",
            "DENEB_DEPLOY_RESTART_MODE": "systemd",
            "FAKE_LOG": str(self.log),
            "FAKE_PID_COUNTER": str(self.pid_counter),
            "FAKE_ACTIVE_MARKER": str(self.active_marker),
            "GIT_BRANCH": "main",
            "GIT_PULL_RC": "0",
            "MAKE_RC": "0",
            "SYSTEMD_LOAD_STATE": "loaded",
            "SYSTEMD_INITIAL_ACTIVE": "1",
            "SYSTEMD_KILL_RC": "0",
            "SS_LISTEN": "1",
            "SS_ADDRESS": "127.0.0.1:18789",
            "CURL_RC": "0",
            "DENEB_EMBEDDING_URL": "http://127.0.0.1:8002",
            "EMBEDDING_CURL_RC": "0",
            "TOPOLOGY_RC": "0",
            "DENEB_MODEL_ROUTE_TOPOLOGY_PYTHON": str(self.topology_python),
            "WORMHOLE_LIVE_SUM_FILE": str(self.wormhole_live_sum),
            # Bound the script's own wait windows. The production defaults (510s
            # restart wait, 420s idle gate) are wall-clock budgets sized for a real
            # gateway; left unbounded here a single missed retry condition outlives
            # any sane test timeout and surfaces as an opaque TimeoutExpired rather
            # than the script's own diagnostic.
            "DENEB_DEPLOY_RESTART_WAIT_SEC": "3",
            "DENEB_DEPLOY_IDLE_WAIT_SEC": "2",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None):
        return run_script(
            "scripts/deploy/deploy.sh",
            *args,
            env=env or self.env(),
            timeout=10,
        )

    def calls(self) -> str:
        return self.log.read_text(encoding="utf-8") if self.log.exists() else ""

    def seed_codegraph_index(self) -> None:
        """Give the production checkout an index, so the refresh syncs it."""
        index = self.prod / ".codegraph"
        index.mkdir()
        (index / "codegraph.db").write_text("", encoding="utf-8")

    def topology_env(self, **values: str) -> dict[str, str]:
        deneb_config = self.root / "deneb.json"
        wormhole_config = self.root / "wormhole.json"
        checker = self.prod / "scripts" / "audit" / "model_route_topology.py"
        checker.parent.mkdir(parents=True, exist_ok=True)
        checker.write_text("# topology checker fixture\n", encoding="utf-8")
        deneb_config.write_text("{}\n", encoding="utf-8")
        wormhole_config.write_text("{}\n", encoding="utf-8")
        return self.env(
            DENEB_CONFIG_PATH=str(deneb_config),
            DENEB_WORMHOLE_CONFIG=str(wormhole_config),
            **values,
        )

    def test_when_non_main_checkout_refuses_before_pull_or_build(self) -> None:
        proc = self.invoke(env=self.env(GIT_BRANCH="feature"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("production must be on main (currently on feature)", proc.stderr)
        calls = self.calls()
        self.assertIn("git cwd=", calls)
        self.assertNotIn("pull --ff-only", calls)
        self.assertNotIn("make ", calls)

    def test_build_only_uses_nonrebase_fast_forward_and_skips_restart(self) -> None:
        proc = self.invoke("--build-only")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("==> git pull", proc.stdout)
        self.assertIn("==> make gateway-prod", proc.stdout)
        self.assertIn("build done (--build-only, skipping restart)", proc.stdout)
        calls = self.calls()
        self.assertIn("git cwd=", calls)
        self.assertIn("args=-c pull.rebase=false pull --ff-only origin main", calls)
        self.assertIn(f"make cwd={self.prod} args=gateway-prod", calls)
        self.assertNotIn("systemctl", calls)
        self.assertNotIn("codegraph", calls)

    def test_changed_wormhole_binary_restarts_and_records_checksum(self) -> None:
        self.wormhole_live_sum.write_text("stale\n", encoding="utf-8")

        proc = self.invoke()

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("wormhole binary changed — restarting the router", proc.stdout)
        self.assertIn("systemctl --user restart wormhole", self.calls())
        self.assertEqual(
            self.wormhole_live_sum.read_text(encoding="utf-8"),
            hashlib.sha256(WORMHOLE_FIXTURE).hexdigest() + "\n",
        )

    def test_pull_failure_stops_before_build_and_propagates_status(self) -> None:
        proc = self.invoke("--build-only", env=self.env(GIT_PULL_RC="7"))
        self.assertEqual(proc.returncode, 7)
        self.assertNotIn("==> make gateway-prod", proc.stdout)
        self.assertNotIn("make ", self.calls())

    def test_model_topology_gate_runs_after_pull_and_before_build(self) -> None:
        proc = self.invoke("--build-only", env=self.topology_env())

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("topology-python cwd=", calls)
        self.assertIn("scripts/audit/model_route_topology.py", calls)
        self.assertLess(calls.index("topology-python cwd="), calls.index("make cwd="))

    def test_model_topology_failure_stops_before_build_or_restart(self) -> None:
        proc = self.invoke(env=self.topology_env(TOPOLOGY_RC="1"))

        self.assertEqual(proc.returncode, 1)
        self.assertIn("==> model route topology", proc.stdout)
        calls = self.calls()
        self.assertIn("topology-python cwd=", calls)
        self.assertNotIn("make ", calls)
        self.assertNotIn("systemctl", calls)

    def test_model_topology_gate_has_an_explicit_emergency_bypass(self) -> None:
        proc = self.invoke(
            "--build-only",
            env=self.topology_env(
                TOPOLOGY_RC="9",
                DENEB_SKIP_MODEL_ROUTE_TOPOLOGY_CHECK="1",
            ),
        )

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("model route topology gate explicitly skipped", proc.stderr)
        self.assertNotIn("topology-python cwd=", self.calls())
        self.assertIn("make cwd=", self.calls())

    def test_build_failure_stops_before_any_restart(self) -> None:
        proc = self.invoke(env=self.env(MAKE_RC="9"))
        self.assertEqual(proc.returncode, 9)
        self.assertIn("==> make gateway-prod", proc.stdout)
        self.assertNotIn("systemctl", self.calls())

    def test_unknown_restart_mode_fails_after_successful_build_with_clear_choices(self) -> None:
        proc = self.invoke(env=self.env(DENEB_DEPLOY_RESTART_MODE="rolling"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn(
            "unknown DENEB_DEPLOY_RESTART_MODE=rolling (want auto, systemd, or nohup)",
            proc.stderr,
        )
        self.assertIn("make cwd=", self.calls())
        self.assertNotIn("systemctl", self.calls())

    def test_active_systemd_service_hot_restarts_and_requires_new_healthy_pid(self) -> None:
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("hot restarting deneb-gateway.service with SIGUSR1 (old pid 111)", proc.stdout)
        self.assertIn("deploy OK (deneb-gateway.service, pid 222, port 18789)", proc.stdout)
        calls = self.calls()
        self.assertIn(
            "systemctl --user kill --kill-who=main -s SIGUSR1 deneb-gateway.service",
            calls,
        )
        self.assertIn("curl -sf http://127.0.0.1:18789/health", calls)

    def test_inactive_systemd_service_is_started_then_waited_until_healthy(self) -> None:
        proc = self.invoke(env=self.env(SYSTEMD_INITIAL_ACTIVE="0"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("starting inactive deneb-gateway.service", proc.stdout)
        self.assertIn("systemctl --user start deneb-gateway.service", self.calls())
        self.assertNotIn("kill --kill-who", self.calls())

    def test_failed_sigusr1_falls_back_to_direct_systemd_restart(self) -> None:
        proc = self.invoke(env=self.env(SYSTEMD_KILL_RC="1"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("SIGUSR1 failed; falling back to systemctl restart", proc.stdout)
        self.assertIn("systemctl --user restart deneb-gateway.service", self.calls())

    def test_auto_mode_selects_loaded_systemd_unit(self) -> None:
        proc = self.invoke(env=self.env(DENEB_DEPLOY_RESTART_MODE="auto"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn(
            "systemctl --user show deneb-gateway.service -p LoadState --value",
            calls,
        )
        self.assertIn("systemctl --user kill --kill-who=main -s SIGUSR1", calls)

    def test_when_health_probe_uses_specific_listen_address_instead_of_loopback(self) -> None:
        proc = self.invoke(env=self.env(SS_ADDRESS="100.64.1.5:18789"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("curl -sf http://100.64.1.5:18789/health", self.calls())

    def test_codegraph_index_refresh_runs_and_stays_non_fatal(self) -> None:
        # The index refresh used to run the real host `codegraph` — expensive and
        # entirely unasserted. Stubbed, its contract is cheap to pin: it runs, and
        # a failing indexer must not fail the deploy.
        proc = self.invoke(env=self.env(CODEGRAPH_RC="1"))

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("==> codegraph index refresh", proc.stdout)
        self.assertIn("codegraph init failed (non-fatal)", proc.stdout)
        self.assertIn("codegraph init .", self.calls())
        # The refresh has to land before the swap: the gateway that comes up next
        # is the one whose self-inspection tools open the index.
        calls = self.calls()
        self.assertLess(calls.index("codegraph init ."), calls.index("systemctl --user kill"))

    def test_existing_codegraph_index_is_synced_rather_than_reinitialized(self) -> None:
        # init and sync are different commands against a ~317MB artifact; a
        # production checkout always has an index, so sync is the live path.
        self.seed_codegraph_index()

        proc = self.invoke()

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("codegraph sync .", calls)
        self.assertNotIn("codegraph init .", calls)

    def test_failed_codegraph_sync_is_reported_without_blocking_the_deploy(self) -> None:
        # The sync path carries its own message, and its own obligation not to
        # strand production on a stale binary because an indexer broke.
        self.seed_codegraph_index()

        proc = self.invoke(env=self.env(CODEGRAPH_RC="3"))

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("codegraph sync failed (non-fatal); serving prior index", proc.stdout)
        self.assertIn("deploy OK", proc.stdout)

    def test_semantic_index_refresh_follows_the_graph_sync_when_the_embedder_answers(self) -> None:
        (self.prod / "gateway-go").mkdir()

        proc = self.invoke()

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("==> code_search semantic index refresh", proc.stdout)
        calls = self.calls()
        self.assertIn(f"go cwd={self.prod}/gateway-go args=run ./cmd/codesearch index", calls)
        self.assertLess(calls.index("codegraph init ."), calls.index("go cwd="))

    def test_unreachable_embedder_skips_the_semantic_index_refresh(self) -> None:
        # Gated on the sidecar, not on the graph index: an embedder that is down
        # must cost nothing, not a failed re-embed.
        (self.prod / "gateway-go").mkdir()

        proc = self.invoke(env=self.env(EMBEDDING_CURL_RC="7"))

        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertNotIn("code_search semantic index refresh", proc.stdout)
        self.assertNotIn("go cwd=", self.calls())

    def test_unreachable_health_check_fails_fast_instead_of_spinning(self) -> None:
        # Regression: deploy.sh's health wait is bounded by wall clock, and the
        # fixture's `sleep` is a stub. With the production 510s default that
        # combination burned ~350 forks/sec until the caller's subprocess timeout
        # killed it, surfacing as an opaque TimeoutExpired in whichever test the
        # blip landed on. A never-healthy gateway must terminate on its own, with
        # the script's own error, well inside the suite's timeout.
        start = time.monotonic()
        proc = self.invoke(
            env=self.env(SS_LISTEN="0", DENEB_DEPLOY_RESTART_WAIT_SEC="1"),
        )
        elapsed = time.monotonic() - start

        self.assertEqual(proc.returncode, 1)
        self.assertIn("gateway service did not become healthy", proc.stderr)
        self.assertLess(elapsed, 8, f"health wait did not stay bounded ({elapsed:.1f}s)")

    def test_wildcard_listen_address_is_normalized_to_loopback(self) -> None:
        proc = self.invoke(env=self.env(SS_ADDRESS="0.0.0.0:18789"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("curl -sf http://127.0.0.1:18789/health", self.calls())


if __name__ == "__main__":
    unittest.main()
