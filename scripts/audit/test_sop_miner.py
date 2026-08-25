"""Deterministic tests for action-aware SOP mining and promotion evidence."""

from __future__ import annotations

import json
import os
import tempfile
import unittest

from sop_miner import (
    SOP_MIN_OCCURRENCES,
    SOP_MIN_SESSIONS,
    TURN_BOUNDARY,
    collect_sequences,
    extract_tool_sequence,
    mine_sops,
    normalize_tool_step,
)

NOW = 1_700_000_000_000


def write_transcript(
    dirpath: str,
    key: str,
    tools: list[str | tuple[str, dict]],
    ts: int = NOW,
    *,
    failed: set[int] | None = None,
) -> None:
    path = os.path.join(dirpath, key + ".jsonl")
    lines = [json.dumps({"role": "user", "content": "run procedure", "timestamp": ts})]
    failed = failed or set()
    for i, spec in enumerate(tools):
        name, payload = spec if isinstance(spec, tuple) else (spec, {})
        call_id = f"t{i}"
        lines.append(json.dumps({
            "role": "assistant",
            "content": [{
                "type": "tool_use", "id": call_id, "name": name,
                "input": json.dumps(payload),
            }],
            "timestamp": ts,
        }))
        lines.append(json.dumps({
            "role": "user",
            "content": [{
                "type": "tool_result", "tool_use_id": call_id,
                "content": "Error: failed" if i in failed else "ok",
                "is_error": i in failed,
            }],
            "timestamp": ts,
        }))
    lines.append('{"role":"user","content":')  # corrupt tail must be tolerated
    with open(path, "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines) + "\n")


class ExtractTest(unittest.TestCase):
    def test_when_normalizes_actions_orders_successes_and_tolerates_corruption(self):
        with tempfile.TemporaryDirectory() as d:
            write_transcript(d, "client:a", [
                ("mail_archive", {"action": "search"}),
                ("wiki", {"action": "read"}),
                ("wiki", {"action": "write"}),
            ])
            seq = extract_tool_sequence(os.path.join(d, "client:a.jsonl"), NOW - 1)
            self.assertEqual(seq, ["mail_archive.search", "wiki.read", "wiki.write"])

    def test_when_setup_is_transparent_repeats_batch_and_failures_split(self):
        with tempfile.TemporaryDirectory() as d:
            write_transcript(d, "client:a", [
                ("fetch_tools", {"names": ["mail_archive"]}),
                ("mail_archive", {"action": "read"}),
                ("mail_archive", {"action": "read"}),
                ("wiki", {"action": "search"}),
                ("wiki", {"action": "read"}),
                ("wiki", {"action": "write"}),
            ], failed={3})
            seq = extract_tool_sequence(os.path.join(d, "client:a.jsonl"), NOW - 1)
            self.assertEqual(seq, [
                "mail_archive.read[]", TURN_BOUNDARY, "wiki.read", "wiki.write",
            ])

    def test_when_real_user_message_splits_procedures(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "client:a.jsonl")
            rows = [
                {"role": "assistant", "content": [{
                    "type": "tool_use", "id": "a", "name": "gmail",
                    "input": '{"action":"search"}',
                }], "timestamp": NOW},
                {"role": "user", "content": [{
                    "type": "tool_result", "tool_use_id": "a", "content": "ok",
                }], "timestamp": NOW},
                {"role": "user", "content": "new request", "timestamp": NOW},
                {"role": "assistant", "content": [{
                    "type": "tool_use", "id": "b", "name": "wiki",
                    "input": '{"action":"write"}',
                }], "timestamp": NOW},
                {"role": "user", "content": [{
                    "type": "tool_result", "tool_use_id": "b", "content": "ok",
                }], "timestamp": NOW},
            ]
            with open(path, "w", encoding="utf-8") as handle:
                handle.write("\n".join(json.dumps(row) for row in rows) + "\n")
            self.assertEqual(
                extract_tool_sequence(path, NOW - 1),
                ["gmail.search", TURN_BOUNDARY, "wiki.write"],
            )

    def test_normalize_tool_step_uses_bounded_action_only(self):
        self.assertEqual(normalize_tool_step({
            "name": "Gmail", "input": '{"action":"thread"}',
        }), "gmail.thread")
        self.assertEqual(normalize_tool_step({
            "name": "exec", "input": '{"command":"rm -rf /"}',
        }), "exec")
        self.assertEqual(normalize_tool_step({
            "name": "office", "input": '{"command":"view"}',
        }), "office.view")

    def test_when_stale_transcript_yields_nothing(self):
        with tempfile.TemporaryDirectory() as d:
            write_transcript(d, "client:old", [
                ("mail_archive", {"action": "search"}),
                ("wiki", {"action": "read"}),
                ("wiki", {"action": "write"}),
            ], ts=NOW - 100)
            self.assertEqual(
                extract_tool_sequence(os.path.join(d, "client:old.jsonl"), NOW), [])


class MineTest(unittest.TestCase):
    def _sequences(self, sessions: int, repeats: int) -> dict[str, list[str]]:
        procedure = [
            "mail_archive.search", "mail_archive.read[]", "wiki.search",
            "wiki.read", "wiki.write", TURN_BOUNDARY,
        ]
        return {f"s{i}": procedure * repeats for i in range(sessions)}

    def test_frequency_gate_passes_and_payload_contract(self):
        seqs = self._sequences(SOP_MIN_SESSIONS, 3)
        mined = mine_sops(seqs)
        self.assertTrue(mined, "recurring procedure above the gate must mine")
        top = mined[0]
        self.assertEqual(top["scope"], "code")
        self.assertTrue(top["source"].startswith("sop-mining:"))
        self.assertIn("successful action-normalized flows", top["evidence"])
        self.assertIn("at least 15 tool calls", top["evidence"])
        self.assertIn("프롬프트/컴플리션 토큰", top["proposedChange"])
        self.assertTrue(top["targetFiles"][0].endswith(".go"))
        self.assertEqual(top["source"], mine_sops(seqs)[0]["source"])

    def test_when_below_session_floor_is_silent(self):
        self.assertEqual(mine_sops(self._sequences(SOP_MIN_SESSIONS - 1, 5)), [])

    def test_when_below_occurrence_floor_is_silent(self):
        seqs = {
            f"s{i}": ["mail.search", "mail.read", "wiki.write"]
            for i in range(SOP_MIN_SESSIONS)
        }
        self.assertLess(SOP_MIN_SESSIONS, SOP_MIN_OCCURRENCES)
        self.assertEqual(mine_sops(seqs), [])

    def test_when_single_tool_loop_is_not_a_procedure(self):
        seqs = {
            f"s{i}": ["wiki.read", "wiki.write", "wiki.read"] * 20
            for i in range(SOP_MIN_SESSIONS + 2)
        }
        self.assertEqual(mine_sops(seqs), [])

    def test_when_generic_tools_have_no_action_semantics_is_silent(self):
        seqs = {
            f"s{i}": ["read", "exec", "exec", "write"] * 4
            for i in range(SOP_MIN_SESSIONS + 2)
        }
        self.assertEqual(mine_sops(seqs), [])

    def test_when_turn_boundary_prevents_cross_request_gram(self):
        seqs = {f"s{i}": [
            "gmail.search", "gmail.read", TURN_BOUNDARY,
            "wiki.search", "wiki.read", "wiki.write",
        ] for i in range(SOP_MIN_SESSIONS + 2)}
        titles = [row["title"] for row in mine_sops(seqs)]
        self.assertFalse(any("gmail.read > wiki.search" in title for title in titles))

    def test_when_subsumed_shorter_gram_is_suppressed(self):
        mined = mine_sops(self._sequences(SOP_MIN_SESSIONS + 1, 4))
        flows = [c["title"] for c in mined]
        for title in flows:
            body = title.removeprefix("절차 후보(SOP): ")
            self.assertFalse(any(
                body in other and title != other for other in flows
            ), f"subsumed gram leaked: {flows}")

    def test_collect_sequences_reads_dir(self):
        with tempfile.TemporaryDirectory() as d:
            for i in range(3):
                write_transcript(d, f"client:s{i}", [
                    ("gmail", {"action": "search"}),
                    ("gmail", {"action": "read"}),
                    ("wiki", {"action": "write"}),
                ])
            self.assertEqual(len(collect_sequences(d, NOW - 1)), 3)


if __name__ == "__main__":
    unittest.main()
