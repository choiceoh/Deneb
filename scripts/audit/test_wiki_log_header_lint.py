"""Fixture tests for wiki_log_header_lint."""

from __future__ import annotations

import os
import tempfile
import unittest

import wiki_log_header_lint as lint


def write_log(root: str, project: str, body: str, name: str = "로그.md") -> None:
    path = os.path.join(root, "프로젝트", project, name)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(body)


class LogHeaderLintTest(unittest.TestCase):
    def test_conforming_entries_pass_and_subheadings_are_reported(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            write_log(
                root,
                "pl2-kia-epc-001",
                "\n".join(
                    [
                        "## [2026-08-20] 회의 | 캐노피 견적 검토",
                        "본문",
                        "",
                        "## 요지",  # entry-internal sub-heading → counted as an entry by rotation
                        "요지 본문",
                        "",
                        "## [2026-08-21] 발주 | 모듈 계약",
                        "본문",
                    ]
                ),
            )
            # Archive files follow the same grammar.
            write_log(root, "pl2-kia-epc-001", "## 2026-06-15 구형 헤더\n본문\n", name="로그-보관.md")

            conforming, violations = lint.scan(root)

            self.assertEqual(conforming, 2)
            self.assertEqual([v[2] for v in violations], ["## 요지", "## 2026-06-15 구형 헤더"])

    def test_clean_log_has_no_violations(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            write_log(root, "pl1-abc-dev-001", "## [2026-08-01] 결정 | 부지 확정\n본문\n")
            conforming, violations = lint.scan(root)
            self.assertEqual((conforming, violations), (1, []))

    def test_non_log_pages_are_ignored(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = os.path.join(root, "프로젝트", "pl1-abc-dev-001", "대표.md")
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                fh.write("## 현재 상태\n본문\n")
            self.assertEqual(lint.scan(root), (0, []))


if __name__ == "__main__":
    unittest.main()
