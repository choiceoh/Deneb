"""Grouping, axis hinting, and CLI behavior for the direct-memory miss report."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from test_support import invoke_main, load_script

report = load_script("scripts/dev/memory-grammar-misses.py")

AXES = [
    {"key": "communication.response_length", "queryAliases": ["length", "길이", "분량"]},
    {"key": "identity.address", "queryAliases": ["address", "호칭", "이름"]},
]


class NormalizeTests(unittest.TestCase):
    def test_command_leads_and_values_collapse_into_one_shape(self) -> None:
        # The lead and the value differ; the request does not.
        shapes = {
            report.normalize("기억해줘. 회의는 9시에만 잡아줘"),
            report.normalize("앞으로 회의는 10시에만 잡아줘"),
            report.normalize("remember: 회의는 11시에만 잡아줘"),
        }
        self.assertEqual(len(shapes), 1, shapes)
        self.assertIn("N시", shapes.pop())

    def test_trailing_remember_is_stripped(self) -> None:
        self.assertEqual(
            report.normalize("내 자리는 3층이야 기억해줘."),
            report.normalize("기억해줘. 내 자리는 3층이야"),
        )

    def test_quoted_payload_is_masked(self) -> None:
        self.assertEqual(report.normalize('나를 "대표"라고 불러'), "나를 〈값〉라고 불러")


class NearestAxisTests(unittest.TestCase):
    def test_published_vocabulary_points_at_the_axis_to_extend(self) -> None:
        self.assertEqual(
            report.nearest_axes("답변 길이를 상황에 맞게 조절해줘", AXES), ["communication.response_length"]
        )

    def test_unrelated_request_names_no_axis(self) -> None:
        self.assertEqual(report.nearest_axes("회의는 화요일 오전에만 잡아줘", AXES), [])


class GroupingTests(unittest.TestCase):
    def test_groups_rank_by_frequency_and_keep_samples(self) -> None:
        rows = [
            {"lead": "remember", "target": "episodic", "message": "기억해줘. 회의는 9시에만"},
            {"lead": "forward", "target": "episodic", "message": "앞으로 회의는 10시에만"},
            {"lead": "remember", "target": "episodic", "message": "기억해줘. 내 호칭 정보"},
            {"message": "   "},
        ]
        groups = report.group_rows(rows, AXES)
        self.assertEqual([g.count for g in groups], [2, 1])
        self.assertEqual(groups[0].leads["remember"], 1)
        self.assertEqual(groups[0].leads["forward"], 1)
        self.assertEqual(groups[1].near, ["identity.address"])
        self.assertLessEqual(len(groups[0].samples), 3)


class CLITests(unittest.TestCase):
    def _ledger(self, root: Path, lines: list[str]) -> Path:
        path = root / "misses.jsonl"
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        return path

    def test_missing_ledger_is_a_healthy_zero_not_an_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "absent.jsonl"
            code, out, _ = invoke_main(report, ["--path", str(path)])
            self.assertEqual(code, 0)
            self.assertIn("미탐 장부 없음", out)

            code, out, _ = invoke_main(report, ["--path", str(path), "--json"])
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(out)["groups"], [])

    def test_json_output_reports_groups_and_skips_unreadable_rows(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = self._ledger(Path(tmp), [
                json.dumps({"lead": "remember", "message": "기억해줘. 회의는 9시에만"}, ensure_ascii=False),
                json.dumps({"lead": "forward", "message": "앞으로 회의는 10시에만"}, ensure_ascii=False),
                "{ not json",
                json.dumps(["not an object"]),
            ])
            code, out, _ = invoke_main(report, ["--path", str(path), "--json"])
            self.assertEqual(code, 0)
            payload = json.loads(out)
            self.assertEqual(payload["rows"], 2)
            self.assertEqual(payload["skipped"], 2)
            self.assertEqual(payload["groups"][0]["count"], 2)

    def test_limit_bounds_the_printed_groups(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = self._ledger(Path(tmp), [
                json.dumps({"lead": "remember", "message": f"기억해줘. 사례 {chr(ord('가') + i)}"}, ensure_ascii=False)
                for i in range(4)
            ])
            code, out, _ = invoke_main(report, ["--path", str(path), "--limit", "2"])
            self.assertEqual(code, 0)
            self.assertIn("2개 묶음 생략", out)


if __name__ == "__main__":
    unittest.main()
