"""Fixture tests for wiki_deal_ledger_lint."""

from __future__ import annotations

import os
import tempfile
import unittest

import wiki_deal_ledger_lint as lint


def write(root: str, rel: str, page_type: str) -> None:
    path = os.path.join(root, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(f"---\ntitle: t\ntype: {page_type}\n---\n\n본문\n")


class DealLedgerLintTest(unittest.TestCase):
    def test_flags_strays_and_splits_but_not_layout_pages(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            # Canonical ledgers — clean.
            write(root, "프로젝트/거래/고려전선.md", "deal")
            write(root, "프로젝트/거래/경비-식대.md", "deal")
            # Strays: the two shapes seen live (verify auto-move, curate rule).
            write(root, "인물/거래/정종호.md", "deal")
            write(root, "인물/케이원일렉트릭.md", "deal")
            write(root, "업무/거래/경비-접대비.md", "deal")
            # Layout-placed inside a project folder — legitimate.
            write(root, "프로젝트/남도에코-케이블/대표.md", "deal")
            # Non-ledger pages are irrelevant.
            write(root, "업무/화신일렉트릭.md", "entity")
            write(root, "인물/김민준.md", "entity")

            stray, split = lint.scan(root)

            self.assertEqual(
                stray,
                ["업무/거래/경비-접대비.md", "인물/거래/정종호.md", "인물/케이원일렉트릭.md"],
            )
            self.assertEqual(split, {})

    def test_flags_the_same_slug_in_two_folders(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            write(root, "프로젝트/거래/정종호.md", "deal")
            write(root, "인물/거래/정종호.md", "deal")

            stray, split = lint.scan(root)

            self.assertEqual(stray, ["인물/거래/정종호.md"])
            self.assertEqual(
                split, {"정종호.md": ["인물/거래/정종호.md", "프로젝트/거래/정종호.md"]}
            )

    def test_clean_tree_has_no_violations(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            write(root, "프로젝트/거래/고려전선.md", "deal")
            write(root, "업무/고려전선.md", "entity")

            stray, split = lint.scan(root)

            self.assertEqual((stray, split), ([], {}))


if __name__ == "__main__":
    unittest.main()
