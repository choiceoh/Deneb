"""Deterministic tests for the SOP miner (frequency gate + payload contract)."""

from __future__ import annotations

import json
import os
import tempfile
import unittest

from sop_miner import (
    SOP_MIN_OCCURRENCES,
    SOP_MIN_SESSIONS,
    collect_sequences,
    extract_tool_sequence,
    mine_sops,
)

NOW = 1_700_000_000_000


def write_transcript(dirpath: str, key: str, tools: list[str], ts: int = NOW) -> None:
    path = os.path.join(dirpath, key + ".jsonl")
    lines = ['{"type":"session","version":1,"id":"%s","timestamp":%d}' % (key, ts)]
    for name in tools:
        lines.append(json.dumps({
            "role": "assistant",
            "content": [{"type": "tool_use", "id": "t", "name": name, "input": {}}],
            "timestamp": ts,
        }))
    lines.append('{"role":"user","content":')  # corrupt tail must be tolerated
    with open(path, "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines) + "\n")


class ExtractTest(unittest.TestCase):
    def test_orders_tools_and_tolerates_corruption(self):
        with tempfile.TemporaryDirectory() as d:
            write_transcript(d, "client:a", ["fs", "exec", "git"])
            seq = extract_tool_sequence(os.path.join(d, "client:a.jsonl"), NOW - 1)
            self.assertEqual(seq, ["fs", "exec", "git"])

    def test_stale_transcript_yields_nothing(self):
        with tempfile.TemporaryDirectory() as d:
            write_transcript(d, "client:old", ["fs", "exec", "git"], ts=NOW - 100)
            self.assertEqual(
                extract_tool_sequence(os.path.join(d, "client:old.jsonl"), NOW), [])


class MineTest(unittest.TestCase):
    def _sequences(self, sessions: int, repeats: int) -> dict[str, list[str]]:
        # Each session repeats the procedure fs > exec > git `repeats` times,
        # separated by noise so the gram count is unambiguous.
        return {
            f"s{i}": (["fs", "exec", "git", "web"] * repeats)
            for i in range(sessions)
        }

    def test_frequency_gate_passes_and_payload_contract(self):
        seqs = self._sequences(SOP_MIN_SESSIONS, 3)  # 3*3=9 >= 5 occurrences
        mined = mine_sops(seqs)
        self.assertTrue(mined, "recurring procedure above the gate must mine")
        top = mined[0]
        self.assertEqual(top["scope"], "code")
        self.assertTrue(top["source"].startswith("sop-mining:"))
        self.assertIn("observed", top["evidence"])
        self.assertTrue(top["targetFiles"][0].endswith(".go"))
        # Deterministic: same input, same source hash.
        self.assertEqual(top["source"], mine_sops(seqs)[0]["source"])

    def test_below_session_floor_is_silent(self):
        mined = mine_sops(self._sequences(SOP_MIN_SESSIONS - 1, 5))
        self.assertEqual(mined, [])

    def test_below_occurrence_floor_is_silent(self):
        seqs = {f"s{i}": ["fs", "exec", "git"] for i in range(SOP_MIN_SESSIONS)}
        # 3 occurrences total < SOP_MIN_OCCURRENCES (5)
        self.assertLess(SOP_MIN_SESSIONS, SOP_MIN_OCCURRENCES)
        self.assertEqual(mine_sops(seqs), [])

    def test_single_tool_loop_is_not_a_procedure(self):
        seqs = {f"s{i}": ["fs"] * 20 for i in range(SOP_MIN_SESSIONS + 2)}
        self.assertEqual(mine_sops(seqs), [])

    def test_subsumed_shorter_gram_is_suppressed(self):
        seqs = self._sequences(SOP_MIN_SESSIONS + 1, 4)
        mined = mine_sops(seqs)
        flows = [c["title"] for c in mined]
        longest = max(len(c["title"]) for c in mined)
        for title in flows:
            if len(title) < longest:
                # any shorter selected gram must not be a substring of a longer one
                self.assertFalse(any(title[len("SOP candidate: "):] in other
                                     for other in flows if other != title),
                                 f"subsumed gram leaked: {flows}")

    def test_collect_sequences_reads_dir(self):
        with tempfile.TemporaryDirectory() as d:
            for i in range(3):
                write_transcript(d, f"client:s{i}", ["fs", "exec", "git", "fs", "exec", "git"])
            seqs = collect_sequences(d, NOW - 1)
            self.assertEqual(len(seqs), 3)


if __name__ == "__main__":
    unittest.main()
