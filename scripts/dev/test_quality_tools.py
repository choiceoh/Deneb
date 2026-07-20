"""Persistence, sampling, evaluator, and reporting tests for quality/reproduce tools."""

from __future__ import annotations

import asyncio
import contextlib
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from test_support import invoke_main, load_script

quality = load_script("scripts/dev/quality-test.py")
reproduce = load_script("scripts/dev/reproduce.py")


def capture(
    text="충분히 의미 있는 한국어 응답입니다.",
    *,
    latency_ms=250,
    ok=True,
    state="done",
    errors=None,
    events=None,
    deltas=None,
    tool_starts=None,
    tool_results=None,
    token_usage=None,
):
    """Build the shared native ChatCapture with precise timing and events."""
    return quality.ChatCapture(
        reply_text=text,
        start_time=10.0,
        end_time=10.0 + latency_ms / 1000,
        final_response={"ok": ok, "payload": {"state": state}},
        errors=list(errors or []),
        events=list(events or []),
        deltas=list(deltas or []),
        tool_starts=list(tool_starts or []),
        tool_results=list(tool_results or []),
        token_usage_data=dict(token_usage or {}),
    )


def result(name, passed, score, checks=(True,)):
    item = quality.QualityResult(name=name, passed=passed, score=score, latency_ms=12.7)
    for index, check_passed in enumerate(checks):
        item.add_check(f"check-{index}", check_passed, f"detail-{index}")
    return item


class QualityDataClassTests(unittest.TestCase):
    def test_result_summary_counts_checks_and_formats_status_score_latency(self) -> None:
        item = result("daily-greeting", True, 2 / 3, (True, False, True))
        self.assertEqual(
            item.summary(),
            "[PASS] daily-greeting (2/3 checks, score=67%, 13ms)",
        )

    def test_apex_state_labels_and_pass_rate_cover_unseen_skip_pass_fail(self) -> None:
        unseen = quality.ApexCaseState(name="unseen")
        self.assertIsNone(unseen.pass_rate)
        self.assertEqual(unseen.current_label, "skip")

        passing = quality.ApexCaseState(name="pass", current_state=1, outcomes=[1, 0, 1])
        failing = quality.ApexCaseState(name="fail", current_state=0, outcomes=[0])
        self.assertAlmostEqual(passing.pass_rate, 2 / 3)
        self.assertEqual(passing.current_label, "pass")
        self.assertEqual(failing.current_label, "fail")


class ResultStoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.db_path = Path(self.tmp.name) / "nested/results.db"
        self.store = quality.ResultStore(self.db_path)
        self.addCleanup(self.store.close)

    @staticmethod
    def metadata(label):
        return {
            "timestamp": f"2026-07-11T00:00:0{label}Z",
            "model": f"model-{label}",
            "scenario": "core",
            "git_branch": "codex/health",
            "git_commit": f"abc00{label}",
            "gateway_version": "v1",
            "duration_ms": 123.4,
        }

    def test_record_round_trip_preserves_aggregates_json_and_disk_durability(self) -> None:
        passed = result("daily-pass", True, 1.0, (True, True))
        passed.tool_calls = ["health", "read"]
        failed = result("code-fail", False, 0.5, (True, False))
        failed.errors = ["provider unavailable"]
        run_id = self.store.record_run([passed, failed], self.metadata("1"))

        self.assertEqual(run_id, 1)
        rows = self.store.list_runs()
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["total_tests"], 2)
        self.assertEqual(rows[0]["passed_tests"], 1)
        self.assertAlmostEqual(rows[0]["overall_score"], 0.75)
        self.assertEqual(rows[0]["all_passed"], 0)

        detail = self.store.get_run_detail(run_id)
        self.assertEqual(detail["total_checks"], 4)
        self.assertEqual(detail["passed_checks"], 3)
        self.assertEqual(json.loads(detail["tests"][0]["tools_used"]), ["health", "read"])
        self.assertEqual(json.loads(detail["tests"][1]["errors"]), ["provider unavailable"])
        self.assertEqual(detail["tests"][0]["latency_ms"], 13.0)

        self.store.close()
        reopened = quality.ResultStore(self.db_path)
        try:
            self.assertEqual(reopened.get_run_detail(run_id)["model"], "model-1")
            self.assertEqual(reopened.list_runs(limit=1)[0]["run_id"], run_id)
        finally:
            reopened.close()

    def test_compare_detects_only_pass_fail_transitions_and_missing_runs(self) -> None:
        run_a = self.store.record_run([
            result("case-regressed", True, 1.0),
            result("case-improved", False, 0.2),
            result("case-stable", True, 0.9),
        ], self.metadata("1"))
        run_b = self.store.record_run([
            result("case-regressed", False, 0.4),
            result("case-improved", True, 0.8),
            result("case-stable", True, 0.7),
        ], self.metadata("2"))

        diff = self.store.compare_runs(run_a, run_b)
        self.assertEqual(diff["regressions"], [
            {"name": "case-regressed", "score_a": 1.0, "score_b": 0.4},
        ])
        self.assertEqual(diff["improvements"], [
            {"name": "case-improved", "score_a": 0.2, "score_b": 0.8},
        ])
        self.assertEqual(self.store.compare_runs(999, run_b), {"error": "Run #999 not found"})
        self.assertEqual(self.store.compare_runs(run_a, 999), {"error": "Run #999 not found"})

    def test_when_trend_is_newest_first_and_limit_is_enforced(self) -> None:
        for index, score in enumerate((0.2, 0.5, 0.9), 1):
            self.store.record_run([result("daily-trend", score >= 0.5, score)], self.metadata(str(index)))
        trend = self.store.test_trend("daily-trend", limit=2)
        self.assertEqual([row["run_id"] for row in trend], [3, 2])
        self.assertEqual([row["score"] for row in trend], [0.9, 0.5])
        self.assertEqual(self.store.test_trend("missing"), [])

    def test_apex_case_states_classify_easy_hard_mixed_and_latest_ignores(self) -> None:
        self.store.record_run([
            result("easy", True, 1.0),
            result("hard", False, 0.0),
            result("mixed", True, 0.8),
            result("skipped-latest", True, 0.7),
        ], self.metadata("1"))
        self.store.record_run([
            result("easy", True, 0.9),
            result("hard", False, 0.1),
            result("mixed", False, 0.3),
        ], self.metadata("2"))

        definitions = [
            {"name": "easy", "cat": "daily"},
            {"name": "hard", "cat": "code"},
            {"name": "mixed", "cat": "edge"},
            {"name": "skipped-latest", "cat": "daily"},
            {"name": "unseen", "cat": "new"},
            {"cat": "invalid"},
        ]
        states = self.store.apex_case_states(definitions, lookback=5)
        self.assertEqual(states["easy"].tier, "easy")
        self.assertEqual(states["easy"].current_state, 1)
        self.assertEqual(states["hard"].tier, "hard")
        self.assertEqual(states["hard"].current_state, 0)
        self.assertEqual(states["mixed"].tier, "mixed")
        self.assertEqual(states["mixed"].outcomes, [0, 1])
        self.assertIsNone(states["skipped-latest"].current_state)
        self.assertEqual(states["unseen"].tier, "unseen")
        self.assertNotIn("", states)


class ApexSelectionTests(unittest.TestCase):
    @staticmethod
    def fixture():
        tests = [{"name": name, "cat": "fixture"} for name in (
            "required", "mixed-pass", "mixed-fail", "easy", "hard", "unseen"
        )]
        states = {
            "required": quality.ApexCaseState(
                name="required", tier="mixed", current_state=None, outcomes=[1, 0]
            ),
            "mixed-pass": quality.ApexCaseState(
                name="mixed-pass", tier="mixed", current_state=1, outcomes=[1, 0]
            ),
            "mixed-fail": quality.ApexCaseState(
                name="mixed-fail", tier="mixed", current_state=0, outcomes=[0, 1]
            ),
            "easy": quality.ApexCaseState(name="easy", tier="easy", current_state=1, outcomes=[1, 1]),
            "hard": quality.ApexCaseState(name="hard", tier="hard", current_state=0, outcomes=[0, 0]),
            "unseen": quality.ApexCaseState(name="unseen", tier="unseen"),
        }
        return tests, states

    def test_full_or_nonpositive_budget_disables_sampling_without_reordering(self) -> None:
        tests, states = self.fixture()
        for budget in (0, len(tests), len(tests) + 1):
            with self.subTest(budget=budget):
                selected, plan = quality.select_apex_tests(tests, states, budget)
                self.assertEqual(selected, tests)
                self.assertFalse(plan["enabled"])
                self.assertEqual(plan["reason"], "budget covers the full scenario")

    def test_frontier_selection_is_unique_seeded_and_always_preserves_required(self) -> None:
        tests, states = self.fixture()
        first, plan = quality.select_apex_tests(
            tests, states, budget=4, anchor_ratio=1.0, seed=17
        )
        second, second_plan = quality.select_apex_tests(
            tests, states, budget=4, anchor_ratio=1.0, seed=17
        )
        names = [item["name"] for item in first]
        self.assertEqual(first, second)
        self.assertEqual(plan, second_plan)
        self.assertEqual(len(names), len(set(names)))
        self.assertEqual(len(names), 4)
        self.assertEqual(names[0], "required")
        self.assertEqual(plan["required_selected"], 1)
        self.assertEqual(plan["selected"], 4)
        self.assertEqual(list(plan["bucket_counts"]), sorted(plan["bucket_counts"]))
        self.assertIn("mixed-fail", plan["mutation_frontier"])

    def test_when_pass_rate_prefers_current_states_then_falls_back_to_history(self) -> None:
        current = [
            quality.ApexCaseState(name="a", current_state=1, outcomes=[0]),
            quality.ApexCaseState(name="b", current_state=0, outcomes=[1]),
        ]
        history = [
            quality.ApexCaseState(name="a", outcomes=[1]),
            quality.ApexCaseState(name="b", outcomes=[1]),
        ]
        self.assertEqual(quality._apex_pass_rate(current), 0.5)
        self.assertEqual(quality._apex_pass_rate(history), 1.0)
        self.assertEqual(quality._apex_pass_rate([]), 0.0)

    def test_plan_render_is_deterministic_and_includes_selected_categories(self) -> None:
        tests, states = self.fixture()
        selected, plan = quality.select_apex_tests(tests, states, budget=3, seed=2)
        output = io.StringIO()
        quality.print_apex_plan(plan, selected, file=output)
        text = output.getvalue()
        self.assertIn("APEX frontier plan", text)
        self.assertIn("selected: 3/3", text)
        self.assertIn("buckets:", text)
        for item in selected:
            self.assertIn(f"- {item['name']} [fixture]", text)


class QualityEvaluatorTests(unittest.TestCase):
    def test_tool_integrity_distinguishes_skip_orphan_error_and_success(self) -> None:
        self.assertEqual(
            quality.check_no_hallucinated_tool(capture()),
            (True, "tool events unsupported on native sync surface (skipped)"),
        )
        orphan = capture(tool_starts=[{"name": "read"}])
        self.assertEqual(quality.check_no_hallucinated_tool(orphan)[0], False)
        errored = capture(
            tool_starts=[{"name": "read"}],
            tool_results=[{"name": "read", "isError": True}],
        )
        self.assertIn("tool errors", quality.check_no_hallucinated_tool(errored)[1])
        good = capture(
            tool_starts=[{"name": "read"}],
            tool_results=[{"name": "read", "isError": False}],
        )
        self.assertEqual(quality.check_no_hallucinated_tool(good), (True, "1 tools OK"))

    def test_streaming_contract_handles_empty_direct_and_chat_event_paths(self) -> None:
        self.assertEqual(quality.check_streaming_flow(capture()), (False, "no events received"))
        direct = capture(events=[{"event": "status"}], ok=True)
        self.assertEqual(quality.check_streaming_flow(direct), (True, "direct response (no streaming)"))
        streamed = capture(events=[{"event": "chat.delta"}], deltas=[{"text": "x"}])
        self.assertEqual(quality.check_streaming_flow(streamed), (True, "1 chat events, 1 deltas"))

    def test_when_parameter_checks_cover_text_list_length_tokens_and_growth_boundaries(self) -> None:
        cap = capture(
            "Alpha\n- one\n- two",
            token_usage={
                "inputTokens": 100,
                "_turn_tokens": [{"input": 0}, {"input": 2}],
            },
        )
        cases = [
            ({"contains": "alpha"}, True),
            ({"contains_any": ["missing", "two"]}, True),
            ({"not_contains": "forbidden"}, True),
            ({"min_length": len(cap.reply_text)}, True),
            ({"has_list": 2}, True),
            ({"max_input_tokens": 100}, True),
            ({"token_bounded": 2}, True),
            ({"token_bounded": 1.9}, False),
        ]
        for definition, expected in cases:
            with self.subTest(definition=definition):
                self.assertEqual(quality.evaluate_check(definition, cap)[1], expected)

    def test_unknown_check_shapes_fail_closed_with_actionable_detail(self) -> None:
        cap = capture()
        self.assertEqual(
            quality.evaluate_check("not-a-check", cap),
            ("not-a-check", False, "unknown simple check: not-a-check"),
        )
        self.assertEqual(
            quality.evaluate_check(42, cap),
            ("unknown", False, "unknown check type: 42"),
        )

    def test_profiles_timeouts_critical_and_yaml_loader_preserve_precedence(self) -> None:
        profiles = {"base": ["rpc_success", "has_reply"]}
        defaults = {"daily": {"profile": "base", "timeout": 7, "critical": "all"}}
        definition = {"name": "daily-one", "cat": "daily", "chk": ["korean"]}
        self.assertEqual(
            quality.resolve_checks(definition, profiles, defaults),
            ["rpc_success", "has_reply", "korean"],
        )
        self.assertEqual(quality.get_timeout(definition, defaults), 7.0)
        self.assertEqual(quality.get_timeout({**definition, "timeout": "3.5"}, defaults), 3.5)
        self.assertEqual(quality.get_critical(definition, defaults), "all")
        self.assertEqual(quality.get_critical({**definition, "critical": 2}, defaults), 2)

        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "tests.yaml"
            path.write_text(
                "profiles: {base: [rpc_success]}\n"
                "category_defaults: {daily: {timeout: 5}}\n"
                "tests: [{name: daily-one, cat: daily}]\n",
                encoding="utf-8",
            )
            yaml_adapter = mock.Mock()
            yaml_adapter.safe_load.return_value = {
                "profiles": {"base": ["rpc_success"]},
                "category_defaults": {"daily": {"timeout": 5}},
                "tests": [{"name": "daily-one", "cat": "daily"}],
            }
            with mock.patch.object(quality, "yaml", yaml_adapter):
                loaded = quality.load_tests(path)
            yaml_adapter.safe_load.assert_called_once()
        self.assertEqual(loaded[0], {"base": ["rpc_success"]})
        self.assertEqual(loaded[1], {"daily": {"timeout": 5}})
        self.assertEqual(loaded[2][0]["name"], "daily-one")

        with mock.patch.object(quality, "yaml", None):
            with self.assertRaisesRegex(RuntimeError, "PyYAML is required"):
                quality.load_tests(Path("unused"))

    def test_json_returns_has_stable_rounding_optional_fields_and_exit_status(self) -> None:
        passing = result("daily-pass", True, 0.8765, (True, True))
        passing.tool_calls = ["health"]
        failing = result("code-fail", False, 0.3333, (True, False))
        failing.errors = ["failure"]
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            rc = quality.print_report([passing, failing], json_output=True)
        payload = json.loads(stdout.getvalue())
        self.assertEqual(rc, 1)
        self.assertEqual(payload["total_tests"], 2)
        self.assertEqual(payload["passed_checks"], 3)
        self.assertEqual(payload["overall_score"], 0.605)
        self.assertEqual(payload["tests"][0]["score"], 0.876)
        self.assertEqual(payload["tests"][0]["tools"], ["health"])
        self.assertEqual(payload["tests"][1]["errors"], ["failure"])


class ReproduceCheckTests(unittest.TestCase):
    def test_check_string_and_turn_pass_ignore_skipped_assertions(self) -> None:
        passed = reproduce.CheckResult("pass", True, "ok")
        failed = reproduce.CheckResult("fail", False)
        skipped = reproduce.CheckResult("tool", True, "unsupported", skipped=True)
        self.assertEqual(str(passed), "  ✓ pass — ok")
        self.assertEqual(str(failed), "  ✗ fail")
        self.assertEqual(str(skipped), "  ~ tool — unsupported")

        turn = reproduce.TurnResult(1, "message", capture(), [passed, skipped])
        self.assertTrue(turn.passed)
        self.assertEqual(turn.failed_checks, [])
        turn.checks.append(failed)
        self.assertFalse(turn.passed)
        self.assertEqual(turn.failed_checks, [failed])

    def test_when_regex_positive_negative_context_and_length_checks(self) -> None:
        self.assertTrue(reproduce.check_expect("Alpha\nBeta", "alpha.*beta").passed)
        self.assertFalse(reproduce.check_expect("Alpha", "missing").passed)
        self.assertTrue(reproduce.check_expect_not("Alpha", "missing").passed)
        self.assertFalse(reproduce.check_expect_not("Alpha", "alpha").passed)
        self.assertTrue(reproduce.check_context_carryover(capture("이름은 홍길동"), "홍길동").passed)
        self.assertTrue(reproduce.check_min_length("  abc  ", 3).passed)
        self.assertFalse(reproduce.check_min_length("  abc  ", 4).passed)

    def test_first_token_no_error_expected_error_and_streaming_boundaries(self) -> None:
        no_delta = capture()
        self.assertFalse(reproduce.check_first_token(no_delta, 100).passed)
        with_delta = capture(deltas=[{"ts": 10.1}])
        self.assertTrue(reproduce.check_first_token(with_delta, 100).passed)
        self.assertFalse(reproduce.check_first_token(with_delta, 99).passed)

        self.assertTrue(reproduce.check_no_error(capture(ok=False, state="done")).passed)
        self.assertFalse(reproduce.check_no_error(capture(errors=["bad"])).passed)
        self.assertFalse(reproduce.check_expect_error(capture()).passed)
        self.assertTrue(reproduce.check_expect_error(capture(state="error")).passed)
        self.assertFalse(reproduce.check_streaming(capture(events=[{}, {}])).passed)
        self.assertTrue(reproduce.check_streaming(capture(events=[{}, {}, {}])).passed)

    def test_when_tool_checks_are_explicitly_skipped_not_false_successes(self) -> None:
        cap = capture()
        checks = [
            reproduce.check_expect_tool(cap, "read"),
            reproduce.check_expect_no_tool(cap),
            reproduce.check_tool_success(cap),
        ]
        self.assertTrue(all(item.passed and item.skipped for item in checks))
        self.assertTrue(all("unsupported on native sync surface" in item.detail for item in checks))

    def test_turn_report_excludes_skips_from_denominator_and_returns_failure(self) -> None:
        turn = reproduce.TurnResult(
            1,
            "message",
            capture("reply"),
            [
                reproduce.CheckResult("ok", True),
                reproduce.CheckResult("skip", True, skipped=True),
                reproduce.CheckResult("bad", False),
            ],
        )
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            rc = reproduce.print_turn_result(turn)
        self.assertEqual(rc, 1)
        self.assertIn("FAILED — 1/2 checks (1 skipped)", stdout.getvalue())

    def test_prerequisite_failure_returns_one_without_constructing_client(self) -> None:
        args = SimpleNamespace(port=18790)
        with mock.patch.object(reproduce, "check_prerequisites", return_value=(False, "token missing")):
            with mock.patch.object(reproduce, "GatewayClient") as client:
                stdout = io.StringIO()
                with contextlib.redirect_stdout(stdout):
                    rc = asyncio.run(reproduce.cmd_chat_check(args))
        self.assertEqual(rc, 1)
        self.assertIn("Native chat prerequisites not met: token missing", stdout.getvalue())
        client.assert_not_called()


class QualityRunnerTests(unittest.TestCase):
    def test_when_chat_runner_scores_all_checks_but_critical_prefix_controls_pass(self) -> None:
        cap = capture("required phrase with enough substance")

        class Client:
            def __init__(self):
                self.calls = []

            async def chat(self, message, timeout):
                self.calls.append((message, timeout))
                return cap

        client = Client()
        definition = {
            "name": "daily-critical",
            "cat": "daily",
            "msg": "question",
            "profile": "fixture",
            "critical": 2,
        }
        profiles = {
            "fixture": ["rpc_success", {"contains": "required"}, {"contains": "missing"}],
        }
        item = asyncio.run(quality.run_chat_test(client, definition, profiles, {}))
        self.assertEqual(client.calls, [("question", quality.TIMEOUT_CHAT)])
        self.assertEqual([passed for _name, passed, _detail in item.checks], [True, True, False])
        self.assertAlmostEqual(item.score, 2 / 3)
        self.assertTrue(item.passed)
        self.assertEqual(cap._bench_message, "question")

    def test_when_multiturn_runner_reuses_session_tracks_tokens_and_delays_only_between_repeats(self) -> None:
        captures = [
            capture("first", token_usage={"inputTokens": 10, "outputTokens": 2}),
            capture("second", token_usage={"inputTokens": 15, "outputTokens": 3}),
            capture("final required", token_usage={"inputTokens": 20, "outputTokens": 4}),
        ]

        class Client:
            def __init__(self):
                self.calls = []

            async def create_session(self):
                return "session-key"

            async def chat(self, message, session_key, timeout):
                self.calls.append((message, session_key, timeout))
                return captures[len(self.calls) - 1]

        definition = {
            "name": "context-repeat",
            "turns": [
                {"msg": "repeat me", "repeat": 2, "delay": 0.25},
                {"msg": "final question"},
            ],
            "chk": [{"contains": "required"}, {"token_bounded": 2.0}],
            "critical": "all",
        }
        client = Client()
        with mock.patch.object(quality.asyncio, "sleep", new=mock.AsyncMock()) as sleep:
            item = asyncio.run(quality.run_multiturn_test(client, definition, {}, {}))
        self.assertEqual([call[0] for call in client.calls], ["repeat me", "repeat me", "final question"])
        self.assertTrue(all(call[1] == "session-key" for call in client.calls))
        sleep.assert_awaited_once_with(0.25)
        self.assertTrue(item.passed)
        self.assertEqual(item.token_usage["_turn_tokens"], [
            {"turn": 1, "input": 10, "output": 2},
            {"turn": 2, "input": 15, "output": 3},
            {"turn": 3, "input": 20, "output": 4},
        ])
        self.assertEqual(captures[-1]._bench_message, "final question")

    def test_runner_returns_actionable_failed_check_for_missing_input_or_rpc_error(self) -> None:
        class FailingClient:
            async def chat(self, *_args, **_kwargs):
                raise TimeoutError("chat timed out")

        missing = asyncio.run(quality.run_chat_test(
            FailingClient(), {"name": "missing"}, {}, {}
        ))
        self.assertEqual(missing.checks, [("has_message", False, "no message defined")])

        failed = asyncio.run(quality.run_chat_test(
            FailingClient(), {"name": "timeout", "msg": "hello"}, {}, {}
        ))
        self.assertEqual(failed.checks, [("rpc_success", False, "chat timed out")])
        self.assertFalse(failed.passed)


class HealthRunnerTests(unittest.TestCase):
    class Response:
        def __init__(self, payload: dict) -> None:
            self.payload = payload

        def __enter__(self):
            return self

        def __exit__(self, *_args) -> None:
            return None

        def read(self) -> bytes:
            return json.dumps(self.payload).encode("utf-8")

    def test_when_rpc_health_checks_transport_status_and_latency(self) -> None:
        class Client:
            def __init__(self) -> None:
                self.methods = []

            async def rpc(self, method):
                self.methods.append(method)
                return {"ok": True, "payload": {"status": "ok"}}

        client = Client()
        with mock.patch.object(quality.time, "time", side_effect=[100.0, 100.125]):
            item = asyncio.run(quality.run_health_test(
                client,
                {"name": "health-rpc", "health": "rpc"},
            ))

        self.assertEqual(client.methods, ["health"])
        self.assertEqual(item.name, "health-rpc")
        self.assertAlmostEqual(item.latency_ms, 125.0)
        self.assertEqual(
            [(name, passed) for name, passed, _detail in item.checks],
            [("rpc_success", True), ("status_ok", True), ("latency", True)],
        )
        self.assertEqual(item.score, 1.0)
        self.assertTrue(item.passed)

    def test_rpc_and_version_failures_remain_visible_in_score(self) -> None:
        class Client:
            async def rpc(self, _method):
                return {
                    "ok": False,
                    "error": "gateway unavailable",
                    "payload": {"status": "starting"},
                }

        with mock.patch.object(
            quality.time,
            "time",
            side_effect=[10.0, 10.6, 20.0, 20.1],
        ):
            rpc_item = asyncio.run(quality.run_health_test(
                Client(),
                {"name": "health-rpc-bad", "health": "rpc"},
            ))
            version_item = asyncio.run(quality.run_health_test(
                Client(),
                {"name": "health-version-bad", "health": "version"},
            ))

        self.assertEqual(
            [passed for _name, passed, _detail in rpc_item.checks],
            [False, False, False],
        )
        self.assertIn("gateway unavailable", rpc_item.checks[0][2])
        self.assertEqual(rpc_item.score, 0.0)
        self.assertFalse(rpc_item.passed)
        self.assertEqual(
            [(name, passed) for name, passed, _detail in version_item.checks],
            [("has_version", False), ("latency", True)],
        )
        self.assertEqual(version_item.score, 0.5)
        self.assertFalse(version_item.passed)

    def test_http_and_core_health_parse_exact_wire_fields(self) -> None:
        client = SimpleNamespace(host="127.0.0.1", port=18790)
        responses = [
            self.Response({"status": "ok", "version": "v1.2.3", "uptime": "4m"}),
            self.Response({"subsystems": {"core": "go"}}),
        ]
        with mock.patch("urllib.request.urlopen", side_effect=responses) as urlopen:
            with mock.patch.object(
                quality.time,
                "time",
                side_effect=[1.0, 1.05, 2.0, 2.075],
            ):
                http_item = asyncio.run(quality.run_health_test(
                    client,
                    {"name": "health-http", "health": "http"},
                ))
                core_item = asyncio.run(quality.run_health_test(
                    client,
                    {"name": "health-core", "health": "core"},
                ))

        self.assertEqual(urlopen.call_count, 2)
        for call in urlopen.call_args_list:
            self.assertEqual(call.args, ("http://127.0.0.1:18790/health",))
            self.assertEqual(call.kwargs, {"timeout": 5})
        self.assertEqual(
            [name for name, _passed, _detail in http_item.checks],
            ["http_ok", "has_version", "has_uptime", "latency"],
        )
        self.assertTrue(http_item.passed)
        self.assertEqual(http_item.score, 1.0)
        self.assertEqual(
            [(name, passed) for name, passed, _detail in core_item.checks],
            [("core_go", True), ("latency", True)],
        )
        self.assertTrue(core_item.passed)

    def test_http_exception_and_unknown_health_type_fail_closed(self) -> None:
        client = SimpleNamespace(host="127.0.0.1", port=18790)
        with mock.patch("urllib.request.urlopen", side_effect=OSError("connection refused")):
            failed = asyncio.run(quality.run_health_test(
                client,
                {"name": "health-http-down", "health": "http"},
            ))
        self.assertEqual(len(failed.checks), 1)
        self.assertEqual(failed.checks[0][0:2], ("http_ok", False))
        self.assertIn("connection refused", failed.checks[0][2])
        self.assertEqual(failed.score, 0.0)
        self.assertFalse(failed.passed)

        unknown = asyncio.run(quality.run_health_test(
            client,
            {"name": "health-unknown", "health": "socket"},
        ))
        self.assertEqual(
            unknown.checks,
            [("health_type", False, "unsupported health type: socket")],
        )
        self.assertEqual(unknown.score, 0.0)
        self.assertFalse(unknown.passed)

    def test_when_dispatch_selects_health_multiturn_or_single_turn_runner(self) -> None:
        client = object()
        health = quality.QualityResult("health")
        multiturn = quality.QualityResult("multiturn")
        single = quality.QualityResult("single")
        with (
            mock.patch.object(
                quality,
                "run_health_test",
                new=mock.AsyncMock(return_value=health),
            ) as health_runner,
            mock.patch.object(
                quality,
                "run_multiturn_test",
                new=mock.AsyncMock(return_value=multiturn),
            ) as multiturn_runner,
            mock.patch.object(
                quality,
                "run_chat_test",
                new=mock.AsyncMock(return_value=single),
            ) as chat_runner,
        ):
            actual_health = asyncio.run(quality.run_test(
                client, {"name": "health", "health": "rpc"}, {}, {}
            ))
            actual_multiturn = asyncio.run(quality.run_test(
                client, {"name": "multiturn", "turns": []}, {}, {}
            ))
            actual_single = asyncio.run(quality.run_test(
                client, {"name": "single", "msg": "hello"}, {}, {}
            ))

        self.assertIs(actual_health, health)
        self.assertIs(actual_multiturn, multiturn)
        self.assertIs(actual_single, single)
        health_runner.assert_awaited_once_with(
            client,
            {"name": "health", "health": "rpc"},
        )
        multiturn_runner.assert_awaited_once_with(
            client,
            {"name": "multiturn", "turns": []},
            {},
            {},
        )
        chat_runner.assert_awaited_once_with(
            client,
            {"name": "single", "msg": "hello"},
            {},
            {},
        )


class EntryPointTests(unittest.TestCase):
    def test_quality_and_reproduce_help_preserves_existing_cli_surface(self) -> None:
        expectations = {
            quality.__file__: ("--scenario", "--sample", "--record", "--history", "--compare"),
            reproduce.__file__: ("chat-check", "multi-chat", "tool-check", "--timeout"),
        }
        for script, options in expectations.items():
            with self.subTest(script=script):
                proc = subprocess.run(
                    [sys.executable, script, "--help"],
                    capture_output=True,
                    text=True,
                    check=False,
                )
                self.assertEqual(proc.returncode, 0)
                for option in options:
                    self.assertIn(option, proc.stdout)

    def test_reproduce_without_command_prints_help_and_exits_one(self) -> None:
        rc, stdout, stderr = invoke_main(reproduce, [])
        self.assertEqual((rc, stderr), (1, ""))
        self.assertIn("chat-check", stdout)
        self.assertIn("tool-check", stdout)


if __name__ == "__main__":
    unittest.main()
