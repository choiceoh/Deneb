"""Tests for the deterministic helpers of the skill-corpus relevance sweep.

The LLM classification path is exercised live (wormhole); here we pin the pure
case→classifier-input extraction that decides what the judge actually sees.
"""
from __future__ import annotations

import unittest

from skill_corpus_relevance_sweep import case_content


class CaseContentTest(unittest.TestCase):
    def test_extracts_topic_and_tools_from_expected_calls(self):
        rec = {
            "skillName": "system-health-check",
            "replay": {
                "requiredTools": ["mail_archive", "wiki"],
                "expectedToolCalls": [
                    {"name": "mail_archive", "inputIncludes": ["search", "당진 EPC 계약금액"],
                     "fixtureOutput": "검색 결과: 당진 솔라빌리지 EPC ...\n두번째 줄"},
                ],
            },
        }
        topic, content = case_content(rec)
        self.assertEqual(topic, "당진 EPC 계약금액")
        self.assertIn("mail_archive", content)
        self.assertIn("당진 솔라빌리지 EPC", content)
        self.assertNotIn("\n두번째 줄\n", content)  # excerpt newlines are flattened

    def test_uses_input_when_present(self):
        _topic, content = case_content({"replay": {"input": "시스템 상태 확인해줘", "requiredTools": ["exec"]}})
        self.assertIn("시스템 상태 확인해줘", content)
        self.assertIn("exec", content)

    def test_empty_replay_is_not_classifiable(self):
        topic, content = case_content({"replay": {}})
        self.assertEqual((topic, content), ("", ""))
        self.assertEqual(case_content({}), ("", ""))


if __name__ == "__main__":
    unittest.main()
