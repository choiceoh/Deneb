"""Deterministic scoring, MIME, IMAP, wire, and output tests for mail-bench."""

from __future__ import annotations

import io
import json
import subprocess
import sys
import tempfile
import unittest
from email.header import Header
from email.message import EmailMessage
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from test_support import JSONResponse, invoke_main, load_script

mail_bench = load_script("scripts/dev/mail-bench.py")


class TokenAndTransportTests(unittest.TestCase):
    def test_environment_token_wins_without_reading_config(self) -> None:
        with mock.patch.dict(mail_bench.os.environ, {"WORMHOLE_TOKEN": "from-env"}, clear=True):
            with mock.patch("builtins.open") as open_file:
                self.assertEqual(mail_bench.wormhole_token(), "from-env")
        open_file.assert_not_called()

    def test_config_token_and_missing_config_have_stable_fallbacks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            config = home / ".wormhole/config.json"
            config.parent.mkdir()
            config.write_text('{"token":"from-file"}', encoding="utf-8")
            with mock.patch.dict(mail_bench.os.environ, {"HOME": str(home)}, clear=True):
                self.assertEqual(mail_bench.wormhole_token(), "from-file")
            config.unlink()
            with mock.patch.dict(mail_bench.os.environ, {"HOME": str(home)}, clear=True):
                self.assertEqual(mail_bench.wormhole_token(), "")

    def test_llm_request_and_usage_response_preserve_wire_contract(self) -> None:
        captured = {}

        def urlopen(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return JSONResponse({
                "choices": [{"message": {"content": "분석 결과"}}],
                "usage": {"prompt_tokens": 321, "completion_tokens": 45},
            })

        with mock.patch.object(mail_bench.urllib.request, "urlopen", side_effect=urlopen):
            with mock.patch.object(mail_bench.time, "monotonic", side_effect=[5.0, 5.125]):
                result = mail_bench.call_llm(
                    "http://wormhole/v1/",
                    "secret",
                    "model-a",
                    "prompt body",
                    1536,
                    27,
                )

        self.assertEqual(result, ("분석 결과", 125, 321, 45))
        request = captured["request"]
        self.assertEqual(request.full_url, "http://wormhole/v1/chat/completions")
        self.assertEqual(request.get_header("Authorization"), "Bearer secret")
        self.assertEqual(request.get_header("Content-type"), "application/json")
        self.assertEqual(captured["timeout"], 27)
        payload = json.loads(request.data)
        self.assertEqual(payload["model"], "model-a")
        self.assertEqual(payload["temperature"], 0)
        self.assertEqual(payload["max_tokens"], 1536)
        self.assertEqual(payload["messages"], [
            {"role": "system", "content": mail_bench.SYSTEM},
            {"role": "user", "content": "prompt body"},
        ])

    def test_missing_usage_and_content_are_returned_as_empty_values(self) -> None:
        response = {"choices": [{"message": {}}]}
        with mock.patch.object(
            mail_bench.urllib.request,
            "urlopen",
            return_value=JSONResponse(response),
        ):
            with mock.patch.object(mail_bench.time, "monotonic", side_effect=[1.0, 1.0]):
                self.assertEqual(
                    mail_bench.call_llm("http://x", "", "m", "p", 1, 1),
                    ("", 0, None, None),
                )


class PromptAndProbeTests(unittest.TestCase):
    def test_when_prompt_section_order_and_optional_anchor_are_deterministic(self) -> None:
        prompt = mail_bench.build_prompt("MAIL", "THREAD", "MEMORY", "ANCHOR")
        self.assertTrue(prompt.startswith(mail_bench.DEFAULT_PROMPT))
        positions = [
            prompt.index("## 이메일 원문\nMAIL"),
            prompt.index("## 당사자 앵커 (헤더 기반 결정적 조립)\nANCHOR"),
            prompt.index("## 이전 메일 맥락\nTHREAD"),
            prompt.index("## 관련 기억\nMEMORY"),
        ]
        self.assertEqual(positions, sorted(positions))
        self.assertNotIn("당사자 앵커", mail_bench.build_prompt("M", "T", "R"))

    def test_when_probe_requires_every_all_group_and_one_any_variant(self) -> None:
        fixture = {
            "probes": [
                {
                    "name": "complete",
                    "all": [["12,200", "12200"], ["9일"]],
                    "any": ["적자", "손실"],
                    "bad": ["위험 없음"],
                },
                {"name": "all-only", "all": [["기한"]], "any": [], "bad": []},
            ]
        }
        hits, detail = mail_bench.score_probes(fixture, "12200 아래라 적자이며 9일 지연, 기한 확인")
        self.assertEqual(hits, 2)
        self.assertEqual(detail, [("complete", True), ("all-only", True)])

        hits, detail = mail_bench.score_probes(fixture, "12200 아래라 적자이며 9일 지연. 위험 없음. 기한")
        self.assertEqual(hits, 1)
        self.assertEqual(detail[0], ("complete", False))

    def test_stability_separates_a_probe_passed_once_from_one_passed_every_run(self) -> None:
        def run(mail, model, detail):
            return {"mail": mail, "model": model, "detail": detail}

        results = [
            run("m1", "a", [("always", True), ("flaky", True), ("never", False)]),
            run("m1", "a", [("always", True), ("flaky", False), ("never", False)]),
            run("m2", "a", [("other", True)]),
            run("m2", "a", [("other", True)]),
            run("m1", "b", [("always", False)]),
        ]

        stats = mail_bench.stability(results)

        self.assertEqual(
            stats["a"],
            {"reps": 2, "probes": 4, "available": 3, "robust": 2, "flaky": 1},
        )
        self.assertEqual(stats["b"]["available"], 0)
        self.assertEqual(stats["b"]["reps"], 1)

    def test_stability_keys_probes_per_mail_so_shared_names_do_not_merge(self) -> None:
        # Two mails can plant a probe under the same name; merging them would let
        # one mail's pass mask the other's failure.
        results = [
            {"mail": "m1", "model": "a", "detail": [("dup", True)]},
            {"mail": "m2", "model": "a", "detail": [("dup", False)]},
        ]

        stats = mail_bench.stability(results)

        self.assertEqual(stats["a"]["probes"], 2)
        self.assertEqual(stats["a"]["available"], 1)
        self.assertEqual(stats["a"]["robust"], 1)

    def test_stability_report_is_silent_for_a_single_rep(self) -> None:
        # With reps=1 availability and robustness both equal TRAP_TOTAL's ratio,
        # so printing them would add a line that carries no information.
        lines: list[str] = []
        mail_bench.print_stability(
            {"a": {"reps": 1, "probes": 3, "available": 2, "robust": 2, "flaky": 0}},
            out=lines.append,
        )
        self.assertEqual(lines, [])

        mail_bench.print_stability(
            {"a": {"reps": 3, "probes": 4, "available": 3, "robust": 2, "flaky": 1}},
            out=lines.append,
        )
        self.assertEqual(
            lines,
            ["TRAP_STABILITY model=a reps=3 probes=4 available=3(75.0%) robust=2(50.0%) flaky=1"],
        )

    def test_hit_any_is_literal_and_empty_variants_do_not_match(self) -> None:
        self.assertTrue(mail_bench.hit_any("Alpha beta", ["beta", "gamma"]))
        self.assertFalse(mail_bench.hit_any("Alpha beta", ["Beta"]))
        self.assertFalse(mail_bench.hit_any("anything", []))


class HeaderAndBodyTests(unittest.TestCase):
    def test_encoded_header_decodes_multiple_fragments_with_replacement(self) -> None:
        encoded = str(Header("홍길동", "utf-8"))
        encoded = Header("홍길동", "utf-8").encode() + " <user@example.com>"
        self.assertEqual(mail_bench.decode_header(encoded), "홍길동 <user@example.com>")
        self.assertEqual(mail_bench.decode_header(None), "")

    def test_when_plain_part_wins_over_html_alternative(self) -> None:
        msg = EmailMessage()
        msg.set_content("plain body")
        msg.add_alternative("<p>html body</p>", subtype="html")
        self.assertEqual(mail_bench.body_text(msg).strip(), "plain body")

    def test_html_fallback_removes_script_style_tags_and_decodes_entities(self) -> None:
        msg = EmailMessage()
        msg.set_content(
            "<style>.x{color:red}</style><p>Hello&nbsp;<b>World</b></p>"
            "<script>bad()</script><div>Second</div>",
            subtype="html",
        )
        text = mail_bench.body_text(msg)
        self.assertNotIn("color:red", text)
        self.assertNotIn("bad()", text)
        self.assertNotIn("<b>", text)
        self.assertIn("Hello\xa0 World", text)
        self.assertIn("Second", text)

    def test_party_labels_handle_case_org_mapping_and_empty_headers(self) -> None:
        domains = {"topsolar.kr"}
        orgs = {"topsolar.kr": "탑솔라", "vendor.com": "Vendor"}
        self.assertEqual(
            mail_bench.side_label("User@TOPSOLAR.KR", domains, orgs),
            "우리 측(탑솔라)",
        )
        self.assertEqual(mail_bench.side_label("x@vendor.com", domains, orgs), "외부(Vendor)")
        self.assertEqual(mail_bench.side_label("not-an-address", domains, orgs), "외부(주소 불명)")
        self.assertEqual(mail_bench.format_party("", domains, orgs), [])

    def test_anchor_preserves_sender_recipient_cc_roles_and_rules(self) -> None:
        mail = {
            "from": "Vendor <sales@vendor.com>",
            "to": "우리팀 <ops@topsolar.kr>",
            "cc": "Other <other@example.net>",
        }
        anchor = mail_bench.build_anchor(mail, {"topsolar.kr"}, {"vendor.com": "판매사"})
        self.assertIn("- 보낸사람: Vendor <sales@vendor.com> — 외부(판매사)", anchor)
        self.assertIn("- 받는사람: 우리팀 <ops@topsolar.kr> — 우리 측(topsolar.kr)", anchor)
        self.assertIn("- 참조: Other <other@example.net> — 외부(example.net)", anchor)
        self.assertTrue(anchor.endswith(mail_bench.ANCHOR_RULES))

    def test_when_trap_anchor_extracts_only_sender_and_injects_known_recipient(self) -> None:
        args = SimpleNamespace(
            our_domain_set={"topsolar.kr"},
            org_map_dict={"vendor.com": "Vendor"},
        )
        anchor = mail_bench.build_anchor_from_text(
            "제목: test\n보낸사람: Partner <p@vendor.com>\n본문",
            args,
        )
        self.assertIn("Partner <p@vendor.com> — 외부(Vendor)", anchor)
        self.assertIn("우리 담당자 <us@topsolar.kr> — 우리 측(topsolar.kr)", anchor)


class IMAPTests(unittest.TestCase):
    @staticmethod
    def raw_message() -> bytes:
        msg = EmailMessage()
        msg["Subject"] = Header("테스트 제목", "utf-8").encode()
        msg["From"] = "Sender <sender@example.com>"
        msg["To"] = "Receiver <receiver@topsolar.kr>"
        msg["Cc"] = "Copy <copy@example.net>"
        msg["Date"] = "Fri, 3 Jul 2026 09:00:00 +0900"
        msg.set_content("line one\n\n\nline two and extra")
        return msg.as_bytes()

    def test_imap_env_prefers_environment_then_parses_systemd_fallback(self) -> None:
        with mock.patch.dict(mail_bench.os.environ, {"DENEB_ARCHIVE_IMAP_USER": "direct"}, clear=True):
            with mock.patch.object(mail_bench.subprocess, "run") as run:
                self.assertEqual(mail_bench.imap_env("DENEB_ARCHIVE_IMAP_USER"), "direct")
        run.assert_not_called()

        unit = 'Environment=DENEB_ARCHIVE_IMAP_USER="archive-user"\n'
        completed = subprocess.CompletedProcess([], 0, stdout=unit, stderr="")
        with mock.patch.dict(mail_bench.os.environ, {}, clear=True):
            with mock.patch.object(mail_bench.subprocess, "run", return_value=completed) as run:
                self.assertEqual(mail_bench.imap_env("DENEB_ARCHIVE_IMAP_USER"), "archive-user")
        self.assertEqual(run.call_args.kwargs["timeout"], 10)

        with mock.patch.dict(mail_bench.os.environ, {}, clear=True):
            with mock.patch.object(mail_bench.subprocess, "run", side_effect=TimeoutError):
                self.assertEqual(mail_bench.imap_env("DENEB_ARCHIVE_IMAP_USER"), "")

    def test_fetch_uses_uid_readonly_and_caps_normalized_body(self) -> None:
        raw = self.raw_message()

        class FakeIMAP:
            instance = None

            def __init__(self, host, port):
                self.host, self.port = host, port
                self.calls = []
                FakeIMAP.instance = self

            def login(self, user, password):
                self.calls.append(("login", user, password))

            def select(self, mailbox, readonly=False):
                self.calls.append(("select", mailbox, readonly))

            def uid(self, command, uid, query):
                self.calls.append(("uid", command, uid, query))
                return "OK", [(b"header", raw), b")"]

            def logout(self):
                self.calls.append(("logout",))

        env = {
            "DENEB_ARCHIVE_IMAP_ADDR": "mail.example:993",
            "DENEB_ARCHIVE_IMAP_USER": "user",
            "DENEB_ARCHIVE_IMAP_PASS": "pass",
        }
        with mock.patch.dict(mail_bench.os.environ, env, clear=True):
            with mock.patch.object(mail_bench.imaplib, "IMAP4_SSL", FakeIMAP):
                with mock.patch.object(mail_bench.imaplib, "IMAP4") as plain:
                    mails = mail_bench.fetch_mails([42], "Archive", body_cap=18)
        plain.assert_not_called()
        instance = FakeIMAP.instance
        self.assertEqual((instance.host, instance.port), ("mail.example", 993))
        self.assertIn(("select", "Archive", True), instance.calls)
        self.assertIn(("uid", "fetch", "42", "(BODY.PEEK[])"), instance.calls)
        self.assertEqual(instance.calls[-1], ("logout",))
        self.assertEqual(mails[0]["uid"], "42")
        self.assertEqual(mails[0]["subject"], "테스트 제목")
        self.assertIn("받는사람: Receiver", mails[0]["text"])
        body = mails[0]["text"].split("\n\n", 1)[1]
        self.assertLessEqual(len(body), 18)
        self.assertNotIn("\n\n\n", body)

    def test_missing_uid_is_a_clear_fatal_error(self) -> None:
        class MissingIMAP:
            def __init__(self, *_args):
                pass

            def login(self, *_args):
                pass

            def select(self, *_args, **_kwargs):
                pass

            def uid(self, *_args):
                return "NO", [None]

        env = {
            "DENEB_ARCHIVE_IMAP_ADDR": "127.0.0.1:1143",
            "DENEB_ARCHIVE_IMAP_USER": "u",
            "DENEB_ARCHIVE_IMAP_PASS": "p",
        }
        with mock.patch.dict(mail_bench.os.environ, env, clear=True):
            with mock.patch.object(mail_bench.imaplib, "IMAP4", MissingIMAP):
                with self.assertRaisesRegex(SystemExit, "IMAP UID FETCH 99 실패"):
                    mail_bench.fetch_mails([99], "INBOX", 100)


class ShadowAndCLIOutputTests(unittest.TestCase):
    def test_shadow_continues_other_models_after_one_model_failure(self) -> None:
        args = SimpleNamespace(
            uids="7",
            mailbox="INBOX",
            body_cap=100,
            model="good",
            model_b="bad",
            anchor=False,
            base_url="http://gw",
            max_tokens=10,
            timeout=2,
            our_domain_set=set(),
            org_map_dict={},
        )
        mails = [{"uid": "7", "subject": "subject", "text": "mail", "from": "", "to": "", "cc": ""}]
        with mock.patch.object(mail_bench, "fetch_mails", return_value=mails):
            with mock.patch.object(
                mail_bench,
                "call_llm",
                side_effect=[("ok", 5, 3, 1), RuntimeError("model down")],
            ):
                with mock.patch("builtins.print"):
                    result = mail_bench.run_shadow(args, "token")
        self.assertEqual(result[0]["good"], {"out": "ok", "ms": 5, "in": 3, "outTok": 1})
        self.assertEqual(result[0]["bad"]["out"], "[ERROR: model down]")
        self.assertEqual(result[0]["bad"]["ms"], -1)

    def test_main_serializes_both_artifacts_with_unique_millisecond_pid_name(self) -> None:
        class Buffer(io.StringIO):
            def close(self):
                pass

        buffers = {}

        def fake_open(path, mode="r", **_kwargs):
            self.assertEqual(mode, "w")
            return buffers.setdefault(path, Buffer())

        results = [{
            "mail": "fixture",
            "model": "model-a",
            "rep": 1,
            "hits": 2,
            "ms": 10,
            "out": "analysis",
        }]
        with mock.patch.object(mail_bench, "wormhole_token", return_value="token"):
            with mock.patch.object(mail_bench, "run_trap", return_value=results):
                with mock.patch.object(mail_bench.time, "time", return_value=123.456):
                    with mock.patch.object(mail_bench.os, "getpid", return_value=77):
                        with mock.patch("builtins.open", side_effect=fake_open):
                            rc, stdout, stderr = invoke_main(
                                mail_bench,
                                ["trap", "--model", "model-a"],
                            )
        self.assertIsNone(rc)
        self.assertEqual(stderr, "")
        base = "/tmp/mail-bench-trap-123456-77"
        self.assertEqual(set(buffers), {base + ".json", base + ".txt"})
        self.assertEqual(json.loads(buffers[base + ".json"].getvalue()), results)
        self.assertIn("fixture [model-a] rep1 probes=2", buffers[base + ".txt"].getvalue())
        self.assertEqual(stdout.strip(), f"saved: {base}.json  {base}.txt")

    def test_real_help_preserves_modes_and_safety_options(self) -> None:
        proc = subprocess.run(
            [sys.executable, mail_bench.__file__, "--help"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0)
        for value in ("trap", "shadow", "--model", "--anchor", "--uids", "--body-cap"):
            self.assertIn(value, proc.stdout)


if __name__ == "__main__":
    unittest.main()
