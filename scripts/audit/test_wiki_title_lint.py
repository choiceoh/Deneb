"""Contract tests for wiki_title_lint — mirror of the Go title_lint_test cases
so the two rule sets cannot drift silently."""

import tempfile
import unittest
from pathlib import Path

from wiki_title_lint import lint_title, rep_pages

VIOLATING = [
    "완도 관산포 태양광 — 현재 현안 (송전선로 + SK 협상)",
    "아르고에너지(Argo Energy) NDA 송부 – 태양광 사업 전략적 협력 검토",
    "중부도시가스 해남 200MW EPC 기회",
    "SunKean 솔라케이블 2,240km + MC4 커넥터 협의",
    "산월풍력 EPC 입찰",
    "세창스틸 1,2공장 태양광 — 6/29 견적 재송부 (재검토)",
    "기아 화성 태양광 2026.06.15 RFx",
]

CLEAN = [
    "완도 관산포 태양광",
    "기아 오토랜드 화성 태양광",
    "중부도시가스 해남 200MW EPC",
    "SunKean 솔라케이블 2,240km + MC4 커넥터",
    "비금도 154kV 해저케이블 (ZTT) — 비금 솔라 현장",
    "영광 한빛 BESS (80MW/480MWh, 한빛에너지저장)",
    "세창스틸 1,2공장 태양광",
    "트리나솔라 2026년 모듈 공급 계약",
    "",
]


class LintTitleTest(unittest.TestCase):
    def test_violating_titles_fire(self):
        for title in VIOLATING:
            with self.subTest(title=title):
                self.assertTrue(lint_title(title), f"expected violation for {title!r}")

    def test_clean_titles_pass(self):
        for title in CLEAN:
            with self.subTest(title=title):
                self.assertFalse(lint_title(title), f"false positive for {title!r}")


class RepPagesTest(unittest.TestCase):
    def test_scans_only_project_reps(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            rep = root / "프로젝트" / "pl2-a-epc-001" / "대표.md"
            rep.parent.mkdir(parents=True)
            rep.write_text("---\ntitle: A 태양광 입찰\n---\n본문", encoding="utf-8")
            # Non-rep and reserved-bucket files must not be scanned.
            (rep.parent / "로그.md").write_text(
                "---\ntitle: A 진행 로그 검토\n---\n", encoding="utf-8"
            )
            ledger = root / "프로젝트" / "거래" / "대표.md"
            ledger.parent.mkdir(parents=True)
            ledger.write_text("---\ntitle: 원장 검토\n---\n", encoding="utf-8")

            rows = [(str(rel), title) for rel, title in rep_pages(root)]
            self.assertEqual(rows, [("프로젝트/pl2-a-epc-001/대표.md", "A 태양광 입찰")])


if __name__ == "__main__":
    unittest.main()
